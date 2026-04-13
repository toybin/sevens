// Package picker is a small expression language for dependent output
// type pickers. It is the Go port of docs/sketch/PickerExpressions.hs.
//
// A picker expression is pure data: no function closures, no user-
// defined primitives, no general lambda. The vocabulary is closed and
// fixed — every constructor corresponds to a named primitive the
// executor understands. This makes expressions:
//
//   - Inspectable (you can print them)
//   - Serializable (EDN maps 1:1 to the constructors)
//   - Statically analyzable (possibleReturnTypes walks the AST and
//     enumerates the set of type literals it might return)
//
// A DependentOutput declares its set of possible resolutions up
// front; the picker expression must evaluate to one of them. This is
// the move that makes multi-step pipeline type-checking decidable
// even when dispatch is data-dependent.
package picker

import (
	"fmt"

	"sevens/internal/types/kernel"
)

// === Evaluation context =================================================
//
// A picker expression reads from a tiny, fixed slice of the world:
// the KB (for existence checks on titled nodes), the target's title
// and conformance set, and the list of prior step output types.
// Anything outside this surface cannot influence picker evaluation —
// which is why static analysis can bound the possible outputs.

// EvalContext is what the picker evaluator consumes. It is a narrower
// slice than the function layer's full StepContext.
type EvalContext struct {
	KB              kernel.KB
	TargetTitle     string
	TargetConforms  map[kernel.TypeName]struct{}
	PriorOutputType []kernel.TypeName // index 0 = first completed step
}

// HasTargetType reports whether the target conforms to the given type.
func (c EvalContext) HasTargetType(t kernel.TypeName) bool {
	if c.TargetConforms == nil {
		return false
	}
	_, ok := c.TargetConforms[t]
	return ok
}

// === Expression AST =====================================================
//
// Expr is a sealed interface. Only the concrete constructors below
// may implement it (the unexported isExpr method enforces this).

// Expr is a picker expression node.
type Expr interface {
	isExpr()
}

// LitType is a type-name literal. Evaluates to VType.
type LitType struct{ Name kernel.TypeName }

// LitStr is a string literal. Evaluates to VString.
type LitStr struct{ S string }

// If is a conditional. Cond must evaluate to VBool.
type If struct{ Cond, Then, Else Expr }

// And is a short-circuiting boolean conjunction.
type And struct{ A, B Expr }

// Or is a short-circuiting boolean disjunction.
type Or struct{ A, B Expr }

// Not negates a boolean.
type Not struct{ X Expr }

// Eq compares two same-typed values for equality.
type Eq struct{ A, B Expr }

// Concat joins a list of string expressions.
type Concat struct{ Parts []Expr }

// TargetTitle evaluates to the target node's title.
type TargetTitle struct{}

// ExistsNode queries the KB: does a node with the computed title exist?
type ExistsNode struct{ Title Expr }

// HasType queries the target's conformance set for a specific type.
type HasType struct{ T kernel.TypeName }

// PriorOutputType returns the type of step i's prior output. If i is
// out of range the evaluator returns an error.
type PriorOutputType struct{ Index int }

func (LitType) isExpr()         {}
func (LitStr) isExpr()          {}
func (If) isExpr()              {}
func (And) isExpr()             {}
func (Or) isExpr()              {}
func (Not) isExpr()             {}
func (Eq) isExpr()              {}
func (Concat) isExpr()          {}
func (TargetTitle) isExpr()     {}
func (ExistsNode) isExpr()      {}
func (HasType) isExpr()         {}
func (PriorOutputType) isExpr() {}

// === Values =============================================================

// Value is the result of evaluating an Expr. Sealed via isValue.
type Value interface {
	isValue()
}

// VType is a type-name value.
type VType struct{ Name kernel.TypeName }

// VString is a string value.
type VString struct{ S string }

// VBool is a boolean value.
type VBool struct{ B bool }

func (VType) isValue()   {}
func (VString) isValue() {}
func (VBool) isValue()   {}

// === Evaluator ==========================================================

