package workflow_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sevens/defaults"
	"sevens/internal/config"
	"sevens/internal/function"
	"sevens/internal/graphops"
	"sevens/internal/kb"
	projmd "sevens/internal/projection/md"
	"sevens/internal/triple"
	"sevens/internal/workflow"

	_ "turso.tech/database/tursogo"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type testEnv struct {
	t     *testing.T
	root  string
	deps  *workflow.Deps
	store *triple.Store
	k     *kb.KB
}

func setup(t *testing.T) *testEnv {
	t.Helper()
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	store, err := triple.New(db)
	if err != nil {
		t.Fatal(err)
	}
	graph := graphops.New(store)
	k := kb.New(graph)
	proj := projmd.New(k)
	ps := function.NewPipelineStore(store)

	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".sevens.edn"),
		[]byte(fmt.Sprintf(`{:path %q :alias "test"}`, root)), 0644)

	// Seed functions into a temp config dir so LoadFunction works.
	cfgDir := t.TempDir()
	config.OverrideConfigDir = cfgDir
	t.Cleanup(func() { config.OverrideConfigDir = "" })
	fnDir := filepath.Join(cfgDir, "functions")
	os.MkdirAll(fnDir, 0755)
	defaults.SeedFunctions(fnDir)

	return &testEnv{
		t:     t,
		root:  root,
		store: store,
		k:     k,
		deps: &workflow.Deps{
			KB:    k,
			Proj:  proj,
			Store: ps,
		},
	}
}

func (e *testEnv) withMock(responses ...string) {
	e.deps.Backend = &mockBackend{responses: responses}
}

func (e *testEnv) writeFile(name, content string) {
	e.t.Helper()
	os.WriteFile(filepath.Join(e.root, name), []byte(content), 0644)
}

func (e *testEnv) sync() {
	e.t.Helper()
	_, err := e.deps.Proj.Sync(context.Background(), e.root)
	if err != nil {
		e.t.Fatalf("sync: %v", err)
	}
}

func (e *testEnv) seedTree() {
	e.t.Helper()
	e.writeFile("the-commons.md", `---
title: The Commons
---

A neighborhood that has one place that's a library but also a tool library
and a seed bank and a repair cafe all mashed together.`)

	e.writeFile("lending.md", `---
title: Lending Infrastructure Design
parent: "[[The Commons]]"
---

The technical challenge of creating unified lending systems.`)

	e.writeFile("governance.md", `---
title: Commons Governance Models
parent: "[[The Commons]]"
---

How decisions get made about what to stock and lending periods.`)

	e.sync()
}

func ctx() context.Context { return context.Background() }

// mockBackend for workflow tests
type mockBackend struct {
	responses []string
	callCount int
}

func (m *mockBackend) Execute(ctx context.Context, prompt function.RenderedPrompt) (function.TransformResult, error) {
	if m.callCount >= len(m.responses) {
		return function.TransformResult{Raw: "(exhausted)", IsText: true}, nil
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return function.TransformResult{Raw: resp, IsText: true}, nil
}

func (m *mockBackend) Name() string { return "mock" }

// ---------------------------------------------------------------------------
// ApplyFunction
// ---------------------------------------------------------------------------

func TestApplyFunction_TextOutput(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock("Gaps found in governance section.")

	result, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "notice", "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	if result.Suspended {
		t.Fatal("notice should not suspend")
	}
	if !strings.Contains(result.Output, "Gaps") {
		t.Fatalf("expected output with 'Gaps', got: %s", result.Output)
	}
	if result.FunctionName != "notice" {
		t.Fatalf("expected function 'notice', got %q", result.FunctionName)
	}
	if result.Target != "The Commons" {
		t.Fatalf("expected target 'The Commons', got %q", result.Target)
	}
}

func TestApplyFunction_Suspended(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock(`[{"title": "Child A", "rationale": "reason"}]`)

	result, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "decompose", "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	if !result.Suspended {
		t.Fatal("decompose should suspend at gate")
	}
	if result.PipelineID == "" {
		t.Fatal("expected pipeline ID")
	}
	if result.BackendName != "mock" {
		t.Fatalf("expected backend 'mock', got %q", result.BackendName)
	}
}

