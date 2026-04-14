package kernel

import (
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"

	"olympos.io/encoding/edn"
)

// Primitive type definitions live as structured EDN files under
// internal/types/kernel/primitives/ and are embedded at compile
// time. The loader parses them, converts them to PrimitiveType
// values, and inserts them into a fresh Registry.
//
// This replaces the previous hand-coded primitivePromptInstruction
// and primitiveShape tables. One source of truth (the EDN file),
// two derivations (validator shape and rendered prompt).
//
//go:embed primitives/*.edn
var primitivesFS embed.FS

// === EDN wire structs ===================================================

type ednPrimitive struct {
	Name      string      `edn:"name"`
	Primitive bool        `edn:"primitive"`
	Envelope  ednEnvelope `edn:"envelope"`
	Example   any         `edn:"example"`
}

type ednEnvelope struct {
	Kind          edn.Keyword    `edn:"kind"`
	Wrapper       string         `edn:"wrapper"`
	ScalarField   *ednField      `edn:"scalar-field,omitempty"`
	ItemConstants []ednNameValue `edn:"item-constants,omitempty"`
	ItemFields    []ednField     `edn:"item-fields,omitempty"`
}

type ednField struct {
	Name     string      `edn:"name"`
	Kind     edn.Keyword `edn:"kind"`
	Required bool        `edn:"required"`
}

type ednNameValue struct {
	Name  string `edn:"name"`
	Value string `edn:"value"`
}

// === Loading ===========================================================

// LoadPrimitives reads the embedded primitive type definitions and
// returns a fresh registry populated with them. Called once from
// NewPrimitivesRegistry at program startup.
func LoadPrimitives() (*Registry, error) {
	r := NewRegistry()
	entries, err := primitivesFS.ReadDir("primitives")
	if err != nil {
		return nil, fmt.Errorf("kernel: reading embedded primitives dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		data, err := primitivesFS.ReadFile("primitives/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("kernel: reading %s: %w", e.Name(), err)
		}
		pt, err := parsePrimitive(data)
		if err != nil {
			return nil, fmt.Errorf("kernel: parsing %s: %w", e.Name(), err)
		}
		r.Insert(pt)
	}
	return r, nil
}

// parsePrimitive converts one EDN file's bytes into a PrimitiveType.
// Fails loudly if the file doesn't declare a known primitive name,
// has an invalid envelope kind, or produces an example that can't
// be marshaled to JSON.
func parsePrimitive(data []byte) (PrimitiveType, error) {
	var p ednPrimitive
	if err := edn.Unmarshal(data, &p); err != nil {
		return PrimitiveType{}, fmt.Errorf("edn unmarshal: %w", err)
	}
	if !p.Primitive {
		return PrimitiveType{}, fmt.Errorf("type %q is not marked :primitive true", p.Name)
	}

	// Map the declared name to a Primitive enum. The enum is the
	// closed vocabulary — new primitives require a Go change.
	var kind Primitive
	switch p.Name {
	case "text":
		kind = PText
	case "create":
		kind = PCreate
	case "edit":
		kind = PEdit
	case "suggestion":
		kind = PSuggestion
	default:
		return PrimitiveType{}, fmt.Errorf("unknown primitive name %q (expected text/create/edit/suggestion)", p.Name)
	}

	env, err := convertEnvelope(p.Envelope)
	if err != nil {
		return PrimitiveType{}, fmt.Errorf("envelope: %w", err)
	}

	// Marshal example to canonical JSON. This guarantees the
	// example shown to the LLM is syntactically valid JSON by
	// construction — no more hand-typed string that might have a
	// typo.
	jsonable := ednToJSONable(p.Example)
	exampleJSON, err := json.Marshal(jsonable)
	if err != nil {
		return PrimitiveType{}, fmt.Errorf("marshaling example to JSON: %w", err)
	}

	return PrimitiveType{
		Kind:     kind,
		Envelope: env,
		Example:  exampleJSON,
	}, nil
}

