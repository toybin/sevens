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

// CodexBackend invokes OpenAI Codex CLI for inference.
type CodexBackend struct {
	Command          string // path to codex binary, default "codex"
	GeneratedConfDir string // path to generated config dir with MCP servers
}

// NewCodexBackend creates a Codex CLI backend.
func NewCodexBackend(command, generatedConfDir string) *CodexBackend {
	if command == "" {
		command = "codex"
	}
	return &CodexBackend{
		Command:          command,
		GeneratedConfDir: generatedConfDir,
	}
}

func (b *CodexBackend) Name() string { return "codex" }

func (b *CodexBackend) Complete(ctx context.Context, req InferenceRequest) (string, error) {
	prompt := PreparePrompt(req)

	// Use "exec" subcommand; prompt piped via stdin to avoid arg length limits
	args := []string{"exec"}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// codex exec is non-interactive — always use --full-auto.
	// For closed pipe, also use --ephemeral (no session persistence).
	// Scoped exploration safety is enforced by the prompt preamble and
	// the read-only sandbox mode.
	switch req.Exploration {
	case "scoped":
		args = append(args, "--full-auto")
		if req.ReadOnly {
			args = append(args, "-c", "sandbox_mode=\"read-only\"")
		}
	default: // "closed" or empty
		args = append(args, "--full-auto", "--ephemeral")
	}

	cmd := exec.CommandContext(ctx, b.Command, args...)
	cmd.Stdin = bytes.NewReader([]byte(prompt))

	// Graceful shutdown: SIGINT first, then SIGKILL after delay
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	// MCP servers are registered in the user's global ~/.codex/config.toml
	// (prefixed sevens-*) by `sevens config generate codex`. No CODEX_HOME override
	// needed — this preserves auth credentials and user config.

	// codex exec writes its full session transcript (headers, echoed prompt,
	// assistant turn, token counts) to stderr. Buffer it silently so the
	// terminal only sees the caller's own progress messages; surface it only
	// on error. If a StreamTo is provided, tee to it as well.
	var stderrBuf bytes.Buffer
	if req.StreamTo != nil {
		cmd.Stderr = io.MultiWriter(req.StreamTo, &stderrBuf)
	} else {
		cmd.Stderr = &stderrBuf
	}

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(stderrBuf.String())
			if stderr == "" {
				stderr = strings.TrimSpace(string(exitErr.Stderr))
			}
			if stderr != "" {
				return "", fmt.Errorf("codex exec failed (exit %d): %s", exitErr.ExitCode(), stderr)
			}
			return "", fmt.Errorf("codex exec failed (exit %d)", exitErr.ExitCode())
		}
		return "", fmt.Errorf("codex exec: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "", fmt.Errorf("codex returned empty response")
	}
	return result, nil
}
