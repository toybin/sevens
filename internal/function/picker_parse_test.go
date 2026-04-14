package function

import (
	"strings"
	"testing"

	"olympos.io/encoding/edn"

	"sevens/internal/function/picker"
)

// parseRaw is a test helper that unmarshals an EDN string into
// generic any-typed values the parser consumes.
func parseRaw(t *testing.T, s string) any {
	t.Helper()
	var out any
	if err := edn.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("edn.Unmarshal: %v", err)
	}
	return out
}

// ---- Leaf expressions ---------------------------------------------------

func TestParsePickerExpr_TypeKeyword(t *testing.T) {
	raw := parseRaw(t, `:create`)
	expr, err := ParsePickerExpr(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	lit, ok := expr.(picker.LitType)
	if !ok {
		t.Fatalf("expected LitType, got %T", expr)
	}
	if lit.Name != "create" {
		t.Errorf("expected LitType{create}, got %v", lit)
	}
}

func TestParsePickerExpr_StringLiteral(t *testing.T) {
	raw := parseRaw(t, `"hello"`)
	expr, err := ParsePickerExpr(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	lit, ok := expr.(picker.LitStr)
	if !ok {
		t.Fatalf("expected LitStr, got %T", expr)
	}
	if lit.S != "hello" {
		t.Errorf("expected LitStr{hello}, got %v", lit)
	}
}

func TestParsePickerExpr_TargetTitleSymbol(t *testing.T) {
	raw := parseRaw(t, `target-title`)
	expr, err := ParsePickerExpr(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := expr.(picker.TargetTitle); !ok {
		t.Fatalf("expected TargetTitle, got %T", expr)
	}
}

// ---- Call forms ---------------------------------------------------------

func TestParsePickerExpr_ConcatCall(t *testing.T) {
	raw := parseRaw(t, `(concat "Discussion - " target-title)`)
	expr, err := ParsePickerExpr(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, ok := expr.(picker.Concat)
	if !ok {
		t.Fatalf("expected Concat, got %T", expr)
	}
	if len(c.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(c.Parts))
	}
	if _, ok := c.Parts[0].(picker.LitStr); !ok {
		t.Errorf("part 0 should be LitStr, got %T", c.Parts[0])
	}
	if _, ok := c.Parts[1].(picker.TargetTitle); !ok {
		t.Errorf("part 1 should be TargetTitle, got %T", c.Parts[1])
	}
}

func TestParsePickerExpr_IfExistsNode(t *testing.T) {
	raw := parseRaw(t, `(if (exists-node? (concat "Discussion - " target-title)) :edit :create)`)
	expr, err := ParsePickerExpr(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ifExpr, ok := expr.(picker.If)
	if !ok {
		t.Fatalf("expected If, got %T", expr)
	}
	if _, ok := ifExpr.Cond.(picker.ExistsNode); !ok {
		t.Errorf("cond should be ExistsNode, got %T", ifExpr.Cond)
	}
	thenT, ok := ifExpr.Then.(picker.LitType)
	if !ok || thenT.Name != "edit" {
		t.Errorf("then should be LitType{edit}, got %v", ifExpr.Then)
	}
	elseT, ok := ifExpr.Else.(picker.LitType)
	if !ok || elseT.Name != "create" {
		t.Errorf("else should be LitType{create}, got %v", ifExpr.Else)
	}
}

func TestParsePickerExpr_AndOrNot(t *testing.T) {
	raw := parseRaw(t, `(and (exists-node? "x") (not (has-type? :draft)))`)
	expr, err := ParsePickerExpr(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	and, ok := expr.(picker.And)
	if !ok {
		t.Fatalf("expected And, got %T", expr)
	}
	if _, ok := and.A.(picker.ExistsNode); !ok {
		t.Errorf("and.A should be ExistsNode, got %T", and.A)
	}
	notExpr, ok := and.B.(picker.Not)
	if !ok {
		t.Fatalf("and.B should be Not, got %T", and.B)
	}
	ht, ok := notExpr.X.(picker.HasType)
	if !ok || ht.T != "draft" {
		t.Errorf("not.X should be HasType{draft}, got %v", notExpr.X)
	}
}

// ---- OutputPicker top-level --------------------------------------------

func TestParseOutputPicker_Discuss(t *testing.T) {
	raw := parseRaw(t, `
		{:alternatives [:create :edit]
		 :expr (if (exists-node? (concat "Discussion - " target-title))
		           :edit
		           :create)}
	`)
	op, err := ParseOutputPicker(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dep, ok := op.(picker.DependentOutput)
	if !ok {
		t.Fatalf("expected DependentOutput, got %T", op)
	}
	if len(dep.Alternatives) != 2 {
		t.Errorf("expected 2 alternatives, got %d", len(dep.Alternatives))
	}
	if dep.Alternatives[0] != "create" || dep.Alternatives[1] != "edit" {
		t.Errorf("unexpected alternatives: %v", dep.Alternatives)
	}
	if _, ok := dep.Expr.(picker.If); !ok {
		t.Errorf("expr should be If, got %T", dep.Expr)
	}
}

func TestParseOutputPicker_ThenResolveIt(t *testing.T) {
	// Load-time parse + runtime resolve, end to end.
	raw := parseRaw(t, `
		{:alternatives [:create :edit]
		 :expr (if (exists-node? (concat "Discussion - " target-title))
		           :edit
		           :create)}
	`)
	op, err := ParseOutputPicker(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Also run the load-time picker declaration check.
	if err := picker.CheckDeclaration(op); err != nil {
		t.Errorf("CheckDeclaration: %v", err)
	}

	// Resolve with a KB that has the discussion file.
	kb1 := &fakeKB{nodes: map[string]bool{"Discussion - CI/CD Substrate": true}}
	got, err := picker.Resolve(op, picker.EvalContext{
		KB:          kb1,
		TargetTitle: "CI/CD Substrate",
	})
	if err != nil {
		t.Fatalf("resolve (has discussion): %v", err)
	}
	if got != "edit" {
		t.Errorf("with discussion present, expected edit, got %s", got)
	}

	// Resolve with a KB that does NOT have the discussion file.
	kb2 := &fakeKB{nodes: map[string]bool{}}
	got2, err := picker.Resolve(op, picker.EvalContext{
		KB:          kb2,
		TargetTitle: "Braindump",
	})
	if err != nil {
		t.Fatalf("resolve (no discussion): %v", err)
	}
	if got2 != "create" {
		t.Errorf("without discussion, expected create, got %s", got2)
	}
}

// ---- Error cases --------------------------------------------------------

func TestParsePickerExpr_UnknownSymbol(t *testing.T) {
	raw := parseRaw(t, `foobar`)
	_, err := ParsePickerExpr(raw)
	if err == nil {
		t.Fatal("expected error on unknown symbol")
	}
	if !strings.Contains(err.Error(), "foobar") {
		t.Errorf("error should name the symbol, got %v", err)
	}
}

func TestParsePickerExpr_IfArity(t *testing.T) {
	raw := parseRaw(t, `(if true)`)
	_, err := ParsePickerExpr(raw)
	if err == nil {
		t.Fatal("expected arity error")
	}
	if !strings.Contains(err.Error(), "if") {
		t.Errorf("error should name 'if', got %v", err)
	}
}

func TestParseOutputPicker_MissingAlternatives(t *testing.T) {
	raw := parseRaw(t, `{:expr :create}`)
	_, err := ParseOutputPicker(raw)
	if err == nil {
		t.Fatal("expected missing-alternatives error")
	}
	if !strings.Contains(err.Error(), "alternatives") {
		t.Errorf("error should mention alternatives, got %v", err)
	}
}

func TestParseOutputPicker_MissingExpr(t *testing.T) {
	raw := parseRaw(t, `{:alternatives [:create]}`)
	_, err := ParseOutputPicker(raw)
	if err == nil {
		t.Fatal("expected missing-expr error")
	}
	if !strings.Contains(err.Error(), "expr") {
		t.Errorf("error should mention expr, got %v", err)
	}
}

// ---- fakeKB for resolve test -------------------------------------------

type fakeKB struct {
	nodes map[string]bool
}

func (f *fakeKB) ResolveNode(title string) (string, bool) {
	if f.nodes[title] {
		return "", true
	}
	return "", false
}
