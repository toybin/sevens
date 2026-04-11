package backend

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Backend is the inference interface. Implementations call an LLM via API or CLI.
type Backend interface {
	// Complete sends a prompt and returns the raw text response.
	Complete(ctx context.Context, req InferenceRequest) (string, error)

	// Name returns the backend identifier (e.g., "anthropic", "codex", "claude").
	Name() string
}

// InferenceRequest holds everything a backend needs to make an inference call.
type InferenceRequest struct {
	SystemPrompt   string    // sevens system prompt (baked into user prompt for CLI backends)
	Prompt         string    // rendered user prompt
	Model          string    // model ID or tier name
	Exploration    string    // "closed" (default), "scoped"
	ReadOnly       bool      // if true, CLI should not write files
	AllowFileReads bool      // if true, CLI may read explicitly-referenced files
	Capabilities   []string  // MCP server names needed for this call
	StreamTo       io.Writer // if non-nil, stream progress output here
}

// BuildPrompt combines the system prompt and user prompt into a single string
// for CLI backends that don't have separate system prompt flags.
func BuildPrompt(req InferenceRequest) string {
	if req.SystemPrompt != "" {
		return req.SystemPrompt + "\n\n---\n\n" + req.Prompt
	}
	return req.Prompt
}

// BuildPreamble generates a context constraint preamble based on the exploration
// tier and capabilities. This is prepended to the prompt for CLI backends to
// steer the agent away from unnecessary filesystem exploration.
func BuildPreamble(exploration string, capabilities []string, allowFileReads bool) string {
	if exploration == "" || exploration == "closed" {
		return "CONTEXT POLICY: All context for this task is provided in full below. " +
			"Do not read files, search the filesystem, or use any tools. " +
			"Respond based solely on the provided content.\n\n"
	}

	var sb strings.Builder

	sb.WriteString("CONTEXT POLICY: All knowledge graph context for this task is provided " +
		"in full below. Do not search for or read additional files unless explicitly instructed.\n\n")

	if len(capabilities) > 0 {
		sb.WriteString("EXTERNAL TOOLS: You have access to external integrations (")
		sb.WriteString(strings.Join(capabilities, ", "))
		sb.WriteString("). Use these to find related issues, tickets, or documentation " +
			"in external systems when relevant to the task.\n\n")
	}

	if allowFileReads {
		sb.WriteString("FILE ACCESS: You may read source files that are explicitly referenced " +
			"by path in the node content. Do not explore or search beyond those references.\n\n")
	} else {
		sb.WriteString("FILE ACCESS: Do not read local files or search the filesystem. " +
			"All necessary context is in this prompt.\n\n")
	}

	return sb.String()
}

// PreparePrompt builds the full prompt for a CLI backend: preamble + system prompt + user prompt.
func PreparePrompt(req InferenceRequest) string {
	preamble := BuildPreamble(req.Exploration, req.Capabilities, req.AllowFileReads)
	combined := BuildPrompt(req)
	return preamble + combined
}

// BackendConfig holds the configuration for selecting and configuring backends.
type BackendConfig struct {
	Default  string                      `edn:"default"`
	Backends map[string]BackendSettings  `edn:"backends"`
}

// BackendSettings holds per-backend configuration.
type BackendSettings struct {
	Type             string `edn:"type"`              // "anthropic", "codex", "claude"
	Command          string `edn:"command"`            // CLI command (e.g., "codex", "claude")
	GeneratedConfDir string `edn:"generated-conf-dir"` // path to generated config directory
}

// ErrBackendNotFound is returned when the requested backend is not configured.
var ErrBackendNotFound = fmt.Errorf("backend not found")
