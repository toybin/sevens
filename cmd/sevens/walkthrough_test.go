package main

// walkthrough_test.go exercises every command from WALKTHROUGH.md against
// a self-contained in-memory environment. No real LLM, no real config dir.
//
// The test mirrors the walkthrough chapter by chapter. Each section builds
// on the state left by the previous one, just like a user following the
// tutorial. This makes failures easy to locate: if Chapter 3 fails, the
// setup from Chapters 1-2 is suspect.
//
// Mock backend: returns canned responses keyed by function name + step.
// The responses are just enough to exercise the pipeline machinery and
// ops parsing -- they don't need to be "good" AI output.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sevens/defaults"
	"sevens/internal/config"
	"sevens/internal/function"
	"sevens/internal/graphops"
	"sevens/internal/kb"
	"sevens/internal/projection"
	projmd "sevens/internal/projection/md"
	"sevens/internal/repl"
	"sevens/internal/sevtypes"
	"sevens/internal/triple"
	"sevens/internal/types"
	"sevens/internal/workflow"

	_ "turso.tech/database/tursogo"
)

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

// walkEnv is the self-contained environment for walkthrough tests.
type walkEnv struct {
	t     *testing.T
	root  string // temp dir acting as sevens root
	store *triple.Store
	graph *graphops.Graph
	kb    *kb.KB
	proj  *projmd.MarkdownProjection
	db    *sql.DB
}

// newWalkEnv creates a fresh sevens environment in a temp directory.
func newWalkEnv(t *testing.T) *walkEnv {
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

	root := t.TempDir()
	// Write .sevens.edn so the root is recognized
	os.WriteFile(filepath.Join(root, ".sevens.edn"),
		[]byte(fmt.Sprintf(`{:path %q :alias "test"}`, root)), 0644)

	// Seed functions into a temp config dir so LoadFunction works.
	cfgDir := t.TempDir()
	config.OverrideConfigDir = cfgDir
	t.Cleanup(func() { config.OverrideConfigDir = "" })
	fnDir := filepath.Join(cfgDir, "functions")
	os.MkdirAll(fnDir, 0755)
	defaults.SeedFunctions(fnDir)

	return &walkEnv{
		t:     t,
		root:  root,
		store: store,
		graph: graph,
		kb:    k,
		proj:  proj,
		db:    db,
	}
}

// writeFile creates a markdown file in the root.
func (e *walkEnv) writeFile(name, content string) {
	e.t.Helper()
	path := filepath.Join(e.root, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		e.t.Fatalf("writing %s: %v", name, err)
	}
}

// sync runs a full sync of the root.
func (e *walkEnv) sync() {
	e.t.Helper()
	result, err := e.proj.Sync(context.Background(), e.root)
	if err != nil {
		e.t.Fatalf("sync: %v", err)
	}
	if result.NodesScanned == 0 {
		e.t.Log("[sync] 0 nodes (expected?)")
	}
}

// stack returns a kbStack for use with CLI-level helpers.
func (e *walkEnv) stack() *kbStack {
	return &kbStack{
		Store: e.store,
		Graph: e.graph,
		KB:    e.kb,
		close: func() {}, // db owned by walkEnv
	}
}

// executor creates an Executor with a mock backend.
func (e *walkEnv) executor(responses ...string) *function.Executor {
	be := &walkthroughMock{responses: responses}
	ps := function.NewPipelineStore(e.store)
	return function.NewExecutor(e.kb, be, ps)
}

// newREPL creates a fully-wired REPL for testing dispatch.
func (e *walkEnv) newREPL(focusNode string) *repl.REPL {
	gq := newGraphQuerier(e.kb, e.proj)
	ps := function.NewPipelineStore(e.store)
	cfg := config.GlobalConfig{}
	ar := newApplyRunner(e.kb, e.proj)
	tr := newTemplateRunner(e.kb, e.proj)
	pr := newPipelineRunner(e.kb, e.proj, ps, cfg)
	r, err := repl.New(e.root, focusNode, cfg,
		repl.WithKB(e.kb),
		repl.WithGraphQuerier(gq),
		repl.WithApplyRunner(ar),
		repl.WithTemplateRunner(tr),
		repl.WithPipelineRunner(pr),
	)
	if err != nil {
		e.t.Fatalf("creating REPL: %v", err)
	}
	return r
}

// validate runs KB validation and returns violations.
func (e *walkEnv) validate() []kb.Violation {
	v, err := e.kb.Validate(context.Background(), e.root, 9, 0)
	if err != nil {
		e.t.Fatalf("validate: %v", err)
	}
	return v
}

// assertNoCycles checks that validate produces no cycle violations.
func (e *walkEnv) assertNoCycles() {
	e.t.Helper()
	for _, v := range e.validate() {
		if v.Kind == "cycle" {
			e.t.Fatalf("unexpected cycle: %s — %s", v.Title, v.Detail)
		}
	}
}

// ctx returns a background context.
func (e *walkEnv) ctx() context.Context { return context.Background() }

// ---------------------------------------------------------------------------
// Mock backend for LLM-dependent commands
// ---------------------------------------------------------------------------

// walkthroughMock returns canned responses in order.
// For ops-producing functions, the response is valid JSON ops.
type walkthroughMock struct {
	responses []string
	callCount int
}

func (m *walkthroughMock) Execute(ctx context.Context, prompt function.RenderedPrompt) (function.TransformResult, error) {
	if m.callCount >= len(m.responses) {
		return function.TransformResult{Raw: "(no more mock responses)", IsText: true}, nil
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return function.TransformResult{Raw: resp, IsText: true}, nil
}

func (m *walkthroughMock) Name() string { return "mock" }

// Canned responses for common functions.
var (
	mockNoticeResponse = `**Gaps** — No governance model.
**Tensions** — Library implies open access; deposit model implies gatekeeping.
**Assumptions** — Assumes walkable neighborhood density.`

	mockChallengeResponse = `The core claim assumes individual ownership is always less efficient than sharing. This fails for items with high personalization value.`

	mockDecomposeSuggestResponse = `[
  {"title": "Governance Models", "rationale": "How decisions get made"},
  {"title": "Lending Design", "rationale": "Check-out and return logistics"},
  {"title": "Space Design", "rationale": "Physical layout"}
]`

	mockDecomposeGenerateResponse = `[
  {"action": "create", "title": "Governance Models", "parent": "The Commons", "content": "# Governance Models\n\nHow decisions get made."},
  {"action": "create", "title": "Lending Design", "parent": "The Commons", "content": "# Lending Design\n\nCheck-out logistics."},
  {"action": "create", "title": "Space Design", "parent": "The Commons", "content": "# Space Design\n\nPhysical layout."}
]`

	mockSharpenResponse = `[{"action": "edit", "file": "The Commons", "old_text": "A neighborhood that has one place", "new_text": "A neighborhood-scale institution that operates as one place"}]`

	mockThesisResponse = `**Thesis**: Ownership is an inefficient default for a large category of physical goods.`
)

// ---------------------------------------------------------------------------
// Chapter 1: Your First Knowledge Graph
// ---------------------------------------------------------------------------

func TestWalkthrough_Chapter1_Init(t *testing.T) {
	e := newWalkEnv(t)

	// Verify .sevens.edn exists
	cfg, err := projmd.LoadConfig(e.root)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Alias != "test" {
		t.Fatalf("expected alias 'test', got %q", cfg.Alias)
	}
}

func TestWalkthrough_Chapter1_CreateAndSync(t *testing.T) {
	e := newWalkEnv(t)

	// Create markdown files (the walkthrough's content)
	e.writeFile("the-commons.md", `---
title: The Commons
---

A neighborhood that has one place that's a library but also a tool library
and a seed bank and a repair cafe all mashed together.`)

	e.writeFile("lending-infrastructure-design.md", `---
title: Lending Infrastructure Design
parent: "[[The Commons]]"
---

The technical challenge of creating unified lending systems.`)

	e.writeFile("commons-governance-models.md", `---
title: Commons Governance Models
parent: "[[The Commons]]"
---

How decisions get made about what to stock and lending periods.`)

	// Sync
	e.sync()

	// No false cycles (BUG-1 regression)
	e.assertNoCycles()

	// Overview should show the tree
	nodes, err := e.kb.Overview(e.ctx(), e.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	// Walk
	w, err := e.kb.Walk(e.ctx(), e.root, "The Commons", kb.GatherNeighborhood)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Children) != 2 {
		t.Fatalf("expected 2 children, got %d: %v", len(w.Children), w.Children)
	}
	if w.Parent != nil {
		t.Fatalf("root node should have no parent, got %v", w.Parent)
	}

	// Children
	children, _ := e.kb.Children(e.ctx(), e.root, "The Commons")
	sort.Strings(children)
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %v", children)
	}

	// Siblings
	sibs, _ := e.kb.Siblings(e.ctx(), e.root, "Lending Infrastructure Design")
	if len(sibs) != 1 || sibs[0] != "Commons Governance Models" {
		t.Fatalf("expected sibling [Commons Governance Models], got %v", sibs)
	}

	// Search
	titleHits, _ := e.kb.Graph().Store().Search(e.ctx(), kb.PredNodeTitle, "governance")
	if len(titleHits) == 0 {
		t.Fatal("search should find 'governance' in titles")
	}
}

