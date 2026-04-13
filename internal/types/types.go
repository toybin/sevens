// Package types implements the node-level type system for sevens.
//
// A type is a named predicate pattern. A node conforms to a type when
// its predicates match the shape defined by the type. Types operate on
// graph position and frontmatter predicates (meta/* namespace).
//
// Types are queries, not assertions -- conformance checking reads the
// graph but never modifies it.
package types

import "sevens/internal/sevtypes"

// TypeDef defines a named predicate pattern.
type TypeDef struct {
	Name              string
	Extends           string // primitive type this extends ("text", "create", "edit", "suggestion", or "")
	Primitive         bool   // true for the four built-in primitives
	ContextPolicy     bool   // true if this is a context policy definition, not a node type
	Description       string // human-readable description
	SchemaInstruction string // prompt material for this type's output format
	Predicates        PredicateSpec
	Structure         StructureSpec
	Projection        ProjectionSpec
	Gather            GatherSpec // for context policies: which graph neighborhoods to include
}

// GatherSpec is a type alias for sevtypes.GatherSpec.
type GatherSpec = sevtypes.GatherSpec

// PredicateSpec declares which predicates constitute the type shape.
type PredicateSpec struct {
	Required []string // predicate names that must be present
	Optional []string // predicate names that may be present
}

// StructureSpec declares structural constraints on graph position.
type StructureSpec struct {
	ParentType  string // parent must conform to this type (empty = any)
	ChildrenMin int
	ChildrenMax int // 0 = unlimited
}

// ProjectionSpec declares how predicates map to projection layers.
type ProjectionSpec struct {
	Frontmatter []string                      // predicates rendered as frontmatter
	Orthography map[string]OrthographyBinding // predicate -> signifier binding (for future use)
}

// OrthographyBinding maps a predicate to an orthographic signifier.
type OrthographyBinding struct {
	Signifier  string // "@", "!", etc. Empty = word-key only
	ValueModel string // "person", "priority", enum name, etc.
}

// BuildSignifierMap builds a reverse mapping from signifier string to predicate
// name, collected from all type definitions' orthography bindings. For example,
// if the task type binds "assignee" to signifier "@", the map will contain
// {"@": "assignee"}.
func BuildSignifierMap(allTypes map[string]*TypeDef) map[string]string {
	m := make(map[string]string)
	for _, td := range allTypes {
		for pred, binding := range td.Projection.Orthography {
			if binding.Signifier != "" {
				// First binding wins; don't overwrite.
				if _, exists := m[binding.Signifier]; !exists {
					m[binding.Signifier] = pred
				}
			}
		}
	}
	return m
}