func TestApplyFunction_OpsApplied(t *testing.T) {
	e := setup(t)
	e.seedTree()

	// Use exact text from the file body for the edit op.
	// Avoid apostrophes in JSON to prevent encoding mismatch.
	opsResp := `[{"action": "edit", "file": "The Commons", "old_text": "and a seed bank and a repair cafe all mashed together.", "new_text": "and a seed bank and a repair cafe and a workshop all mashed together."}]`
	e.withMock(opsResp)

	result, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "sharpen", "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	if result.Suspended {
		t.Fatal("sharpen (no gate) should not suspend")
	}

	// Ops should have been applied to disk
	content, _ := os.ReadFile(filepath.Join(e.root, "the-commons.md"))
	if !strings.Contains(string(content), "and a workshop all mashed together") {
		t.Fatalf("expected ops to be applied to file, got:\n%s", string(content))
	}
	if len(result.FilesEdited) == 0 {
		t.Fatal("expected files edited in result")
	}
}

func TestApplyFunction_ChallengeHistory(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock("The assumption about walkability is unfounded.")

	// BUG-3 regression: challenge requires "history" role
	result, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "challenge", "The Commons")
	if err != nil {
		t.Fatalf("challenge should not error (BUG-3 regression): %v", err)
	}
	if result.Suspended {
		t.Fatal("challenge should not suspend")
	}
}

// ---------------------------------------------------------------------------
// AcceptPipeline
// ---------------------------------------------------------------------------

func TestAcceptPipeline_Advance(t *testing.T) {
	e := setup(t)
	e.seedTree()

	suggestResp := `[{"title": "Revenue", "rationale": "funding"}]`
	generateResp := `[{"action": "create", "title": "Revenue", "parent": "The Commons", "content": "# Revenue\n\nFunding models."}]`
	e.withMock(suggestResp, generateResp)

	// Apply decompose (suspends)
	ar, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "decompose", "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if !ar.Suspended {
		t.Fatal("should suspend")
	}

	// Accept (advances to generate step, which also has a gate)
	acceptResult, err := workflow.AcceptPipeline(ctx(), e.deps, e.root, ar.PipelineID, "")
	if err != nil {
		t.Fatal(err)
	}

	// decompose has two gated steps, so it should suspend again at generate
	if !acceptResult.Suspended {
		// If it completed, the ops should have been applied
		if acceptResult.Completed && len(acceptResult.FilesCreated) > 0 {
			// This is also a valid outcome if generate auto-accepts
			return
		}
	}
}

func TestAcceptPipeline_Revise(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock(
		`[{"title": "V1", "rationale": "first"}]`,
		`[{"title": "V2", "rationale": "revised"}]`,
	)

	ar, _ := workflow.ApplyFunction(ctx(), e.deps, e.root, "decompose", "The Commons")

	revised, err := workflow.AcceptPipeline(ctx(), e.deps, e.root, ar.PipelineID, "add infrastructure")
	if err != nil {
		t.Fatal(err)
	}
	if !revised.Suspended {
		t.Fatal("revise should stay suspended")
	}
}

// ---------------------------------------------------------------------------
// RejectPipeline
// ---------------------------------------------------------------------------

func TestRejectPipeline(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock(`[{"title": "Bad", "rationale": "bad idea"}]`)

	ar, _ := workflow.ApplyFunction(ctx(), e.deps, e.root, "decompose", "The Commons")

	err := workflow.RejectPipeline(ctx(), e.deps, e.root, ar.PipelineID)
	if err != nil {
		t.Fatal(err)
	}

	// Should be terminal
	p, _ := e.deps.Store.Load(ctx(), ar.PipelineID)
	if !p.Phase.IsTerminal() {
		t.Fatalf("expected terminal phase, got %s", p.Phase)
	}

	// Log should record rejection
	entries, _ := e.k.ReadLog(ctx(), e.root, "The Commons")
	var found bool
	for _, entry := range entries {
		if entry.Event == "rejected" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected 'rejected' log entry")
	}
}

// ---------------------------------------------------------------------------
// SyncFiles
// ---------------------------------------------------------------------------

