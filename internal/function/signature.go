package function

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"sevens/internal/types"
)

// StepSignature describes one step's input → output type for display.
type StepSignature struct {
	Name       string
	InputShape string // reified type name or "{role, role, ...}"
	OutputType string // primitive name or "primitive/subtype"
}

// FunctionSignature computes the displayable type signature for a function.
func FunctionSignature(fn *Function) []StepSignature {
	steps := fn.EffectiveSteps()
	allTypes, _ := types.LoadAllTypeDefs()

	var sigs []StepSignature
	for _, step := range steps {
		sig := StepSignature{
			Name:       step.Name,
			InputShape: resolveInputShape(step, allTypes),
			OutputType: resolveOutputType(step),
		}
		sigs = append(sigs, sig)
	}
	return sigs
}

// FormatSignature renders a function's type signature in Haskell-style notation.
// Single-step: "discuss :: Node -> Text"
// Multi-step:  "decompose :: Node -> Suggestion -> Create"
func FormatSignature(fn *Function) string {
	sigs := FunctionSignature(fn)
	if len(sigs) == 0 {
		return fn.Name + " :: ?"
	}

	var parts []string
	for i, sig := range sigs {
		if i == 0 || sig.InputShape != sigs[i-1].OutputType {
			parts = append(parts, capitalize(sig.InputShape))
		}
		parts = append(parts, capitalize(sig.OutputType))
	}
	return fn.Name + " :: " + strings.Join(parts, " -> ")
}

// capitalize uppercases the first letter of a type name.
// For compound shapes like "{node, parent?}", it simplifies to "Node".
func capitalize(s string) string {
	// Compound shape — extract the first role name and capitalize it.
	if strings.HasPrefix(s, "{") {
		inner := strings.TrimPrefix(s, "{")
		inner = strings.TrimSuffix(inner, "}")
		parts := strings.SplitN(inner, ",", 2)
		base := strings.TrimSpace(parts[0])
		base = strings.TrimSuffix(base, "?")
		return ucFirst(base)
	}
	// Strip array suffix and slash-subtypes for display.
	clean := strings.TrimSuffix(s, "[]")
	if idx := strings.Index(clean, "/"); idx >= 0 {
		clean = clean[idx+1:]
	}
	return ucFirst(clean)
}

func ucFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

func resolveInputShape(step Step, allTypes map[string]*types.TypeDef) string {
	// Collect required roles/predicates from the step.
	var required []string
	var optional []string

	for _, r := range step.Requires {
		name := r.Role
		if r.As != "" {
			name = r.As
		}
		if r.Optional {
			optional = append(optional, name+"?")
		} else {
			required = append(required, name)
		}
	}

	for _, p := range step.Paths {
		name := p.As
		if name == "" {
			name = strings.Join(p.Path, "/")
		}
		required = append(required, name)
	}

	// Always include "node" as the base input.
	if len(required) == 0 && len(optional) == 0 {
		required = []string{"node"}
	}

	// Check if this input shape matches a reified type.
	allRoles := make([]string, 0, len(required)+len(optional))
	allRoles = append(allRoles, required...)
	allRoles = append(allRoles, optional...)
	sort.Strings(allRoles)
	if allTypes != nil {
		for name, td := range allTypes {
			if td.Primitive {
				continue
			}
			if matchesShape(allRoles, td) {
				return name
			}
		}
	}

	// No match — show raw shape.
	all := make([]string, 0, len(required)+len(optional))
	all = append(all, required...)
	all = append(all, optional...)
	return "{" + strings.Join(all, ", ") + "}"
}

func resolveOutputType(step Step) string {
	primitives := map[string]bool{"text": true, "create": true, "edit": true, "suggestion": true}

	if step.Output.TypeName != "" {
		if primitives[step.Output.TypeName] {
			// TypeName is itself a primitive — use directly.
			return step.Output.TypeName
		}
		// TypeName is a subtype — show as primitive/subtype.
		prim := PrimitiveTypeName(step.Output.Shape)
		return fmt.Sprintf("%s/%s", prim, step.Output.TypeName)
	}
	return PrimitiveTypeName(step.Output.Shape)
}

// matchesShape checks if a set of role names matches a type's required+optional predicates.
func matchesShape(roles []string, td *types.TypeDef) bool {
	tdRoles := make([]string, 0, len(td.Predicates.Required)+len(td.Predicates.Optional))
	tdRoles = append(tdRoles, td.Predicates.Required...)
	for _, o := range td.Predicates.Optional {
		tdRoles = append(tdRoles, o+"?")
	}
	sort.Strings(tdRoles)

	if len(roles) != len(tdRoles) {
		return false
	}
	for i := range roles {
		if roles[i] != tdRoles[i] {
			return false
		}
	}
	return true
}
