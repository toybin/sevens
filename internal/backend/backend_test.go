package backend

import (
	"strings"
	"testing"
)

// ─── BuildPrompt ──────────────────────────────────────────────────────────────

func TestBuildPrompt_WithSystemPrompt(t *testing.T) {
	req := InferenceRequest{
		SystemPrompt: "You are a helpful assistant.",
		Prompt:       "What is Go?",
	}
	out := BuildPrompt(req)
	if !strings.Contains(out, "You are a helpful assistant.") {
		t.Errorf("expected system prompt in output, got: %q", out)
	}
	if !strings.Contains(out, "What is Go?") {
		t.Errorf("expected user prompt in output, got: %q", out)
	}
	// Separator must appear between them
	if !strings.Contains(out, "---") {
		t.Errorf("expected '---' separator in output, got: %q", out)
	}
	// System prompt must come before user prompt
	sysIdx := strings.Index(out, "You are a helpful assistant.")
	userIdx := strings.Index(out, "What is Go?")
	if sysIdx >= userIdx {
		t.Errorf("expected system prompt before user prompt, got: %q", out)
	}
}

func TestBuildPrompt_NoSystemPrompt(t *testing.T) {
	req := InferenceRequest{
		SystemPrompt: "",
		Prompt:       "Tell me about Go.",
	}
	out := BuildPrompt(req)
	if out != "Tell me about Go." {
		t.Errorf("expected prompt returned verbatim, got: %q", out)
	}
	if strings.Contains(out, "---") {
		t.Errorf("expected no separator when no system prompt, got: %q", out)
	}
}

func TestBuildPrompt_CombinesWithSeparator(t *testing.T) {
	req := InferenceRequest{
		SystemPrompt: "sys",
		Prompt:       "user",
	}
	out := BuildPrompt(req)
	// Exact separator form
	if !strings.Contains(out, "\n\n---\n\n") {
		t.Errorf("expected '\\n\\n---\\n\\n' separator, got: %q", out)
	}
}

// ─── BuildPreamble ────────────────────────────────────────────────────────────

func TestBuildPreamble_ClosedExploration(t *testing.T) {
	out := BuildPreamble("closed", nil, false)
	if !strings.Contains(out, "CONTEXT POLICY") {
		t.Errorf("expected 'CONTEXT POLICY' in closed preamble, got: %q", out)
	}
	if !strings.Contains(out, "Do not read files") {
		t.Errorf("expected file-read restriction in closed preamble, got: %q", out)
	}
}

func TestBuildPreamble_EmptyExplorationActsLikeClosed(t *testing.T) {
	out := BuildPreamble("", nil, false)
	if !strings.Contains(out, "CONTEXT POLICY") {
		t.Errorf("expected 'CONTEXT POLICY' for empty exploration, got: %q", out)
	}
	if !strings.Contains(out, "Do not read files") {
		t.Errorf("expected file-read restriction for empty exploration, got: %q", out)
	}
}

func TestBuildPreamble_ScopedExploration(t *testing.T) {
	out := BuildPreamble("scoped", nil, false)
	// Scoped should NOT have the closed-mode "Do not read files, search..." line
	if strings.Contains(out, "Do not read files, search the filesystem, or use any tools.") {
		t.Errorf("scoped exploration should not have full file-read block, got: %q", out)
	}
	// Should still have a CONTEXT POLICY line (different wording)
	if !strings.Contains(out, "CONTEXT POLICY") {
		t.Errorf("expected CONTEXT POLICY header even for scoped, got: %q", out)
	}
}

func TestBuildPreamble_WithCapabilities(t *testing.T) {
	out := BuildPreamble("scoped", []string{"github", "jira"}, false)
	if !strings.Contains(out, "EXTERNAL TOOLS") {
		t.Errorf("expected 'EXTERNAL TOOLS' section with capabilities, got: %q", out)
	}
	if !strings.Contains(out, "github") {
		t.Errorf("expected 'github' in capabilities list, got: %q", out)
	}
	if !strings.Contains(out, "jira") {
		t.Errorf("expected 'jira' in capabilities list, got: %q", out)
	}
}

func TestBuildPreamble_NoCapabilities(t *testing.T) {
	out := BuildPreamble("scoped", nil, false)
	if strings.Contains(out, "EXTERNAL TOOLS") {
		t.Errorf("expected no EXTERNAL TOOLS section without capabilities, got: %q", out)
	}
}

