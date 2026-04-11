package apply

import (
	"database/sql"
	"strings"
	"testing"

	_ "turso.tech/database/tursogo"

	"sevens/internal/graph"
	"sevens/internal/store"
)

// testDB opens an in-memory SQLite database, initialises the triples schema,
// and registers a cleanup to close it when the test finishes.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	if err := store.InitTriplesSchema(db); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ── SanitizeFilename ─────────────────────────────────────────────────────────

func TestSanitizeFilename_Basic(t *testing.T) {
	got := SanitizeFilename("My Note")
	want := "my note.md"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeFilename_Lowercase(t *testing.T) {
	got := SanitizeFilename("UPPERCASE Title")
	if got != "uppercase title.md" {
		t.Errorf("got %q", got)
	}
}

func TestSanitizeFilename_SpacesPreserved(t *testing.T) {
	got := SanitizeFilename("hello world foo")
	if got != "hello world foo.md" {
		t.Errorf("spaces should be preserved, got %q", got)
	}
}

func TestSanitizeFilename_SpecialCharsStripped(t *testing.T) {
	// All path-unsafe characters should be dropped
	cases := []struct {
		input string
		want  string
	}{
		{"file/name", "filename.md"},
		{"file\\name", "filename.md"},
		{"file:name", "filename.md"},
		{"file*name", "filename.md"},
		{"file?name", "filename.md"},
		{`file"name`, "filename.md"},
		{"file<name", "filename.md"},
		{"file>name", "filename.md"},
		{"file|name", "filename.md"},
	}
	for _, c := range cases {
		got := SanitizeFilename(c.input)
		if got != c.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestSanitizeFilename_AddsMdExtension(t *testing.T) {
	got := SanitizeFilename("note")
	if !strings.HasSuffix(got, ".md") {
		t.Errorf("expected .md suffix, got %q", got)
	}
}

func TestSanitizeFilename_TrimsLeadingTrailingSpaces(t *testing.T) {
	got := SanitizeFilename("  spaced  ")
	if got != "spaced.md" {
		t.Errorf("got %q", got)
	}
}

// ── ParseOps ─────────────────────────────────────────────────────────────────

func TestParseOps_ValidJSONArray(t *testing.T) {
	input := `[{"action":"create","title":"New Note","content":"Hello world"}]`
	ops, err := ParseOps(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Action != "create" {
		t.Errorf("action: got %q, want %q", ops[0].Action, "create")
	}
	if ops[0].Title != "New Note" {
		t.Errorf("title: got %q, want %q", ops[0].Title, "New Note")
	}
}

func TestParseOps_JSONInCodeBlock(t *testing.T) {
	input := "```json\n[{\"action\":\"create\",\"title\":\"A\",\"content\":\"B\"}]\n```"
	ops, err := ParseOps(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 || ops[0].Title != "A" {
		t.Errorf("unexpected ops: %+v", ops)
	}
}

func TestParseOps_JSONInUnlabelledCodeBlock(t *testing.T) {
	input := "```\n[{\"action\":\"create\",\"title\":\"T\",\"content\":\"C\"}]\n```"
	ops, err := ParseOps(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
}

func TestParseOps_MalformedJSON(t *testing.T) {
	_, err := ParseOps("{not valid json}")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestParseOps_EmptyArray(t *testing.T) {
	ops, err := ParseOps("[]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 0 {
		t.Errorf("expected empty ops, got %d", len(ops))
	}
}

func TestParseOps_CreateMissingTitle(t *testing.T) {
	input := `[{"action":"create","title":"","content":"some content"}]`
	_, err := ParseOps(input)
	if err == nil {
		t.Error("expected error for create with empty title")
	}
}

func TestParseOps_CreateMissingContent(t *testing.T) {
	input := `[{"action":"create","title":"T","content":""}]`
	_, err := ParseOps(input)
	if err == nil {
		t.Error("expected error for create with empty content")
	}
}

func TestParseOps_EditMissingFile(t *testing.T) {
	input := `[{"action":"edit","file":"","old_text":"a","new_text":"b"}]`
	_, err := ParseOps(input)
	if err == nil {
		t.Error("expected error for edit with empty file")
	}
}

func TestParseOps_EditMissingOldText(t *testing.T) {
	input := `[{"action":"edit","file":"f.md","old_text":"","new_text":"b"}]`
	_, err := ParseOps(input)
	if err == nil {
		t.Error("expected error for edit with empty old_text")
	}
}

func TestParseOps_EditMissingNewText(t *testing.T) {
	input := `[{"action":"edit","file":"f.md","old_text":"a","new_text":""}]`
	_, err := ParseOps(input)
	if err == nil {
		t.Error("expected error for edit with empty new_text")
	}
}

func TestParseOps_UnknownAction(t *testing.T) {
	input := `[{"action":"delete","file":"f.md"}]`
	_, err := ParseOps(input)
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestParseOps_StripsBrackets(t *testing.T) {
	// [[Title]] notation should be stripped from title and parent fields
	input := `[{"action":"create","title":"[[My Note]]","parent":"[[Parent]]","content":"body"}]`
	ops, err := ParseOps(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ops[0].Title != "My Note" {
		t.Errorf("title brackets not stripped: %q", ops[0].Title)
	}
	if ops[0].Parent != "Parent" {
		t.Errorf("parent brackets not stripped: %q", ops[0].Parent)
	}
}

func TestParseOps_MultipleOps(t *testing.T) {
	input := `[
		{"action":"create","title":"A","content":"a"},
		{"action":"edit","file":"B","old_text":"old","new_text":"new"}
	]`
	ops, err := ParseOps(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 2 {
		t.Errorf("expected 2 ops, got %d", len(ops))
	}
}

// ── RenderStepPrompt ──────────────────────────────────────────────────────────

func TestRenderStepPrompt_AllVariables(t *testing.T) {
	prompt := "Title: {{title}}\nContent: {{content}}\nParent: {{parent}}\nChildren: {{children}}\nPrev: {{prev}}\nContext: {{context}}"
	got := RenderStepPrompt(prompt, "My Title", "My Content", "My Parent", []string{"Child A", "Child B"}, "prev output", "ctx text")

	if !strings.Contains(got, "My Title") {
		t.Error("missing title")
	}
	if !strings.Contains(got, "My Content") {
		t.Error("missing content")
	}
	if !strings.Contains(got, "My Parent") {
		t.Error("missing parent")
	}
	if !strings.Contains(got, "Child A, Child B") {
		t.Error("missing children")
	}
	if !strings.Contains(got, "prev output") {
		t.Error("missing prev")
	}
	if !strings.Contains(got, "ctx text") {
		t.Error("missing context")
	}
}

func TestRenderStepPrompt_EmptyParentBecomesNone(t *testing.T) {
	got := RenderStepPrompt("Parent: {{parent}}", "t", "c", "", nil, "", "")
	if !strings.Contains(got, "none") {
		t.Errorf("empty parent should become 'none', got: %q", got)
	}
}

func TestRenderStepPrompt_EmptyChildrenBecomesNone(t *testing.T) {
	got := RenderStepPrompt("Children: {{children}}", "t", "c", "p", nil, "", "")
	if !strings.Contains(got, "none") {
		t.Errorf("empty children should become 'none', got: %q", got)
	}
}

func TestRenderStepPrompt_ChildrenJoinedWithComma(t *testing.T) {
	got := RenderStepPrompt("{{children}}", "t", "c", "p", []string{"A", "B", "C"}, "", "")
	if !strings.Contains(got, "A, B, C") {
		t.Errorf("children not joined correctly: %q", got)
	}
}

func TestRenderStepPrompt_NoSubstitutions(t *testing.T) {
	prompt := "no template vars here"
	got := RenderStepPrompt(prompt, "t", "c", "p", []string{"ch"}, "prev", "ctx")
	if got != prompt {
		t.Errorf("prompt without vars should be unchanged, got %q", got)
	}
}

func TestRenderPrompt_DelegatesToRenderStepPrompt(t *testing.T) {
	fn := &Function{Prompt: "Hello {{title}}"}
	got := RenderPrompt(fn, "World", "content", "parent", nil)
	if got != "Hello World" {
		t.Errorf("got %q", got)
	}
}

func TestRenderStepPromptWithVars_BlockTarget(t *testing.T) {
	got := RenderStepPromptWithVars(
		"Target={{target-label}}\nKind={{target-kind}}\nNode={{node-title}}\nPath={{block-path}}\nScope={{block-scope}}\nContent={{content}}",
		PromptVars{
			Title:       "Braindump",
			Content:     "- [!!] decide stable block identity",
			NodeTitle:   "Braindump",
			NodeContent: "# Braindump\n- [!!] decide stable block identity",
			TargetKind:  "block",
			TargetLabel: "Braindump#1.0",
			BlockPath:   "1.0",
			BlockScope:  "Today > Blocked",
		},
	)
	for _, want := range []string{
		"Target=Braindump#1.0",
		"Kind=block",
		"Node=Braindump",
		"Path=1.0",
		"Scope=Today > Blocked",
		"Content=- [!!] decide stable block identity",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

// ── LookupPricing ─────────────────────────────────────────────────────────────

func TestLookupPricing_KnownModels(t *testing.T) {
	cases := []struct {
		model        string
		wantInputPM  float64
		wantOutputPM float64
	}{
		{"claude-opus-4", 15.0, 75.0},
		{"claude-sonnet-4", 3.0, 15.0},
		{"claude-haiku-4", 0.80, 4.0},
	}
	for _, c := range cases {
		p, ok := LookupPricing(c.model)
		if !ok {
			t.Errorf("LookupPricing(%q): expected found=true", c.model)
			continue
		}
		if p.InputPerMillion != c.wantInputPM {
			t.Errorf("%q InputPerMillion: got %v, want %v", c.model, p.InputPerMillion, c.wantInputPM)
		}
		if p.OutputPerMillion != c.wantOutputPM {
			t.Errorf("%q OutputPerMillion: got %v, want %v", c.model, p.OutputPerMillion, c.wantOutputPM)
		}
	}
}

func TestLookupPricing_UnknownModel(t *testing.T) {
	_, ok := LookupPricing("gpt-4o")
	if ok {
		t.Error("expected found=false for unknown model")
	}
}

func TestLookupPricing_PrefixMatching(t *testing.T) {
	// Model IDs often include version suffixes — prefix matching should still find them
	cases := []string{
		"claude-opus-4-5",
		"claude-opus-4-20250514",
		"claude-sonnet-4-5",
		"claude-haiku-4-20250307",
	}
	for _, model := range cases {
		_, ok := LookupPricing(model)
		if !ok {
			t.Errorf("LookupPricing(%q): prefix match should succeed", model)
		}
	}
}

func TestLookupPricing_EmptyString(t *testing.T) {
	_, ok := LookupPricing("")
	if ok {
		t.Error("expected found=false for empty model string")
	}
}

// ── Function.EffectiveSteps ───────────────────────────────────────────────────

func TestEffectiveSteps_SinglePrompt(t *testing.T) {
	fn := &Function{
		Prompt: "do the thing",
		Input:  "node",
		Output: "ops",
	}
	steps := fn.EffectiveSteps()
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Name != "default" {
		t.Errorf("step name: got %q, want %q", steps[0].Name, "default")
	}
	if steps[0].Prompt != "do the thing" {
		t.Errorf("step prompt: got %q", steps[0].Prompt)
	}
	if steps[0].Input != "node" {
		t.Errorf("step input: got %q", steps[0].Input)
	}
	if steps[0].Output != "ops" {
		t.Errorf("step output: got %q", steps[0].Output)
	}
}

func TestEffectiveSteps_PipelineSteps(t *testing.T) {
	fn := &Function{
		Steps: []Step{
			{Name: "analyze", Prompt: "step 1", Output: "text"},
			{Name: "write", Prompt: "step 2", Input: "text", Output: "ops"},
		},
	}
	steps := fn.EffectiveSteps()
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].Name != "analyze" {
		t.Errorf("step 0 name: %q", steps[0].Name)
	}
	if steps[1].Name != "write" {
		t.Errorf("step 1 name: %q", steps[1].Name)
	}
}

func TestEffectiveSteps_StepsPreferredOverPrompt(t *testing.T) {
	fn := &Function{
		Prompt: "ignored",
		Steps: []Step{
			{Name: "only", Prompt: "used"},
		},
	}
	steps := fn.EffectiveSteps()
	if len(steps) != 1 || steps[0].Prompt != "used" {
		t.Errorf("steps should be preferred over top-level prompt")
	}
}

// ── Function.ValidateComposition ─────────────────────────────────────────────

func TestValidateComposition_MatchingTypes(t *testing.T) {
	fn := &Function{
		Steps: []Step{
			{Name: "a", Output: "text"},
			{Name: "b", Input: "text", Output: "ops"},
		},
	}
	if err := fn.ValidateComposition(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposition_MismatchedTypes(t *testing.T) {
	fn := &Function{
		Steps: []Step{
			{Name: "a", Output: "ops"},
			{Name: "b", Input: "text"},
		},
	}
	if err := fn.ValidateComposition(); err == nil {
		t.Error("expected composition error for mismatched types")
	}
}

func TestValidateComposition_SingleStep(t *testing.T) {
	fn := &Function{Steps: []Step{{Name: "only"}}}
	if err := fn.ValidateComposition(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposition_FnStepsSkipTypeCheck(t *testing.T) {
	// Steps that delegate via :fn should not be type-checked against each other
	fn := &Function{
		Steps: []Step{
			{Name: "a", Fn: "other-function", Output: "ops"},
			{Name: "b", Input: "text"},
		},
	}
	if err := fn.ValidateComposition(); err != nil {
		t.Errorf("fn-delegating steps should skip type check, got: %v", err)
	}
}

// ── GlobalConfig.ResolveModel ─────────────────────────────────────────────────

func TestResolveModel_EmptyName(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4", APIKeyEnv: "ANTHROPIC_API_KEY"},
	}
	got := cfg.ResolveModel("")
	if got.Model != "claude-sonnet-4" {
		t.Errorf("empty name should return default LLM, got model %q", got.Model)
	}
}

func TestResolveModel_NamedProfile(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4", APIKeyEnv: "KEY"},
		Models: map[string]LLMConfig{
			"fast": {Model: "claude-haiku-4"},
		},
	}
	got := cfg.ResolveModel("fast")
	if got.Model != "claude-haiku-4" {
		t.Errorf("named profile model: got %q", got.Model)
	}
	// Provider and APIKeyEnv should be inherited from the default
	if got.Provider != "anthropic" {
		t.Errorf("provider should be inherited, got %q", got.Provider)
	}
	if got.APIKeyEnv != "KEY" {
		t.Errorf("api-key-env should be inherited, got %q", got.APIKeyEnv)
	}
}

func TestResolveModel_MissingProfile(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4"},
	}
	got := cfg.ResolveModel("nonexistent")
	if got.Model != "claude-sonnet-4" {
		t.Errorf("missing profile should fall back to default, got %q", got.Model)
	}
}

func TestResolveModel_ProfileOverridesModel(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4", APIKey: "sk-default"},
		Models: map[string]LLMConfig{
			"powerful": {Model: "claude-opus-4", APIKey: "sk-opus"},
		},
	}
	got := cfg.ResolveModel("powerful")
	if got.Model != "claude-opus-4" {
		t.Errorf("profile model not applied, got %q", got.Model)
	}
	// Profile provides its own API key, so it should NOT inherit default
	if got.APIKey != "sk-opus" {
		t.Errorf("profile api key: got %q", got.APIKey)
	}
}

func TestResolveModel_ProfileInheritsProvider(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4"},
		Models: map[string]LLMConfig{
			"fast": {Model: "claude-haiku-4", Provider: ""},
		},
	}
	got := cfg.ResolveModel("fast")
	if got.Provider != "anthropic" {
		t.Errorf("empty provider in profile should inherit default, got %q", got.Provider)
	}
}

// ── EffectiveRequires ─────────────────────────────────────────────────────────

func TestEffectiveRequires_StepLevelTakesPrecedence(t *testing.T) {
	fn := &Function{
		Requires: []Require{
			{Role: "parent", Type: "node"},
			{Role: "children", Type: "node[]"},
		},
	}
	step := &Step{
		Requires: []Require{
			{Role: "parent", Type: "node", Optional: true}, // overrides function-level
		},
	}
	reqs := EffectiveRequires(fn, step)

	// Should have parent (from step) + children (from function)
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requires, got %d: %+v", len(reqs), reqs)
	}

	// First should be step-level parent (Optional: true)
	if reqs[0].Role != "parent" || !reqs[0].Optional {
		t.Errorf("step-level parent should be optional, got: %+v", reqs[0])
	}
}

func TestEffectiveRequires_FunctionLevelFillsMissing(t *testing.T) {
	fn := &Function{
		Requires: []Require{
			{Role: "parent", Type: "node"},
			{Role: "siblings", Type: "node[]"},
		},
	}
	step := &Step{} // no step-level requires
	reqs := EffectiveRequires(fn, step)

	if len(reqs) != 2 {
		t.Fatalf("expected 2 requires from function level, got %d", len(reqs))
	}
}

func TestEffectiveRequires_NoRequires(t *testing.T) {
	fn := &Function{}
	step := &Step{}
	reqs := EffectiveRequires(fn, step)
	if len(reqs) != 0 {
		t.Errorf("expected empty requires, got %d", len(reqs))
	}
}

func TestEffectiveRequires_OnlyStepRequires(t *testing.T) {
	fn := &Function{}
	step := &Step{
		Requires: []Require{{Role: "target", Type: "node"}},
	}
	reqs := EffectiveRequires(fn, step)
	if len(reqs) != 1 || reqs[0].Role != "target" {
		t.Errorf("unexpected requires: %+v", reqs)
	}
}

// ── HasRequires ───────────────────────────────────────────────────────────────

func TestHasRequires_WithFunctionLevelRequires(t *testing.T) {
	fn := &Function{
		Requires: []Require{{Role: "parent", Type: "node"}},
	}
	if !HasRequires(fn) {
		t.Error("expected HasRequires=true for function with requires")
	}
}

func TestHasRequires_WithStepLevelRequires(t *testing.T) {
	fn := &Function{
		Steps: []Step{
			{Name: "s", Requires: []Require{{Role: "children", Type: "node[]"}}},
		},
	}
	if !HasRequires(fn) {
		t.Error("expected HasRequires=true for step with requires")
	}
}

func TestHasRequires_WithContext(t *testing.T) {
	fn := &Function{
		Context: []PathSpec{{Path: []string{"node/parent"}, As: "parent"}},
	}
	if !HasRequires(fn) {
		t.Error("expected HasRequires=true for function with context paths")
	}
}

func TestHasRequires_WithCrossWalk(t *testing.T) {
	fn := &Function{CrossWalk: "other-function"}
	if !HasRequires(fn) {
		t.Error("expected HasRequires=true for function with cross-walk")
	}
}

func TestHasRequires_WithoutRequires(t *testing.T) {
	fn := &Function{
		Prompt: "just a prompt",
		Steps:  []Step{{Name: "s", Prompt: "step prompt"}},
	}
	if HasRequires(fn) {
		t.Error("expected HasRequires=false for function with no requires/context/cross-walk")
	}
}

func TestHasRequires_EmptyFunction(t *testing.T) {
	fn := &Function{}
	if HasRequires(fn) {
		t.Error("expected HasRequires=false for empty function")
	}
}

func TestResolveContext_PathSpecsUseSubjects(t *testing.T) {
	db := testDB(t)
	root := "/tmp/root"

	parentSubject := store.NodeSubject(root, "Parent")
	childSubject := store.NodeSubject(root, "Discussion - Parent")
	if err := store.InsertTriples(db, []store.Triple{
		{Subject: parentSubject, Predicate: "node/root", Object: root},
		{Subject: parentSubject, Predicate: "node/title", Object: "Parent"},
		{Subject: parentSubject, Predicate: "node/content", Object: "Parent body"},
		{Subject: childSubject, Predicate: "node/root", Object: root},
		{Subject: childSubject, Predicate: "node/title", Object: "Discussion - Parent"},
		{Subject: childSubject, Predicate: "node/content", Object: "Discussion body"},
		{Subject: childSubject, Predicate: "node/parent", Object: parentSubject},
	}); err != nil {
		t.Fatalf("insert triples: %v", err)
	}

	walk := &graph.WalkOutput{
		Node: graph.WalkNode{
			Subject:  parentSubject,
			Title:    "Parent",
			Content:  "Parent body",
			Children: []string{"Discussion - Parent"},
		},
	}
	fn := &Function{
		Context: []PathSpec{{
			Path: []string{"node/parent~"},
			With: []string{"node/content"},
			As:   "children",
		}},
	}
	step := &Step{Name: "default"}

	ctx, err := ResolveContext(db, root, fn, step, walk, nil)
	if err != nil {
		t.Fatalf("ResolveContext returned error: %v", err)
	}
	if len(ctx.Children) != 1 {
		t.Fatalf("children = %d, want 1", len(ctx.Children))
	}
	if ctx.Children[0].Title != "Discussion - Parent" {
		t.Fatalf("child title = %q, want %q", ctx.Children[0].Title, "Discussion - Parent")
	}
	if ctx.Children[0].Content != "Discussion body" {
		t.Fatalf("child content = %q, want %q", ctx.Children[0].Content, "Discussion body")
	}
}

// ── FormatHistory ─────────────────────────────────────────────────────────────

func TestFormatHistory_EmptyEntries(t *testing.T) {
	got := FormatHistory(nil)
	if got != "" {
		t.Errorf("empty entries should return empty string, got %q", got)
	}
}

func TestFormatHistory_CompletedEvent(t *testing.T) {
	entries := []LogEntry{
		{
			Event:     "completed",
			Function:  "summarize",
			Step:      "default",
			Timestamp: "2026-01-01T10:00:00Z",
			Summary:   "Summarized the node content",
		},
	}
	got := FormatHistory(entries)
	if !strings.Contains(got, "<history>") {
		t.Error("missing <history> tag")
	}
	if !strings.Contains(got, "</history>") {
		t.Error("missing </history> closing tag")
	}
	if !strings.Contains(got, "summarize/default") {
		t.Error("missing function/step")
	}
	if !strings.Contains(got, "Summarized the node content") {
		t.Error("missing summary text")
	}
	if !strings.Contains(got, "2026-01-01T10:00:00Z") {
		t.Error("missing timestamp")
	}
}

func TestFormatHistory_SuggestedEvent(t *testing.T) {
	entries := []LogEntry{
		{
			Event:     "suggested",
			Function:  "expand",
			Step:      "write",
			Timestamp: "2026-01-02T09:00:00Z",
			Summary:   "Wrote new sections",
		},
	}
	got := FormatHistory(entries)
	if !strings.Contains(got, "expand/write") {
		t.Errorf("missing function/step in suggested entry: %q", got)
	}
}

func TestFormatHistory_AppliedEvent(t *testing.T) {
	entries := []LogEntry{
		{
			Event:     "applied",
			Function:  "expand",
			Timestamp: "2026-01-03T12:00:00Z",
			Commit:    "abc1234",
		},
	}
	got := FormatHistory(entries)
	if !strings.Contains(got, "applied expand") {
		t.Errorf("missing applied text: %q", got)
	}
	if !strings.Contains(got, "abc1234") {
		t.Errorf("missing commit hash: %q", got)
	}
}

func TestFormatHistory_RawOutputFallback(t *testing.T) {
	// When summary is empty and raw output is short, raw output should appear
	entries := []LogEntry{
		{
			Event:     "completed",
			Function:  "f",
			Step:      "s",
			Timestamp: "2026-01-01T00:00:00Z",
			RawOutput: "short raw output",
		},
	}
	got := FormatHistory(entries)
	if !strings.Contains(got, "short raw output") {
		t.Errorf("expected raw output as fallback: %q", got)
	}
}

func TestFormatHistory_RawOutputTruncated(t *testing.T) {
	long := strings.Repeat("x", 300)
	entries := []LogEntry{
		{
			Event:     "completed",
			Function:  "f",
			Step:      "s",
			Timestamp: "2026-01-01T00:00:00Z",
			RawOutput: long,
		},
	}
	got := FormatHistory(entries)
	if !strings.Contains(got, "...") {
		t.Errorf("long raw output should be truncated with ...: %q", got[:50])
	}
}

func TestFormatHistory_MultipleEntries(t *testing.T) {
	entries := []LogEntry{
		{Event: "completed", Function: "f1", Step: "s", Timestamp: "2026-01-01T00:00:00Z", Summary: "first"},
		{Event: "applied", Function: "f1", Timestamp: "2026-01-02T00:00:00Z", Commit: "deadbeef"},
	}
	got := FormatHistory(entries)
	if !strings.Contains(got, "first") {
		t.Error("missing first entry")
	}
	if !strings.Contains(got, "deadbeef") {
		t.Error("missing second entry")
	}
}

// ── AppendLogDB / ReadLogDB ───────────────────────────────────────────────────

func TestAppendLogDB_BasicRoundTrip(t *testing.T) {
	db := testDB(t)

	entry := LogEntry{
		Event:     "completed",
		Function:  "summarize",
		Target:    "My Node",
		Step:      "default",
		StepIndex: 0,
		Timestamp: "2026-01-01T10:00:00Z",
		Summary:   "did some work",
	}
	if err := AppendLogDB(db, entry); err != nil {
		t.Fatalf("AppendLogDB: %v", err)
	}

	entries, err := ReadLogDB(db, "My Node")
	if err != nil {
		t.Fatalf("ReadLogDB: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	got := entries[0]
	if got.Event != "completed" {
		t.Errorf("event: got %q, want %q", got.Event, "completed")
	}
	if got.Function != "summarize" {
		t.Errorf("function: got %q", got.Function)
	}
	if got.Target != "My Node" {
		t.Errorf("target: got %q", got.Target)
	}
	if got.Summary != "did some work" {
		t.Errorf("summary: got %q", got.Summary)
	}
}

func TestAppendLogDB_MultipleEntries(t *testing.T) {
	db := testDB(t)

	for i, ts := range []string{"2026-01-01T10:00:00Z", "2026-01-02T10:00:00Z"} {
		_ = i
		if err := AppendLogDB(db, LogEntry{
			Event:     "completed",
			Function:  "f",
			Target:    "Node",
			Step:      "s",
			Timestamp: ts,
			Summary:   ts,
		}); err != nil {
			t.Fatalf("AppendLogDB: %v", err)
		}
	}

	entries, err := ReadLogDB(db, "Node")
	if err != nil {
		t.Fatalf("ReadLogDB: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestReadLogDB_NoEntries(t *testing.T) {
	db := testDB(t)
	entries, err := ReadLogDB(db, "Nonexistent Node")
	if err != nil {
		t.Fatalf("ReadLogDB: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestAppendLogDB_WithRawOutput(t *testing.T) {
	db := testDB(t)
	entry := LogEntry{
		Event:     "suggested",
		Function:  "expand",
		Target:    "Target Node",
		Step:      "write",
		Timestamp: "2026-02-01T00:00:00Z",
		RawOutput: "some llm output here",
	}
	if err := AppendLogDB(db, entry); err != nil {
		t.Fatalf("AppendLogDB: %v", err)
	}
	entries, err := ReadLogDB(db, "Target Node")
	if err != nil {
		t.Fatalf("ReadLogDB: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].RawOutput != "some llm output here" {
		t.Errorf("raw output: got %q", entries[0].RawOutput)
	}
}

func TestAppendLogDB_WithCommitAndFiles(t *testing.T) {
	db := testDB(t)
	entry := LogEntry{
		Event:        "applied",
		Function:     "expand",
		Target:       "N",
		Timestamp:    "2026-03-01T00:00:00Z",
		Commit:       "abc123",
		FilesCreated: []string{"new-note.md"},
		FilesEdited:  []string{"existing.md"},
	}
	if err := AppendLogDB(db, entry); err != nil {
		t.Fatalf("AppendLogDB: %v", err)
	}
	entries, err := ReadLogDB(db, "N")
	if err != nil {
		t.Fatalf("ReadLogDB: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	got := entries[0]
	if got.Commit != "abc123" {
		t.Errorf("commit: got %q", got.Commit)
	}
	if len(got.FilesCreated) != 1 || got.FilesCreated[0] != "new-note.md" {
		t.Errorf("files created: %v", got.FilesCreated)
	}
	if len(got.FilesEdited) != 1 || got.FilesEdited[0] != "existing.md" {
		t.Errorf("files edited: %v", got.FilesEdited)
	}
}

func TestAppendLogDB_WithOps(t *testing.T) {
	db := testDB(t)
	entry := LogEntry{
		Event:     "completed",
		Function:  "f",
		Target:    "T",
		Timestamp: "2026-04-01T00:00:00Z",
		Ops: []FileOp{
			{Action: "create", Title: "New Note", Content: "body"},
		},
	}
	if err := AppendLogDB(db, entry); err != nil {
		t.Fatalf("AppendLogDB: %v", err)
	}
	entries, err := ReadLogDB(db, "T")
	if err != nil {
		t.Fatalf("ReadLogDB: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if len(entries[0].Ops) != 1 || entries[0].Ops[0].Title != "New Note" {
		t.Errorf("ops not round-tripped: %+v", entries[0].Ops)
	}
}

func TestReadLogDB_IsolatesNodeEntries(t *testing.T) {
	db := testDB(t)

	_ = AppendLogDB(db, LogEntry{Event: "completed", Function: "f", Target: "Node A", Timestamp: "2026-01-01T00:00:00Z", Summary: "A"})
	_ = AppendLogDB(db, LogEntry{Event: "completed", Function: "f", Target: "Node B", Timestamp: "2026-01-02T00:00:00Z", Summary: "B"})

	entriesA, _ := ReadLogDB(db, "Node A")
	entriesB, _ := ReadLogDB(db, "Node B")

	if len(entriesA) != 1 {
		t.Errorf("Node A: expected 1 entry, got %d", len(entriesA))
	}
	if len(entriesB) != 1 {
		t.Errorf("Node B: expected 1 entry, got %d", len(entriesB))
	}
	if len(entriesA) > 0 && entriesA[0].Summary != "A" {
		t.Errorf("Node A entry has wrong summary: %q", entriesA[0].Summary)
	}
}
