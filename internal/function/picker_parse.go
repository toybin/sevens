package function

import (
	"fmt"

	"olympos.io/encoding/edn"

	"sevens/internal/function/picker"
	"sevens/internal/types/kernel"
)

// ParseOutputPicker converts a raw EDN value into a picker.OutputPicker.
//
// Expected EDN shape:
//
//	{:alternatives [:create :edit]
//	 :expr (if (exists-node? (concat "Discussion - " target-title))
//	           :edit
//	           :create)}
//
// Type literals are EDN keywords (`:create`, `:edit`). String
// literals are EDN strings. Boolean primitives are bare symbols
// (`target-title`). Function calls are EDN lists with a symbol head
// (`if`, `and`, `exists-node?`, etc.). The vocabulary is fixed and
// matches the picker.Expr constructors.
//
// Returns (nil, nil) if the raw value is nil — a step without an
// `:output-picker` key.
func ParseOutputPicker(raw any) (picker.OutputPicker, error) {
	if raw == nil {
		return nil, nil
	}

	// The raw value from go-edn may be wrapped in an edn.Keyword
	// map keyed by string or edn.Keyword. Normalize.
	m, err := asMap(raw)
	if err != nil {
		return nil, fmt.Errorf("output-picker must be a map: %w", err)
	}

	altRaw, ok := mapGet(m, "alternatives")
	if !ok {
		return nil, fmt.Errorf("output-picker missing :alternatives")
	}
	alts, err := parseTypeNameList(altRaw)
	if err != nil {
		return nil, fmt.Errorf("output-picker alternatives: %w", err)
	}

	exprRaw, ok := mapGet(m, "expr")
	if !ok {
		return nil, fmt.Errorf("output-picker missing :expr")
	}
	expr, err := ParsePickerExpr(exprRaw)
	if err != nil {
		return nil, fmt.Errorf("output-picker expr: %w", err)
	}

	name, _ := mapGet(m, "name")
	nameStr, _ := name.(string)

	return picker.DependentOutput{
		Name:         nameStr,
		Alternatives: alts,
		Expr:         expr,
	}, nil
}

// ParsePickerExpr parses a single EDN value into a picker.Expr.
//
// Rules (match the picker package's AST):
//   - edn.Keyword → LitType
//   - string      → LitStr
//   - edn.Symbol  → bare primitive (target-title)
//   - []any       → function call (EDN list or vector), dispatched
//                    on the head symbol
//
// Any other shape returns an error naming the position.
func ParsePickerExpr(raw any) (picker.Expr, error) {
	switch v := raw.(type) {
	case edn.Keyword:
		return picker.LitType{Name: kernel.TypeName(string(v))}, nil

	case string:
		return picker.LitStr{S: v}, nil

	case edn.Symbol:
		return parseBareSymbol(string(v))

	case []any:
		return parseCall(v)
	}
	return nil, fmt.Errorf("unsupported picker expression: %T %v", raw, raw)
}

func parseBareSymbol(name string) (picker.Expr, error) {
	switch name {
	case "target-title":
		return picker.TargetTitle{}, nil
	}
	return nil, fmt.Errorf("unknown bare symbol %q (expected target-title)", name)
}

