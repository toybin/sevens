package backend

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	// Claude CLI supports --system-prompt natively, so pass system prompt
	// via the flag and user prompt via stdin (not baked together).
	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "Follow the instructions in the user message."
	}
	userPrompt := req.Prompt

	args := []string{
		"-p",
		"--output-format", "text",
		"--system-prompt", systemPrompt,
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	switch req.Exploration {
	case "scoped":
		// Scoped: use MCP servers from generated config. No --bare.
		mcpPath := b.mcpConfigPath()
		if len(req.Capabilities) > 0 && mcpPath != "" {
			args = append(args, "--mcp-config", mcpPath)
		}
		allowed := b.buildAllowedTools(req)
		if allowed != "" {
			args = append(args, "--allowedTools", allowed)
		}
	default: // "closed" or empty — no tools
		args = append(args, "--tools", "")
	}

	cmd := exec.CommandContext(ctx, b.Command, args...)
	cmd.Stdin = bytes.NewReader([]byte(userPrompt))

	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	var stderrBuf bytes.Buffer
	if req.StreamTo != nil {
		// Tee stderr to both the stream target and our buffer.
		cmd.Stderr = io.MultiWriter(req.StreamTo, &stderrBuf)
	} else {
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	}

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(stderrBuf.String())
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
