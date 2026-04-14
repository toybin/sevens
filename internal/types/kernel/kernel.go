// Package kernel is the runtime type kernel for sevens.
//
// It is a straight port of docs/sketch/TypesKernel.hs — see that file
// for the design rationale and the 19-test invariant battery this
// package mirrors. The kernel operates on runtime data: a Registry
// holds TypeDef values, Validate checks a candidate Value against a
// named type, and SchemaInstruction renders the same type definition
// into the prompt instruction the LLM is told to satisfy.
//
// A kernel TypeDef is one of two concrete implementations:
//
//   - PrimitiveType — one of the four built-in shapes (text, create,
//     edit, suggestion). Carries no refinements. Every user type
//     extends exactly one primitive.
//
//   - DerivedType — a named subtype of an existing type. May add
//     extra fields and refinements. Extends is resolved by name at
//     call time.
//
// Refinements are either Intrinsic (pure over Value) or Contextual
// (take a KB handle). The split is load-bearing: intrinsic checks can
// be cached; contextual checks must re-run when the KB changes.
//
// This package is intentionally independent of internal/types (the
// legacy package) and has no external dependencies. It is the eventual
// replacement for that package's schema composition and validation
// responsibilities.
package kernel

import (
	"fmt"
	"strings"
)

// === Names =============================================================

type TypeName string
type FieldName string

// === Primitives ========================================================

// Primitive is one of the four built-in base types.
type Primitive int

const (
	PText Primitive = iota
	PCreate
	PEdit
	PSuggestion
)

// Name returns the TypeName for a primitive.
func (p Primitive) Name() TypeName {
	switch p {
	case PText:
		return "text"
	case PCreate:
		return "create"
	case PEdit:
		return "edit"
	case PSuggestion:
		return "suggestion"
	}
	return ""
}

// FieldKind is the storage/display hint for a field in the schema.
type FieldKind int

const (
	FKString FieldKind = iota
	FKContent
	FKExtra
)

// FieldSpec describes one field in a type's shape.
type FieldSpec struct {
	Name     FieldName
	Kind     FieldKind
	Required bool
}

func primitiveShape(p Primitive) []FieldSpec {
	switch p {
	case PText:
		return []FieldSpec{{Name: "text", Kind: FKString, Required: true}}
	case PCreate:
		return []FieldSpec{
			{Name: "title", Kind: FKString, Required: true},
			{Name: "parent", Kind: FKString, Required: false},
			{Name: "content", Kind: FKContent, Required: true},
			{Name: "extra", Kind: FKExtra, Required: false},
		}
	case PEdit:
		return []FieldSpec{
			{Name: "file", Kind: FKString, Required: true},
			{Name: "old_text", Kind: FKString, Required: true},
			{Name: "new_text", Kind: FKString, Required: true},
		}
	case PSuggestion:
		return []FieldSpec{
			{Name: "title", Kind: FKString, Required: true},
			{Name: "rationale", Kind: FKString, Required: true},
		}
	}
	return nil
}

// === Values ============================================================
//
// A Value is the runtime representation of an LLM-produced record.
// Fields are untyped at this level — the type system decides which
// fields are expected and how they should be interpreted. FieldValue
// is a small sum.

type FieldValue interface {
	isFieldValue()
}

// VString is a string-valued field.
type VString string

// VMap is a map-of-strings field (used for `extra` on create ops).
type VMap map[string]string

// VAbsent is the value returned when a field is missing from a Value.
type VAbsent struct{}

func (VString) isFieldValue() {}
func (VMap) isFieldValue()    {}
func (VAbsent) isFieldValue() {}

// Value is a runtime field map. Use NewValue to construct one.
type Value struct {
	Fields map[FieldName]FieldValue
}

// NewValue constructs a Value from a variadic list of pairs.
func NewValue(pairs ...FieldPair) Value {
	m := make(map[FieldName]FieldValue, len(pairs))
	for _, p := range pairs {
		m[p.Name] = p.Value
	}
	return Value{Fields: m}
}

// FieldPair is a single name/value binding used with NewValue.
type FieldPair struct {
	Name  FieldName
	Value FieldValue
}

