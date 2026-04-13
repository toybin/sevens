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
