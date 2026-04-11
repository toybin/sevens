package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"olympos.io/encoding/edn"
	"sevens/internal/store"
)

// kwStr converts an edn.Keyword to a plain string (strips leading colon).
func kwStr(k edn.Keyword) string {
	s := string(k)
	if len(s) > 0 && s[0] == ':' {
		return s[1:]
	}
	return s
}

// MCPServerDef is a single MCP server definition from capabilities.edn.
type MCPServerDef struct {
	Description string                      `edn:"description"`
	Command     string                      `edn:"command"`
	Args        []string                    `edn:"args"`
	Env         map[edn.Keyword]string      `edn:"env"`
}

// Capabilities holds the central MCP server and skill definitions.
type Capabilities struct {
	MCPServers map[edn.Keyword]MCPServerDef `edn:"mcp-servers"`
}

// LoadCapabilities reads capabilities.edn from the sevens config directory.
func LoadCapabilities() (*Capabilities, error) {
	dir, err := store.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("config dir: %w", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "capabilities.edn"))
	if os.IsNotExist(err) {
		return &Capabilities{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read capabilities.edn: %w", err)
	}

	var caps Capabilities
	if err := edn.Unmarshal(data, &caps); err != nil {
		return nil, fmt.Errorf("parse capabilities.edn: %w", err)
	}
	return &caps, nil
}

// GenerateCodexConfig appends MCP server stanzas to the user's ~/.codex/config.toml.
// It first removes any previously generated sevens MCP stanzas (marked by comments),
// then appends the new ones.
func GenerateCodexConfig(caps *Capabilities, _ string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	configPath := filepath.Join(home, ".codex", "config.toml")

	// Read existing config
	existing, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	// Strip previously generated sevens section
	content := string(existing)
	const marker = "\n# --- sevens-managed MCP servers (do not edit below) ---"
	if idx := strings.Index(content, marker); idx >= 0 {
		content = strings.TrimRight(content[:idx], "\n")
	}

	// Build new MCP stanzas
	var sb strings.Builder
	sb.WriteString(marker)
	sb.WriteString("\n")

	for kw, server := range caps.MCPServers {
		name := "sevens-" + kwStr(kw)
		fmt.Fprintf(&sb, "\n[mcp_servers.%s]\n", name)
		fmt.Fprintf(&sb, "command = %q\n", server.Command)

		if len(server.Args) > 0 {
			args := make([]string, len(server.Args))
			for i, a := range server.Args {
				args[i] = fmt.Sprintf("%q", a)
			}
			fmt.Fprintf(&sb, "args = [%s]\n", strings.Join(args, ", "))
		}

		if len(server.Env) > 0 {
			var envPairs []string
			for k, v := range server.Env {
				envPairs = append(envPairs, fmt.Sprintf("%q = %q", kwStr(k), v))
			}
			fmt.Fprintf(&sb, "env = { %s }\n", strings.Join(envPairs, ", "))
		}
	}

	// Write back
	result := content + sb.String()
	if err := os.WriteFile(configPath, []byte(result), 0644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	fmt.Fprintf(os.Stderr, "wrote %d MCP servers to %s (prefixed sevens-*)\n", len(caps.MCPServers), configPath)
	return nil
}

// claudeMCPConfig is the JSON structure for Claude Code's .mcp.json.
type claudeMCPConfig struct {
	MCPServers map[string]claudeMCPServer `json:"mcpServers"`
}

type claudeMCPServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// GenerateClaudeConfig writes a Claude Code mcp.json with MCP server definitions.
func GenerateClaudeConfig(caps *Capabilities, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	config := claudeMCPConfig{
		MCPServers: make(map[string]claudeMCPServer),
	}

	for kw, server := range caps.MCPServers {
		name := kwStr(kw)

		env := make(map[string]string)
		for k, v := range server.Env {
			env[kwStr(k)] = v
		}

		cs := claudeMCPServer{
			Command: server.Command,
			Args:    server.Args,
		}
		if len(env) > 0 {
			cs.Env = env
		}
		config.MCPServers[name] = cs
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mcp.json: %w", err)
	}

	path := filepath.Join(outputDir, "mcp.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(os.Stderr, "wrote %s (%d MCP servers)\n", path, len(caps.MCPServers))
	return nil
}

// CheckCapabilities verifies that capabilities.edn has definitions
// for all the requested capability names. Returns missing capability names.
func CheckCapabilities(caps *Capabilities, requested []string) []string {
	var missing []string
	for _, name := range requested {
		kw := edn.Keyword(":" + name)
		if _, ok := caps.MCPServers[kw]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}