func parseCall(list []any) (picker.Expr, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("empty picker expression")
	}
	headRaw := list[0]
	var head string
	switch h := headRaw.(type) {
	case edn.Symbol:
		head = string(h)
	case string:
		head = h
	default:
		return nil, fmt.Errorf("picker expression head must be a symbol, got %T", headRaw)
	}
	args := list[1:]

	switch head {
	case "if":
		if len(args) != 3 {
			return nil, fmt.Errorf("if: want 3 args, got %d", len(args))
		}
		cond, err := ParsePickerExpr(args[0])
		if err != nil {
			return nil, fmt.Errorf("if cond: %w", err)
		}
		thenE, err := ParsePickerExpr(args[1])
		if err != nil {
			return nil, fmt.Errorf("if then: %w", err)
		}
		elseE, err := ParsePickerExpr(args[2])
		if err != nil {
			return nil, fmt.Errorf("if else: %w", err)
		}
		return picker.If{Cond: cond, Then: thenE, Else: elseE}, nil

	case "and":
		if len(args) != 2 {
			return nil, fmt.Errorf("and: want 2 args, got %d", len(args))
		}
		a, err := ParsePickerExpr(args[0])
		if err != nil {
			return nil, fmt.Errorf("and lhs: %w", err)
		}
		b, err := ParsePickerExpr(args[1])
		if err != nil {
			return nil, fmt.Errorf("and rhs: %w", err)
		}
		return picker.And{A: a, B: b}, nil

	case "or":
		if len(args) != 2 {
			return nil, fmt.Errorf("or: want 2 args, got %d", len(args))
		}
		a, err := ParsePickerExpr(args[0])
		if err != nil {
			return nil, fmt.Errorf("or lhs: %w", err)
		}
		b, err := ParsePickerExpr(args[1])
		if err != nil {
			return nil, fmt.Errorf("or rhs: %w", err)
		}
		return picker.Or{A: a, B: b}, nil

	case "not":
		if len(args) != 1 {
			return nil, fmt.Errorf("not: want 1 arg, got %d", len(args))
		}
		x, err := ParsePickerExpr(args[0])
		if err != nil {
			return nil, fmt.Errorf("not arg: %w", err)
		}
		return picker.Not{X: x}, nil

	case "=":
		if len(args) != 2 {
			return nil, fmt.Errorf("=: want 2 args, got %d", len(args))
		}
		a, err := ParsePickerExpr(args[0])
		if err != nil {
			return nil, fmt.Errorf("= lhs: %w", err)
		}
		b, err := ParsePickerExpr(args[1])
		if err != nil {
			return nil, fmt.Errorf("= rhs: %w", err)
		}
		return picker.Eq{A: a, B: b}, nil

	case "concat":
		parts := make([]picker.Expr, 0, len(args))
		for i, a := range args {
			e, err := ParsePickerExpr(a)
			if err != nil {
				return nil, fmt.Errorf("concat arg %d: %w", i, err)
			}
			parts = append(parts, e)
		}
		return picker.Concat{Parts: parts}, nil

	case "exists-node?":
		if len(args) != 1 {
			return nil, fmt.Errorf("exists-node?: want 1 arg, got %d", len(args))
		}
		title, err := ParsePickerExpr(args[0])
		if err != nil {
			return nil, fmt.Errorf("exists-node? title: %w", err)
		}
		return picker.ExistsNode{Title: title}, nil

	case "has-type?":
		if len(args) != 1 {
			return nil, fmt.Errorf("has-type?: want 1 arg, got %d", len(args))
		}
		kw, ok := args[0].(edn.Keyword)
		if !ok {
			return nil, fmt.Errorf("has-type? arg must be a type keyword, got %T", args[0])
		}
		return picker.HasType{T: kernel.TypeName(string(kw))}, nil

	case "prior-output-type":
		if len(args) != 1 {
			return nil, fmt.Errorf("prior-output-type: want 1 arg, got %d", len(args))
		}
		idx, ok := args[0].(int64)
		if !ok {
			// go-edn may deliver ints as `int` too.
			if i2, ok2 := args[0].(int); ok2 {
				idx = int64(i2)
			} else {
				return nil, fmt.Errorf("prior-output-type arg must be an int, got %T", args[0])
			}
		}
		return picker.PriorOutputType{Index: int(idx)}, nil
	}

	return nil, fmt.Errorf("unknown picker primitive %q", head)
}

// --- map/list helpers --------------------------------------------------

// asMap coerces a raw EDN value into a map[string]any with string-
// coerced keys. go-edn delivers generic maps as map[any]any with
// edn.Keyword keys; we normalize here.
func asMap(raw any) (map[string]any, error) {
	switch m := raw.(type) {
	case map[string]any:
		return m, nil

	case map[any]any:
		out := make(map[string]any, len(m))
		for k, v := range m {
			ks, err := keyToString(k)
			if err != nil {
				return nil, err
			}
			out[ks] = v
		}
		return out, nil

	case map[edn.Keyword]any:
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[string(k)] = v
		}
		return out, nil
	}
	return nil, fmt.Errorf("not a map: %T", raw)
}

func keyToString(k any) (string, error) {
	switch kk := k.(type) {
	case string:
		return kk, nil
	case edn.Keyword:
		return string(kk), nil
	case edn.Symbol:
		return string(kk), nil
	}
	return "", fmt.Errorf("unsupported map key type: %T", k)
}

// mapGet looks up a key by bare string name, tolerating the leading
// colon that edn.Keyword carries when go-edn stringifies keyword
// keys.
func mapGet(m map[string]any, name string) (any, bool) {
	if v, ok := m[name]; ok {
		return v, true
	}
	if v, ok := m[":"+name]; ok {
		return v, true
	}
	return nil, false
}

// parseTypeNameList expects a list/vector of type keywords and
// returns the corresponding TypeNames.
func parseTypeNameList(raw any) ([]kernel.TypeName, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected vector of type keywords, got %T", raw)
	}
	out := make([]kernel.TypeName, 0, len(items))
	for i, it := range items {
		kw, ok := it.(edn.Keyword)
		if !ok {
			// Strings are also acceptable ("create" vs :create)
			if s, ok := it.(string); ok {
				out = append(out, kernel.TypeName(s))
				continue
			}
			return nil, fmt.Errorf("alternative[%d] must be a type keyword, got %T", i, it)
		}
		out = append(out, kernel.TypeName(string(kw)))
	}
	return out, nil
}
