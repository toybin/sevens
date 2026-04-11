// Package config loads the global sevens configuration from
// ~/.config/sevens/config.edn. It has no graph, function, or LLM
// dependencies — only the store package for ConfigDir.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"olympos.io/encoding/edn"
	"sevens/internal/store"
)

// LLMConfig holds connection details for an LLM provider.
type LLMConfig struct {
	Provider  string `edn:"provider"`
	Model     string `edn:"model"`
	APIKeyEnv string `edn:"api-key-env"`
	APIKey    string `edn:"api-key"` // direct API key, takes precedence over api-key-env
}

// BackendConfig holds the configuration for a specific backend.
type BackendConfig struct {
	Type             string `edn:"type"`               // "anthropic", "codex", "claude"
	Command          string `edn:"command"`             // CLI binary path (for codex/claude)
	GeneratedConfDir string `edn:"generated-conf-dir"` // path to generated config dir (for codex/claude)
}

// GlobalConfig is the top-level config from ~/.config/sevens/config.edn.
type GlobalConfig struct {
	LLM           LLMConfig                `edn:"llm"`
	Models        map[string]LLMConfig     `edn:"models"`   // named profiles like "fast", "capable", "powerful"
	Backend       string                   `edn:"backend"`  // default backend name (e.g., "anthropic", "codex", "claude")
	Backends      map[string]BackendConfig `edn:"backends"` // backend configurations
	SystemPrompt  string                   `edn:"system-prompt"`
	ContextFiles  []string                 `edn:"context-files"`  // global context files injected into every call
	CostThreshold float64                  `edn:"cost-threshold"` // auto-approve below this USD amount
	Theme         string                   `edn:"theme"`          // "light" or "dark" for glamour rendering
}

// ResolveModel looks up a named model profile from the Models map and returns the
// resolved LLMConfig. Fields not set in the profile are inherited from the default
// LLM config. If name is empty or not found, the default LLM config is returned.
func (g *GlobalConfig) ResolveModel(name string) LLMConfig {
	if name == "" {
		return g.LLM
	}
	profile, ok := g.Models[name]
	if !ok {
		return g.LLM
	}
	// Inherit missing fields from the default LLM config.
	if profile.Provider == "" {
		profile.Provider = g.LLM.Provider
	}
	if profile.Model == "" {
		profile.Model = g.LLM.Model
	}
	if profile.APIKey == "" && profile.APIKeyEnv == "" {
		profile.APIKeyEnv = g.LLM.APIKeyEnv
		profile.APIKey = g.LLM.APIKey
	}
	return profile
}

var defaultLLMConfig = LLMConfig{
	Provider:  "anthropic",
	Model:     "claude-sonnet-4-20250514",
	APIKeyEnv: "ANTHROPIC_API_KEY",
}

// LoadGlobalConfig reads and parses ~/.config/sevens/config.edn.
// If the file does not exist, sensible defaults are returned.
func LoadGlobalConfig() (GlobalConfig, error) {
	configDir, err := store.ConfigDir()
	if err != nil {
		return GlobalConfig{}, fmt.Errorf("get config dir: %w", err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, "config.edn"))
	if os.IsNotExist(err) {
		return GlobalConfig{LLM: defaultLLMConfig}, nil
	}
	if err != nil {
		return GlobalConfig{}, fmt.Errorf("read config.edn: %w", err)
	}

	var cfg GlobalConfig
	if err := edn.Unmarshal(data, &cfg); err != nil {
		return GlobalConfig{}, fmt.Errorf("parse config.edn: %w", err)
	}

	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = defaultLLMConfig.Provider
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = defaultLLMConfig.Model
	}
	if cfg.LLM.APIKeyEnv == "" {
		cfg.LLM.APIKeyEnv = defaultLLMConfig.APIKeyEnv
	}
	if cfg.CostThreshold == 0 {
		cfg.CostThreshold = 0.01
	}

	return cfg, nil
}