// Get returns the field value or VAbsent{} if the field is missing.
func (v Value) Get(name FieldName) FieldValue {
	if v.Fields == nil {
		return VAbsent{}
	}
	if fv, ok := v.Fields[name]; ok {
		return fv
	}
	return VAbsent{}
}

// === KB ================================================================
//
// The kernel is parameterized on a KB interface. Any consumer can
// implement it — this avoids an import cycle with internal/kb and
// keeps the kernel self-contained.

// KB is the minimal knowledge-base interface refinements can call.
// The full sevens KB satisfies it.
type KB interface {
	ResolveNode(title string) (content string, ok bool)
}

// === Refinement interface ==============================================
//
// A Refinement is a named predicate on a Value, optionally with
// access to a KB. The interface collapses IntrinsicRefinement and
// ContextualRefinement behind a single Check method so the validator
// only needs one dispatch path. IntrinsicRefinement ignores the kb
// argument.

type Refinement interface {
	Name() string
	Check(kb KB, v Value) error
}

// IntrinsicRefinement is a Refinement that does not touch the KB.
type IntrinsicRefinement struct {
	NameStr string
	Fn      func(v Value) error
}

func (r IntrinsicRefinement) Name() string { return r.NameStr }

// Check runs the intrinsic predicate. The kb argument is ignored.
func (r IntrinsicRefinement) Check(_ KB, v Value) error {
	return r.Fn(v)
}

// ContextualRefinement is a Refinement that reads the KB.
type ContextualRefinement struct {
	NameStr string
	Fn      func(kb KB, v Value) error
}

func (r ContextualRefinement) Name() string { return r.NameStr }

// Check runs the contextual predicate.
func (r ContextualRefinement) Check(kb KB, v Value) error {
	return r.Fn(kb, v)
}

// === TypeDef interface =================================================
//
// TypeDef is a sealed interface satisfied by PrimitiveType and
// DerivedType only. The unexported isTypeDef method prevents outside
// implementations so subsumption + validation only ever see one of
// these two concrete shapes.

type TypeDef interface {
	Name() TypeName
	Parent() TypeName
	LocalFields() []FieldSpec
	LocalRefinements() []Refinement
	isTypeDef()
}

// PrimitiveType is the TypeDef for one of the four built-in primitives.
type PrimitiveType struct {
	Kind Primitive
}

func (p PrimitiveType) Name() TypeName   { return p.Kind.Name() }
func (p PrimitiveType) Parent() TypeName { return "" }
func (p PrimitiveType) LocalFields() []FieldSpec {
	return primitiveShape(p.Kind)
}
func (p PrimitiveType) LocalRefinements() []Refinement { return nil }
func (p PrimitiveType) isTypeDef()                     {}

// DerivedType is the TypeDef for a user-defined type extending another.
//
// ChildType is an optional type-family declaration: "nodes of this
// type have children of type X." Used by the function layer to
// resolve input-dependent output types (Node<T> → Node<child-of<T>>).
// Empty TypeName means no child-type is declared at this level; the
// lookup walks the :extends chain until it finds one (or fails).
type DerivedType struct {
	TName       TypeName
	ParentName  TypeName
	ChildType   TypeName // optional; empty = not declared
	ExtraFields []FieldSpec
	Refinements []Refinement
}

func (d DerivedType) Name() TypeName                 { return d.TName }
func (d DerivedType) Parent() TypeName               { return d.ParentName }
func (d DerivedType) LocalFields() []FieldSpec       { return d.ExtraFields }
func (d DerivedType) LocalRefinements() []Refinement { return d.Refinements }
func (d DerivedType) isTypeDef()                     {}

// === Registry ==========================================================

// Registry holds a set of TypeDefs keyed by name. It is the central
// data structure of the kernel: Validate, SchemaInstruction,
// IsSubtype, and ComposedShape all take a *Registry and walk it.
type Registry struct {
	types map[TypeName]TypeDef
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{types: make(map[TypeName]TypeDef)}
}

