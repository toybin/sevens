package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ClaudeBackend invokes Claude Code CLI for inference.
type ClaudeBackend struct {
	Command          string // path to claude binary, default "claude"
	GeneratedConfDir string // path to generated config dir with MCP configs
}

// NewClaudeBackend creates a Claude Code CLI backend.
func NewClaudeBackend(command, generatedConfDir string) *ClaudeBackend {
	if command == "" {
		command = "claude"
	}
	return &ClaudeBackend{
		Command:          command,
		GeneratedConfDir: generatedConfDir,
	}
}

func (b *ClaudeBackend) Name() string { return "claude" }

func (b *ClaudeBackend) Complete(ctx context.Context, req InferenceRequest) (string, error) {
	prompt := PreparePrompt(req)

	args := []string{
		"-p",
		"--output-format", "text",
		"--system-prompt", "Follow the instructions in the user message.",
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	switch req.Exploration {
	case "scoped":
		mcpPath := b.mcpConfigPath()
		if len(req.Capabilities) > 0 && mcpPath != "" {
			args = append(args, "--mcp-config", mcpPath)
		}
		allowed := b.buildAllowedTools(req)
		if allowed != "" {
			args = append(args, "--allowedTools", allowed)
		}
	default: // "closed" or empty
		args = append(args, "--tools", "")
	}

	cmd := exec.CommandContext(ctx, b.Command, args...)
	cmd.Stdin = bytes.NewReader([]byte(prompt))

	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	if req.StreamTo != nil {
		cmd.Stderr = req.StreamTo
	} else {
		cmd.Stderr = os.Stderr
	}

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if stderr != "" {
				return "", fmt.Errorf("claude failed (exit %d): %s", exitErr.ExitCode(), stderr)
			}
			return "", fmt.Errorf("claude failed (exit %d)", exitErr.ExitCode())
		}
		return "", fmt.Errorf("claude: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "", fmt.Errorf("claude returned empty response")
	}
	return result, nil
}

// mcpConfigPath returns the path to the generated MCP config JSON file.
func (b *ClaudeBackend) mcpConfigPath() string {
	if b.GeneratedConfDir == "" {
		return ""
	}
	return b.GeneratedConfDir + "/mcp.json"
}

// buildAllowedTools constructs the --allowedTools flag value for scoped exploration.
func (b *ClaudeBackend) buildAllowedTools(req InferenceRequest) string {
	if req.Exploration != "scoped" {
		return ""
	}

	var tools []string

	if req.AllowFileReads {
		tools = append(tools, "Read", "Grep", "Glob")
	}

	for _, cap := range req.Capabilities {
		tools = append(tools, "mcp__"+cap+"__*")
	}

	if len(tools) == 0 {
		return ""
	}
	return strings.Join(tools, ",")
}