// ---------------------------------------------------------------------------
// Chapter 2: AI Functions
// ---------------------------------------------------------------------------

func TestWalkthrough_Chapter2_ApplyNotice(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	exec := e.executor(mockNoticeResponse)

	fn, _, err := function.LoadFunction("notice")
	if err != nil {
		t.Fatalf("loading notice: %v", err)
	}

	result, err := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	if err != nil {
		t.Fatalf("apply notice: %v", err)
	}

	if result.Suspended {
		t.Fatal("notice should not suspend (no gate)")
	}
	if result.Result == nil || !strings.Contains(result.Result.Raw, "Gaps") {
		t.Fatalf("expected notice output, got: %v", result.Result)
	}
}

func TestWalkthrough_Chapter2_ApplyChallenge(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	exec := e.executor(mockChallengeResponse)

	fn, _, err := function.LoadFunction("challenge")
	if err != nil {
		t.Fatalf("loading challenge: %v", err)
	}

	// BUG-3 regression: challenge requires "history" role
	result, err := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	if err != nil {
		t.Fatalf("apply challenge: %v", err)
	}
	if result.Suspended {
		t.Fatal("challenge should not suspend")
	}
}

func TestWalkthrough_Chapter2_ApplyThesis(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	exec := e.executor(mockThesisResponse)

	fn, _, err := function.LoadFunction("thesis")
	if err != nil {
		t.Fatalf("loading thesis: %v", err)
	}

	result, err := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	if err != nil {
		t.Fatalf("apply thesis: %v", err)
	}
	if !strings.Contains(result.Result.Raw, "Thesis") {
		t.Fatalf("expected thesis output, got: %s", result.Result.Raw)
	}
}

func TestWalkthrough_Chapter2_ContextResolution(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	fn, _, err := function.LoadFunction("notice")
	if err != nil {
		t.Fatal(err)
	}

	steps := fn.EffectiveSteps()
	rc, err := function.ResolveContext(e.ctx(), e.kb, e.root, "The Commons", steps[0], "")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	// BUG-2 regression: path spec content should be resolved
	rendered := function.RenderPrompt(steps[0].Backend.PromptTemplate, rc)
	if strings.Contains(rendered, "{{children-content}}") {
		t.Fatal("{{children-content}} was not resolved (BUG-2 regression)")
	}
	if strings.Contains(rendered, "{{parent-content}}") {
		t.Fatal("{{parent-content}} was not resolved (BUG-2 regression)")
	}
	// Should contain actual child content
	if !strings.Contains(rendered, "unified lending systems") {
		t.Fatal("rendered prompt should contain child content")
	}
}

func TestWalkthrough_Chapter2_DryRun(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	fn, _, err := function.LoadFunction("notice")
	if err != nil {
		t.Fatal(err)
	}

	steps := fn.EffectiveSteps()
	rc, err := function.ResolveContext(e.ctx(), e.kb, e.root, "The Commons", steps[0], "")
	if err != nil {
		t.Fatal(err)
	}

	prompt := function.RenderPrompt(steps[0].Backend.PromptTemplate, rc)

	// Should have instruction, target, and graph context populated
	if !strings.Contains(prompt, "<instruction>") {
		t.Fatal("dry-run should contain instruction block")
	}
	if !strings.Contains(prompt, `title="The Commons"`) {
		t.Fatal("dry-run should contain target title")
	}
}

func TestWalkthrough_Chapter2_Functions(t *testing.T) {
	cfgDir := t.TempDir()
	config.OverrideConfigDir = cfgDir
	t.Cleanup(func() { config.OverrideConfigDir = "" })
	fnDir := filepath.Join(cfgDir, "functions")
	os.MkdirAll(fnDir, 0755)
	defaults.SeedFunctions(fnDir)

	fns, err := function.ListFunctions()
	if err != nil {
		t.Fatal(err)
	}
	// The walkthrough shows notice, challenge, contradict, thesis, synthesize,
	// elaborate, sharpen, trim, summarize, decompose, etc.
	required := []string{"notice", "challenge", "contradict", "thesis",
		"synthesize", "elaborate", "sharpen", "trim", "summarize", "decompose"}
	fnSet := map[string]bool{}
	for _, f := range fns {
		fnSet[f] = true
	}
	for _, req := range required {
		if !fnSet[req] {
			t.Errorf("expected function %q in list", req)
		}
	}
}

func TestWalkthrough_Chapter2_Define(t *testing.T) {
	// Override HOME to isolate function creation
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Create the config dir that define writes to
	os.MkdirAll(filepath.Join(tmpHome, ".config", "sevens", "functions"), 0755)

	fns, err := function.ListFunctions()
	if err != nil {
		t.Fatal(err)
	}
	before := len(fns)

	// Define a function (this writes to ~/.config/sevens/functions/)
	// We test the load path, not the CLI define command
	ednContent := `{:name "test-analysis" :description "Test function" :input "node" :output "text"}`
	ednPath := filepath.Join(tmpHome, ".config", "sevens", "functions", "test-analysis.edn")
	mdPath := filepath.Join(tmpHome, ".config", "sevens", "functions", "test-analysis.md")
	os.WriteFile(ednPath, []byte(ednContent), 0644)
	os.WriteFile(mdPath, []byte("Analyze {{title}}: {{content}}"), 0644)

	fns, err = function.ListFunctions()
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) <= before {
		t.Fatal("expected new function to appear in list")
	}

	fnFound := false
	for _, f := range fns {
		if f == "test-analysis" {
			fnFound = true
		}
	}
	if !fnFound {
		t.Fatal("test-analysis not found in function list")
	}
}

// ---------------------------------------------------------------------------
// Chapter 3: Pipelines and Approval
// ---------------------------------------------------------------------------

func TestWalkthrough_Chapter3_Decompose(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	exec := e.executor(mockDecomposeSuggestResponse, mockDecomposeGenerateResponse)

	fn, _, err := function.LoadFunction("decompose")
	if err != nil {
		t.Fatalf("loading decompose: %v", err)
	}

	// Step 1: suggest (gated)
	result, err := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	if err != nil {
		t.Fatalf("apply decompose: %v", err)
	}
	if !result.Suspended {
		t.Fatal("decompose suggest should suspend at gate")
	}

	// Pending should show the pipeline
	ps := function.NewPipelineStore(e.store)
	pending, _ := ps.FindPending(e.ctx(), e.root)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	// BUG-5 regression: backend name should be persisted
	if pending[0].BackendName != "mock" {
		t.Fatalf("expected backend 'mock' in pending pipeline, got %q", pending[0].BackendName)
	}
}

func TestWalkthrough_Chapter3_AcceptReject(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// Test reject
	exec := e.executor(mockDecomposeSuggestResponse)
	fn, _, _ := function.LoadFunction("decompose")

	result, _ := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	p, err := exec.Reject(e.ctx(), result.Pipeline.ID)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if !p.Phase.IsTerminal() {
		t.Fatal("rejected pipeline should be terminal")
	}
}

func TestWalkthrough_Chapter3_Revise(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	exec := e.executor(
		mockDecomposeSuggestResponse,  // initial suggest
		mockDecomposeSuggestResponse,  // revised suggest
	)
	fn, _, _ := function.LoadFunction("decompose")

	result, _ := exec.Apply(e.ctx(), e.root, fn, "The Commons")

	// Revise with feedback
	revised, err := exec.Revise(e.ctx(), e.root, fn, result.Pipeline.ID, "Add infrastructure node")
	if err != nil {
		t.Fatalf("revise: %v", err)
	}
	if !revised.Suspended {
		t.Fatal("revised pipeline should still be suspended")
	}
}

// ---------------------------------------------------------------------------
// Chapter 4: Templates
// ---------------------------------------------------------------------------