func convertEnvelope(e ednEnvelope) (Envelope, error) {
	switch string(e.Kind) {
	case "scalar":
		if e.ScalarField == nil {
			return Envelope{}, fmt.Errorf("scalar envelope missing :scalar-field")
		}
		fs, err := convertField(*e.ScalarField)
		if err != nil {
			return Envelope{}, err
		}
		if e.Wrapper == "" {
			return Envelope{}, fmt.Errorf("scalar envelope missing :wrapper")
		}
		return Envelope{
			Kind:        ScalarEnvelope,
			Wrapper:     e.Wrapper,
			ScalarField: fs,
		}, nil

	case "array":
		if e.Wrapper == "" {
			return Envelope{}, fmt.Errorf("array envelope missing :wrapper")
		}
		items := make([]FieldSpec, 0, len(e.ItemFields))
		for _, f := range e.ItemFields {
			fs, err := convertField(f)
			if err != nil {
				return Envelope{}, err
			}
			items = append(items, fs)
		}
		constants := make([]NameValue, 0, len(e.ItemConstants))
		for _, c := range e.ItemConstants {
			constants = append(constants, NameValue{Name: c.Name, Value: c.Value})
		}
		return Envelope{
			Kind:          ArrayEnvelope,
			Wrapper:       e.Wrapper,
			ItemFields:    items,
			ItemConstants: constants,
		}, nil
	}
	return Envelope{}, fmt.Errorf("unknown envelope kind %q (expected :scalar or :array)", e.Kind)
}

func convertField(f ednField) (FieldSpec, error) {
	kind, err := fieldKindFromKeyword(string(f.Kind))
	if err != nil {
		return FieldSpec{}, fmt.Errorf("field %q: %w", f.Name, err)
	}
	return FieldSpec{
		Name:     FieldName(f.Name),
		Kind:     kind,
		Required: f.Required,
	}, nil
}

func fieldKindFromKeyword(k string) (FieldKind, error) {
	switch k {
	case "string":
		return FKString, nil
	case "markdown":
		return FKContent, nil
	case "map":
		return FKExtra, nil
	}
	return 0, fmt.Errorf("unknown field kind %q (expected :string, :markdown, or :map)", k)
}

// ednToJSONable recursively converts go-edn's generic decoded form
// (map[any]any, edn.Keyword) into JSON-compatible Go values
// (map[string]any, string). Used to turn a hand-written EDN
// example into canonical JSON via json.Marshal.
func ednToJSONable(v any) any {
	switch x := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			var key string
			switch kk := k.(type) {
			case string:
				key = kk
			case edn.Keyword:
				key = string(kk)
			case edn.Symbol:
				key = string(kk)
			default:
				continue
			}
			out[key] = ednToJSONable(v)
		}
		return out
	case []any:
		conv := make([]any, len(x))
		for i, e := range x {
			conv[i] = ednToJSONable(e)
		}
		return conv
	case edn.Keyword:
		return string(x)
	case edn.Symbol:
		return string(x)
	default:
		return v
	}
}

// === Prompt rendering ===================================================
//
// Replaces primitivePromptInstruction. The renderer walks the
// envelope structure and produces prompt text; no hardcoded prose
// per primitive.

// renderPrimitivePrompt turns a PrimitiveType's envelope + example
// into the "you MUST respond with JSON..." preamble the LLM sees.
func renderPrimitivePrompt(p PrimitiveType) string {
	var out []byte
	switch p.Envelope.Kind {
	case ScalarEnvelope:
		out = appendScalarPrompt(out, p.Envelope, p.Example)
	case ArrayEnvelope:
		out = appendArrayPrompt(out, p.Envelope, p.Example)
	}
	return string(out)
}

func appendScalarPrompt(out []byte, e Envelope, example []byte) []byte {
	out = fmt.Appendf(out,
		"You MUST respond with a JSON object containing a %q field (%s).\n",
		e.Wrapper, kindLabel(e.ScalarField.Kind))
	if len(example) > 0 {
		out = fmt.Appendf(out, "Example: %s\n", string(example))
	}
	out = append(out, "Respond ONLY with valid JSON. No text outside the JSON object."...)
	return out
}

func appendArrayPrompt(out []byte, e Envelope, example []byte) []byte {
	out = fmt.Appendf(out,
		"You MUST respond with a JSON object containing a %q field with an array of objects.\n",
		e.Wrapper)
	if len(e.ItemConstants) > 0 || len(e.ItemFields) > 0 {
		out = append(out, "Each object has these fields:\n"...)
		for _, c := range e.ItemConstants {
			out = fmt.Appendf(out, "  - %s: %q (required, constant)\n", c.Name, c.Value)
		}
		for _, f := range e.ItemFields {
			req := "optional"
			if f.Required {
				req = "required"
			}
			out = fmt.Appendf(out, "  - %s: %s (%s)\n", f.Name, kindLabel(f.Kind), req)
		}
	}
	if len(example) > 0 {
		out = fmt.Appendf(out, "Example: %s\n", string(example))
	}
	out = append(out, "Respond ONLY with valid JSON. No text outside the JSON object."...)
	return out
}

func kindLabel(k FieldKind) string {
	switch k {
	case FKString:
		return "string"
	case FKContent:
		return "markdown"
	case FKExtra:
		return "map<string,string>"
	}
	return "unknown"
}
