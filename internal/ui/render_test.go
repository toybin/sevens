package ui

import (
	"regexp"
	"strings"
	"testing"
)

// helper to make a string pointer
func strPtr(s string) *string { return &s }

// stripANSI removes all ANSI escape sequences for test assertions.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// ─── RenderMarkdown ───────────────────────────────────────────────────────────

func TestRenderMarkdown_Heading(t *testing.T) {
	out, err := RenderMarkdown("# Hello World")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(stripANSI(out), "Hello World") {
		t.Errorf("expected output to contain 'Hello World', got: %q", out)
	}
}

func TestRenderMarkdown_Bold(t *testing.T) {
	out, err := RenderMarkdown("**bold text**")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stripANSI(out), "bold text") {
		t.Errorf("expected output to contain 'bold text', got: %q", out)
	}
}

func TestRenderMarkdown_List(t *testing.T) {
	out, err := RenderMarkdown("- item one\n- item two")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stripANSI(out), "item one") {
		t.Errorf("expected output to contain 'item one', got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "item two") {
		t.Errorf("expected output to contain 'item two', got: %q", out)
	}
}

func TestRenderMarkdown_ReturnsNonEmpty(t *testing.T) {
	out, err := RenderMarkdown("some content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("expected non-empty output for non-empty input")
	}
}

// ─── RenderMarkdownOrPlain ────────────────────────────────────────────────────

