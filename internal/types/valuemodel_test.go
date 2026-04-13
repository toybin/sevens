package types

import (
	"os"
	"path/filepath"
	"testing"

	"sevens/internal/config"
)

func TestValueModelValidateEnum(t *testing.T) {
	vm := &ValueModel{
		Name:    "priority",
		Kind:    VMEnum,
		Members: []string{"low", "medium", "high", "urgent"},
		Aliases: map[string]string{"!": "urgent", "!!": "high"},
	}

	// Valid members.
	for _, v := range []string{"low", "medium", "high", "urgent"} {
		if err := vm.Validate(v); err != nil {
			t.Errorf("expected %q to be valid, got: %v", v, err)
		}
	}

	// Valid aliases.
	for _, v := range []string{"!", "!!"} {
		if err := vm.Validate(v); err != nil {
			t.Errorf("expected alias %q to be valid, got: %v", v, err)
		}
	}

	// Invalid value.
	if err := vm.Validate("critical"); err == nil {
		t.Error("expected 'critical' to be invalid")
	}
}

func TestValueModelValidateStateMachine(t *testing.T) {
	vm := &ValueModel{
		Name:    "task-status",
		Kind:    VMStateMachine,
		Members: []string{"todo", "in-progress", "done", "blocked"},
		Transitions: [][2]string{
			{"todo", "in-progress"},
			{"todo", "blocked"},
			{"in-progress", "done"},
			{"in-progress", "blocked"},
			{"blocked", "in-progress"},
		},
		Aliases: map[string]string{"x": "done", " ": "todo"},
	}

	// Valid states.
	if err := vm.Validate("todo"); err != nil {
		t.Errorf("expected 'todo' valid: %v", err)
	}
	if err := vm.Validate("done"); err != nil {
		t.Errorf("expected 'done' valid: %v", err)
	}

	// Valid alias.
	if err := vm.Validate("x"); err != nil {
		t.Errorf("expected alias 'x' valid: %v", err)
	}

	// Invalid state.
	if err := vm.Validate("cancelled"); err == nil {
		t.Error("expected 'cancelled' to be invalid")
	}

	// Valid transition.
	if err := vm.ValidateTransition("todo", "in-progress"); err != nil {
		t.Errorf("expected todo->in-progress valid: %v", err)
	}

	// Invalid transition.
	if err := vm.ValidateTransition("done", "todo"); err == nil {
		t.Error("expected done->todo to be invalid transition")
	}
}

func TestValueModelValidateDate(t *testing.T) {
	vm := &ValueModel{
		Name:   "deadline",
		Kind:   VMDate,
		Format: "2006-01-02",
	}

	if err := vm.Validate("2026-04-12"); err != nil {
		t.Errorf("expected valid date: %v", err)
	}

	if err := vm.Validate("04/12/2026"); err == nil {
		t.Error("expected invalid date format to fail")
	}

	if err := vm.Validate("not-a-date"); err == nil {
		t.Error("expected non-date to fail")
	}
}

func TestValueModelValidateDateDefaultFormat(t *testing.T) {
	vm := &ValueModel{
		Name: "date",
		Kind: VMDate,
	}
	if err := vm.Validate("2026-01-15"); err != nil {
		t.Errorf("expected valid date with default format: %v", err)
	}
}

func TestValueModelValidateReference(t *testing.T) {
	vm := &ValueModel{
		Name:       "person",
		Kind:       VMReference,
		ResolvesTo: "node",
		Signifier:  "@",
	}

	if err := vm.Validate("julian"); err != nil {
		t.Errorf("expected non-empty reference valid: %v", err)
	}
	if err := vm.Validate(""); err == nil {
		t.Error("expected empty reference to fail")
	}
}

func TestValueModelValidateString(t *testing.T) {
	vm := &ValueModel{
		Name: "freetext",
		Kind: VMString,
	}
	if err := vm.Validate("anything goes"); err != nil {
		t.Errorf("string should accept anything: %v", err)
	}
	if err := vm.Validate(""); err != nil {
		t.Errorf("string should accept empty: %v", err)
	}
}