// NewPrimitivesRegistry returns a registry pre-populated with the
// four primitive types. User-defined types should be Inserted after.
func NewPrimitivesRegistry() *Registry {
	r := NewRegistry()
	r.Insert(PrimitiveType{Kind: PText})
	r.Insert(PrimitiveType{Kind: PCreate})
	r.Insert(PrimitiveType{Kind: PEdit})
	r.Insert(PrimitiveType{Kind: PSuggestion})
	return r
}

// Insert adds (or replaces) a TypeDef in the registry.
func (r *Registry) Insert(td TypeDef) {
	r.types[td.Name()] = td
}

// Get returns a TypeDef by name. The second return is false if
// the name is not in the registry.
func (r *Registry) Get(name TypeName) (TypeDef, bool) {
	td, ok := r.types[name]
	return td, ok
}

// Names returns all registered type names. Order is unspecified.
func (r *Registry) Names() []TypeName {
	out := make([]TypeName, 0, len(r.types))
	for n := range r.types {
		out = append(out, n)
	}
	return out
}

// === Subsumption =======================================================

// Ancestors returns name followed by its parent chain up to the
// primitive root (inclusive). Returns nil if name is not in the
// registry. A cycle in the :extends chain (malformed registry) is
// broken at the first repeat; the returned slice contains each name
// at most once.
func (r *Registry) Ancestors(name TypeName) []TypeName {
	var chain []TypeName
	seen := make(map[TypeName]bool)
	current := name
	for current != "" {
		if seen[current] {
			return chain
		}
		td, ok := r.types[current]
		if !ok {
			return chain
		}
		seen[current] = true
		chain = append(chain, current)
		current = td.Parent()
	}
	return chain
}

// IsSubtype reports whether sub is a subtype of super. Reflexive:
// every type is a subtype of itself. Returns false if either name
// is unknown.
func (r *Registry) IsSubtype(sub, super TypeName) bool {
	for _, a := range r.Ancestors(sub) {
		if a == super {
			return true
		}
	}
	return false
}

// ChildTypeOf returns the type-family entry "children of this type
// are of type X" for the given name. Walks the :extends chain until
// it finds a DerivedType with ChildType set, or returns ("", false)
// if no declaration exists in the chain.
//
// This is the runtime half of input-dependent output type resolution:
// a function declared as `:output :child-of-input` will call this at
// dispatch time with the target's most-specific type.
//
// Cycles in the :extends chain return ("", false) rather than
// infinite-looping.
func (r *Registry) ChildTypeOf(name TypeName) (TypeName, bool) {
	td, ok := r.types[name]
	if !ok {
		return "", false
	}
	seen := make(map[TypeName]bool)
	for {
		currentName := td.Name()
		if seen[currentName] {
			return "", false
		}
		seen[currentName] = true
		d, isDerived := td.(DerivedType)
		if !isDerived {
			return "", false // primitive — no child type
		}
		if d.ChildType != "" {
			return d.ChildType, true
		}
		parent, ok := r.types[d.ParentName]
		if !ok {
			return "", false
		}
		td = parent
	}
}

// SubtypesOf returns every registered type that is a subtype of name
// (including name itself). Used by the load-time check for
// input-dependent output pickers: every concrete input must have a
// resolvable child type. Order is unspecified.
func (r *Registry) SubtypesOf(name TypeName) []TypeName {
	var out []TypeName
	for n := range r.types {
		if r.IsSubtype(n, name) {
			out = append(out, n)
		}
	}
	return out
}

// RootPrimitive returns the primitive at the root of name's extends
// chain. Returns (0, false) if name is unknown, the chain is broken,
// or a cycle is detected.
func (r *Registry) RootPrimitive(name TypeName) (Primitive, bool) {
	td, ok := r.types[name]
	if !ok {
		return 0, false
	}
	seen := make(map[TypeName]bool)
	for {
		currentName := td.Name()
		if seen[currentName] {
			return 0, false
		}
		seen[currentName] = true
		if p, isPrim := td.(PrimitiveType); isPrim {
			return p.Kind, true
		}
		parent, ok := r.types[td.Parent()]
		if !ok {
			return 0, false
		}
		td = parent
	}
}

// === Shape composition =================================================

