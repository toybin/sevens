package edn_test

import (
	"context"
	"database/sql"
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
	ednproj "sevens/internal/projection/edn"
	"sevens/internal/triple"
	"sevens/internal/types"

	_ "turso.tech/database/tursogo"
)

func ctx() context.Context { return context.Background() }

// setup creates an in-memory triple store and a temp config dir seeded
// with the bundled defaults.
func setup(t *testing.T) (*ednproj.EDNProjection, *triple.Store, string) {
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

	// Register predicates via KB (which calls allSpecs)
	graph := graphops.New(store)
	_ = kb.New(graph)

	tmpDir := t.TempDir()
	config.OverrideConfigDir = tmpDir
	t.Cleanup(func() { config.OverrideConfigDir = "" })

	fnDir := filepath.Join(tmpDir, "functions")
	typeDir := filepath.Join(tmpDir, "types")
	os.MkdirAll(fnDir, 0755)
	os.MkdirAll(typeDir, 0755)

	if _, err := defaults.SeedFunctions(fnDir); err != nil {
		t.Fatal(err)
	}
	if _, err := defaults.SeedTypes(typeDir); err != nil {
		t.Fatal(err)
	}

	proj := ednproj.New(store)
	return proj, store, tmpDir
}

func TestSyncFunctions(t *testing.T) {
	proj, store, tmpDir := setup(t)
	fnDir := filepath.Join(tmpDir, "functions")

	if err := proj.SyncFunctions(ctx(), fnDir); err != nil {
		t.Fatal(err)
	}

	// Verify fn/name triples exist
	triples, err := store.ByPredicate(ctx(), kb.PredFnName)
	if err != nil {
		t.Fatal(err)
	}

	nameSet := make(map[string]bool)
	for _, tr := range triples {
		nameSet[tr.Object] = true
	}

	if !nameSet["notice"] {
		t.Fatal("expected fn:notice to be synced")
	}
	if !nameSet["decompose"] {
		t.Fatal("expected fn:decompose to be synced")
	}
	if !nameSet["daily-note"] {
		t.Fatal("expected fn:daily-note to be synced")
	}
}

func TestSyncTypes(t *testing.T) {
	proj, store, tmpDir := setup(t)
	typeDir := filepath.Join(tmpDir, "types")

	if err := proj.SyncTypes(ctx(), typeDir); err != nil {
		t.Fatal(err)
	}

	// Verify type/name triples exist
	triples, err := store.ByPredicate(ctx(), kb.PredTypeName)
	if err != nil {
		t.Fatal(err)
	}

	nameSet := make(map[string]bool)
	for _, tr := range triples {
		nameSet[tr.Object] = true
	}

	if !nameSet["task"] {
		t.Fatal("expected type:task to be synced")
	}
	if !nameSet["text"] {
		t.Fatal("expected type:text to be synced")
	}
	if !nameSet["create"] {
		t.Fatal("expected type:create to be synced")
	}
}

func TestLoadFunctionRoundTrip(t *testing.T) {
	proj, _, tmpDir := setup(t)
	fnDir := filepath.Join(tmpDir, "functions")

	if err := proj.SyncFunctions(ctx(), fnDir); err != nil {
		t.Fatal(err)
	}

	fn, err := proj.LoadFunction(ctx(), "notice")
	if err != nil {
		t.Fatal(err)
	}

	if fn.Name != "notice" {
		t.Fatalf("expected name 'notice', got %q", fn.Name)
	}
	if fn.Description == "" {
		t.Fatal("expected non-empty description")
	}

	// Single-step functions have one step named "default"
	if len(fn.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(fn.Steps))
	}
	step := fn.Steps[0]
	if step.Name != "default" {
		t.Fatalf("expected step name 'default', got %q", step.Name)
	}
	if step.Output.Shape != function.ShapeText {
		t.Fatalf("expected text output shape, got %d", step.Output.Shape)
	}
	if step.Backend.PromptTemplate == "" {
		t.Fatal("expected non-empty prompt template (loaded from .md sidecar)")
	}
	if !strings.Contains(step.Backend.PromptTemplate, "Examine this node") {
		t.Fatal("expected prompt to contain sidecar content")
	}

	// Context paths should be loaded
	if len(step.Paths) != 3 {
		t.Fatalf("expected 3 context paths, got %d", len(step.Paths))
	}
}

