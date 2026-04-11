package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sevens/internal/config"
)

// FromConfig creates a Backend from the global config and an optional override name.
// If backendName is empty, uses the config's default backend.
// If no backend is configured, falls back to "anthropic".
func FromConfig(globalConfig config.GlobalConfig, backendName string) (Backend, error) {
	name := backendName
	if name == "" {
		name = globalConfig.Backend
	}
	if name == "" {
		name = "anthropic"
	}

	// Check for explicit backend config
	if cfg, ok := globalConfig.Backends[name]; ok {
		return fromBackendConfig(name, cfg, globalConfig)
	}

	// Infer backend type from name
	switch strings.ToLower(name) {
	case "anthropic", "api":
		return newAnthropicFromGlobal(globalConfig)
	case "codex":
		return NewCodexBackend("", generatedDir("codex")), nil
	case "claude":
		return NewClaudeBackend("", generatedDir("claude")), nil
	default:
		return nil, fmt.Errorf("unknown backend %q: configure it in :backends in config.edn", name)
	}
}

func fromBackendConfig(name string, cfg config.BackendConfig, globalConfig config.GlobalConfig) (Backend, error) {
	typ := cfg.Type
	if typ == "" {
		typ = name
	}

	switch strings.ToLower(typ) {
	case "anthropic", "api":
		return newAnthropicFromGlobal(globalConfig)
	case "codex":
		confDir := cfg.GeneratedConfDir
		if confDir == "" {
			confDir = generatedDir("codex")
		}
		return NewCodexBackend(cfg.Command, expandHome(confDir)), nil
	case "claude":
		confDir := cfg.GeneratedConfDir
		if confDir == "" {
			confDir = generatedDir("claude")
		}
		return NewClaudeBackend(cfg.Command, expandHome(confDir)), nil
	default:
		return nil, fmt.Errorf("unknown backend type %q for backend %q", typ, name)
	}
}

func newAnthropicFromGlobal(globalConfig config.GlobalConfig) (*AnthropicBackend, error) {
	return NewAnthropicBackend(globalConfig.LLM.APIKey, globalConfig.LLM.APIKeyEnv)
}

// generatedDir returns the default generated config directory for a backend.
func generatedDir(backendName string) string {
	configDir, err := configDirPath()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "generated", backendName)
}

func configDirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "sevens"), nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