func TestBuildPreamble_AllowFileReads(t *testing.T) {
	out := BuildPreamble("scoped", nil, true)
	if !strings.Contains(out, "FILE ACCESS") {
		t.Errorf("expected 'FILE ACCESS' section, got: %q", out)
	}
	if !strings.Contains(out, "You may read source files") {
		t.Errorf("expected file-read allowance text, got: %q", out)
	}
}

func TestBuildPreamble_DisallowFileReads(t *testing.T) {
	out := BuildPreamble("scoped", nil, false)
	if !strings.Contains(out, "FILE ACCESS") {
		t.Errorf("expected 'FILE ACCESS' section, got: %q", out)
	}
	if !strings.Contains(out, "Do not read local files") {
		t.Errorf("expected file-read prohibition text, got: %q", out)
	}
}

func TestBuildPreamble_ClosedDoesNotHaveFileAccessSection(t *testing.T) {
	// closed mode returns a single-line policy; no FILE ACCESS sub-section
	out := BuildPreamble("closed", nil, false)
	if strings.Contains(out, "FILE ACCESS") {
		t.Errorf("closed preamble should not have FILE ACCESS section, got: %q", out)
	}
}

// ─── PreparePrompt ────────────────────────────────────────────────────────────

func TestPreparePrompt_Full(t *testing.T) {
	req := InferenceRequest{
		SystemPrompt: "Be concise.",
		Prompt:       "Summarize the node.",
		Exploration:  "scoped",
	}
	out := PreparePrompt(req)
	// Preamble first
	if !strings.Contains(out, "CONTEXT POLICY") {
		t.Errorf("expected preamble in output, got: %q", out)
	}
	// Then system prompt
	if !strings.Contains(out, "Be concise.") {
		t.Errorf("expected system prompt in output, got: %q", out)
	}
	// Then user prompt
	if !strings.Contains(out, "Summarize the node.") {
		t.Errorf("expected user prompt in output, got: %q", out)
	}
	// Preamble should come before both
	preambleIdx := strings.Index(out, "CONTEXT POLICY")
	sysIdx := strings.Index(out, "Be concise.")
	userIdx := strings.Index(out, "Summarize the node.")
	if preambleIdx >= sysIdx || preambleIdx >= userIdx {
		t.Errorf("expected preamble to appear before system and user prompts, got: %q", out)
	}
}

func TestPreparePrompt_NoSystemPrompt(t *testing.T) {
	req := InferenceRequest{
		SystemPrompt: "",
		Prompt:       "Just the user prompt.",
		Exploration:  "closed",
	}
	out := PreparePrompt(req)
	if !strings.Contains(out, "CONTEXT POLICY") {
		t.Errorf("expected preamble in output, got: %q", out)
	}
	if !strings.Contains(out, "Just the user prompt.") {
		t.Errorf("expected user prompt in output, got: %q", out)
	}
	if strings.Contains(out, "---") {
		t.Errorf("expected no separator when no system prompt, got: %q", out)
	}
}

func TestPreparePrompt_NoExploration(t *testing.T) {
	req := InferenceRequest{
		SystemPrompt: "sys",
		Prompt:       "user",
		Exploration:  "",
	}
	out := PreparePrompt(req)
	// Empty exploration defaults to "closed" behaviour
	if !strings.Contains(out, "CONTEXT POLICY") {
		t.Errorf("expected preamble even with empty exploration, got: %q", out)
	}
	if !strings.Contains(out, "sys") {
		t.Errorf("expected system prompt in output, got: %q", out)
	}
	if !strings.Contains(out, "user") {
		t.Errorf("expected user prompt in output, got: %q", out)
	}
}

func TestPreparePrompt_PreamblePrecedesContent(t *testing.T) {
	req := InferenceRequest{
		SystemPrompt: "system content",
		Prompt:       "user content",
		Exploration:  "closed",
	}
	out := PreparePrompt(req)
	pIdx := strings.Index(out, "CONTEXT POLICY")
	sIdx := strings.Index(out, "system content")
	uIdx := strings.Index(out, "user content")
	if pIdx < 0 || sIdx < 0 || uIdx < 0 {
		t.Fatalf("one or more expected substrings missing from output: %q", out)
	}
	if pIdx >= sIdx || pIdx >= uIdx {
		t.Errorf("preamble must come before system and user prompts; order in output: %q", out)
	}
}