// ComposedShape returns the complete field list for a type, walking
// its parent chain. Parent fields come first; a child type's extra
// fields override parent fields of the same name. A cycle in the
// :extends chain (malformed registry) is broken at the first
// repeat and the chain's accumulation stops there.
func (r *Registry) ComposedShape(name TypeName) []FieldSpec {
	return r.composedShape(name, nil)
}

func (r *Registry) composedShape(name TypeName, seen map[TypeName]bool) []FieldSpec {
	td, ok := r.types[name]
	if !ok {
		return nil
	}
	if seen == nil {
		seen = make(map[TypeName]bool)
	}
	if seen[name] {
		return nil
	}
	seen[name] = true
	switch t := td.(type) {
	case PrimitiveType:
		return primitiveShape(t.Kind)
	case DerivedType:
		parent := r.composedShape(t.ParentName, seen)
		return overrideFields(parent, t.ExtraFields)
	}
	return nil
}

func overrideFields(old, overrides []FieldSpec) []FieldSpec {
	if len(overrides) == 0 {
		// return a copy so callers can't mutate the cached slice
		out := make([]FieldSpec, len(old))
		copy(out, old)
		return out
	}
	overrideNames := make(map[FieldName]bool, len(overrides))
	for _, f := range overrides {
		overrideNames[f.Name] = true
	}
	kept := make([]FieldSpec, 0, len(old)+len(overrides))
	for _, f := range old {
		if !overrideNames[f.Name] {
			kept = append(kept, f)
		}
	}
	return append(kept, overrides...)
}

// === Refinement collection =============================================

// CollectRefinements walks the extends chain and returns all
// refinements in root-first order (parent refinements before child).
// This ordering is load-bearing: refinement errors fire top-down so
// the user sees structural constraints before nested ones. A cycle
// in the :extends chain is broken at the first repeat.
func (r *Registry) CollectRefinements(name TypeName) []Refinement {
	return r.collectRefinements(name, nil)
}

func (r *Registry) collectRefinements(name TypeName, seen map[TypeName]bool) []Refinement {
	td, ok := r.types[name]
	if !ok {
		return nil
	}
	if seen == nil {
		seen = make(map[TypeName]bool)
	}
	if seen[name] {
		return nil
	}
	seen[name] = true
	switch t := td.(type) {
	case PrimitiveType:
		return nil
	case DerivedType:
		parent := r.collectRefinements(t.ParentName, seen)
		out := make([]Refinement, 0, len(parent)+len(t.Refinements))
		out = append(out, parent...)
		out = append(out, t.Refinements...)
		return out
	}
	return nil
}

// === Validation ========================================================

// Validate checks a Value against a named type. Returns nil on
// success, or the first error encountered — either a missing
// required field or a failed refinement. Errors are prefixed with
// the refinement name so failures are specific.
func (r *Registry) Validate(kb KB, name TypeName, v Value) error {
	if _, ok := r.types[name]; !ok {
		return fmt.Errorf("unknown type: %s", name)
	}
	if err := checkFields(r.ComposedShape(name), v); err != nil {
		return err
	}
	for _, ref := range r.CollectRefinements(name) {
		if err := ref.Check(kb, v); err != nil {
			return fmt.Errorf("%s: %w", ref.Name(), err)
		}
	}
	return nil
}

func checkFields(specs []FieldSpec, v Value) error {
	for _, f := range specs {
		if !f.Required {
			continue
		}
		fv := v.Get(f.Name)
		switch val := fv.(type) {
		case VAbsent:
			return fmt.Errorf("field %s required but absent", f.Name)
		case VString:
			if val == "" {
				return fmt.Errorf("field %s required but empty", f.Name)
			}
		case VMap:
			if len(val) == 0 {
				return fmt.Errorf("field %s required but empty", f.Name)
			}
		}
	}
	return nil
}

// === Schema instruction ================================================