func TestLoadTypeDefRoundTrip(t *testing.T) {
	proj, _, tmpDir := setup(t)
	typeDir := filepath.Join(tmpDir, "types")

	if err := proj.SyncTypes(ctx(), typeDir); err != nil {
		t.Fatal(err)
	}

	td, err := proj.LoadTypeDef(ctx(), "task")
	if err != nil {
		t.Fatal(err)
	}

	if td.Name != "task" {
		t.Fatalf("expected name 'task', got %q", td.Name)
	}
	if td.Extends != "create" {
		t.Fatalf("expected extends 'create', got %q", td.Extends)
	}
	if td.Primitive {
		t.Fatal("expected task to not be primitive")
	}

	sort.Strings(td.Predicates.Required)
	if len(td.Predicates.Required) != 2 {
		t.Fatalf("expected 2 required predicates, got %d: %v", len(td.Predicates.Required), td.Predicates.Required)
	}
	if td.Predicates.Required[0] != "deadline" || td.Predicates.Required[1] != "status" {
		t.Fatalf("expected required [deadline, status], got %v", td.Predicates.Required)
	}

	sort.Strings(td.Predicates.Optional)
	if len(td.Predicates.Optional) != 3 {
		t.Fatalf("expected 3 optional predicates, got %d: %v", len(td.Predicates.Optional), td.Predicates.Optional)
	}

	if td.Structure.ParentType != "project" {
		t.Fatalf("expected parent type 'project', got %q", td.Structure.ParentType)
	}
	if td.Structure.ChildrenMin != 0 || td.Structure.ChildrenMax != 5 {
		t.Fatalf("expected children [0, 5], got [%d, %d]", td.Structure.ChildrenMin, td.Structure.ChildrenMax)
	}

	sort.Strings(td.Projection.Frontmatter)
	if len(td.Projection.Frontmatter) != 4 {
		t.Fatalf("expected 4 frontmatter fields, got %d", len(td.Projection.Frontmatter))
	}

	if td.Projection.Orthography == nil {
		t.Fatal("expected orthography bindings")
	}
	if td.Projection.Orthography["assignee"].Signifier != "@" {
		t.Fatalf("expected assignee signifier '@', got %q", td.Projection.Orthography["assignee"].Signifier)
	}
	if td.Projection.Orthography["status"].ValueModel != "task-status" {
		t.Fatalf("expected status value-model 'task-status', got %q", td.Projection.Orthography["status"].ValueModel)
	}
}

func TestMultiStepFunction(t *testing.T) {
	proj, store, tmpDir := setup(t)
	fnDir := filepath.Join(tmpDir, "functions")

	if err := proj.SyncFunctions(ctx(), fnDir); err != nil {
		t.Fatal(err)
	}

	// Verify step triples exist
	stepTriples, err := store.BySubject(ctx(), "step:decompose:suggest")
	if err != nil {
		t.Fatal(err)
	}
	if len(stepTriples) == 0 {
		t.Fatal("expected step:decompose:suggest triples to exist")
	}

	// Verify pipeline order
	order, err := store.BySubjectPredicate(ctx(), "fn:decompose", kb.PredFnPipelineOrder)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 1 || order[0] != "suggest,generate" {
		t.Fatalf("expected pipeline order 'suggest,generate', got %v", order)
	}

	// Load and verify
	fn, err := proj.LoadFunction(ctx(), "decompose")
	if err != nil {
		t.Fatal(err)
	}

	if len(fn.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(fn.Steps))
	}
	if fn.Steps[0].Name != "suggest" {
		t.Fatalf("expected first step 'suggest', got %q", fn.Steps[0].Name)
	}
	if fn.Steps[1].Name != "generate" {
		t.Fatalf("expected second step 'generate', got %q", fn.Steps[1].Name)
	}

	// Suggest step should have a gate
	if fn.Steps[0].Gate == nil {
		t.Fatal("expected suggest step to have a gate")
	}
	if fn.Steps[0].Output.Shape != function.ShapeStructured {
		t.Fatalf("expected suggest output shape Structured, got %d", fn.Steps[0].Output.Shape)
	}

	// Generate step produces file ops (create)
	if fn.Steps[1].Output.Shape != function.ShapeFileOps {
		t.Fatalf("expected generate output shape FileOps, got %d", fn.Steps[1].Output.Shape)
	}
}

