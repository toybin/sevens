package picker

import (
	"strings"
	"testing"

	"sevens/internal/types/kernel"
)

// ========================================================================
// Fixtures
// ========================================================================

// testKB is a small in-memory KB for picker evaluation tests.
type testKB struct {
	nodes map[string]string
}

func (k *testKB) ResolveNode(title string) (string, bool) {
	content, ok := k.nodes[title]
	return content, ok
}

func newKBWith(entries map[string]string) *testKB {
	return &testKB{nodes: entries}
}

// kbWithDiscussion and kbWithoutDiscussion model the discuss routing case.
func kbWithDiscussion() *testKB {
	return newKBWith(map[string]string{
		"Discussion - CI/CD Substrate": "# Discussion\n",
	})
}
func kbWithoutDiscussion() *testKB {
	return newKBWith(map[string]string{})
}

func ctxFor(kb kernel.KB, title string, conforms ...kernel.TypeName) EvalContext {
	set := make(map[kernel.TypeName]struct{}, len(conforms))
	for _, t := range conforms {
		set[t] = struct{}{}
	}
	return EvalContext{
		KB:             kb,
		TargetTitle:    title,
		TargetConforms: set,
	}
}

// ---- Example pickers ------------------------------------------------

// discussPicker is the canonical dependent-output picker: route based
// on whether a "Discussion - <title>" child exists in the KB.
var discussPicker = DependentOutput{
	Name: "discuss-router",
	Alternatives: []kernel.TypeName{
		"discussion-turn",
		"discussion-start",
	},
	Expr: If{
		Cond: ExistsNode{
			Title: Concat{Parts: []Expr{
				LitStr{S: "Discussion - "},
				TargetTitle{},
			}},
		},
		Then: LitType{Name: "discussion-turn"},
		Else: LitType{Name: "discussion-start"},
	},
}

// brokenPicker: expression can return a type NOT in Alternatives.
var brokenPicker = DependentOutput{
	Name:         "broken",
	Alternatives: []kernel.TypeName{"discussion-turn"},
	Expr: If{
		Cond: HasType{T: "nothing"},
		Then: LitType{Name: "discussion-turn"},
		Else: LitType{Name: "discussion-start"},
	},
}

// complexPicker: route to discussion-turn if discussion exists
// AND target is not already a draft.
var complexPicker = DependentOutput{
	Name: "complex",
	Alternatives: []kernel.TypeName{
		"discussion-turn",
		"discussion-start",
	},
	Expr: If{
		Cond: And{
			A: ExistsNode{
				Title: Concat{Parts: []Expr{
					LitStr{S: "Discussion - "},
					TargetTitle{},
				}},
			},
			B: Not{X: HasType{T: "draft"}},
		},
		Then: LitType{Name: "discussion-turn"},
		Else: LitType{Name: "discussion-start"},
	},
}

// ========================================================================
// Evaluator basics
// ========================================================================

func TestEval_LitType(t *testing.T) {
	v, err := Eval(LitType{Name: "x"}, ctxFor(nil, ""))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != (VType{Name: "x"}) {
		t.Errorf("expected VType{x}, got %v", v)
	}
}

func TestEval_LitStr(t *testing.T) {
	v, err := Eval(LitStr{S: "hello"}, ctxFor(nil, ""))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != (VString{S: "hello"}) {
		t.Errorf("expected VString{hello}, got %v", v)
	}
}

func TestEval_TargetTitle(t *testing.T) {
	v, err := Eval(TargetTitle{}, ctxFor(nil, "Braindump"))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != (VString{S: "Braindump"}) {
		t.Errorf("expected VString{Braindump}, got %v", v)
	}
}

func TestEval_Concat(t *testing.T) {
	e := Concat{Parts: []Expr{
		LitStr{S: "a"},
		LitStr{S: "b"},
		LitStr{S: "c"},
	}}
	v, err := Eval(e, ctxFor(nil, ""))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != (VString{S: "abc"}) {
		t.Errorf("expected abc, got %v", v)
	}
}

func TestEval_Eq_Strings(t *testing.T) {
	e := Eq{A: LitStr{S: "x"}, B: LitStr{S: "x"}}
	v, err := Eval(e, ctxFor(nil, ""))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != (VBool{B: true}) {
		t.Errorf("expected true, got %v", v)
	}
}

// ========================================================================
// KB queries
// ========================================================================

func TestEval_ExistsNode_True(t *testing.T) {
	e := ExistsNode{
		Title: Concat{Parts: []Expr{
			LitStr{S: "Discussion - "},
			TargetTitle{},
		}},
	}
	v, err := Eval(e, ctxFor(kbWithDiscussion(), "CI/CD Substrate"))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != (VBool{B: true}) {
		t.Errorf("expected true, got %v", v)
	}
}