// Eval evaluates an expression in the given context. Returns an error
// for type mismatches, out-of-range indices, or any other runtime
// failure.
func Eval(e Expr, ctx EvalContext) (Value, error) {
	switch x := e.(type) {
	case LitType:
		return VType{Name: x.Name}, nil

	case LitStr:
		return VString{S: x.S}, nil

	case If:
		cv, err := Eval(x.Cond, ctx)
		if err != nil {
			return nil, err
		}
		bv, ok := cv.(VBool)
		if !ok {
			return nil, fmt.Errorf("if condition must be bool, got %T", cv)
		}
		if bv.B {
			return Eval(x.Then, ctx)
		}
		return Eval(x.Else, ctx)

	case And:
		return boolOp("and", x.A, x.B, ctx, func(a, b bool) bool { return a && b })

	case Or:
		return boolOp("or", x.A, x.B, ctx, func(a, b bool) bool { return a || b })

	case Not:
		v, err := Eval(x.X, ctx)
		if err != nil {
			return nil, err
		}
		bv, ok := v.(VBool)
		if !ok {
			return nil, fmt.Errorf("not requires bool, got %T", v)
		}
		return VBool{B: !bv.B}, nil

	case Eq:
		av, err := Eval(x.A, ctx)
		if err != nil {
			return nil, err
		}
		bv, err := Eval(x.B, ctx)
		if err != nil {
			return nil, err
		}
		switch at := av.(type) {
		case VString:
			bt, ok := bv.(VString)
			if !ok {
				return nil, fmt.Errorf("eq operands mismatched: %T vs %T", av, bv)
			}
			return VBool{B: at.S == bt.S}, nil
		case VType:
			bt, ok := bv.(VType)
			if !ok {
				return nil, fmt.Errorf("eq operands mismatched: %T vs %T", av, bv)
			}
			return VBool{B: at.Name == bt.Name}, nil
		case VBool:
			bt, ok := bv.(VBool)
			if !ok {
				return nil, fmt.Errorf("eq operands mismatched: %T vs %T", av, bv)
			}
			return VBool{B: at.B == bt.B}, nil
		}
		return nil, fmt.Errorf("eq: unsupported operand type %T", av)

	case Concat:
		var sb []byte
		for _, p := range x.Parts {
			v, err := Eval(p, ctx)
			if err != nil {
				return nil, err
			}
			s, ok := v.(VString)
			if !ok {
				return nil, fmt.Errorf("concat requires string args, got %T", v)
			}
			sb = append(sb, s.S...)
		}
		return VString{S: string(sb)}, nil

	case TargetTitle:
		return VString{S: ctx.TargetTitle}, nil

	case ExistsNode:
		v, err := Eval(x.Title, ctx)
		if err != nil {
			return nil, err
		}
		s, ok := v.(VString)
		if !ok {
			return nil, fmt.Errorf("exists-node? requires string arg, got %T", v)
		}
		if ctx.KB == nil {
			return VBool{B: false}, nil
		}
		_, present := ctx.KB.ResolveNode(s.S)
		return VBool{B: present}, nil

	case HasType:
		return VBool{B: ctx.HasTargetType(x.T)}, nil

	case PriorOutputType:
		if x.Index < 0 || x.Index >= len(ctx.PriorOutputType) {
			return nil, fmt.Errorf(
				"prior-output-type index %d out of range [0,%d)",
				x.Index, len(ctx.PriorOutputType))
		}
		return VType{Name: ctx.PriorOutputType[x.Index]}, nil
	}
	return nil, fmt.Errorf("unknown expression type %T", e)
}

func boolOp(
	name string,
	a, b Expr, ctx EvalContext,
	op func(bool, bool) bool,
) (Value, error) {
	av, err := Eval(a, ctx)
	if err != nil {
		return nil, err
	}
	ab, ok := av.(VBool)
	if !ok {
		return nil, fmt.Errorf("%s requires bool args, got %T", name, av)
	}
	bv, err := Eval(b, ctx)
	if err != nil {
		return nil, err
	}
	bb, ok := bv.(VBool)
	if !ok {
		return nil, fmt.Errorf("%s requires bool args, got %T", name, bv)
	}
	return VBool{B: op(ab.B, bb.B)}, nil
}

// === Static analysis: possibleReturnTypes ===============================

