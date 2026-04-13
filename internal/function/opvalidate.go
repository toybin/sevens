package function

import (
	"fmt"

	"sevens/internal/types/kernel"
)

// primitiveRegistry is a package-level registry pre-populated with
// just the four primitive types. It has no user-defined types and no
// contextual refinements, so validation does not need a KB handle.
//
// When we finish wiring the kernel into the executor, this will be
// replaced by a registry loaded from defaults/types/ that holds
// user-defined subtypes too.
var primitiveRegistry = kernel.NewPrimitivesRegistry()

// ValidateOps checks each FileOp against its primitive's schema using
// the kernel validator. Returns a specific error on the first
// malformed op; nil if all ops pass.
//
// This is the strict parse-time check that replaces the silent
// "drop bad ops and carry on" behavior in ParseOps. The current
// fallthrough behavior was the root cause of the discuss "untitled.md"
// bug: an edit op with no file field would slip through ParseOutput's
// envelope path and get slugified downstream.
func ValidateOps(ops []FileOp) error {
	for i, op := range ops {
		if err := validateOp(op); err != nil {
			return fmt.Errorf("op[%d] (%s): %w", i, opLabel(op), err)
		}
	}
	return nil
}

func opLabel(op FileOp) string {
	if op.Action == "" {
		return "unknown-action"
	}
	return op.Action
}

func validateOp(op FileOp) error {
	switch op.Action {
	case "create":
		value := kernel.NewValue(
			kernel.FieldPair{Name: "title", Value: kernel.VString(op.Title)},
			kernel.FieldPair{Name: "content", Value: kernel.VString(op.Content)},
			kernel.FieldPair{Name: "parent", Value: kernel.VString(op.Parent)},
			kernel.FieldPair{Name: "extra", Value: extraToVMap(op.Extra)},
		)
		return primitiveRegistry.Validate(nil, "create", value)

	case "edit":
		value := kernel.NewValue(
			kernel.FieldPair{Name: "file", Value: kernel.VString(op.File)},
			kernel.FieldPair{Name: "old_text", Value: kernel.VString(op.OldText)},
			kernel.FieldPair{Name: "new_text", Value: kernel.VString(op.NewText)},
		)
		return primitiveRegistry.Validate(nil, "edit", value)

	case "":
		return fmt.Errorf("missing 'action' field")

	default:
		return fmt.Errorf("unknown action %q (expected 'create' or 'edit')", op.Action)
	}
}

// shapeLabel returns a short human-readable label for an OutputShape,
// used in error messages.
func shapeLabel(s OutputShape) string {
	switch s {
	case ShapeText:
		return "text"
	case ShapeStructured:
		return "structured"
	case ShapeFileOps:
		return "ops"
	}
	return "unknown"
}

// extraToVMap converts a FileOp's Extra map into a kernel VMap.
// Returns VAbsent when the map is nil so the kernel treats extra as
// genuinely unset rather than an empty map that would fail the
// "required but empty" rule if extra were required. (It isn't, on
// the primitive create type.)
func extraToVMap(m map[string]string) kernel.FieldValue {
	if m == nil {
		return kernel.VAbsent{}
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return kernel.VMap(out)
}