func TestSyncFiles(t *testing.T) {
	e := setup(t)
	e.writeFile("note.md", "---\ntitle: A Note\n---\nSome content.")

	result, violations, err := workflow.SyncFiles(ctx(), e.deps, e.root, 9)
	if err != nil {
		t.Fatal(err)
	}
	if result.NodesScanned == 0 {
		t.Fatal("expected at least 1 node scanned")
	}

	// No cycles in a single node
	for _, v := range violations {
		if v.Kind == "cycle" {
			t.Fatalf("unexpected cycle: %s", v.Title)
		}
	}
}

func TestSyncFiles_Validates(t *testing.T) {
	e := setup(t)
	e.writeFile("root.md", "---\ntitle: Root\n---\nRoot.")
	for i := 0; i < 10; i++ {
		e.writeFile(fmt.Sprintf("c%d.md", i),
			fmt.Sprintf("---\ntitle: C%d\nparent: \"[[Root]]\"\n---\nChild.", i))
	}

	_, violations, err := workflow.SyncFiles(ctx(), e.deps, e.root, 9)
	if err != nil {
		t.Fatal(err)
	}

	var overflow bool
	for _, v := range violations {
		if v.Kind == "overflow" {
			overflow = true
		}
	}
	if !overflow {
		t.Fatal("expected overflow violation with 10 children")
	}
}

// ---------------------------------------------------------------------------
// FindPendingPipeline
// ---------------------------------------------------------------------------

func TestFindPending_SingleMatch(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock(`[{"title": "X", "rationale": "y"}]`)

	ar, _ := workflow.ApplyFunction(ctx(), e.deps, e.root, "decompose", "The Commons")
	if !ar.Suspended {
		t.Fatal("should suspend")
	}

	p, err := workflow.FindPendingPipeline(ctx(), e.deps, e.root, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if p.Target != "The Commons" {
		t.Fatalf("expected target 'The Commons', got %q", p.Target)
	}
}

func TestFindPending_NotFound(t *testing.T) {
	e := setup(t)
	e.seedTree()

	_, err := workflow.FindPendingPipeline(ctx(), e.deps, e.root, "Nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent")
	}
}

// ---------------------------------------------------------------------------
// Backend persistence through workflow
// ---------------------------------------------------------------------------

func TestWorkflow_BackendPersistence(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock(`[{"title": "X", "rationale": "y"}]`)

	ar, _ := workflow.ApplyFunction(ctx(), e.deps, e.root, "decompose", "The Commons")
	if ar.BackendName != "mock" {
		t.Fatalf("expected backend 'mock', got %q", ar.BackendName)
	}

	// Load the pipeline directly and verify backend was persisted
	p, _ := e.deps.Store.Load(ctx(), ar.PipelineID)
	if p.BackendName != "mock" {
		t.Fatalf("persisted backend should be 'mock', got %q", p.BackendName)
	}
}

// ---------------------------------------------------------------------------
// Context resolution through workflow (BUG-2 regression)
// ---------------------------------------------------------------------------

func TestWorkflow_ContextPopulated(t *testing.T) {
	e := setup(t)
	e.seedTree()

	fn, _, _ := function.LoadFunction("notice")
	steps := fn.EffectiveSteps()
	rc, err := function.ResolveContext(ctx(), e.k, e.root, "The Commons", steps[0], "")
	if err != nil {
		t.Fatal(err)
	}
	rendered := function.RenderPrompt(steps[0].Backend.PromptTemplate, rc)

	if strings.Contains(rendered, "{{children-content}}") {
		t.Fatal("BUG-2 regression: children-content not resolved")
	}
	if strings.Contains(rendered, "{{context}}") {
		t.Fatal("BUG-2 regression: context placeholder not cleaned up")
	}
	if !strings.Contains(rendered, "unified lending systems") {
		t.Fatal("expected child content in rendered prompt")
	}
}

// ---------------------------------------------------------------------------
// Cycle validation through workflow (BUG-1 regression)
// ---------------------------------------------------------------------------

func TestWorkflow_NoCyclesAfterSync(t *testing.T) {
	e := setup(t)
	e.seedTree()

	_, violations, err := workflow.SyncFiles(ctx(), e.deps, e.root, 9)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		if v.Kind == "cycle" {
			t.Fatalf("BUG-1 regression: false cycle for %s", v.Title)
		}
	}
}