func TestEval_ExistsNode_False(t *testing.T) {
	e := ExistsNode{
		Title: Concat{Parts: []Expr{
			LitStr{S: "Discussion - "},
			TargetTitle{},
		}},
	}
	v, err := Eval(e, ctxFor(kbWithoutDiscussion(), "Braindump"))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != (VBool{B: false}) {
		t.Errorf("expected false, got %v", v)
	}
}

// ========================================================================
// Discuss picker end-to-end
// ========================================================================

func TestDiscussPicker_WithDiscussion(t *testing.T) {
	got, err := Resolve(discussPicker, ctxFor(kbWithDiscussion(), "CI/CD Substrate"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "discussion-turn" {
		t.Errorf("expected discussion-turn, got %s", got)
	}
}

func TestDiscussPicker_WithoutDiscussion(t *testing.T) {
	got, err := Resolve(discussPicker, ctxFor(kbWithoutDiscussion(), "Braindump"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "discussion-start" {
		t.Errorf("expected discussion-start, got %s", got)
	}
}

// ========================================================================
// HasType
// ========================================================================

func TestEval_HasType_True(t *testing.T) {
	v, err := Eval(HasType{T: "draft"}, ctxFor(nil, "n", "draft"))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != (VBool{B: true}) {
		t.Errorf("expected true, got %v", v)
	}
}

func TestEval_HasType_False(t *testing.T) {
	v, err := Eval(HasType{T: "draft"}, ctxFor(nil, "n"))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != (VBool{B: false}) {
		t.Errorf("expected false, got %v", v)
	}
}

// ========================================================================
// Complex picker: conjunction
// ========================================================================

func TestComplexPicker_NormalTargetWithDiscussion(t *testing.T) {
	got, err := Resolve(complexPicker, ctxFor(kbWithDiscussion(), "CI/CD Substrate"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "discussion-turn" {
		t.Errorf("expected discussion-turn, got %s", got)
	}
}

func TestComplexPicker_DraftTargetWithDiscussion(t *testing.T) {
	got, err := Resolve(complexPicker, ctxFor(kbWithDiscussion(), "CI/CD Substrate", "draft"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "discussion-start" {
		t.Errorf("expected discussion-start (draft bypass), got %s", got)
	}
}

// ========================================================================
// Load-time checks
// ========================================================================

func TestCheckDeclaration_DiscussPassesLoad(t *testing.T) {
	if err := CheckDeclaration(discussPicker); err != nil {
		t.Errorf("expected discussPicker to pass load check, got %v", err)
	}
}

func TestCheckDeclaration_BrokenFailsLoad(t *testing.T) {
	err := CheckDeclaration(brokenPicker)
	if err == nil {
		t.Fatal("expected broken picker to fail load check")
	}
	if !strings.Contains(err.Error(), "not in declared alternatives") {
		t.Errorf("expected 'not in declared alternatives', got %v", err)
	}
}

func TestCheckDeclaration_StaticAlwaysPasses(t *testing.T) {
	if err := CheckDeclaration(StaticOutput{T: "whatever"}); err != nil {
		t.Errorf("static output should always pass, got %v", err)
	}
}

// ========================================================================
// Runtime picker contract: return NOT in alternatives
// ========================================================================

func TestResolve_BrokenCaughtAtRuntime(t *testing.T) {
	_, err := Resolve(brokenPicker, ctxFor(kbWithoutDiscussion(), "Braindump"))
	if err == nil {
		t.Fatal("expected runtime picker error")
	}
	if !strings.Contains(err.Error(), "not in declared alternatives") {
		t.Errorf("expected 'not in declared alternatives', got %v", err)
	}
}

// Picker expression that evaluates to a string instead of a type.
func TestResolve_ExprMustReturnType(t *testing.T) {
	bad := DependentOutput{
		Name:         "bad",
		Alternatives: []kernel.TypeName{"x"},
		Expr:         LitStr{S: "oops"},
	}
	_, err := Resolve(bad, ctxFor(nil, ""))
	if err == nil {
		t.Fatal("expected error from non-type return")
	}
	if !strings.Contains(err.Error(), "must evaluate to a type") {
		t.Errorf("expected 'must evaluate to a type', got %v", err)
	}
}

// ========================================================================
// Static analysis
// ========================================================================

func TestPossibleReturnTypes_Discuss(t *testing.T) {
	set, ok := PossibleReturnTypes(discussPicker.Expr)
	if !ok {
		t.Fatal("expected bounded return set")
	}
	if len(set) != 2 {
		t.Fatalf("expected 2 possible types, got %d", len(set))
	}
	if _, has := set["discussion-turn"]; !has {
		t.Errorf("missing discussion-turn")
	}
	if _, has := set["discussion-start"]; !has {
		t.Errorf("missing discussion-start")
	}
}

func TestPossibleReturnTypes_PriorOutputIsUnbounded(t *testing.T) {
	_, ok := PossibleReturnTypes(PriorOutputType{Index: 0})
	if ok {
		t.Error("expected PriorOutputType to be unbounded")
	}
}