func TestPromptSidecarLoading(t *testing.T) {
	proj, store, tmpDir := setup(t)
	fnDir := filepath.Join(tmpDir, "functions")

	if err := proj.SyncFunctions(ctx(), fnDir); err != nil {
		t.Fatal(err)
	}

	// notice.md should be loaded as fn/prompt
	prompts, err := store.BySubjectPredicate(ctx(), "fn:notice", kb.PredFnPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) == 0 {
		t.Fatal("expected fn:notice to have a prompt triple")
	}
	if !strings.Contains(prompts[0], "Examine this node") {
		t.Fatalf("expected notice prompt to contain sidecar content, got %q", prompts[0][:min(80, len(prompts[0]))])
	}

	// decompose.suggest.md and decompose.generate.md should be step/prompt
	suggestPrompts, err := store.BySubjectPredicate(ctx(), "step:decompose:suggest", kb.PredStepPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestPrompts) == 0 {
		t.Fatal("expected step:decompose:suggest to have a prompt triple")
	}

	generatePrompts, err := store.BySubjectPredicate(ctx(), "step:decompose:generate", kb.PredStepPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if len(generatePrompts) == 0 {
		t.Fatal("expected step:decompose:generate to have a prompt triple")
	}
}

func TestPrimitiveTypes(t *testing.T) {
	proj, _, tmpDir := setup(t)
	typeDir := filepath.Join(tmpDir, "types")

	if err := proj.SyncTypes(ctx(), typeDir); err != nil {
		t.Fatal(err)
	}

	primitives := []string{"text", "create", "edit", "suggestion"}
	for _, name := range primitives {
		td, err := proj.LoadTypeDef(ctx(), name)
		if err != nil {
			t.Fatalf("loading primitive %q: %v", name, err)
		}
		if !td.Primitive {
			t.Fatalf("expected %q to be primitive", name)
		}
		if td.SchemaInstruction == "" {
			t.Fatalf("expected %q to have schema instruction", name)
		}
		if td.Extends != "" {
			t.Fatalf("expected %q to have no extends, got %q", name, td.Extends)
		}
		if len(td.Predicates.Required) > 0 {
			t.Fatalf("expected %q to have no required predicates, got %v", name, td.Predicates.Required)
		}
	}
}

func TestSyncClearsStaleTriples(t *testing.T) {
	proj, store, tmpDir := setup(t)
	fnDir := filepath.Join(tmpDir, "functions")
	typeDir := filepath.Join(tmpDir, "types")

	// First sync
	if err := proj.Sync(ctx(), fnDir, typeDir); err != nil {
		t.Fatal(err)
	}

	// Verify notice exists
	names, _ := proj.ListFunctions(ctx())
	found := false
	for _, n := range names {
		if n == "notice" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected notice after first sync")
	}

	// Delete notice.edn and notice.md
	os.Remove(filepath.Join(fnDir, "notice.edn"))
	os.Remove(filepath.Join(fnDir, "notice.md"))

	// Re-sync
	if err := proj.Sync(ctx(), fnDir, typeDir); err != nil {
		t.Fatal(err)
	}

	// notice should be gone
	vals, _ := store.BySubjectPredicate(ctx(), "fn:notice", kb.PredFnName)
	if len(vals) > 0 {
		t.Fatal("expected fn:notice to be cleared after resync without file")
	}
}

func TestDeterministicFunction(t *testing.T) {
	proj, _, tmpDir := setup(t)
	fnDir := filepath.Join(tmpDir, "functions")

	if err := proj.SyncFunctions(ctx(), fnDir); err != nil {
		t.Fatal(err)
	}

	fn, err := proj.LoadFunction(ctx(), "daily-note")
	if err != nil {
		t.Fatal(err)
	}

	if fn.Name != "daily-note" {
		t.Fatalf("expected name 'daily-note', got %q", fn.Name)
	}
	if len(fn.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(fn.Steps))
	}

	step := fn.Steps[0]
	if step.Backend.Kind != function.BackendDeterministic {
		t.Fatalf("expected deterministic backend, got %d", step.Backend.Kind)
	}
	if step.Backend.Handler == "" {
		t.Fatal("expected non-empty handler JSON")
	}
	if !strings.Contains(step.Backend.Handler, "create-node") {
		t.Fatalf("expected handler to contain 'create-node', got %q", step.Backend.Handler)
	}
}

func TestLoadAllTypes(t *testing.T) {
	proj, _, tmpDir := setup(t)
	typeDir := filepath.Join(tmpDir, "types")

	if err := proj.SyncTypes(ctx(), typeDir); err != nil {
		t.Fatal(err)
	}

	allTypes, err := proj.LoadAllTypes(ctx())
	if err != nil {
		t.Fatal(err)
	}

	if len(allTypes) < 4 {
		t.Fatalf("expected at least 4 types (primitives), got %d", len(allTypes))
	}

	if _, ok := allTypes["task"]; !ok {
		t.Fatal("expected task in all types")
	}
	if _, ok := allTypes["text"]; !ok {
		t.Fatal("expected text in all types")
	}
}

