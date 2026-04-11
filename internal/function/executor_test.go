package function

import (
	"context"
	"database/sql"
	"testing"

	"sevens/internal/graphops"
	"sevens/internal/kb"
	"sevens/internal/triple"

	_ "turso.tech/database/tursogo"
)

// --- Test helpers ---

// mockBackend returns a fixed response for every Execute call.
type mockBackend struct {
	responses []string
	callCount int
}

func (m *mockBackend) Execute(ctx context.Context, prompt RenderedPrompt) (TransformResult, error) {
	if m.callCount >= len(m.responses) {
		return TransformResult{Raw: "default-response", IsText: true}, nil
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return TransformResult{Raw: resp, IsText: true}, nil
}

func (m *mockBackend) Name() string { return "mock" }

func setupTestKB(t *testing.T) (*kb.KB, *triple.Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	store, err := triple.New(db)
	if err != nil {
		t.Fatal(err)
	}
	graph := graphops.New(store)
	k := kb.New(graph)
	return k, store, db
}

func seedNode(t *testing.T, k *kb.KB, root, title, content string, parent *string) {
	t.Helper()
	ctx := context.Background()
	_, err := k.CreateNode(ctx, root, title, content, parent)
	if err != nil {
		t.Fatalf("seeding node %q: %v", title, err)
	}
}

func singleStepFunction(name, prompt, gate string) *Function {
	fn := &Function{
		Name: name,
		Steps: []Step{
			{
				Name:   "step-0",
				Output: Signature{Shape: ShapeText},
				Backend: BackendSpec{
					Kind:           BackendLLM,
					PromptTemplate: prompt,
				},
			},
		},
	}
	if gate != "" {
		fn.Steps[0].Gate = &GateSpec{Revisable: true, Cancelable: true}
	}
	return fn
}

func multiStepFunction(name string) *Function {
	return &Function{
		Name: name,
		Steps: []Step{
			{
				Name:   "suggest",
				Output: Signature{Shape: ShapeText},
				Gate:   &GateSpec{Revisable: true, Cancelable: true, HistoryPolicy: HistoryFull},
				Backend: BackendSpec{
					Kind:           BackendLLM,
					PromptTemplate: "Suggest changes for {{title}}:\n{{content}}",
				},
			},
			{
				Name:   "generate",
				Output: Signature{Shape: ShapeText},
				Gate:   &GateSpec{Revisable: true, Cancelable: true},
				Backend: BackendSpec{
					Kind:           BackendLLM,
					PromptTemplate: "Generate based on: {{prev}}",
				},
			},
		},
	}
}

// --- Tests ---

func TestApplySingleStepUngated(t *testing.T) {
	k, store, db := setupTestKB(t)
	defer db.Close()

	seedNode(t, k, "/root", "My Note", "Some content", nil)

	backend := &mockBackend{responses: []string{"LLM output here"}}
	ps := NewPipelineStore(store)
	exec := NewExecutor(k, backend, ps)

	fn := singleStepFunction("notice", "Analyze {{title}}: {{content}}", "")

	result, err := exec.Apply(context.Background(), "/root", fn, "My Note")
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.Suspended {
		t.Fatal("expected ungated step to not suspend")
	}
	if result.Pipeline.Phase != PhaseCompleted {
		t.Fatalf("expected Completed, got %s", result.Pipeline.Phase)
	}
	if result.Result == nil || result.Result.Raw != "LLM output here" {
		t.Fatalf("unexpected result: %v", result.Result)
	}
}

func TestApplySingleStepGated(t *testing.T) {
	k, store, db := setupTestKB(t)
	defer db.Close()

	seedNode(t, k, "/root", "My Note", "Some content", nil)

	backend := &mockBackend{responses: []string{"suggestions"}}
	ps := NewPipelineStore(store)
	exec := NewExecutor(k, backend, ps)

	fn := singleStepFunction("decompose", "Suggest for {{title}}", "approve")

	result, err := exec.Apply(context.Background(), "/root", fn, "My Note")
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if !result.Suspended {
		t.Fatal("expected gated step to suspend")
	}
	if result.Pipeline.Phase != PhasePending {
		t.Fatalf("expected Pending, got %s", result.Pipeline.Phase)
	}
	if result.Result == nil || result.Result.Raw != "suggestions" {
		t.Fatalf("unexpected result: %v", result.Result)
	}

	// Pipeline should be persisted
	loaded, err := ps.Load(context.Background(), result.Pipeline.ID)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded.Phase != PhasePending {
		t.Fatalf("loaded pipeline expected Pending, got %s", loaded.Phase)
	}
}

func TestAcceptSingleStep(t *testing.T) {
	k, store, db := setupTestKB(t)
	defer db.Close()

	seedNode(t, k, "/root", "My Note", "Some content", nil)

	backend := &mockBackend{responses: []string{"suggestions"}}
	ps := NewPipelineStore(store)
	exec := NewExecutor(k, backend, ps)

	fn := singleStepFunction("decompose", "Suggest for {{title}}", "approve")

	// Apply (suspends at gate)
	applyResult, err := exec.Apply(context.Background(), "/root", fn, "My Note")
	if err != nil {
		t.Fatal(err)
	}

	// Accept
	acceptResult, err := exec.Accept(context.Background(), "/root", fn, applyResult.Pipeline.ID)
	if err != nil {
		t.Fatalf("Accept error: %v", err)
	}

	if acceptResult.Suspended {
		t.Fatal("expected completed after accept of single-step")
	}
	if acceptResult.Pipeline.Phase != PhaseCompleted {
		t.Fatalf("expected Completed, got %s", acceptResult.Pipeline.Phase)
	}
}

func TestRejectSingleStep(t *testing.T) {
	k, store, db := setupTestKB(t)
	defer db.Close()

	seedNode(t, k, "/root", "My Note", "Some content", nil)

	backend := &mockBackend{responses: []string{"bad output"}}
	ps := NewPipelineStore(store)
	exec := NewExecutor(k, backend, ps)

	fn := singleStepFunction("decompose", "Suggest for {{title}}", "approve")

	applyResult, err := exec.Apply(context.Background(), "/root", fn, "My Note")
	if err != nil {
		t.Fatal(err)
	}

	p, err := exec.Reject(context.Background(), applyResult.Pipeline.ID)
	if err != nil {
		t.Fatalf("Reject error: %v", err)
	}

	if p.Phase != PhaseRejected {
		t.Fatalf("expected Rejected, got %s", p.Phase)
	}
	if !p.Phase.IsTerminal() {
		t.Fatal("Rejected should be terminal")
	}
}

func TestMultiStepWithGates(t *testing.T) {
	k, store, db := setupTestKB(t)
	defer db.Close()

	seedNode(t, k, "/root", "My Note", "Some content", nil)

	backend := &mockBackend{responses: []string{"3 suggestions", "generated output"}}
	ps := NewPipelineStore(store)
	exec := NewExecutor(k, backend, ps)

	fn := multiStepFunction("decompose")

	// Step 0: suggest (gated)
	result, err := exec.Apply(context.Background(), "/root", fn, "My Note")
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !result.Suspended {
		t.Fatal("expected suspension at suggest gate")
	}
	if result.Pipeline.CurrentStep != 0 {
		t.Fatalf("expected step 0, got %d", result.Pipeline.CurrentStep)
	}
	if result.Result.Raw != "3 suggestions" {
		t.Fatalf("expected '3 suggestions', got %q", result.Result.Raw)
	}

	// Accept step 0 -> runs step 1 (also gated)
	result, err = exec.Accept(context.Background(), "/root", fn, result.Pipeline.ID)
	if err != nil {
		t.Fatalf("Accept step 0 error: %v", err)
	}
	if !result.Suspended {
		t.Fatal("expected suspension at generate gate")
	}
	if result.Pipeline.CurrentStep != 1 {
		t.Fatalf("expected step 1, got %d", result.Pipeline.CurrentStep)
	}
	if result.Result.Raw != "generated output" {
		t.Fatalf("expected 'generated output', got %q", result.Result.Raw)
	}

	// Accept step 1 -> completes
	result, err = exec.Accept(context.Background(), "/root", fn, result.Pipeline.ID)
	if err != nil {
		t.Fatalf("Accept step 1 error: %v", err)
	}
	if result.Suspended {
		t.Fatal("expected completed after last accept")
	}
	if result.Pipeline.Phase != PhaseCompleted {
		t.Fatalf("expected Completed, got %s", result.Pipeline.Phase)
	}
}

func TestReviseThenAccept(t *testing.T) {
	k, store, db := setupTestKB(t)
	defer db.Close()

	seedNode(t, k, "/root", "My Note", "Some content", nil)

	backend := &mockBackend{responses: []string{"v1", "v2"}}
	ps := NewPipelineStore(store)
	exec := NewExecutor(k, backend, ps)

	fn := singleStepFunction("decompose", "Suggest for {{title}}", "approve")
	fn.Steps[0].Gate.HistoryPolicy = HistoryFull

	// Apply
	result, err := exec.Apply(context.Background(), "/root", fn, "My Note")
	if err != nil {
		t.Fatal(err)
	}
	if result.Result.Raw != "v1" {
		t.Fatalf("expected v1, got %q", result.Result.Raw)
	}

	// Revise
	result, err = exec.Revise(context.Background(), "/root", fn, result.Pipeline.ID, "try again")
	if err != nil {
		t.Fatalf("Revise error: %v", err)
	}
	if result.Result.Raw != "v2" {
		t.Fatalf("expected v2, got %q", result.Result.Raw)
	}
	if !result.Suspended {
		t.Fatal("expected still suspended after revise")
	}

	// Accept revised version
	result, err = exec.Accept(context.Background(), "/root", fn, result.Pipeline.ID)
	if err != nil {
		t.Fatalf("Accept after revise error: %v", err)
	}
	if result.Pipeline.Phase != PhaseCompleted {
		t.Fatalf("expected Completed, got %s", result.Pipeline.Phase)
	}
}

func TestFindPending(t *testing.T) {
	k, store, db := setupTestKB(t)
	defer db.Close()

	seedNode(t, k, "/root", "Note A", "Content A", nil)
	seedNode(t, k, "/root", "Note B", "Content B", nil)

	backend := &mockBackend{responses: []string{"a", "b"}}
	ps := NewPipelineStore(store)
	exec := NewExecutor(k, backend, ps)

	fn := singleStepFunction("decompose", "Suggest for {{title}}", "approve")

	// Apply to two nodes
	_, err := exec.Apply(context.Background(), "/root", fn, "Note A")
	if err != nil {
		t.Fatal(err)
	}
	// Reset backend call counter
	backend.callCount = 1
	_, err = exec.Apply(context.Background(), "/root", fn, "Note B")
	if err != nil {
		t.Fatal(err)
	}

	// Find pending
	pending, err := ps.FindPending(context.Background(), "/root")
	if err != nil {
		t.Fatalf("FindPending error: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	// Verify they're different targets
	targets := map[string]bool{}
	for _, p := range pending {
		targets[p.Target] = true
	}
	if !targets["Note A"] || !targets["Note B"] {
		t.Fatalf("expected Note A and Note B in pending, got %v", targets)
	}
}

func TestPipelineStoreSaveLoad(t *testing.T) {
	_, store, db := setupTestKB(t)
	defer db.Close()

	ps := NewPipelineStore(store)
	ctx := context.Background()

	p := NewPipeline("/root", "test-fn", "My Node")
	p.CurrentResult = &TransformResult{Raw: "some result", IsText: true}
	p.PriorStepResults = []TransformResult{
		{Raw: "prior-1", IsText: true},
	}
	p.RevisionChain = []RevisionEntry{
		{Attempt: TransformResult{Raw: "attempt-1", IsText: true}, Feedback: "nope"},
	}

	if err := ps.Save(ctx, p); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := ps.Load(ctx, p.ID)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if loaded.Root != p.Root {
		t.Fatalf("Root mismatch: %q vs %q", loaded.Root, p.Root)
	}
	if loaded.FunctionName != p.FunctionName {
		t.Fatalf("FunctionName mismatch: %q vs %q", loaded.FunctionName, p.FunctionName)
	}
	if loaded.Target != p.Target {
		t.Fatalf("Target mismatch: %q vs %q", loaded.Target, p.Target)
	}
	if loaded.Phase != p.Phase {
		t.Fatalf("Phase mismatch: %s vs %s", loaded.Phase, p.Phase)
	}
	if loaded.CurrentStep != p.CurrentStep {
		t.Fatalf("CurrentStep mismatch: %d vs %d", loaded.CurrentStep, p.CurrentStep)
	}
	if loaded.CurrentResult == nil || loaded.CurrentResult.Raw != "some result" {
		t.Fatal("CurrentResult mismatch")
	}
	if len(loaded.PriorStepResults) != 1 || loaded.PriorStepResults[0].Raw != "prior-1" {
		t.Fatal("PriorStepResults mismatch")
	}
	if len(loaded.RevisionChain) != 1 || loaded.RevisionChain[0].Feedback != "nope" {
		t.Fatal("RevisionChain mismatch")
	}
}

func TestContextResolution(t *testing.T) {
	k, _, db := setupTestKB(t)
	defer db.Close()
	ctx := context.Background()

	parent := "Parent Note"
	seedNode(t, k, "/root", "Parent Note", "Parent content", nil)
	seedNode(t, k, "/root", "Child A", "Child A content", &parent)
	seedNode(t, k, "/root", "Child B", "Child B content", &parent)

	step := Step{
		Name: "test-step",
		Requires: []Require{
			{Role: "target"},
			{Role: "parent", As: "par"},
			{Role: "children"},
		},
		Backend: BackendSpec{
			Kind:           BackendLLM,
			PromptTemplate: "Title: {{title}}\nContent: {{content}}\nParent: {{par.title}}",
		},
	}

	rc, err := ResolveContext(ctx, k, "/root", "Child A", step, "")
	if err != nil {
		t.Fatalf("ResolveContext error: %v", err)
	}

	if rc.Target.Title != "Child A" {
		t.Fatalf("expected target title 'Child A', got %q", rc.Target.Title)
	}

	parWalk, ok := rc.Roles["par"]
	if !ok || parWalk == nil {
		t.Fatal("expected parent in roles")
	}
	if parWalk.Title != "Parent Note" {
		t.Fatalf("expected parent title 'Parent Note', got %q", parWalk.Title)
	}

	rendered := RenderPrompt(step.Backend.PromptTemplate, rc)
	expected := "Title: Child A\nContent: Child A content\nParent: Parent Note"
	if rendered != expected {
		t.Fatalf("rendered prompt mismatch:\n  got:  %q\n  want: %q", rendered, expected)
	}
}

func TestConvertFunction(t *testing.T) {
	// Verify that old apply.Function types convert correctly
	// This is a compile-time check more than a behavioral one
	old := &Function{
		Name:        "test",
		Description: "A test function",
		Steps: []Step{
			{
				Name:   "step-0",
				Output: Signature{Shape: ShapeText},
				Backend: BackendSpec{
					Kind:           BackendLLM,
					PromptTemplate: "Hello {{title}}",
				},
			},
		},
	}

	steps := old.EffectiveSteps()
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Name != "step-0" {
		t.Fatalf("expected step name 'step-0', got %q", steps[0].Name)
	}
}

func TestCancelPendingPipeline(t *testing.T) {
	k, store, db := setupTestKB(t)
	defer db.Close()

	seedNode(t, k, "/root", "My Note", "Some content", nil)

	backend := &mockBackend{responses: []string{"suggestions"}}
	ps := NewPipelineStore(store)
	exec := NewExecutor(k, backend, ps)

	fn := singleStepFunction("decompose", "Suggest for {{title}}", "approve")

	result, err := exec.Apply(context.Background(), "/root", fn, "My Note")
	if err != nil {
		t.Fatal(err)
	}

	p, err := exec.Cancel(context.Background(), fn, result.Pipeline.ID)
	if err != nil {
		t.Fatalf("Cancel error: %v", err)
	}
	if p.Phase != PhaseCancelled {
		t.Fatalf("expected Cancelled, got %s", p.Phase)
	}
}
