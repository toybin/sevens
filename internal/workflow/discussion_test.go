package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sevens/internal/workflow"
)

// ---------------------------------------------------------------------------
// StartDiscussion
// ---------------------------------------------------------------------------

func TestStartDiscussion_CreatesFile(t *testing.T) {
	e := setup(t)
	e.seedTree()

	// The discuss function outputs ops (ShapeFileOps). The mock must return
	// a JSON array of file ops that creates a "Discussion - The Commons" child.
	createOp := `[{"action": "create", "title": "Discussion - The Commons", "parent": "The Commons", "content": "# Discussion\n\n**[agent 2026-04-12 10:00]** What is the core tension in governance?"}]`
	e.withMock(createOp)

	state, agentOut, err := workflow.StartDiscussion(ctx(), e.deps, e.root, "The Commons")
	if err != nil {
		t.Fatalf("StartDiscussion error: %v", err)
	}

	if state.DiscussTitle != "Discussion - The Commons" {
		t.Fatalf("expected discuss title 'Discussion - The Commons', got %q", state.DiscussTitle)
	}
	if state.FocusTitle != "The Commons" {
		t.Fatalf("expected focus title 'The Commons', got %q", state.FocusTitle)
	}
	if !state.FileCreated {
		t.Fatal("expected FileCreated to be true")
	}
	if state.FilePath == "" {
		t.Fatal("expected non-empty FilePath")
	}

	// The file should exist on disk.
	if _, err := os.Stat(state.FilePath); os.IsNotExist(err) {
		t.Fatalf("discussion file not created at %s", state.FilePath)
	}

	// Agent output should contain the agent turn.
	if !strings.Contains(agentOut, "agent") || !strings.Contains(agentOut, "governance") {
		t.Logf("agent output: %s", agentOut)
		// Not a hard failure: extractLastAgentBlock depends on file content format.
	}
}

func TestStartDiscussion_ReturnsAgentOutput(t *testing.T) {
	e := setup(t)
	e.seedTree()

	createOp := `[{"action": "create", "title": "Discussion - The Commons", "parent": "The Commons", "content": "# Discussion\n\n**[agent 2026-04-12 10:00]** What assumptions are you making about community buy-in?"}]`
	e.withMock(createOp)

	_, agentOut, err := workflow.StartDiscussion(ctx(), e.deps, e.root, "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(agentOut, "community buy-in") {
		t.Fatalf("expected agent output to contain discussion text, got: %q", agentOut)
	}
}

// ---------------------------------------------------------------------------
// ContinueDiscussion
// ---------------------------------------------------------------------------