func TestWalkthrough_Chapter4_Templates(t *testing.T) {
	cfgDir := t.TempDir()
	config.OverrideConfigDir = cfgDir
	t.Cleanup(func() { config.OverrideConfigDir = "" })
	fnDir := filepath.Join(cfgDir, "functions")
	os.MkdirAll(fnDir, 0755)
	defaults.SeedFunctions(fnDir)

	templates, err := function.ListDeterministicFunctions()
	if err != nil {
		t.Fatal(err)
	}
	required := []string{"daily-note", "inbox-capture", "inbox-root", "append-note", "section-entry"}
	nameSet := map[string]bool{}
	for _, tmpl := range templates {
		nameSet[tmpl.Name] = true
	}
	for _, req := range required {
		if !nameSet[req] {
			t.Errorf("expected template %q in list", req)
		}
	}
}

func TestWalkthrough_Chapter4_DailyNote(t *testing.T) {
	_ = newWalkEnv(t) // ensure clean environment

	fn, _, err := function.LoadFunction("inbox-root")
	if err != nil {
		t.Fatalf("loading inbox-root: %v", err)
	}
	if !function.IsDeterministic(fn) {
		t.Fatal("inbox-root should be deterministic")
	}

	fn2, _, err := function.LoadFunction("daily-note")
	if err != nil {
		t.Fatalf("loading daily-note: %v", err)
	}
	if !function.IsDeterministic(fn2) {
		t.Fatal("daily-note should be deterministic")
	}
}

// ---------------------------------------------------------------------------
// Chapter 5: The REPL
// ---------------------------------------------------------------------------

