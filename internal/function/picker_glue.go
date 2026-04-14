package function

import (
	"context"
	"fmt"

	"sevens/internal/function/picker"
	"sevens/internal/kb"
	"sevens/internal/types/kernel"
)

// resolveStepPicker is the executor-side adapter that calls
// picker.Resolve with a properly constructed EvalContext. It maps
// sevens's KB, target, and pipeline state onto the narrower slice
// the picker package consumes.
//
// It is the single crossing point between the function package and
// the picker package. Every other part of the executor treats the
// picker as opaque.
func resolveStepPicker(
	ctx context.Context,
	k *kb.KB,
	root, target, prevOutput string,
	step Step,
	p *Pipeline,
) (kernel.TypeName, error) {
	if step.OutputPicker == nil {
		return "", fmt.Errorf("resolveStepPicker called on step with no OutputPicker")
	}

	evalCtx := picker.EvalContext{
		KB:              &kbPickerAdapter{k: k, ctx: ctx, root: root},
		TargetTitle:     target,
		TargetConforms:  nil, // TODO: populate from conformance index when wired
		PriorOutputType: priorOutputTypes(p),
	}

	return picker.Resolve(step.OutputPicker, evalCtx)
}

// kbPickerAdapter bridges sevens's *kb.KB (which needs ctx + root on
// every call) to the picker package's kernel.KB interface (which
// only takes a title).
//
// The kernel.KB interface returns (content, ok). For picker
// ExistsNode checks we only need truthiness, so we return
// ("", true) on hits. A future refinement that needs actual file
// content would want a different adapter that reads the file from
// disk — but the picker language as it stands only queries presence.
type kbPickerAdapter struct {
	k    *kb.KB
	ctx  context.Context
	root string
}

func (a *kbPickerAdapter) ResolveNode(title string) (string, bool) {
	subject := a.k.Resolve(a.ctx, a.root, title)
	if subject == "" {
		return "", false
	}
	return "", true
}

// DeriveShapeFromPicker walks an OutputPicker's declared alternatives
// and returns the OutputShape that all of them share. Returns an
// error if the alternatives mix shape families (e.g. create + text).
// Used at load time when a step declares :output-picker without a
// matching static :output.
func DeriveShapeFromPicker(op picker.OutputPicker) (OutputShape, error) {
	dep, ok := op.(picker.DependentOutput)
	if !ok {
		// StaticOutput: look up the declared type's primitive.
		if static, ok := op.(picker.StaticOutput); ok {
			return shapeForPrimitiveName(string(static.T))
		}
		return ShapeText, fmt.Errorf("unknown output picker type %T", op)
	}
	if len(dep.Alternatives) == 0 {
		return ShapeText, fmt.Errorf("picker %q has no alternatives", dep.Name)
	}
	first, err := shapeForPrimitiveName(string(dep.Alternatives[0]))
	if err != nil {
		return ShapeText, fmt.Errorf("picker %q alternative[0]: %w", dep.Name, err)
	}
	for i, alt := range dep.Alternatives[1:] {
		got, err := shapeForPrimitiveName(string(alt))
		if err != nil {
			return ShapeText, fmt.Errorf("picker %q alternative[%d]: %w", dep.Name, i+1, err)
		}
		if got != first {
			return ShapeText, fmt.Errorf(
				"picker %q: alternatives mix shape families (%v != %v)",
				dep.Name, first, got)
		}
	}
	return first, nil
}

// shapeForPrimitiveName maps a primitive type name to its
// corresponding OutputShape. Returns an error for unrecognized names.
func shapeForPrimitiveName(name string) (OutputShape, error) {
	switch name {
	case "text":
		return ShapeText, nil
	case "create", "edit":
		return ShapeFileOps, nil
	case "suggestion", "suggestions":
		return ShapeStructured, nil
	}
	return ShapeText, fmt.Errorf("unknown primitive type %q", name)
}

// priorOutputTypes returns a slice of kernel TypeNames for the
// outputs of all previously-completed steps in the pipeline.
// Indexed in order so a picker expression can reference step i's
// output type via PriorOutputType{Index: i}.
func priorOutputTypes(p *Pipeline) []kernel.TypeName {
	if p == nil || len(p.PriorStepResults) == 0 {
		return nil
	}
	// The function package has no tracking of resolved output types
	// per prior step — it stores raw results. For now, return a
	// best-effort list based on what's in the Signature. Callers
	// that need tighter typing should wait for the full
	// function-contract-layer port.
	out := make([]kernel.TypeName, 0, len(p.PriorStepResults))
	for _, r := range p.PriorStepResults {
		if r.IsText {
			out = append(out, "text")
		} else if len(r.Ops) > 0 {
			// Use the first op's action as a proxy for the shape.
			first := r.Ops[0].Action
			out = append(out, kernel.TypeName(first))
		} else {
			out = append(out, "")
		}
	}
	return out
}