func TestRenderMarkdownOrPlain_Success(t *testing.T) {
	out := RenderMarkdownOrPlain("# Title\n\nSome text.")
	if !strings.Contains(stripANSI(out), "Title") {
		t.Errorf("expected rendered output to contain 'Title', got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "Some text.") {
		t.Errorf("expected rendered output to contain 'Some text.', got: %q", out)
	}
}

func TestRenderMarkdownOrPlain_EmptyInput(t *testing.T) {
	// Should return without panicking; output is at minimum empty/whitespace.
	out := RenderMarkdownOrPlain("")
	_ = out // just verify no panic
}

// ─── FormatNodeHeader ─────────────────────────────────────────────────────────

func TestFormatNodeHeader_NoParent(t *testing.T) {
	out := FormatNodeHeader("My Node", nil, "", nil, nil, nil, nil, nil)
	if !strings.Contains(stripANSI(out), "My Node") {
		t.Errorf("expected title in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "(none)") {
		t.Errorf("expected '(none)' for missing parent, got: %q", out)
	}
}

func TestFormatNodeHeader_WithParent(t *testing.T) {
	p := "Parent Node"
	out := FormatNodeHeader("Child Node", &p, "", nil, nil, nil, nil, nil)
	if !strings.Contains(stripANSI(out), "Child Node") {
		t.Errorf("expected title in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "Parent Node") {
		t.Errorf("expected parent name in output, got: %q", out)
	}
}

func TestFormatNodeHeader_WithRole(t *testing.T) {
	out := FormatNodeHeader("Node", nil, "primary", nil, nil, nil, nil, nil)
	if !strings.Contains(stripANSI(out), "role") {
		t.Errorf("expected 'role' label in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "primary") {
		t.Errorf("expected role value in output, got: %q", out)
	}
}

func TestFormatNodeHeader_WithChildren(t *testing.T) {
	children := []string{"ChildA", "ChildB"}
	childRoles := map[string]string{"ChildA": "sub"}
	out := FormatNodeHeader("Node", nil, "", children, nil, childRoles, nil, nil)
	if !strings.Contains(stripANSI(out), "ChildA") {
		t.Errorf("expected ChildA in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "ChildB") {
		t.Errorf("expected ChildB in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "sub") {
		t.Errorf("expected child role 'sub' in output, got: %q", out)
	}
}

func TestFormatNodeHeader_WithSiblings(t *testing.T) {
	siblings := []string{"SiblingX"}
	siblingRoles := map[string]string{"SiblingX": "peer"}
	out := FormatNodeHeader("Node", nil, "", nil, siblings, nil, siblingRoles, nil)
	if !strings.Contains(stripANSI(out), "SiblingX") {
		t.Errorf("expected SiblingX in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "peer") {
		t.Errorf("expected sibling role 'peer' in output, got: %q", out)
	}
}

func TestFormatNodeHeader_WithCrossRefs(t *testing.T) {
	crossRefs := []string{"RefOne", "RefTwo"}
	out := FormatNodeHeader("Node", nil, "", nil, nil, nil, nil, crossRefs)
	if !strings.Contains(stripANSI(out), "RefOne") {
		t.Errorf("expected cross-ref 'RefOne' in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "RefTwo") {
		t.Errorf("expected cross-ref 'RefTwo' in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "cross-refs") {
		t.Errorf("expected 'cross-refs' label in output, got: %q", out)
	}
}

func TestFormatNodeHeader_NoChildren(t *testing.T) {
	out := FormatNodeHeader("Node", nil, "", nil, nil, nil, nil, nil)
	if !strings.Contains(stripANSI(out), "children") {
		t.Errorf("expected 'children' label in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "(none)") {
		t.Errorf("expected '(none)' for missing children, got: %q", out)
	}
}

// ─── FormatStep ───────────────────────────────────────────────────────────────

func TestFormatStep(t *testing.T) {
	out := FormatStep("myFunc", "draft", "Target Node")
	if !strings.Contains(stripANSI(out), "myFunc") {
		t.Errorf("expected function name in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "draft") {
		t.Errorf("expected step name in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "Target Node") {
		t.Errorf("expected node title in output, got: %q", out)
	}
}

// ─── FormatPersona ────────────────────────────────────────────────────────────

func TestFormatPersona(t *testing.T) {
	out := FormatPersona("Editor")
	if !strings.Contains(stripANSI(out), "Editor") {
		t.Errorf("expected persona text in output, got: %q", out)
	}
}

// ─── FormatCost ───────────────────────────────────────────────────────────────

func TestFormatCost_AutoApproved(t *testing.T) {
	out := FormatCost(1500, 0.0025, true, 0.05)
	if !strings.Contains(stripANSI(out), "1500") {
		t.Errorf("expected token count in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "0.0025") {
		t.Errorf("expected cost value in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "auto-approved") {
		t.Errorf("expected 'auto-approved' in output, got: %q", out)
	}
}

func TestFormatCost_Manual(t *testing.T) {
	out := FormatCost(3000, 0.0150, false, 0.01)
	if !strings.Contains(stripANSI(out), "3000") {
		t.Errorf("expected token count in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "0.0150") {
		t.Errorf("expected cost value in output, got: %q", out)
	}
	if strings.Contains(out, "auto-approved") {
		t.Errorf("expected no 'auto-approved' in manual cost output, got: %q", out)
	}
}

// ─── FormatOp ─────────────────────────────────────────────────────────────────

func TestFormatOp_Create(t *testing.T) {
	out := FormatOp("create", "foo/bar.go")
	if !strings.Contains(stripANSI(out), "+") {
		t.Errorf("expected '+' symbol for create, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "foo/bar.go") {
		t.Errorf("expected file name in output, got: %q", out)
	}
}

func TestFormatOp_Edit(t *testing.T) {
	out := FormatOp("edit", "foo/bar.go")
	if !strings.Contains(stripANSI(out), "~") {
		t.Errorf("expected '~' symbol for edit, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "foo/bar.go") {
		t.Errorf("expected file name in output, got: %q", out)
	}
}

func TestFormatOp_Unknown(t *testing.T) {
	out := FormatOp("delete", "foo/bar.go")
	if !strings.Contains(stripANSI(out), "delete") {
		t.Errorf("expected action name in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "foo/bar.go") {
		t.Errorf("expected file name in output, got: %q", out)
	}
}

// ─── FormatLogEntry ───────────────────────────────────────────────────────────

func TestFormatLogEntry_Completed(t *testing.T) {
	out := FormatLogEntry("2024-01-01T12:00:00Z", "completed", "draft", "write", "", "")
	if !strings.Contains(stripANSI(out), "completed") {
		t.Errorf("expected event in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "draft") {
		t.Errorf("expected function in output, got: %q", out)
	}
}

func TestFormatLogEntry_Suggested(t *testing.T) {
	out := FormatLogEntry("2024-01-01T12:00:00Z", "suggested", "", "", "", "")
	if !strings.Contains(stripANSI(out), "suggested") {
		t.Errorf("expected event in output, got: %q", out)
	}
}

func TestFormatLogEntry_Accepted(t *testing.T) {
	out := FormatLogEntry("2024-01-01T12:00:00Z", "accepted", "", "", "", "")
	if !strings.Contains(stripANSI(out), "accepted") {
		t.Errorf("expected event in output, got: %q", out)
	}
}

func TestFormatLogEntry_Rejected(t *testing.T) {
	out := FormatLogEntry("2024-01-01T12:00:00Z", "rejected", "", "", "", "")
	if !strings.Contains(stripANSI(out), "rejected") {
		t.Errorf("expected event in output, got: %q", out)
	}
}

func TestFormatLogEntry_WithCommit(t *testing.T) {
	out := FormatLogEntry("2024-01-01T12:00:00Z", "completed", "fn", "step", "abc1234", "")
	if !strings.Contains(stripANSI(out), "abc1234") {
		t.Errorf("expected commit hash in output, got: %q", out)
	}
}

func TestFormatLogEntry_WithNote(t *testing.T) {
	out := FormatLogEntry("2024-01-01T12:00:00Z", "completed", "", "", "", "short note")
	if !strings.Contains(stripANSI(out), "short note") {
		t.Errorf("expected note in output, got: %q", out)
	}
}

func TestFormatLogEntry_NoteTruncation(t *testing.T) {
	longNote := strings.Repeat("x", 80)
	out := FormatLogEntry("2024-01-01T12:00:00Z", "completed", "", "", "", longNote)
	// The note should be truncated to 60 chars + "..."
	if !strings.Contains(stripANSI(out), "...") {
		t.Errorf("expected truncated note with '...', got: %q", out)
	}
	// Full 80-char note should NOT appear verbatim
	if strings.Contains(out, longNote) {
		t.Errorf("expected note to be truncated, but full note appeared in output: %q", out)
	}
}

func TestFormatLogEntry_WithoutFunctionAndStep(t *testing.T) {
	out := FormatLogEntry("2024-01-01T12:00:00Z", "completed", "", "", "", "")
	if !strings.Contains(stripANSI(out), "completed") {
		t.Errorf("expected event in output, got: %q", out)
	}
}

// ─── FormatPending ────────────────────────────────────────────────────────────

func TestFormatPending_WithAll(t *testing.T) {
	out := FormatPending("My Node", "draft", "write", "some summary", "")
	if !strings.Contains(stripANSI(out), "My Node") {
		t.Errorf("expected target in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "draft") {
		t.Errorf("expected function in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "write") {
		t.Errorf("expected step in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "some summary") {
		t.Errorf("expected summary in output, got: %q", out)
	}
}

func TestFormatPending_WithoutFunction(t *testing.T) {
	out := FormatPending("My Node", "", "", "", "")
	if !strings.Contains(stripANSI(out), "My Node") {
		t.Errorf("expected target in output, got: %q", out)
	}
	// No function means no arrow separator
	if strings.Contains(out, "→") {
		t.Errorf("expected no arrow when no function/step, got: %q", out)
	}
}

func TestFormatPending_WithFunctionNoStep(t *testing.T) {
	out := FormatPending("My Node", "draft", "", "", "")
	if !strings.Contains(stripANSI(out), "draft") {
		t.Errorf("expected function in output, got: %q", out)
	}
}

func TestFormatPending_WithSummaryNoFunction(t *testing.T) {
	out := FormatPending("My Node", "", "", "pending summary", "")
	if !strings.Contains(stripANSI(out), "pending summary") {
		t.Errorf("expected summary in output, got: %q", out)
	}
}

func TestFormatPending_WithSubject(t *testing.T) {
	subject := "suspension:My Node:20260408T120000"
	out := FormatPending("My Node", "draft", "write", "", subject)
	plain := stripANSI(out)
	// Only the timestamp portion should be shown.
	if !strings.Contains(plain, "20260408T120000") {
		t.Errorf("expected timestamp portion of subject id in output, got: %q", plain)
	}
	if strings.Contains(plain, "suspension:") {
		t.Errorf("expected full subject prefix to be stripped, got: %q", plain)
	}
}

// ─── SetTheme / Theme ─────────────────────────────────────────────────────────

func TestSetTheme_Light(t *testing.T) {
	SetTheme("light")
	if Theme() != "light" {
		t.Errorf("expected theme 'light', got: %q", Theme())
	}
}

func TestSetTheme_Dark(t *testing.T) {
	SetTheme("dark")
	if Theme() != "dark" {
		t.Errorf("expected theme 'dark', got: %q", Theme())
	}
	// Restore default for other tests
	SetTheme("light")
}

func TestSetTheme_InvalidIgnored(t *testing.T) {
	SetTheme("light")
	SetTheme("solarized") // invalid — should be ignored
	if Theme() != "light" {
		t.Errorf("invalid theme should be ignored; expected 'light', got: %q", Theme())
	}
}

// ─── RenderPrepareChecklist ───────────────────────────────────────────────────

func TestRenderPrepareChecklist_Basic(t *testing.T) {
	d := PrepareData{
		FnName:    "draft",
		NodeTitle: "My Task",
		Steps: []PrepareStep{
			{
				Name:   "write",
				Output: "ops",
				Prompt: "Write the section.",
			},
		},
	}
	out := RenderPrepareChecklist(d)
	if !strings.Contains(stripANSI(out), "draft") {
		t.Errorf("expected function name in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "My Task") {
		t.Errorf("expected node title in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "[task]") {
		t.Errorf("expected '[task]' header in output, got: %q", out)
	}
}

func TestRenderPrepareChecklist_MultiStep(t *testing.T) {
	d := PrepareData{
		FnName:    "research",
		NodeTitle: "Research Node",
		Steps: []PrepareStep{
			{Name: "gather", Output: "notes", Prompt: "Collect sources."},
			{Name: "synthesize", Output: "ops", Prompt: "Write synthesis."},
		},
	}
	out := RenderPrepareChecklist(d)
	if !strings.Contains(stripANSI(out), "[pipeline]") {
		t.Errorf("expected '[pipeline]' header for multi-step, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "gather") {
		t.Errorf("expected step 'gather' in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "synthesize") {
		t.Errorf("expected step 'synthesize' in output, got: %q", out)
	}
}

func TestRenderPrepareChecklist_WithGate(t *testing.T) {
	d := PrepareData{
		FnName:    "review",
		NodeTitle: "Gated Node",
		Steps: []PrepareStep{
			{Name: "analyze", Gate: "human", Output: "notes", Prompt: "Analyze."},
		},
	}
	out := RenderPrepareChecklist(d)
	if !strings.Contains(stripANSI(out), "[gate]") {
		t.Errorf("expected '[gate]' in gated step output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "human") {
		t.Errorf("expected gate name in output, got: %q", out)
	}
}

func TestRenderPrepareChecklist_WithDelegate(t *testing.T) {
	d := PrepareData{
		FnName:    "compose",
		NodeTitle: "Composed Node",
		Steps: []PrepareStep{
			{Name: "sub", Fn: "draft", Output: "ops"},
		},
	}
	out := RenderPrepareChecklist(d)
	if !strings.Contains(stripANSI(out), "delegates to") {
		t.Errorf("expected delegation label in output, got: %q", out)
	}
	if !strings.Contains(stripANSI(out), "draft") {
		t.Errorf("expected delegated function name in output, got: %q", out)
	}
}