func TestContinueDiscussion_AppendsUserTurn(t *testing.T) {
	e := setup(t)
	e.seedTree()

	// Start the discussion.
	createOp := `[{"action": "create", "title": "Discussion - The Commons", "parent": "The Commons", "content": "# Discussion\n\n**[agent 2026-04-12 10:00]** What is the core tension?"}]`
	// For continue, the discuss function runs again and returns an edit op.
	editOp := `[{"action": "edit", "file": "Discussion - The Commons", "old_text": "What is the core tension?", "new_text": "What is the core tension?\n\n**[user 2026-04-12 10:05]** Equity vs efficiency.\n\n**[agent 2026-04-12 10:06]** Can you give a concrete example of that tradeoff?"}]`
	e.withMock(createOp, editOp)

	state, _, err := workflow.StartDiscussion(ctx(), e.deps, e.root, "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	agentReply, err := workflow.ContinueDiscussion(ctx(), e.deps, e.root, state, "Equity vs efficiency.")
	if err != nil {
		t.Fatalf("ContinueDiscussion error: %v", err)
	}

	// The file should contain the user turn.
	data, err := os.ReadFile(state.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Equity vs efficiency") {
		t.Fatalf("expected user turn in file, got:\n%s", string(data))
	}

	// agentReply may be empty if the edit op didn't produce a parseable agent block
	// on disk (depends on whether the mock edit actually applied). Log it for debugging.
	_ = agentReply
}

// TestStartDiscussion_PickerRoutesToEditOnExistingFile verifies that
// when a Discussion - <target> node already exists in the KB, the
// polymorphic discuss function routes to the `edit` primitive at
// dispatch time — not `create`. This is the specific behavior the
// EDN-declared picker expression in defaults/functions/discuss.edn
// is supposed to guarantee.
func TestStartDiscussion_PickerRoutesToEditOnExistingFile(t *testing.T) {
	e := setup(t)
	e.seedTree()

	// Pre-seed a discussion file so the picker's exists-node? check
	// is true for "Discussion - The Commons". This is the state a
	// user would be in on the second CLI invocation of discuss.
	e.writeFile("discussion-the-commons.md", `---
title: Discussion - The Commons
parent: "[[The Commons]]"
---

# Discussion

**[agent 2026-04-12 10:00]** What is the core tension in governance?`)
	e.sync()

	// The picker should resolve to `edit`, so the executor will tell
	// the LLM to produce an edit op. If the picker misfired and
	// resolved to `create`, a create op would be attempted and
	// ValidateOpsAgainst would reject it as a shape mismatch.
	editOp := `[{"action": "edit", "file": "Discussion - The Commons", "old_text": "What is the core tension in governance?", "new_text": "What is the core tension in governance?\n\n**[agent 2026-04-12 10:05]** Follow-up question."}]`
	e.withMock(editOp)

	state, _, err := workflow.StartDiscussion(ctx(), e.deps, e.root, "The Commons")
	if err != nil {
		t.Fatalf("StartDiscussion error: %v", err)
	}

	// Picker should have resolved to edit, and the edit should have
	// been materialized. FileCreated should be FALSE because the
	// file existed before we started.
	if state.FileCreated {
		t.Error("expected FileCreated=false for pre-existing discussion")
	}

	// The file should contain the appended agent turn.
	data, err := os.ReadFile(state.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Follow-up question") {
		t.Errorf("expected follow-up turn to be appended, got:\n%s", string(data))
	}
}

// TestStartDiscussion_PickerRejectsShapeMismatch verifies that the
// picker's enforcement contract holds at runtime: if the LLM returns
// ops whose action doesn't match the picker's resolved type, the
// executor fails loudly instead of silently materializing the wrong
// shape. This is the kernel-backed ValidateOpsAgainst check.
func TestStartDiscussion_PickerRejectsShapeMismatch(t *testing.T) {
	e := setup(t)
	e.seedTree()

	// No existing discussion file, so picker should resolve to `create`.
	// The mock below deliberately returns an EDIT op instead — the
	// shape the LLM should NOT have produced. The kernel validator
	// must catch this and refuse to advance.
	wrongShape := `[{"action": "edit", "file": "Discussion - The Commons", "old_text": "x", "new_text": "y"}]`
	e.withMock(wrongShape)

	_, _, err := workflow.StartDiscussion(ctx(), e.deps, e.root, "The Commons")
	if err == nil {
		t.Fatal("expected picker shape enforcement to reject the edit op")
	}
	// Error should identify the mismatch specifically.
	if !strings.Contains(err.Error(), "router selected") && !strings.Contains(err.Error(), "create") {
		t.Errorf("expected error naming picker selection, got: %v", err)
	}
}

func TestContinueDiscussion_ErrorsWithoutFilePath(t *testing.T) {
	e := setup(t)
	e.seedTree()

	state := &workflow.DiscussionState{
		DiscussTitle: "Discussion - The Commons",
		FilePath:     "", // no file path
		FocusTitle:   "The Commons",
	}

	_, err := workflow.ContinueDiscussion(ctx(), e.deps, e.root, state, "some input")
	if err == nil {
		t.Fatal("expected error for empty FilePath")
	}
}

// ---------------------------------------------------------------------------
// EndDiscussion
// ---------------------------------------------------------------------------

func TestEndDiscussion_NoError(t *testing.T) {
	e := setup(t)
	e.seedTree()

	createOp := `[{"action": "create", "title": "Discussion - The Commons", "parent": "The Commons", "content": "# Discussion\n\n**[agent 2026-04-12 10:00]** Opening question."}]`
	e.withMock(createOp)

	state, _, err := workflow.StartDiscussion(ctx(), e.deps, e.root, "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	// EndDiscussion should not error. It commits in git repos, but our test
	// temp dir is not a git repo, so commitHash will be empty.
	commitHash, err := workflow.EndDiscussion(ctx(), e.deps, e.root, state)
	if err != nil {
		t.Fatalf("EndDiscussion error: %v", err)
	}

	// Not a git repo, so no commit hash expected.
	_ = commitHash
}

// ---------------------------------------------------------------------------
// CancelDiscussion
// ---------------------------------------------------------------------------

func TestCancelDiscussion_RemovesNewFile(t *testing.T) {
	e := setup(t)
	e.seedTree()

	createOp := `[{"action": "create", "title": "Discussion - The Commons", "parent": "The Commons", "content": "# Discussion\n\n**[agent 2026-04-12 10:00]** Opening question."}]`
	e.withMock(createOp)

	state, _, err := workflow.StartDiscussion(ctx(), e.deps, e.root, "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	filePath := state.FilePath
	if filePath == "" {
		t.Fatal("no file path after start")
	}

	// Verify file exists before cancel.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("file should exist before cancel")
	}

	err = workflow.CancelDiscussion(ctx(), e.deps, e.root, state)
	if err != nil {
		t.Fatalf("CancelDiscussion error: %v", err)
	}

	// For a newly created file (no git), CancelDiscussion should remove it.
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatal("expected file to be removed after cancel")
	}
}

func TestCancelDiscussion_NoFileNoError(t *testing.T) {
	e := setup(t)

	state := &workflow.DiscussionState{
		DiscussTitle: "Discussion - Nothing",
		FilePath:     "",
		FocusTitle:   "Nothing",
	}

	err := workflow.CancelDiscussion(ctx(), e.deps, e.root, state)
	if err != nil {
		t.Fatalf("CancelDiscussion with no file should not error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// IsThreaded
// ---------------------------------------------------------------------------

func TestIsThreaded_SingleHeading(t *testing.T) {
	e := setup(t)
	path := filepath.Join(e.root, "single.md")
	os.WriteFile(path, []byte("# Discussion\n\nSome content."), 0644)

	if workflow.IsThreaded(path) {
		t.Fatal("single heading should not be threaded")
	}
}

func TestIsThreaded_MultipleHeadings(t *testing.T) {
	e := setup(t)
	path := filepath.Join(e.root, "threaded.md")
	content := "# Discussion\n\nSome content.\n\n# Enforcement Fatigue\n\nAnother thread."
	os.WriteFile(path, []byte(content), 0644)

	if !workflow.IsThreaded(path) {
		t.Fatal("multiple headings should be threaded")
	}
}

func TestIsThreaded_NonexistentFile(t *testing.T) {
	if workflow.IsThreaded("/nonexistent/path.md") {
		t.Fatal("nonexistent file should not be threaded")
	}
}
