package function

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"sevens/defaults"
	"sevens/internal/config"
)

func TestLoadDeterministicFunctions(t *testing.T) {
	cfgDir := t.TempDir()
	config.OverrideConfigDir = cfgDir
	t.Cleanup(func() { config.OverrideConfigDir = "" })
	fnDir := filepath.Join(cfgDir, "functions")
	os.MkdirAll(fnDir, 0755)
	defaults.SeedFunctions(fnDir)

	for _, name := range []string{"daily-note", "inbox-capture", "inbox-root", "append-note", "section-entry"} {
		fn, _, err := LoadFunction(name)
		if err != nil {
			t.Fatalf("LoadFunction(%q): %v", name, err)
		}
		if fn.Name != name {
			t.Fatalf("expected name %q, got %q", name, fn.Name)
		}
		if len(fn.Steps) == 0 {
			t.Fatalf("%s: expected at least one step", name)
		}
		step := fn.Steps[0]
		if step.Backend.Kind != BackendDeterministic {
			t.Fatalf("%s: expected deterministic backend, got %d", name, step.Backend.Kind)
		}
		if step.Backend.Handler == "" {
			t.Fatalf("%s: expected non-empty handler JSON", name)
		}
		var cfg DeterministicConfig
		if err := json.Unmarshal([]byte(step.Backend.Handler), &cfg); err != nil {
			t.Fatalf("%s: parse handler JSON: %v", name, err)
		}
		if cfg.Mode == "" {
			t.Fatalf("%s: expected non-empty mode", name)
		}
		if step.Backend.PromptTemplate == "" {
			t.Fatalf("%s: expected non-empty prompt template from .md sidecar", name)
		}
		t.Logf("OK %s: mode=%s, prompt=%d chars", name, cfg.Mode, len(step.Backend.PromptTemplate))
	}
}
