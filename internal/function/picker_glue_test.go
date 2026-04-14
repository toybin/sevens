package function

import (
	"testing"

	"sevens/internal/function/picker"
	"sevens/internal/types/kernel"
)

// TestDeriveShapeFromPicker_StaticAlternatives verifies that a
// picker whose alternatives all share a primitive shape family
// resolves to that shape.
func TestDeriveShapeFromPicker_StaticAlternatives(t *testing.T) {
	cases := []struct {
		name     string
		alts     []kernel.TypeName
		expected OutputShape
	}{
		{"create-only", []kernel.TypeName{"create"}, ShapeFileOps},
		{"edit-only", []kernel.TypeName{"edit"}, ShapeFileOps},
		{"create+edit", []kernel.TypeName{"create", "edit"}, ShapeFileOps},
		{"text-only", []kernel.TypeName{"text"}, ShapeText},
		{"suggestion-only", []kernel.TypeName{"suggestion"}, ShapeStructured},
		// "suggestions" (plural) is the EDN-level alias — the
		// kernel and parser both accept it.
		{"suggestions-alias", []kernel.TypeName{"suggestions"}, ShapeStructured},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op := picker.DependentOutput{
				Name:         "test",
				Alternatives: c.alts,
				Expr:         picker.LitType{Name: c.alts[0]},
			}
			shape, err := DeriveShapeFromPicker(op)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if shape != c.expected {
				t.Errorf("DeriveShapeFromPicker(%v) = %v, want %v",
					c.alts, shape, c.expected)
			}
		})
	}
}

// TestDeriveShapeFromPicker_MixedFamiliesFails verifies that a
// picker whose alternatives span shape families (create + text,
// say) is rejected at load time.
func TestDeriveShapeFromPicker_MixedFamiliesFails(t *testing.T) {
	op := picker.DependentOutput{
		Name:         "mixed",
		Alternatives: []kernel.TypeName{"create", "text"},
		Expr:         picker.LitType{Name: "create"},
	}
	_, err := DeriveShapeFromPicker(op)
	if err == nil {
		t.Fatal("expected error for mixed shape families")
	}
}

// TestDeriveShapeFromPicker_UnknownPrimitiveFails verifies that an
// alternative naming a type the kernel doesn't recognize as a
// primitive is rejected. Unknown user types route through the
// legacy type registry path, which is not this function's concern.
func TestDeriveShapeFromPicker_UnknownPrimitiveFails(t *testing.T) {
	op := picker.DependentOutput{
		Name:         "unknown",
		Alternatives: []kernel.TypeName{"my-custom-type"},
		Expr:         picker.LitType{Name: "my-custom-type"},
	}
	_, err := DeriveShapeFromPicker(op)
	if err == nil {
		t.Fatal("expected error for unknown primitive")
	}
}

// TestDeriveShapeFromPicker_StaticOutput verifies that a
// StaticOutput picker also derives its shape correctly. Used when
// a function declares :output-picker with a literal alternative
// (uncommon, but valid).
func TestDeriveShapeFromPicker_StaticOutput(t *testing.T) {
	op := picker.StaticOutput{T: "edit"}
	shape, err := DeriveShapeFromPicker(op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shape != ShapeFileOps {
		t.Errorf("expected ShapeFileOps for static edit, got %v", shape)
	}
}

// TestDeriveShapeFromPicker_EmptyAlternativesFails verifies that a
// DependentOutput with no alternatives is rejected.
func TestDeriveShapeFromPicker_EmptyAlternativesFails(t *testing.T) {
	op := picker.DependentOutput{
		Name:         "empty",
		Alternatives: nil,
		Expr:         picker.LitType{Name: "create"},
	}
	_, err := DeriveShapeFromPicker(op)
	if err == nil {
		t.Fatal("expected error for empty alternatives")
	}
}

// TestShapeForPrimitiveName_AllPrimitives verifies the name-to-
// shape map covers all four primitives plus the "suggestions"
// alias.
func TestShapeForPrimitiveName_AllPrimitives(t *testing.T) {
	cases := map[string]OutputShape{
		"text":        ShapeText,
		"create":      ShapeFileOps,
		"edit":        ShapeFileOps,
		"suggestion":  ShapeStructured,
		"suggestions": ShapeStructured,
	}
	for name, want := range cases {
		got, err := shapeForPrimitiveName(name)
		if err != nil {
			t.Errorf("shapeForPrimitiveName(%q) errored: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("shapeForPrimitiveName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestShapeForPrimitiveName_UnknownRejected(t *testing.T) {
	_, err := shapeForPrimitiveName("not-a-primitive")
	if err == nil {
		t.Fatal("expected error for unknown primitive")
	}
}

// TestPriorOutputTypes_ReadsResolvedType verifies that the pipeline
// state reader surfaces TransformResult.ResolvedType — the source
// that replaced the earlier "guess from op action" hack.
func TestPriorOutputTypes_ReadsResolvedType(t *testing.T) {
	p := &Pipeline{
		PriorStepResults: []TransformResult{
			{ResolvedType: "create", IsText: false},
			{ResolvedType: "edit", IsText: false},
			{ResolvedType: "text", IsText: true},
		},
	}
	got := priorOutputTypes(p)
	expected := []kernel.TypeName{"create", "edit", "text"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(got))
	}
	for i, want := range expected {
		if got[i] != want {
			t.Errorf("types[%d] = %s, want %s", i, got[i], want)
		}
	}
}

func TestPriorOutputTypes_EmptyPipeline(t *testing.T) {
	got := priorOutputTypes(&Pipeline{})
	if len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}

func TestPriorOutputTypes_NilPipeline(t *testing.T) {
	got := priorOutputTypes(nil)
	if len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}