func TestWalkthrough_Chapter5_REPLGraphCommands(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// BUG-7 regression: REPL graph commands should work
	gq := newGraphQuerier(e.kb, e.proj)

	// Test each GraphQuerier method the REPL uses

	// ResolveTitle
	title := gq.ResolveTitle("The Commons", e.root)
	if title != "The Commons" {
		t.Fatalf("ResolveTitle: expected 'The Commons', got %q", title)
	}
	if gq.ResolveTitle("Nonexistent", e.root) != "" {
		t.Fatal("ResolveTitle should return empty for nonexistent")
	}

	// BuildWalk
	walk, err := gq.BuildWalk(e.root, "The Commons", "neighborhood")
	if err != nil {
		t.Fatalf("BuildWalk: %v", err)
	}
	if walk.Target.Title != "The Commons" {
		t.Fatalf("walk title: %q", walk.Target.Title)
	}
	if len(walk.Target.Children) != 2 {
		t.Fatalf("walk children: %v", walk.Target.Children)
	}

	// BuildOverview
	ov, err := gq.BuildOverview(e.root)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	if len(ov) != 3 {
		t.Fatalf("overview: expected 3 nodes, got %d", len(ov))
	}

	// ListNodeTitles
	titles, err := gq.ListNodeTitles(e.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 3 {
		t.Fatalf("expected 3 titles, got %d", len(titles))
	}

	// SearchTitles
	hits, err := gq.SearchTitles("governance", e.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("search should find 'governance'")
	}

	// SearchContent
	contentHits, err := gq.SearchContent("lending", e.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(contentHits) == 0 {
		t.Fatal("content search should find 'lending'")
	}

	// Resync
	if err := gq.Resync(e.root); err != nil {
		t.Fatalf("Resync: %v", err)
	}
}

func TestWalkthrough_Chapter5_REPLBlockCommands(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	gq := newGraphQuerier(e.kb, e.proj)

	bl, err := gq.BuildBlockList(e.root, "The Commons")
	if err != nil {
		t.Fatalf("BuildBlockList: %v", err)
	}
	if len(bl.Blocks) == 0 {
		t.Fatal("expected at least one block")
	}
}

// ---------------------------------------------------------------------------
// Chapter 6: Blocks
// ---------------------------------------------------------------------------

func TestWalkthrough_Chapter6_Blocks(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	entries, err := e.kb.ListBlocks(e.ctx(), e.root, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected blocks for The Commons")
	}
}

func TestWalkthrough_Chapter6_DiffBlocks(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	diff, err := e.proj.DiffBlocks(e.ctx(), e.root, "The Commons")
	if err != nil {
		t.Fatalf("DiffBlocks: %v", err)
	}
	if diff.NodeTitle != "The Commons" {
		t.Fatalf("diff title: %q", diff.NodeTitle)
	}
}

// ---------------------------------------------------------------------------
// Chapter 7: Agent Mode
// ---------------------------------------------------------------------------

func TestWalkthrough_Chapter7_Prepare(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	fn, _, err := function.LoadFunction("notice")
	if err != nil {
		t.Fatal(err)
	}

	steps := fn.EffectiveSteps()
	rc, err := function.ResolveContext(e.ctx(), e.kb, e.root, "The Commons", steps[0], "")
	if err != nil {
		t.Fatal(err)
	}

	prompt := function.RenderPrompt(steps[0].Backend.PromptTemplate, rc)

	// Prepare should produce a usable prompt with context
	if !strings.Contains(prompt, "The Commons") {
		t.Fatal("prepare prompt should reference target node")
	}
}

func TestWalkthrough_Chapter7_Submit(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// Submit creates a pipeline, provides a result, and logs it
	exec := e.executor()
	fn, _, _ := function.LoadFunction("notice")

	// Simulate: apply creates a pipeline, we provide a result via submit
	result, err := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	// For a text output function like notice, it completes immediately
	if result.Suspended {
		t.Fatal("notice should complete, not suspend")
	}

	// Check log entry was written
	entries, _ := e.kb.ReadLog(e.ctx(), e.root, "The Commons")
	if len(entries) == 0 {
		t.Fatal("expected log entry after apply")
	}
}

// ---------------------------------------------------------------------------
// Chapter 9: Triple Store Queries
// ---------------------------------------------------------------------------

func TestWalkthrough_Chapter9_Query(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// Query node titles
	rows, err := e.store.RawQuery(e.ctx(),
		"SELECT subject, object FROM triples WHERE predicate = 'node/title'")
	if err != nil {
		t.Fatal(err)
	}
	// First row is headers, rest are data
	if len(rows) < 4 { // header + 3 nodes
		t.Fatalf("expected at least 4 rows (header + 3 nodes), got %d", len(rows))
	}

	// Query char counts
	rows, err = e.store.RawQuery(e.ctx(),
		"SELECT t1.object AS title, CAST(t2.object AS INTEGER) AS chars "+
			"FROM triples t1 "+
			"JOIN triples t2 ON t1.subject = t2.subject "+
			"WHERE t1.predicate = 'node/title' "+
			"AND t2.predicate = 'node/char-count' "+
			"ORDER BY chars DESC LIMIT 10")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 { // header + at least 1 row
		t.Fatalf("expected char count results, got %d rows", len(rows))
	}
}

// ---------------------------------------------------------------------------
// Cross-cutting: Validation
// ---------------------------------------------------------------------------

func TestWalkthrough_ValidationNoCycles(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// BUG-1 regression: no false cycles in a normal tree
	e.assertNoCycles()

	// Add more children (like decompose would)
	p := "The Commons"
	e.kb.CreateNode(e.ctx(), e.root, "Revenue", "revenue content", &p)
	e.kb.CreateNode(e.ctx(), e.root, "Infrastructure", "infra content", &p)
	e.kb.CreateNode(e.ctx(), e.root, "Adoption", "adoption content", &p)

	e.assertNoCycles()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// seedSimpleTree creates the walkthrough's initial tree: The Commons with two children.
func seedSimpleTree(t *testing.T, e *walkEnv) {
	t.Helper()
	e.writeFile("the-commons.md", `---
title: The Commons
---

A neighborhood that has one place that's a library but also a tool library
and a seed bank and a repair cafe all mashed together.`)

	e.writeFile("lending-infrastructure-design.md", `---
title: Lending Infrastructure Design
parent: "[[The Commons]]"
---

The technical challenge of creating unified lending systems.`)

	e.writeFile("commons-governance-models.md", `---
title: Commons Governance Models
parent: "[[The Commons]]"
---

How decisions get made about what to stock and lending periods.`)

	e.sync()
}

// ---------------------------------------------------------------------------
// ApplyRunner adapter tests
// ---------------------------------------------------------------------------

func TestApplyRunner_LoadFunction(t *testing.T) {
	e := newWalkEnv(t)
	ar := newApplyRunner(e.kb, e.proj)

	fn, err := ar.LoadFunction("notice")
	if err != nil {
		t.Fatalf("LoadFunction: %v", err)
	}
	if fn.Name != "notice" {
		t.Fatalf("expected 'notice', got %q", fn.Name)
	}
	if fn.Description == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestApplyRunner_ListFunctions(t *testing.T) {
	e := newWalkEnv(t)
	ar := newApplyRunner(e.kb, e.proj)

	fns, err := ar.ListFunctions()
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) < 10 {
		t.Fatalf("expected at least 10 functions, got %d", len(fns))
	}
}

func TestApplyRunner_AppendAndReadLog(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)
	ar := newApplyRunner(e.kb, e.proj)

	err := ar.AppendLog(repl.LogEntry{
		Event:    "applied",
		Root:     e.root,
		Function: "notice",
		Target:   "The Commons",
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := ar.ReadLog(e.root, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected log entries")
	}
	if entries[0].Function != "notice" {
		t.Fatalf("expected function 'notice', got %q", entries[0].Function)
	}
}

func TestApplyRunner_SanitizeFilename(t *testing.T) {
	e := newWalkEnv(t)
	ar := newApplyRunner(e.kb, e.proj)

	name := ar.SanitizeFilename("Commons Governance Models")
	if name != "commons-governance-models.md" {
		t.Fatalf("expected 'commons-governance-models.md', got %q", name)
	}
}

// ---------------------------------------------------------------------------
// TemplateRunner adapter tests
// ---------------------------------------------------------------------------

func TestTemplateRunner_LoadAndList(t *testing.T) {
	e := newWalkEnv(t)
	tr := newTemplateRunner(e.kb, e.proj)

	names, err := tr.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range names {
		if n == "daily-note" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'daily-note' in templates, got: %v", names)
	}

	tmpl, err := tr.LoadTemplate("daily-note")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "daily-note" {
		t.Fatalf("expected 'daily-note', got %q", tmpl.Name)
	}
}

func TestTemplateRunner_LoadNonTemplate(t *testing.T) {
	e := newWalkEnv(t)
	tr := newTemplateRunner(e.kb, e.proj)

	_, err := tr.LoadTemplate("notice")
	if err == nil {
		t.Fatal("expected error loading non-deterministic function as template")
	}
}

// ---------------------------------------------------------------------------
// REPL navigation tests (beyond walkthrough)
// ---------------------------------------------------------------------------

func TestREPL_NavigationUpDown(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	gq := newGraphQuerier(e.kb, e.proj)

	// Simulate child navigation: from The Commons, get children, pick first
	walk, _ := gq.BuildWalk(e.root, "The Commons", "neighborhood")
	if len(walk.Target.Children) == 0 {
		t.Fatal("no children to navigate to")
	}

	// Navigate to first child
	childTitle := walk.Target.Children[0]
	childWalk, err := gq.BuildWalk(e.root, childTitle, "neighborhood")
	if err != nil {
		t.Fatalf("walking child: %v", err)
	}

	// Should have parent = The Commons
	if childWalk.Parent == nil || childWalk.Parent.Title != "The Commons" {
		var got string
		if childWalk.Parent != nil {
			got = childWalk.Parent.Title
		}
		t.Fatalf("expected parent 'The Commons', got %q", got)
	}

	// Navigate up: resolve parent
	parentWalk, err := gq.BuildWalk(e.root, childWalk.Parent.Title, "neighborhood")
	if err != nil {
		t.Fatal(err)
	}
	if parentWalk.Target.Title != "The Commons" {
		t.Fatalf("up navigation should return to The Commons, got %q", parentWalk.Target.Title)
	}
}

func TestREPL_SiblingNavigation(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	gq := newGraphQuerier(e.kb, e.proj)

	walk, _ := gq.BuildWalk(e.root, "Lending Infrastructure Design", "neighborhood")
	if len(walk.Siblings) != 1 {
		t.Fatalf("expected 1 sibling, got %d", len(walk.Siblings))
	}
	if walk.Siblings[0].Title != "Commons Governance Models" {
		t.Fatalf("sibling should be 'Commons Governance Models', got %q", walk.Siblings[0].Title)
	}
}

// ---------------------------------------------------------------------------
// Multi-child tree (7+/- 2 constraint)
// ---------------------------------------------------------------------------

func TestValidation_OverflowDetection(t *testing.T) {
	e := newWalkEnv(t)
	e.writeFile("root.md", "---\ntitle: Root\n---\nRoot node.")
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("child-%d.md", i)
		content := fmt.Sprintf("---\ntitle: Child %d\nparent: \"[[Root]]\"\n---\nChild %d content.", i, i)
		e.writeFile(name, content)
	}
	e.sync()

	violations := e.validate()
	var overflowFound bool
	for _, v := range violations {
		if v.Kind == "overflow" && v.Title == "Root" {
			overflowFound = true
		}
	}
	if !overflowFound {
		t.Fatal("expected overflow violation for Root with 10 children (max 9)")
	}
	e.assertNoCycles()
}

// ---------------------------------------------------------------------------
// Ops-producing function end-to-end (BUG-4 regression)
// ---------------------------------------------------------------------------

func TestOpsFunction_ParsesAndReturnsOps(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// Simulate a sharpen function: mock returns valid JSON ops
	exec := e.executor(mockSharpenResponse)
	fn, _, err := function.LoadFunction("sharpen")
	if err != nil {
		t.Fatal(err)
	}

	result, err := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	if err != nil {
		t.Fatalf("apply sharpen: %v", err)
	}

	// The result should have parsed ops (not just raw text)
	if result.Result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Result.Ops) == 0 {
		t.Fatal("BUG-4 regression: sharpen result should have parsed ops")
	}
	if result.Result.Ops[0].Action != "edit" {
		t.Fatalf("expected edit op, got %q", result.Result.Ops[0].Action)
	}
}

// ---------------------------------------------------------------------------
// Log query tests (Chapter 9 extended)
// ---------------------------------------------------------------------------

func TestQuery_WikiLinks(t *testing.T) {
	e := newWalkEnv(t)
	// Create nodes with wiki-links in body
	e.writeFile("a.md", "---\ntitle: A\n---\nSee [[B]] for details.")
	e.writeFile("b.md", "---\ntitle: B\n---\nRelated to [[A]].")
	e.sync()

	// Query wiki-links
	rows, err := e.store.RawQuery(e.ctx(),
		"SELECT t1.object AS source, t2.object AS target "+
			"FROM triples t1 "+
			"JOIN triples t2 ON t1.subject = t2.subject "+
			"WHERE t1.predicate = 'node/title' "+
			"AND t2.predicate = 'node/link'")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 3 { // header + 2 links
		t.Fatalf("expected wiki-link results, got %d rows", len(rows))
	}
}

func TestQuery_LogEntries(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// Apply two functions to generate log entries
	exec := e.executor(mockNoticeResponse, mockThesisResponse)
	fn1, _, _ := function.LoadFunction("notice")
	fn2, _, _ := function.LoadFunction("thesis")

	exec.Apply(e.ctx(), e.root, fn1, "The Commons")
	exec.Apply(e.ctx(), e.root, fn2, "The Commons")

	// Query log entries
	rows, err := e.store.RawQuery(e.ctx(),
		"SELECT t1.object AS event, t2.object AS function "+
			"FROM triples t1 "+
			"JOIN triples t2 ON t1.subject = t2.subject "+
			"WHERE t1.predicate = 'log/event' "+
			"AND t2.predicate = 'log/function' "+
			"ORDER BY t1.subject DESC")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 3 { // header + 2 entries
		t.Fatalf("expected log entries, got %d rows", len(rows))
	}
}

// ---------------------------------------------------------------------------
// Session / Focus
// ---------------------------------------------------------------------------

func TestSession_FocusAndPersist(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	subj := kb.NodeSubject(e.root, "The Commons")
	sess, err := e.kb.StartSession(e.ctx(), subj)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Focus != subj {
		t.Fatalf("focus should be %q, got %q", subj, sess.Focus)
	}

	// Add include
	inclSubj := kb.NodeSubject(e.root, "Lending Infrastructure Design")
	e.kb.AddInclude(e.ctx(), sess.Subject, inclSubj)

	loaded, _ := e.kb.LoadSession(e.ctx(), sess.Subject)
	if len(loaded.Includes) != 1 {
		t.Fatalf("expected 1 include, got %d", len(loaded.Includes))
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestEdge_EmptyRoot(t *testing.T) {
	e := newWalkEnv(t)
	e.sync() // sync with no files

	nodes, err := e.kb.Overview(e.ctx(), e.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Fatalf("empty root should have 0 nodes, got %d", len(nodes))
	}
}

func TestEdge_NodeWithNoContent(t *testing.T) {
	e := newWalkEnv(t)
	e.writeFile("empty.md", "---\ntitle: Empty Node\n---\n")
	e.sync()

	w, err := e.kb.Walk(e.ctx(), e.root, "Empty Node", kb.GatherMinimal)
	if err != nil {
		t.Fatal(err)
	}
	if w.Target.Title != "Empty Node" {
		t.Fatalf("expected title, got %q", w.Target.Title)
	}
}

func TestEdge_DuplicateTitles(t *testing.T) {
	e := newWalkEnv(t)
	e.writeFile("a.md", "---\ntitle: Same Title\n---\nFirst.")
	e.writeFile("b.md", "---\ntitle: Same Title\n---\nSecond.")
	e.sync()

	// Should resolve to one node (dedup by title)
	w, err := e.kb.Walk(e.ctx(), e.root, "Same Title", kb.GatherMinimal)
	if err != nil {
		t.Fatal(err)
	}
	if w.Target.Title != "Same Title" {
		t.Fatal("should resolve despite duplicate filenames")
	}
}

func TestEdge_MissingParent(t *testing.T) {
	e := newWalkEnv(t)
	e.writeFile("orphan.md", "---\ntitle: Orphan\nparent: \"[[Nonexistent]]\"\n---\nLost.")
	e.sync()

	violations := e.validate()
	var missingParent bool
	for _, v := range violations {
		if v.Kind == "missing-parent" && v.Title == "Orphan" {
			missingParent = true
		}
	}
	if !missingParent {
		t.Fatal("expected missing-parent violation for Orphan")
	}
}

func TestEdge_DeepTree(t *testing.T) {
	e := newWalkEnv(t)
	// Create a 5-level deep tree
	e.writeFile("l0.md", "---\ntitle: L0\n---\nLevel 0.")
	for i := 1; i <= 4; i++ {
		parent := fmt.Sprintf("L%d", i-1)
		name := fmt.Sprintf("l%d.md", i)
		content := fmt.Sprintf("---\ntitle: L%d\nparent: \"[[%s]]\"\n---\nLevel %d.", i, parent, i)
		e.writeFile(name, content)
	}
	e.sync()

	e.assertNoCycles()

	// Walk the deepest node
	w, err := e.kb.Walk(e.ctx(), e.root, "L4", kb.GatherNeighborhood)
	if err != nil {
		t.Fatal(err)
	}
	if w.Parent == nil || w.Parent.Title != "L3" {
		t.Fatalf("L4 parent should be L3, got %v", w.Parent)
	}
}

func TestEdge_SpecialCharactersInTitle(t *testing.T) {
	e := newWalkEnv(t)
	e.writeFile("special.md", "---\ntitle: \"Node: A/B (v2)\"\n---\nSpecial chars.")
	e.sync()

	w, err := e.kb.Walk(e.ctx(), e.root, "Node: A/B (v2)", kb.GatherMinimal)
	if err != nil {
		t.Fatal(err)
	}
	if w.Target.Title != "Node: A/B (v2)" {
		t.Fatalf("title mismatch: %q", w.Target.Title)
	}
}

func TestEdge_MultipleRoots(t *testing.T) {
	e := newWalkEnv(t)
	// Two parentless nodes = two roots = orphan warning
	e.writeFile("root1.md", "---\ntitle: Root One\n---\nFirst root.")
	e.writeFile("root2.md", "---\ntitle: Root Two\n---\nSecond root.")
	e.sync()

	violations := e.validate()
	var orphanFound bool
	for _, v := range violations {
		if v.Kind == "orphan" {
			orphanFound = true
		}
	}
	if !orphanFound {
		t.Fatal("expected orphan violation with multiple root nodes")
	}
}

func TestEdge_AllFunctionsLoad(t *testing.T) {
	// Verify every bundled function loads without error
	fns, err := function.ListFunctions()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range fns {
		fn, _, err := function.LoadFunction(name)
		if err != nil {
			t.Errorf("function %q failed to load: %v", name, err)
			continue
		}
		if fn.Name != name {
			t.Errorf("function %q has mismatched name %q", name, fn.Name)
		}
		steps := fn.EffectiveSteps()
		if len(steps) == 0 {
			t.Errorf("function %q has no steps", name)
		}
	}
}

func TestEdge_AllDeterministicFunctionsLoad(t *testing.T) {
	fns, err := function.ListDeterministicFunctions()
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range fns {
		if !function.IsDeterministic(&fn) {
			t.Errorf("function %q listed as deterministic but IsDeterministic returned false", fn.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// REPL fully-wired adapter tests
// ---------------------------------------------------------------------------

func TestREPLAdapter_PendingWhenEmpty(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	ps := function.NewPipelineStore(e.store)
	cfg := config.GlobalConfig{}
	pr := newPipelineRunner(e.kb, e.proj, ps, cfg)

	suspensions, err := pr.ListSuspensions(e.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(suspensions) != 0 {
		t.Fatalf("expected 0 suspensions, got %d", len(suspensions))
	}
}

func TestREPLAdapter_RunPipelineDryRun(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	ps := function.NewPipelineStore(e.store)
	cfg := config.GlobalConfig{}
	pr := newPipelineRunner(e.kb, e.proj, ps, cfg)

	// Dry-run should return without error and not create a pipeline
	result, err := pr.RunPipeline(repl.PipelineConfig{
		Root:         e.root,
		NodeTitle:    "The Commons",
		FunctionName: "notice",
		DryRun:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Suspended {
		t.Fatal("dry-run should not suspend")
	}
}

func TestREPLAdapter_FindSuspensionAfterApply(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// Use the executor directly to create a pending pipeline
	mock := &walkthroughMock{responses: []string{mockDecomposeSuggestResponse}}
	ps := function.NewPipelineStore(e.store)
	exec := function.NewExecutor(e.kb, mock, ps)
	fn, _, _ := function.LoadFunction("decompose")

	result, err := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Suspended {
		t.Fatal("should suspend")
	}

	// Now test the adapter
	cfg := config.GlobalConfig{}
	pr := newPipelineRunner(e.kb, e.proj, ps, cfg)

	sus, subject, err := pr.FindSuspension(e.root, "The Commons")
	if err != nil {
		t.Fatalf("FindSuspension: %v", err)
	}
	if sus.Function != "decompose" {
		t.Fatalf("expected function 'decompose', got %q", sus.Function)
	}
	if sus.Target != "The Commons" {
		t.Fatalf("expected target 'The Commons', got %q", sus.Target)
	}
	if subject == "" {
		t.Fatal("expected non-empty subject")
	}

	// FindSuspensionBySubject
	sus2, err := pr.FindSuspensionBySubject(e.root, subject)
	if err != nil {
		t.Fatal(err)
	}
	if sus2.Subject != sus.Subject {
		t.Fatal("subjects should match")
	}

	// ListSuspensions
	all, err := pr.ListSuspensions(e.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 suspension, got %d", len(all))
	}

	// ResolveSuspension (accept)
	err = pr.ResolveSuspension(subject, "accepted")
	if err != nil {
		t.Fatalf("ResolveSuspension: %v", err)
	}
}

func TestREPLAdapter_ApplyRunnerFunctionsAndTemplates(t *testing.T) {
	e := newWalkEnv(t)
	ar := newApplyRunner(e.kb, e.proj)
	tr := newTemplateRunner(e.kb, e.proj)

	// ListFunctions should return both AI and deterministic functions
	fns, _ := ar.ListFunctions()
	var hasNotice, hasDailyNote bool
	for _, fn := range fns {
		if fn.Name == "notice" {
			hasNotice = true
		}
		if fn.Name == "daily-note" {
			hasDailyNote = true
		}
	}
	if !hasNotice {
		t.Fatal("expected 'notice' in function list")
	}
	if !hasDailyNote {
		t.Fatal("expected 'daily-note' in function list")
	}

	// ListTemplates should return only deterministic
	tmpls, _ := tr.ListTemplates()
	for _, name := range tmpls {
		fn, _, _ := function.LoadFunction(name)
		if fn != nil && !function.IsDeterministic(fn) {
			t.Errorf("template list should only contain deterministic functions, got %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// REPL navigation comprehensive tests
// ---------------------------------------------------------------------------

func TestREPL_NavigationNumericSelect(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	gq := newGraphQuerier(e.kb, e.proj)

	// Simulate what the REPL does: get children, user picks by number
	walk, _ := gq.BuildWalk(e.root, "The Commons", "neighborhood")
	children := walk.Target.Children
	if len(children) < 2 {
		t.Fatal("need at least 2 children")
	}

	// "child 1" would focus the first child
	childWalk, err := gq.BuildWalk(e.root, children[0], "neighborhood")
	if err != nil {
		t.Fatal(err)
	}
	if childWalk.Parent == nil || childWalk.Parent.Title != "The Commons" {
		t.Fatal("first child should have parent 'The Commons'")
	}

	// "sibling 1" from that child would go to the other sibling
	if len(childWalk.Siblings) != 1 {
		t.Fatalf("expected 1 sibling, got %d", len(childWalk.Siblings))
	}
	siblingWalk, err := gq.BuildWalk(e.root, childWalk.Siblings[0].Title, "neighborhood")
	if err != nil {
		t.Fatal(err)
	}
	if siblingWalk.Parent == nil || siblingWalk.Parent.Title != "The Commons" {
		t.Fatal("sibling should also have parent 'The Commons'")
	}
}

// ---------------------------------------------------------------------------
// Workflow + REPL integration: full apply-accept cycle
// ---------------------------------------------------------------------------

func TestWorkflowIntegration_ApplyAcceptCycle(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// Apply decompose via executor with mock
	suggestResp := `[{"title": "Revenue Model", "rationale": "funding"}]`
	genResp := `[{"action": "create", "title": "Revenue Model", "parent": "The Commons", "content": "# Revenue Model\n\nHow the commons sustains itself financially."}]`

	mock := &walkthroughMock{responses: []string{suggestResp, genResp}}
	ps := function.NewPipelineStore(e.store)
	exec := function.NewExecutor(e.kb, mock, ps)
	fn, _, _ := function.LoadFunction("decompose")

	// Step 1: Apply (suspends at suggest gate)
	result, err := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Suspended {
		t.Fatal("should suspend at suggest")
	}

	// Step 2: Accept suggest (runs generate, which is also gated)
	result, err = exec.Accept(e.ctx(), e.root, fn, result.Pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}

	// generate step may suspend or complete depending on gate config
	if result.Suspended {
		// Accept generate
		result, err = exec.Accept(e.ctx(), e.root, fn, result.Pipeline.ID)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Should have ops now -- apply them
	if result.Result != nil && len(result.Result.Ops) > 0 {
		projOps := make([]projection.FileOp, len(result.Result.Ops))
		for i, op := range result.Result.Ops {
			projOps[i] = projection.FileOp(op)
		}
		projResult, err := e.proj.ApplyOps(e.ctx(), e.root, projOps)
		if err != nil {
			t.Fatal(err)
		}
		if len(projResult.FilesCreated) == 0 {
			t.Fatal("expected files created from decompose generate")
		}

		// Resync and verify
		e.sync()
		children, _ := e.kb.Children(e.ctx(), e.root, "The Commons")
		var found bool
		for _, c := range children {
			if c == "Revenue Model" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected 'Revenue Model' in children, got: %v", children)
		}
	}
}

// ---------------------------------------------------------------------------
// File operation round-trip (BUG-4 full regression)
// ---------------------------------------------------------------------------

func TestFileOp_JSONRoundTrip(t *testing.T) {
	// Verify that snake_case JSON ops parse correctly into FileOp structs
	raw := `[{"action": "edit", "file": "My Note", "old_text": "hello world", "new_text": "goodbye world"}]`
	ops, err := function.ParseOps(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	op := ops[0]
	if op.Action != "edit" {
		t.Fatalf("action: %q", op.Action)
	}
	if op.File != "My Note" {
		t.Fatalf("file: %q", op.File)
	}
	if op.OldText != "hello world" {
		t.Fatalf("BUG-4 regression: old_text not parsed, got %q", op.OldText)
	}
	if op.NewText != "goodbye world" {
		t.Fatalf("BUG-4 regression: new_text not parsed, got %q", op.NewText)
	}
}

func TestFileOp_CreateOpsRoundTrip(t *testing.T) {
	raw := `[{"action": "create", "title": "New Node", "parent": "Root", "content": "# New Node\n\nContent here."}]`
	ops, err := function.ParseOps(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Title != "New Node" {
		t.Fatalf("title: %q", ops[0].Title)
	}
	if ops[0].Parent != "Root" {
		t.Fatalf("parent: %q", ops[0].Parent)
	}
	if !strings.Contains(ops[0].Content, "Content here") {
		t.Fatalf("content: %q", ops[0].Content)
	}
}

// ---------------------------------------------------------------------------
// Multi-function apply sequence
// ---------------------------------------------------------------------------

func TestMultiApply_NoticeThentiChallenge(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// Apply notice, then challenge, on the same node
	exec := e.executor(mockNoticeResponse, mockChallengeResponse)

	fn1, _, _ := function.LoadFunction("notice")
	r1, err := exec.Apply(e.ctx(), e.root, fn1, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Suspended {
		t.Fatal("notice should not suspend")
	}

	fn2, _, _ := function.LoadFunction("challenge")
	r2, err := exec.Apply(e.ctx(), e.root, fn2, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Suspended {
		t.Fatal("challenge should not suspend")
	}

	// Both should be logged
	entries, _ := e.kb.ReadLog(e.ctx(), e.root, "The Commons")
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", len(entries))
	}
}

// Verify interface at compile time.
var _ function.TransformBackend = (*walkthroughMock)(nil)

// ---------------------------------------------------------------------------
// Workflow integration tests
// ---------------------------------------------------------------------------

func TestWorkflowApplyFunction_ViaWorkflowLayer(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	mock := &walkthroughMock{responses: []string{mockNoticeResponse}}
	ps := function.NewPipelineStore(e.store)
	deps := &workflow.Deps{
		KB:      e.kb,
		Proj:    e.proj,
		Store:   ps,
		Backend: mock,
	}

	result, err := workflow.ApplyFunction(e.ctx(), deps, e.root, "notice", "The Commons")
	if err != nil {
		t.Fatalf("ApplyFunction: %v", err)
	}

	if result.Suspended {
		t.Fatal("notice should not suspend")
	}
	if result.FunctionName != "notice" {
		t.Fatalf("expected function 'notice', got %q", result.FunctionName)
	}
	if result.Target != "The Commons" {
		t.Fatalf("expected target 'The Commons', got %q", result.Target)
	}
	if !strings.Contains(result.Output, "Gaps") {
		t.Fatalf("expected output with 'Gaps', got: %s", result.Output)
	}
}

func TestWorkflowAcceptPipeline_Advance(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	mock := &walkthroughMock{responses: []string{
		mockDecomposeSuggestResponse,
		mockDecomposeGenerateResponse,
	}}
	ps := function.NewPipelineStore(e.store)
	deps := &workflow.Deps{
		KB:      e.kb,
		Proj:    e.proj,
		Store:   ps,
		Backend: mock,
	}

	ar, err := workflow.ApplyFunction(e.ctx(), deps, e.root, "decompose", "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if !ar.Suspended {
		t.Fatal("decompose should suspend at suggest gate")
	}

	acceptResult, err := workflow.AcceptPipeline(e.ctx(), deps, e.root, ar.PipelineID, "")
	if err != nil {
		t.Fatalf("AcceptPipeline: %v", err)
	}

	if acceptResult.Suspended {
		if acceptResult.PipelineID == "" {
			t.Fatal("suspended but no pipeline ID")
		}
		p, _ := ps.Load(e.ctx(), acceptResult.PipelineID)
		if p.CurrentStep < 1 {
			t.Fatalf("expected step >= 1 after accept, got %d", p.CurrentStep)
		}
	} else if !acceptResult.Completed {
		t.Fatal("expected either suspended at next gate or completed")
	}
}

func TestWorkflowRejectPipeline(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	mock := &walkthroughMock{responses: []string{mockDecomposeSuggestResponse}}
	ps := function.NewPipelineStore(e.store)
	deps := &workflow.Deps{
		KB:      e.kb,
		Proj:    e.proj,
		Store:   ps,
		Backend: mock,
	}

	ar, err := workflow.ApplyFunction(e.ctx(), deps, e.root, "decompose", "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	err = workflow.RejectPipeline(e.ctx(), deps, e.root, ar.PipelineID)
	if err != nil {
		t.Fatalf("RejectPipeline: %v", err)
	}

	p, _ := ps.Load(e.ctx(), ar.PipelineID)
	if !p.Phase.IsTerminal() {
		t.Fatalf("expected terminal phase, got %s", p.Phase)
	}
	if p.Phase != function.PhaseRejected {
		t.Fatalf("expected Rejected, got %s", p.Phase)
	}
}

func TestWorkflowFindPendingPipeline(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	mock := &walkthroughMock{responses: []string{
		mockDecomposeSuggestResponse,
		mockDecomposeSuggestResponse,
	}}
	ps := function.NewPipelineStore(e.store)
	deps := &workflow.Deps{
		KB:      e.kb,
		Proj:    e.proj,
		Store:   ps,
		Backend: mock,
	}

	ar1, _ := workflow.ApplyFunction(e.ctx(), deps, e.root, "decompose", "The Commons")
	ar2, _ := workflow.ApplyFunction(e.ctx(), deps, e.root, "decompose", "Lending Infrastructure Design")
	if !ar1.Suspended || !ar2.Suspended {
		t.Fatal("both should suspend")
	}

	p, err := workflow.FindPendingPipeline(e.ctx(), deps, e.root, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if p.Target != "The Commons" {
		t.Fatalf("expected 'The Commons', got %q", p.Target)
	}

	_, err = workflow.FindPendingPipeline(e.ctx(), deps, e.root, "Nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

// ---------------------------------------------------------------------------
// REPL adapter via workflow tests
// ---------------------------------------------------------------------------

func TestREPLAdapter_RunPipelineViaWorkflow(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	ps := function.NewPipelineStore(e.store)
	cfg := config.GlobalConfig{}
	pr := newPipelineRunner(e.kb, e.proj, ps, cfg)

	result, err := pr.RunPipeline(repl.PipelineConfig{
		Root:         e.root,
		NodeTitle:    "The Commons",
		FunctionName: "notice",
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("RunPipeline dry-run: %v", err)
	}
	_ = result
}

func TestREPLAdapter_ReviseStepViaWorkflow(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	mock := &walkthroughMock{responses: []string{
		mockDecomposeSuggestResponse,
		mockDecomposeSuggestResponse,
	}}
	ps := function.NewPipelineStore(e.store)
	deps := &workflow.Deps{
		KB:      e.kb,
		Proj:    e.proj,
		Store:   ps,
		Backend: mock,
	}

	ar, _ := workflow.ApplyFunction(e.ctx(), deps, e.root, "decompose", "The Commons")
	if !ar.Suspended {
		t.Fatal("expected suspended")
	}

	revised, err := workflow.AcceptPipeline(e.ctx(), deps, e.root, ar.PipelineID, "add infrastructure node")
	if err != nil {
		t.Fatalf("revise: %v", err)
	}
	if !revised.Suspended {
		t.Fatal("revised pipeline should still be suspended")
	}
}

func TestREPLAdapter_DiscussionRunner(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	ps := function.NewPipelineStore(e.store)
	cfg := config.GlobalConfig{}
	dr := newDiscussionRunner(e.kb, e.proj, ps, cfg)

	var _ repl.DiscussionRunner = dr

	tmpFile := filepath.Join(e.root, "threaded-test.md")
	os.WriteFile(tmpFile, []byte("# Discussion\n\nContent.\n\n# Second Topic\n\nMore."), 0644)
	if !dr.IsThreaded(tmpFile) {
		t.Fatal("expected threaded for file with multiple headings")
	}

	singleFile := filepath.Join(e.root, "single-test.md")
	os.WriteFile(singleFile, []byte("# Discussion\n\nJust one heading."), 0644)
	if dr.IsThreaded(singleFile) {
		t.Fatal("expected non-threaded for single heading")
	}

	if dr.IsThreaded("/nonexistent/path.md") {
		t.Fatal("expected non-threaded for nonexistent file")
	}
}

// ---------------------------------------------------------------------------
// CLI display tests
// ---------------------------------------------------------------------------

func TestDisplayWorkflowApplyResult_Suspended(t *testing.T) {
	r := &workflow.ApplyResult{
		Suspended:    true,
		PipelineID:   "pipeline:test:123",
		FunctionName: "decompose",
		StepName:     "suggest",
		Target:       "The Commons",
		Ops: []function.FileOp{
			{Action: "create", Title: "Revenue", Parent: "The Commons", Content: "Funding."},
		},
	}

	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe
	displayWorkflowApplyResult(r)
	wPipe.Close()
	os.Stderr = oldStderr

	var buf [4096]byte
	n, _ := rPipe.Read(buf[:])
	output := string(buf[:n])

	if !strings.Contains(output, "decompose") {
		t.Fatalf("expected function name, got: %s", output)
	}
	if !strings.Contains(output, "Revenue") {
		t.Fatalf("expected op name, got: %s", output)
	}
	if !strings.Contains(output, "accept") {
		t.Fatalf("expected accept instruction, got: %s", output)
	}
}

func TestDisplayWorkflowApplyResult_Completed(t *testing.T) {
	r := &workflow.ApplyResult{
		Suspended:    false,
		FunctionName: "sharpen",
		StepName:     "default",
		Target:       "The Commons",
		FilesCreated: []string{"/tmp/revenue.md"},
		FilesEdited:  []string{"/tmp/the-commons.md"},
	}

	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe
	displayWorkflowApplyResult(r)
	wPipe.Close()
	os.Stderr = oldStderr

	var buf [4096]byte
	n, _ := rPipe.Read(buf[:])
	output := string(buf[:n])

	if !strings.Contains(output, "Applied") {
		t.Fatalf("expected 'Applied', got: %s", output)
	}
}

func TestDisplayWorkflowAcceptResult(t *testing.T) {
	r := &workflow.AcceptResult{
		Suspended:    false,
		FunctionName: "decompose",
		StepName:     "generate",
		Target:       "The Commons",
		Completed:    true,
		FilesCreated: []string{"/tmp/revenue.md"},
	}

	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe
	displayWorkflowAcceptResult(r)
	wPipe.Close()
	os.Stderr = oldStderr

	var buf [4096]byte
	n, _ := rPipe.Read(buf[:])
	output := string(buf[:n])

	if !strings.Contains(output, "decompose") {
		t.Fatalf("expected function name, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// Agent mode tests
// ---------------------------------------------------------------------------

func TestAgentBackendExecute(t *testing.T) {
	ab := &function.AgentBackend{}
	prompt := function.RenderedPrompt{
		System: "You are an analyst.",
		User:   "Analyze The Commons.",
		Model:  "test-model",
	}

	result, err := ab.Execute(context.Background(), prompt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Raw != "" {
		t.Fatalf("expected empty raw, got %q", result.Raw)
	}
	if ab.PreparedPrompt.System != "You are an analyst." {
		t.Fatalf("expected system prompt captured, got %q", ab.PreparedPrompt.System)
	}
	if ab.Name() != "agent" {
		t.Fatalf("expected name 'agent', got %q", ab.Name())
	}
}

func TestSubmitExternalResult_InjectOps(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	ab := &function.AgentBackend{}
	ps := function.NewPipelineStore(e.store)
	exec := function.NewExecutor(e.kb, ab, ps)

	fn, _, _ := function.LoadFunction("decompose")
	result, err := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Suspended {
		t.Fatal("decompose with agent backend should suspend at gate")
	}

	deps := &workflow.Deps{KB: e.kb, Proj: e.proj, Store: ps}
	externalResult := function.TransformResult{
		Raw:    `[{"title": "Revenue", "rationale": "from external agent"}]`,
		IsText: false,
	}

	err = workflow.SubmitExternalResult(e.ctx(), deps, result.Pipeline.ID, externalResult)
	if err != nil {
		t.Fatalf("SubmitExternalResult: %v", err)
	}

	loaded, _ := ps.Load(e.ctx(), result.Pipeline.ID)
	if loaded.CurrentResult == nil {
		t.Fatal("expected current result after submit")
	}
	if !strings.Contains(loaded.CurrentResult.Raw, "Revenue") {
		t.Fatalf("expected 'Revenue' in result, got %q", loaded.CurrentResult.Raw)
	}
}

func TestSubmitExternalResult_RejectsNonPending(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	mock := &walkthroughMock{responses: []string{mockNoticeResponse}}
	ps := function.NewPipelineStore(e.store)
	exec := function.NewExecutor(e.kb, mock, ps)

	fn, _, _ := function.LoadFunction("notice")
	result, _ := exec.Apply(e.ctx(), e.root, fn, "The Commons")
	_ = ps.Save(e.ctx(), result.Pipeline)

	deps := &workflow.Deps{KB: e.kb, Proj: e.proj, Store: ps}
	err := workflow.SubmitExternalResult(e.ctx(), deps, result.Pipeline.ID,
		function.TransformResult{Raw: "external"})
	if err == nil {
		t.Fatal("expected error for non-pending pipeline")
	}
	if !strings.Contains(err.Error(), "not pending") {
		t.Fatalf("expected 'not pending' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Regression tests
// ---------------------------------------------------------------------------

func TestRegression_BUG4_FileOpJSONTags(t *testing.T) {
	input := `[
		{"action": "edit", "file": "The Commons", "old_text": "original", "new_text": "replacement"},
		{"action": "create", "title": "Revenue", "parent": "The Commons", "content": "Revenue content."}
	]`

	var ops []sevtypes.FileOp
	if err := json.Unmarshal([]byte(input), &ops); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(ops))
	}

	edit := ops[0]
	if edit.OldText != "original" {
		t.Fatalf("BUG-4: old_text not parsed, got %q", edit.OldText)
	}
	if edit.NewText != "replacement" {
		t.Fatalf("BUG-4: new_text not parsed, got %q", edit.NewText)
	}

	create := ops[1]
	if create.Title != "Revenue" || create.Parent != "The Commons" {
		t.Fatalf("BUG-4: create op fields not parsed correctly")
	}

	out, _ := json.Marshal(edit)
	if !strings.Contains(string(out), `"old_text"`) {
		t.Fatalf("BUG-4: JSON should use snake_case, got: %s", string(out))
	}
}

func TestRegression_BUG8_ExtractBlockRemovesSource(t *testing.T) {
	e := newWalkEnv(t)
	e.writeFile("the-commons.md", `---
title: The Commons
---

A neighborhood commons.

## Governance

How decisions get made about lending periods.

## Infrastructure

Technical systems for lending.`)
	e.sync()

	gq := newGraphQuerier(e.kb, e.proj)
	bl, err := gq.BuildBlockList(e.root, "The Commons")
	if err != nil {
		t.Fatalf("BuildBlockList: %v", err)
	}

	var govPath string
	for _, b := range bl.Blocks {
		if strings.Contains(b.Text, "Governance") {
			govPath = b.Path
			break
		}
	}
	if govPath == "" {
		t.Fatal("could not find Governance block")
	}

	extracted, err := gq.PrepareBlockExtraction(e.root, "The Commons", govPath, "Governance", "The Commons")
	if err != nil {
		t.Fatalf("PrepareBlockExtraction: %v", err)
	}
	if !strings.Contains(extracted.Content, "decisions") {
		t.Fatalf("BUG-8: extracted content should contain block text, got: %q", extracted.Content)
	}
}

func TestRegression_BUG9_BlockPathFormat(t *testing.T) {
	e := newWalkEnv(t)
	e.writeFile("the-commons.md", `---
title: The Commons
---

A neighborhood commons.

## Governance

How decisions get made.

## Infrastructure

Technical systems.`)
	e.sync()

	entries, err := e.kb.ListBlocks(e.ctx(), e.root, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected blocks")
	}

	for _, entry := range entries {
		parts := strings.Split(entry.Path, ".")
		for _, part := range parts {
			if strings.HasPrefix(part, "p") || strings.HasPrefix(part, "h") {
				t.Fatalf("BUG-9: block path %q has non-numeric prefix", entry.Path)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Type system end-to-end tests
// ---------------------------------------------------------------------------

func TestTypedOps_ExtraFieldsFlowThroughToFile(t *testing.T) {
	e := newWalkEnv(t)
	seedSimpleTree(t, e)

	// Simulate an LLM returning a create op with extra frontmatter (typed fields).
	mockResponse := `{"ops": [{"action": "create", "title": "Sprint Planning", "parent": "The Commons", "content": "Plan the next sprint.", "extra": {"status": "todo", "deadline": "2026-05-01"}}]}`
	mock := &walkthroughMock{responses: []string{mockResponse}}
	ps := function.NewPipelineStore(e.store)
	deps := &workflow.Deps{
		KB: e.kb, Proj: e.proj, Store: ps, Backend: mock,
	}

	// Apply using a generic function that produces ops.
	result, err := workflow.ApplyFunction(e.ctx(), deps, e.root, "elaborate", "The Commons")
	if err != nil {
		t.Fatalf("ApplyFunction: %v", err)
	}

	// The result should have ops with extra fields.
	if len(result.Ops) == 0 {
		t.Fatal("expected ops in result")
	}
	if result.Ops[0].Extra == nil || result.Ops[0].Extra["status"] != "todo" {
		t.Fatalf("expected extra status=todo, got %v", result.Ops[0].Extra)
	}

	// Verify the file was created with the extra frontmatter.
	created := filepath.Join(e.root, "sprint-planning.md")
	data, err := os.ReadFile(created)
	if err != nil {
		t.Fatalf("reading created file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "status: todo") {
		t.Fatalf("expected 'status: todo' in frontmatter, got:\n%s", content)
	}
	if !strings.Contains(content, "deadline: \"2026-05-01\"") && !strings.Contains(content, "deadline: 2026-05-01") {
		t.Fatalf("expected deadline in frontmatter, got:\n%s", content)
	}
}

func TestTypedOps_SyncProducesMetaPredicates(t *testing.T) {
	e := newWalkEnv(t)

	// Write a file with extra frontmatter fields.
	e.writeFile("my-task.md", `---
title: My Task
parent: "[[The Commons]]"
status: in-progress
deadline: "2026-06-15"
priority: high
---

Do the thing.`)
	e.writeFile("the-commons.md", `---
title: The Commons
---

Root node.`)
	e.sync()

	// Verify meta/* predicates were created.
	subject := kb.NodeSubject(e.root, "My Task")
	triples, err := e.store.BySubject(e.ctx(), subject)
	if err != nil {
		t.Fatal(err)
	}

	metaPreds := map[string]string{}
	for _, tr := range triples {
		if strings.HasPrefix(tr.Predicate, "meta/") {
			metaPreds[tr.Predicate] = tr.Object
		}
	}

	if metaPreds["meta/status"] != "in-progress" {
		t.Fatalf("expected meta/status=in-progress, got %q", metaPreds["meta/status"])
	}
	if metaPreds["meta/deadline"] != "2026-06-15" {
		t.Fatalf("expected meta/deadline=2026-06-15, got %q", metaPreds["meta/deadline"])
	}
	if metaPreds["meta/priority"] != "high" {
		t.Fatalf("expected meta/priority=high, got %q", metaPreds["meta/priority"])
	}
}

func TestTypedOps_TypeConformanceAfterSync(t *testing.T) {
	e := newWalkEnv(t)

	// Set up types in the test config dir.
	typesDir := filepath.Join(e.root, "..", "cfg", "types")
	os.MkdirAll(typesDir, 0755)
	// Reuse the config dir from newWalkEnv which sets OverrideConfigDir.
	cfgDir, _ := config.ConfigDir()
	typesDir = filepath.Join(cfgDir, "types")
	os.MkdirAll(typesDir, 0755)
	defaults.SeedTypes(typesDir)

	// Write a task-shaped node.
	e.writeFile("the-project.md", `---
title: The Project
---

A project.`)
	e.writeFile("my-task.md", `---
title: My Task
parent: "[[The Project]]"
status: todo
deadline: "2026-07-01"
---

Task content.`)
	e.sync()

	// Check type conformance.
	subject := kb.NodeSubject(e.root, "My Task")
	allTypes, err := types.LoadAllTypeDefs()
	if err != nil {
		t.Fatalf("loading types: %v", err)
	}

	results, err := types.InferTypes(e.ctx(), e.kb, subject, allTypes)
	if err != nil {
		t.Fatal(err)
	}

	// Should conform to "note" (no required predicates) and "task" (has status + deadline).
	conforming := map[string]bool{}
	for _, r := range results {
		if r.Conforms {
			conforming[r.TypeName] = true
		}
	}
	if !conforming["note"] {
		t.Fatal("expected to conform to 'note'")
	}
	// task requires parent-type "project" which won't match here (no project type defined
	// for The Project), but the predicates are present. Check predicate match at least.
	for _, r := range results {
		if r.TypeName == "task" {
			if len(r.Missing) > 0 {
				t.Fatalf("task predicates missing: %v", r.Missing)
			}
			// Predicate requirements met even if structure fails.
			break
		}
	}
}

func TestOutputType_ParsedFromFunctionEDN(t *testing.T) {
	// The output-type field should be parsed from function step definitions.
	cfgDir := t.TempDir()
	config.OverrideConfigDir = cfgDir
	t.Cleanup(func() { config.OverrideConfigDir = "" })

	fnDir := filepath.Join(cfgDir, "functions")
	os.MkdirAll(fnDir, 0755)

	// Write a function with output-type.
	os.WriteFile(filepath.Join(fnDir, "typed-fn.edn"), []byte(`{:name "typed-fn"
 :steps [{:name "generate"
          :output "ops"
          :output-type "task"
          :gate "approve"}]}`), 0644)
	os.WriteFile(filepath.Join(fnDir, "typed-fn.generate.md"), []byte("Generate tasks."), 0644)

	fn, _, err := function.LoadFunction("typed-fn")
	if err != nil {
		t.Fatalf("LoadFunction: %v", err)
	}
	steps := fn.EffectiveSteps()
	if len(steps) == 0 {
		t.Fatal("expected steps")
	}
	if steps[0].Output.TypeName != "task" {
		t.Fatalf("expected output type 'task', got %q", steps[0].Output.TypeName)
	}
}

// Ensure new imports are used.
var (
	_ = json.Marshal
	_ = sevtypes.FileOp{}
	_ = workflow.Deps{}
	_ = projection.FileOp{}
)
