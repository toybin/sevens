package function

import (
	"strings"
	"testing"
)

func TestValidateOps_GoodCreate(t *testing.T) {
	ops := []FileOp{
		{
			Action:  "create",
			Title:   "New Node",
			Content: "body",
		},
	}
	if err := ValidateOps(ops); err != nil {
		t.Errorf("expected valid create to pass: %v", err)
	}
}

func TestValidateOps_CreateMissingTitle(t *testing.T) {
	// This is the "untitled.md" failure mode: a create op without a
	// title should fail loudly, not slug-fall-back to untitled.
	ops := []FileOp{
		{Action: "create", Content: "body"},
	}
	err := ValidateOps(ops)
	if err == nil {
		t.Fatal("expected create without title to fail")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("expected title error, got %v", err)
	}
}

func TestValidateOps_CreateMissingContent(t *testing.T) {
	ops := []FileOp{
		{Action: "create", Title: "Some Title"},
	}
	err := ValidateOps(ops)
	if err == nil {
		t.Fatal("expected create without content to fail")
	}
	if !strings.Contains(err.Error(), "content") {
		t.Errorf("expected content error, got %v", err)
	}
}

func TestValidateOps_GoodEdit(t *testing.T) {
	ops := []FileOp{
		{
			Action:  "edit",
			File:    "Discussion - Braindump",
			OldText: "last line",
			NewText: "last line\nnew content",
		},
	}
	if err := ValidateOps(ops); err != nil {
		t.Errorf("expected valid edit to pass: %v", err)
	}
}

// The concrete discuss failure mode: edit op with empty file field.
// Before this validator, bad ops silently slugified to untitled.md.
func TestValidateOps_EditMissingFile(t *testing.T) {
	ops := []FileOp{
		{
			Action:  "edit",
			OldText: "foo",
			NewText: "bar",
		},
	}
	err := ValidateOps(ops)
	if err == nil {
		t.Fatal("expected edit without file to fail")
	}
	if !strings.Contains(err.Error(), "file") {
		t.Errorf("expected file error, got %v", err)
	}
}

func TestValidateOps_EditMissingOldText(t *testing.T) {
	ops := []FileOp{
		{
			Action:  "edit",
			File:    "some-file",
			NewText: "bar",
		},
	}
	err := ValidateOps(ops)
	if err == nil {
		t.Fatal("expected edit without old_text to fail")
	}
	if !strings.Contains(err.Error(), "old_text") {
		t.Errorf("expected old_text error, got %v", err)
	}
}

func TestValidateOps_MultipleOps_FirstValidSecondBad(t *testing.T) {
	// Second op is bad (edit with no file). Should report op[1] specifically.
	ops := []FileOp{
		{Action: "create", Title: "ok", Content: "body"},
		{Action: "edit", OldText: "x", NewText: "y"},
	}
	err := ValidateOps(ops)
	if err == nil {
		t.Fatal("expected failure on second op")
	}
	if !strings.Contains(err.Error(), "op[1]") {
		t.Errorf("expected op[1] in error, got %v", err)
	}
}

func TestValidateOps_MissingAction(t *testing.T) {
	ops := []FileOp{{Title: "no action"}}
	err := ValidateOps(ops)
	if err == nil {
		t.Fatal("expected missing-action error")
	}
	if !strings.Contains(err.Error(), "action") {
		t.Errorf("expected action error, got %v", err)
	}
}

func TestValidateOps_UnknownAction(t *testing.T) {
	ops := []FileOp{{Action: "frobnicate", Title: "x"}}
	err := ValidateOps(ops)
	if err == nil {
		t.Fatal("expected unknown-action error")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("expected unknown action error, got %v", err)
	}
}

func TestValidateOps_EmptyListPasses(t *testing.T) {
	if err := ValidateOps(nil); err != nil {
		t.Errorf("empty list should pass: %v", err)
	}
}
