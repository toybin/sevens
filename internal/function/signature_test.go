package function

import "testing"

func TestFormatSignature_SingleStepText(t *testing.T) {
	fn := &Function{
		Name: "notice",
		Steps: []Step{{
			Name:   "default",
			Output: Signature{Shape: ShapeText},
			Backend: BackendSpec{
				Kind:           BackendLLM,
				PromptTemplate: "Analyze {{title}}",
			},
		}},
	}

	sig := FormatSignature(fn)
	if sig == "" {
		t.Fatalf("expected non-empty signature, got %q", sig)
	}
	// Should start with function name and :: separator
	if !contains(sig, "notice :: ") {
		t.Fatalf("expected signature to start with 'notice :: ', got %q", sig)
	}
	// Should contain capitalized Text output
	if !contains(sig, "Text") {
		t.Fatalf("expected signature to contain 'Text', got %q", sig)
	}
	// Should use -> arrow
	if !contains(sig, " -> ") {
		t.Fatalf("expected ' -> ' separator, got %q", sig)
	}
}

func TestFormatSignature_MultiStep(t *testing.T) {
	fn := &Function{
		Name: "decompose",
		Steps: []Step{
			{
				Name:   "suggest",
				Output: Signature{Shape: ShapeStructured},
				Gate:   &GateSpec{Revisable: true},
				Backend: BackendSpec{
					Kind:           BackendLLM,
					PromptTemplate: "Suggest for {{title}}",
				},
			},
			{
				Name:   "generate",
				Output: Signature{Shape: ShapeFileOps, TypeName: "task"},
				Backend: BackendSpec{
					Kind:           BackendLLM,
					PromptTemplate: "Generate based on: {{prev}}",
				},
			},
		},
	}

	sig := FormatSignature(fn)
	if sig == "" {
		t.Fatalf("expected non-empty signature, got %q", sig)
	}
	// Should start with function name and :: separator
	if !contains(sig, "decompose :: ") {
		t.Fatalf("expected signature to start with 'decompose :: ', got %q", sig)
	}
	// Should use -> arrow (not →)
	if !contains(sig, " -> ") {
		t.Fatalf("expected ' -> ' separator, got %q", sig)
	}
	// Should not contain old-style arrow
	if contains(sig, "→") {
		t.Fatalf("should not contain old-style arrow '→', got %q", sig)
	}
}

func TestParseOutputSignature_Text(t *testing.T) {
	sig := ParseOutputSignature("", "")
	if sig.Shape != ShapeText {
		t.Fatalf("expected ShapeText for empty output, got %v", sig.Shape)
	}
}

func TestParseOutputSignature_Ops(t *testing.T) {
	sig := ParseOutputSignature("ops", "task")
	if sig.Shape != ShapeFileOps {
		t.Fatalf("expected ShapeFileOps for 'ops', got %v", sig.Shape)
	}
	if sig.TypeName != "task" {
		t.Fatalf("expected TypeName 'task', got %q", sig.TypeName)
	}
}

func TestParseOutputSignature_Create(t *testing.T) {
	sig := ParseOutputSignature("create", "")
	if sig.Shape != ShapeFileOps {
		t.Fatalf("expected ShapeFileOps for 'create', got %v", sig.Shape)
	}
	if sig.TypeName != "create" {
		t.Fatalf("expected TypeName 'create', got %q", sig.TypeName)
	}
}

func TestParseOutputSignature_Edit(t *testing.T) {
	sig := ParseOutputSignature("edit", "")
	if sig.Shape != ShapeFileOps {
		t.Fatalf("expected ShapeFileOps for 'edit', got %v", sig.Shape)
	}
	if sig.TypeName != "edit" {
		t.Fatalf("expected TypeName 'edit', got %q", sig.TypeName)
	}
}

func TestParseOutputSignature_Suggestions(t *testing.T) {
	sig := ParseOutputSignature("suggestions", "")
	if sig.Shape != ShapeStructured {
		t.Fatalf("expected ShapeStructured for 'suggestions', got %v", sig.Shape)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsStr(s, substr)))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