func TestValueModelResolve(t *testing.T) {
	vm := &ValueModel{
		Name:    "priority",
		Kind:    VMEnum,
		Members: []string{"low", "medium", "high", "urgent"},
		Aliases: map[string]string{"!": "urgent", "!!": "high"},
	}

	if got := vm.Resolve("!"); got != "urgent" {
		t.Errorf("Resolve('!') = %q, want 'urgent'", got)
	}
	if got := vm.Resolve("low"); got != "low" {
		t.Errorf("Resolve('low') = %q, want 'low'", got)
	}
}

func TestLoadValueModelFromEDN(t *testing.T) {
	// Set up a temp config directory.
	tmpDir := t.TempDir()
	config.OverrideConfigDir = tmpDir
	defer func() { config.OverrideConfigDir = "" }()

	vmDir := filepath.Join(tmpDir, "value-models")
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a priority value model.
	ednData := `{:name "priority"
 :kind "enum"
 :members ["low" "medium" "high" "urgent"]
 :aliases {"!" "urgent" "!!" "high"}}`

	if err := os.WriteFile(filepath.Join(vmDir, "priority.vm.edn"), []byte(ednData), 0644); err != nil {
		t.Fatal(err)
	}

	vm, err := LoadValueModel("priority")
	if err != nil {
		t.Fatalf("LoadValueModel: %v", err)
	}

	if vm.Name != "priority" {
		t.Errorf("Name = %q, want 'priority'", vm.Name)
	}
	if vm.Kind != VMEnum {
		t.Errorf("Kind = %q, want 'enum'", vm.Kind)
	}
	if len(vm.Members) != 4 {
		t.Errorf("Members len = %d, want 4", len(vm.Members))
	}
	if vm.Aliases["!"] != "urgent" {
		t.Errorf("Aliases['!'] = %q, want 'urgent'", vm.Aliases["!"])
	}

	// Validation should work.
	if err := vm.Validate("high"); err != nil {
		t.Errorf("Validate('high') failed: %v", err)
	}
	if err := vm.Validate("!"); err != nil {
		t.Errorf("Validate('!') failed: %v", err)
	}
	if err := vm.Validate("invalid"); err == nil {
		t.Error("Validate('invalid') should have failed")
	}
}

func TestLoadValueModelStateMachine(t *testing.T) {
	tmpDir := t.TempDir()
	config.OverrideConfigDir = tmpDir
	defer func() { config.OverrideConfigDir = "" }()

	vmDir := filepath.Join(tmpDir, "value-models")
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		t.Fatal(err)
	}

	ednData := `{:name "task-status"
 :kind "state-machine"
 :states ["todo" "in-progress" "done" "blocked"]
 :transitions [["todo" "in-progress"]
               ["in-progress" "done"]]
 :aliases {"x" "done"}}`

	if err := os.WriteFile(filepath.Join(vmDir, "task-status.vm.edn"), []byte(ednData), 0644); err != nil {
		t.Fatal(err)
	}

	vm, err := LoadValueModel("task-status")
	if err != nil {
		t.Fatalf("LoadValueModel: %v", err)
	}

	if vm.Kind != VMStateMachine {
		t.Errorf("Kind = %q, want 'state-machine'", vm.Kind)
	}
	if len(vm.Members) != 4 {
		t.Errorf("Members (states) len = %d, want 4", len(vm.Members))
	}
	if len(vm.Transitions) != 2 {
		t.Errorf("Transitions len = %d, want 2", len(vm.Transitions))
	}

	if err := vm.ValidateTransition("todo", "in-progress"); err != nil {
		t.Errorf("valid transition failed: %v", err)
	}
	if err := vm.ValidateTransition("todo", "done"); err == nil {
		t.Error("invalid transition should have failed")
	}
}

func TestListValueModels(t *testing.T) {
	tmpDir := t.TempDir()
	config.OverrideConfigDir = tmpDir
	defer func() { config.OverrideConfigDir = "" }()

	vmDir := filepath.Join(tmpDir, "value-models")
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"alpha", "beta"} {
		edn := `{:name "` + name + `" :kind "string"}`
		if err := os.WriteFile(filepath.Join(vmDir, name+".vm.edn"), []byte(edn), 0644); err != nil {
			t.Fatal(err)
		}
	}

	models, err := ListValueModels()
	if err != nil {
		t.Fatalf("ListValueModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].Name != "alpha" {
		t.Errorf("first model = %q, want 'alpha'", models[0].Name)
	}
}