func TestListFunctions(t *testing.T) {
	proj, _, tmpDir := setup(t)
	fnDir := filepath.Join(tmpDir, "functions")

	if err := proj.SyncFunctions(ctx(), fnDir); err != nil {
		t.Fatal(err)
	}

	names, err := proj.ListFunctions(ctx())
	if err != nil {
		t.Fatal(err)
	}

	if len(names) < 5 {
		t.Fatalf("expected at least 5 functions, got %d", len(names))
	}

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range []string{"notice", "decompose", "daily-note", "discuss", "scaffold"} {
		if !nameSet[expected] {
			t.Fatalf("expected %q in function list", expected)
		}
	}
}

func TestListTypes(t *testing.T) {
	proj, _, tmpDir := setup(t)
	typeDir := filepath.Join(tmpDir, "types")

	if err := proj.SyncTypes(ctx(), typeDir); err != nil {
		t.Fatal(err)
	}

	names, err := proj.ListTypes(ctx())
	if err != nil {
		t.Fatal(err)
	}

	if len(names) < 4 {
		t.Fatalf("expected at least 4 types, got %d", len(names))
	}
}

func TestFunctionWithAgentConfig(t *testing.T) {
	proj, _, tmpDir := setup(t)
	fnDir := filepath.Join(tmpDir, "functions")

	if err := proj.SyncFunctions(ctx(), fnDir); err != nil {
		t.Fatal(err)
	}

	fn, err := proj.LoadFunction(ctx(), "discuss")
	if err != nil {
		t.Fatal(err)
	}

	if fn.Name != "discuss" {
		t.Fatalf("expected name 'discuss', got %q", fn.Name)
	}

	// discuss has a single step with persona from the agent block
	if len(fn.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(fn.Steps))
	}
	step := fn.Steps[0]
	if step.Backend.Persona == "" {
		t.Fatal("expected non-empty persona from agent config")
	}
	if step.Backend.SystemPrompt == "" {
		t.Fatal("expected non-empty system prompt from agent config")
	}
}

func TestFunctionWithRequires(t *testing.T) {
	proj, _, tmpDir := setup(t)
	fnDir := filepath.Join(tmpDir, "functions")

	if err := proj.SyncFunctions(ctx(), fnDir); err != nil {
		t.Fatal(err)
	}

	fn, err := proj.LoadFunction(ctx(), "scaffold")
	if err != nil {
		t.Fatal(err)
	}

	// scaffold has requires at function level
	if len(fn.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(fn.Steps))
	}
	step := fn.Steps[0]
	if len(step.Requires) < 1 {
		t.Fatal("expected at least one require")
	}

	roleSet := make(map[string]bool)
	for _, r := range step.Requires {
		roleSet[r.Role] = true
	}
	if !roleSet["children"] {
		t.Fatal("expected 'children' in requires")
	}
}

