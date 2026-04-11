package apply

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFunction_BundledDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fn, err := LoadFunction("notice")
	if err != nil {
		t.Fatalf("LoadFunction bundled default: %v", err)
	}
	if fn.Name != "notice" {
		t.Fatalf("expected bundled function name %q, got %q", "notice", fn.Name)
	}
	if fn.Description == "" {
		t.Fatal("expected bundled function description")
	}
}

func TestLoadFunction_UserOverrideWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	fnDir := filepath.Join(home, ".config", "sevens", "functions")
	if err := os.MkdirAll(fnDir, 0755); err != nil {
		t.Fatalf("mkdir functions dir: %v", err)
	}

	data := []byte(`{:name "notice"
 :description "override description"
 :prompt "<instruction>override</instruction>"
 :input "node"
 :output "text"}`)
	if err := os.WriteFile(filepath.Join(fnDir, "notice.edn"), data, 0644); err != nil {
		t.Fatalf("write override edn: %v", err)
	}

	fn, err := LoadFunction("notice")
	if err != nil {
		t.Fatalf("LoadFunction override: %v", err)
	}
	if fn.Description != "override description" {
		t.Fatalf("expected override description, got %q", fn.Description)
	}
	if fn.Prompt != "<instruction>override</instruction>" {
		t.Fatalf("expected override prompt, got %q", fn.Prompt)
	}
}

func TestListFunctions_IncludesBundledDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fns, err := ListFunctions()
	if err != nil {
		t.Fatalf("ListFunctions: %v", err)
	}
	if len(fns) == 0 {
		t.Fatal("expected bundled functions")
	}

	found := false
	for _, fn := range fns {
		if fn.Name == "notice" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected bundled function notice in list")
	}
}