// PossibleReturnTypes walks the expression tree and enumerates the
// set of TypeName literals that can appear in return position. Used
// at load time to verify that a DependentOutput's declared
// alternatives is a superset of what the expression can actually
// return.
//
// Returns (nil, false) if the expression's return set cannot be
// statically bounded — e.g. it references a PriorOutputType whose
// possible values depend on upstream step outputs. In that case the
// caller should fall back to runtime checking.
func PossibleReturnTypes(e Expr) (map[kernel.TypeName]struct{}, bool) {
	switch x := e.(type) {
	case LitType:
		out := map[kernel.TypeName]struct{}{x.Name: {}}
		return out, true

	case If:
		tSet, tOK := PossibleReturnTypes(x.Then)
		eSet, eOK := PossibleReturnTypes(x.Else)
		if !tOK || !eOK {
			return nil, false
		}
		out := make(map[kernel.TypeName]struct{}, len(tSet)+len(eSet))
		for k := range tSet {
			out[k] = struct{}{}
		}
		for k := range eSet {
			out[k] = struct{}{}
		}
		return out, true

	case PriorOutputType:
		// Unbounded at static analysis time; defer to runtime.
		return nil, false

	// Everything else is either a non-type value (LitStr, Concat,
	// TargetTitle, ExistsNode, HasType, bool combinators) or an Eq
	// of same-typed values. None of these can return a type literal
	// as a final value, so their possible-return-type set is empty.
	case LitStr, And, Or, Not, Eq, Concat, TargetTitle, ExistsNode, HasType:
		return map[kernel.TypeName]struct{}{}, true
	}
	return nil, false
}

// === OutputPicker =======================================================
//
// The shape a function step's output type can take. Either a single
// static TypeName or a dependent expression with declared
// alternatives.

// OutputPicker is a sealed interface.
type OutputPicker interface {
	isOutputPicker()
}

// StaticOutput declares a fixed output type.
type StaticOutput struct {
	T kernel.TypeName
}

// DependentOutput declares a dependent output type. Alternatives is
// the set the expression is allowed to evaluate to; at runtime the
// evaluator's result is checked against this set.
type DependentOutput struct {
	Name         string
	Alternatives []kernel.TypeName
	Expr         Expr
}

func (StaticOutput) isOutputPicker()    {}
func (DependentOutput) isOutputPicker() {}

// Resolve evaluates an OutputPicker in the given context and returns
// the chosen TypeName. Enforces the "declared alternatives" contract:
// a DependentOutput whose expression returns a type not in its
// Alternatives list is rejected at runtime.
func Resolve(p OutputPicker, ctx EvalContext) (kernel.TypeName, error) {
	switch x := p.(type) {
	case StaticOutput:
		return x.T, nil
	case DependentOutput:
		v, err := Eval(x.Expr, ctx)
		if err != nil {
			return "", fmt.Errorf("picker %q: %w", x.Name, err)
		}
		tv, ok := v.(VType)
		if !ok {
			return "", fmt.Errorf(
				"picker %q must evaluate to a type, got %T", x.Name, v)
		}
		for _, alt := range x.Alternatives {
			if alt == tv.Name {
				return tv.Name, nil
			}
		}
		return "", fmt.Errorf(
			"picker %q returned %q which is not in declared alternatives %v",
			x.Name, tv.Name, x.Alternatives)
	}
	return "", fmt.Errorf("unknown output picker type %T", p)
}

// CheckDeclaration verifies statically that a DependentOutput's
// declared alternatives is a superset of the expression's
// possible-return-type set. StaticOutput always passes. A
// DependentOutput whose return set cannot be bounded (because it
// uses PriorOutputType, say) also passes — runtime will catch any
// violation.
func CheckDeclaration(p OutputPicker) error {
	dep, ok := p.(DependentOutput)
	if !ok {
		return nil
	}
	set, bounded := PossibleReturnTypes(dep.Expr)
	if !bounded {
		return nil
	}
	declared := make(map[kernel.TypeName]struct{}, len(dep.Alternatives))
	for _, t := range dep.Alternatives {
		declared[t] = struct{}{}
	}
	var extra []kernel.TypeName
	for t := range set {
		if _, found := declared[t]; !found {
			extra = append(extra, t)
		}
	}
	if len(extra) > 0 {
		return fmt.Errorf(
			"picker %q can return %v which is not in declared alternatives %v",
			dep.Name, extra, dep.Alternatives)
	}
	return nil
}
