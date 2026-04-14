package function

import (
	"context"
	"strings"

	"sevens/internal/kb"
	"sevens/internal/types"
	"sevens/internal/types/kernel"
)

// PreviewStepPrompt returns what the executor would build as the
// system prompt for a given step + target, without actually calling
// the LLM. Used by `sevens discuss --dry-run` and similar debugging
// paths to let the user inspect exactly what the model would see.
//
// Returns:
//   - resolvedType: the kernel type the picker resolved to, or ""
//     for static-shape steps.
//   - systemPrompt: the composed system prompt including persona,
//     step.Backend.SystemPrompt, and the kernel (or legacy) schema
//     instruction.
//   - err: a non-nil error if the picker failed to evaluate, the
//     target couldn't be resolved, or the schema couldn't be built.
//
// This function intentionally mirrors the layering inside
// Executor.executeStep so the preview matches production behavior.
// When the executor's dispatch changes, update this helper too.
func PreviewStepPrompt(
	ctx context.Context,
	k *kb.KB,
	root, target string,
	step Step,
) (resolvedType kernel.TypeName, systemPrompt string, err error) {
	// Phase 1: picker resolution (matches executor.go Phase 1).
	if step.OutputPicker != nil {
		resolvedType, err = resolveStepPicker(ctx, k, root, target, "", step, nil)
		if err != nil {
			return "", "", err
		}
	}

	// Phase 2: schema composition (matches executor.go's three-way
	// dispatch exactly — picker, subtype, or bare primitive).
	allTypes, _ := types.LoadAllTypeDefs()
	var schema string
	switch {
	case resolvedType != "":
		schema = primitiveRegistry.SchemaInstruction(resolvedType)

	case step.Output.TypeName != "":
		if td, ok := allTypes[step.Output.TypeName]; ok {
			schema = types.ComposeSchemaInstruction(td, allTypes)
		}
		if schema == "" {
			primName := kernel.TypeName(PrimitiveTypeName(step.Output.Shape))
			schema = primitiveRegistry.SchemaInstruction(primName)
		}

	default:
		primName := kernel.TypeName(PrimitiveTypeName(step.Output.Shape))
		schema = primitiveRegistry.SchemaInstruction(primName)
	}

	// Phase 3: assemble the system prompt (matches executor.go's
	// final concatenation).
	var parts []string
	if step.Backend.Persona != "" {
		parts = append(parts, "Persona: "+step.Backend.Persona)
	}
	if step.Backend.SystemPrompt != "" {
		parts = append(parts, step.Backend.SystemPrompt)
	}
	if schema != "" {
		parts = append(parts, schema)
	}
	systemPrompt = strings.Join(parts, "\n\n")
	return resolvedType, systemPrompt, nil
}