// primitivePromptInstruction returns the prescriptive prompt text
// for a primitive — the "you MUST respond with JSON..." preamble
// that tells the LLM exactly what wire format to produce. This is
// what makes the schema instruction actually useful as prompt
// material rather than just a type summary.
func primitivePromptInstruction(p Primitive) string {
	switch p {
	case PText:
		return `You MUST respond with a JSON object containing a "text" field with your markdown-formatted response.
Example: {"text": "**Observation** — The governance model assumes..."}
Respond ONLY with valid JSON. No text outside the JSON object.`
	case PCreate:
		return `You MUST respond with a JSON object containing an "ops" field with an array of file operations.
Each create operation has fields: action (always "create"), title, parent, content, extra.
The "extra" field sets frontmatter metadata on the new node.
Example: {"ops":[{"action":"create","title":"Node Title","parent":"Parent Title","content":"markdown body","extra":{"key":"value"}}]}
Respond ONLY with valid JSON. No text outside the JSON object.`
	case PEdit:
		return `You MUST respond with a JSON object containing an "ops" field with an array of edit operations.
Each edit operation has fields: action (always "edit"), file, old_text, new_text.
Example: {"ops":[{"action":"edit","file":"Node Title","old_text":"exact text to find","new_text":"replacement text"}]}
Respond ONLY with valid JSON. No text outside the JSON object.`
	case PSuggestion:
		return `You MUST respond with a JSON object containing a "suggestions" field with an array of suggestion objects.
Each suggestion has a "title" and a "rationale".
Example: {"suggestions": [{"title": "Revenue Models", "rationale": "Financial sustainability is unaddressed"}]}
Respond ONLY with valid JSON. No text outside the JSON object.`
	}
	return ""
}

// SchemaInstruction composes the prompt-facing description of a type.
// This is what the executor injects into the system prompt so the
// LLM knows the exact shape it must return. It is the single source
// of truth for both the prompt AND the validator — changing a field
// or refinement updates both at once.
//
// Output structure:
//
//  1. The primitive's prescriptive prompt instruction (tells the
//     LLM to return JSON in the right wire format).
//  2. For derived types only: a "This type adds:" section listing
//     extra fields on top of the primitive's shape.
//  3. For types with refinements: a "Constraints:" section naming
//     each refinement. Constraints are authoritative — the
//     validator enforces them.
func (r *Registry) SchemaInstruction(name TypeName) string {
	td, ok := r.types[name]
	if !ok {
		return ""
	}

	var sb strings.Builder

	// Start with the primitive's prescriptive instruction.
	if prim, havePrim := r.RootPrimitive(name); havePrim {
		if preamble := primitivePromptInstruction(prim); preamble != "" {
			sb.WriteString(preamble)
		}
	}

	// For derived types, list the extra fields.
	if _, isDerived := td.(DerivedType); isDerived {
		extras := collectLocalFields(r, name)
		if len(extras) > 0 {
			sb.WriteString("\n\nThis type (")
			sb.WriteString(string(name))
			sb.WriteString(") also requires:\n")
			for _, f := range extras {
				kindStr := "string"
				switch f.Kind {
				case FKContent:
					kindStr = "markdown"
				case FKExtra:
					kindStr = "map<string,string>"
				}
				req := "optional"
				if f.Required {
					req = "required"
				}
				fmt.Fprintf(&sb, "  - %s : %s (%s)\n", f.Name, kindStr, req)
			}
		}
	}

	// List refinements last — they're the invariants the validator
	// enforces, so the LLM needs to know about them explicitly.
	refs := r.CollectRefinements(name)
	if len(refs) > 0 {
		sb.WriteString("\n\nConstraints (enforced by the parser):\n")
		for _, ref := range refs {
			fmt.Fprintf(&sb, "  - %s\n", ref.Name())
		}
	}

	return sb.String()
}

// collectLocalFields walks the extends chain root-first and returns
// only the fields added at levels above the primitive — i.e. the
// fields a derived type contributes on top of its primitive's base
// shape. Used by SchemaInstruction to describe subtype constructors
// without repeating the primitive's fields.
func collectLocalFields(r *Registry, name TypeName) []FieldSpec {
	td, ok := r.types[name]
	if !ok {
		return nil
	}
	if _, isPrim := td.(PrimitiveType); isPrim {
		return nil
	}
	dt := td.(DerivedType)
	parentFields := collectLocalFields(r, dt.ParentName)
	return append(parentFields, dt.ExtraFields...)
}