func TestNoteTypeRoundTrip(t *testing.T) {
	proj, _, tmpDir := setup(t)
	typeDir := filepath.Join(tmpDir, "types")

	if err := proj.SyncTypes(ctx(), typeDir); err != nil {
		t.Fatal(err)
	}

	td, err := proj.LoadTypeDef(ctx(), "note")
	if err != nil {
		t.Fatal(err)
	}

	if td.Name != "note" {
		t.Fatalf("expected name 'note', got %q", td.Name)
	}
	if td.Extends != "create" {
		t.Fatalf("expected extends 'create', got %q", td.Extends)
	}
	if td.Primitive {
		t.Fatal("expected note to not be primitive")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Graph-backed package loader integration tests
// ---------------------------------------------------------------------------
// These test that function.LoadFunction and types.LoadTypeDef use the
// graph when GraphFunctionLoader / GraphTypeLoader are set.

func TestGraphLoader_FunctionLoadFunction(t *testing.T) {
	proj, _, cfgDir := setup(t)
	fnDir := filepath.Join(cfgDir, "functions")
	tyDir := filepath.Join(cfgDir, "types")

	if err := proj.Sync(ctx(), fnDir, tyDir); err != nil {
		t.Fatal(err)
	}

	// Set the graph loader.
	function.GraphFunctionLoader = proj
	t.Cleanup(func() { function.GraphFunctionLoader = nil })

	fn, _, err := function.LoadFunction("notice")
	if err != nil {
		t.Fatalf("LoadFunction via graph: %v", err)
	}
	if fn.Name != "notice" {
		t.Fatalf("expected name 'notice', got %q", fn.Name)
	}
	if len(fn.Steps) == 0 {
		t.Fatal("expected steps")
	}
	// Verify the prompt template was loaded.
	if fn.Steps[0].Backend.PromptTemplate == "" {
		t.Fatal("expected non-empty prompt template from graph")
	}
}

func TestGraphLoader_FunctionListFunctions(t *testing.T) {
	proj, _, cfgDir := setup(t)
	fnDir := filepath.Join(cfgDir, "functions")
	tyDir := filepath.Join(cfgDir, "types")

	if err := proj.Sync(ctx(), fnDir, tyDir); err != nil {
		t.Fatal(err)
	}

	function.GraphFunctionLoader = proj
	t.Cleanup(func() { function.GraphFunctionLoader = nil })

	names, err := function.ListFunctions()
	if err != nil {
		t.Fatalf("ListFunctions via graph: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("expected functions")
	}
	// Should include known built-in functions.
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	for _, want := range []string{"notice", "decompose", "discuss", "sharpen"} {
		if !nameSet[want] {
			t.Fatalf("expected function %q in list", want)
		}
	}
}

func TestGraphLoader_TypeLoadTypeDef(t *testing.T) {
	proj, _, cfgDir := setup(t)
	fnDir := filepath.Join(cfgDir, "functions")
	tyDir := filepath.Join(cfgDir, "types")

	if err := proj.Sync(ctx(), fnDir, tyDir); err != nil {
		t.Fatal(err)
	}

	types.GraphTypeLoader = proj
	t.Cleanup(func() { types.GraphTypeLoader = nil })

	td, err := types.LoadTypeDef("task")
	if err != nil {
		t.Fatalf("LoadTypeDef via graph: %v", err)
	}
	if td.Name != "task" {
		t.Fatalf("expected name 'task', got %q", td.Name)
	}
	if td.Extends != "create" {
		t.Fatalf("expected extends 'create', got %q", td.Extends)
	}
	if len(td.Predicates.Required) == 0 {
		t.Fatal("expected required predicates")
	}
}

func TestGraphLoader_TypeLoadAllTypeDefs(t *testing.T) {
	proj, _, cfgDir := setup(t)
	fnDir := filepath.Join(cfgDir, "functions")
	tyDir := filepath.Join(cfgDir, "types")

	if err := proj.Sync(ctx(), fnDir, tyDir); err != nil {
		t.Fatal(err)
	}

	types.GraphTypeLoader = proj
	t.Cleanup(func() { types.GraphTypeLoader = nil })

	all, err := types.LoadAllTypeDefs()
	if err != nil {
		t.Fatalf("LoadAllTypeDefs via graph: %v", err)
	}
	// Should have primitives + user types.
	for _, want := range []string{"text", "create", "edit", "suggestion", "task", "note"} {
		if _, ok := all[want]; !ok {
			t.Fatalf("expected type %q in all types", want)
		}
	}
	// Verify primitive flag.
	if !all["text"].Primitive {
		t.Fatal("text should be primitive")
	}
	if all["task"].Primitive {
		t.Fatal("task should not be primitive")
	}
}

func TestGraphLoader_MultiStepFunctionRoundTrip(t *testing.T) {
	proj, _, cfgDir := setup(t)
	fnDir := filepath.Join(cfgDir, "functions")
	tyDir := filepath.Join(cfgDir, "types")

	if err := proj.Sync(ctx(), fnDir, tyDir); err != nil {
		t.Fatal(err)
	}

	function.GraphFunctionLoader = proj
	t.Cleanup(func() { function.GraphFunctionLoader = nil })

	fn, _, err := function.LoadFunction("decompose")
	if err != nil {
		t.Fatalf("LoadFunction decompose: %v", err)
	}
	steps := fn.EffectiveSteps()
	if len(steps) < 2 {
		t.Fatalf("expected at least 2 steps, got %d", len(steps))
	}
	if steps[0].Name != "suggest" {
		t.Fatalf("expected first step 'suggest', got %q", steps[0].Name)
	}
	if steps[1].Name != "generate" {
		t.Fatalf("expected second step 'generate', got %q", steps[1].Name)
	}
	// Suggest should have a gate.
	if steps[0].Gate == nil {
		t.Fatal("suggest step should have a gate")
	}
	// Both should have prompts.
	if steps[0].Backend.PromptTemplate == "" {
		t.Fatal("suggest step should have a prompt")
	}
	if steps[1].Backend.PromptTemplate == "" {
		t.Fatal("generate step should have a prompt")
	}
}

// Import types package for GraphTypeLoader.
var _ = types.GraphTypeLoader
