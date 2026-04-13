package types

import (
	"fmt"
	"strings"
)

// ComposeSchemaInstruction walks the type chain (subtype -> primitive)
// and composes the full schema instruction.
// For a primitive, returns its own instruction.
// For a subtype, returns: primitive instruction + subtype constructor fields.
func ComposeSchemaInstruction(td *TypeDef, allTypes map[string]*TypeDef) string {
	if td.Primitive {
		return td.SchemaInstruction
	}

	// If this type extends a primitive, compose the instructions.
	if td.Extends != "" {
		if base, ok := allTypes[td.Extends]; ok {
			baseInstruction := ComposeSchemaInstruction(base, allTypes)
			constructor := renderTypeConstructor(td)
			if constructor != "" {
				return baseInstruction + "\n\n" + constructor
			}
			return baseInstruction
		}
	}

	// No extends and not primitive: return own instruction if present,
	// otherwise empty.
	return td.SchemaInstruction
}

// renderTypeConstructor renders a subtype's constructor fields as prompt
// material. This tells the LLM what frontmatter fields to include when
// creating or suggesting nodes of this type.
func renderTypeConstructor(td *TypeDef) string {
	// For suggestion-extending types, use the suggestion hint format.
	if td.Extends == "suggestion" {
		return renderTypeSuggestionHint(td)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Nodes you create should conform to type %q.\n", td.Name))
	if len(td.Predicates.Required) > 0 {
		b.WriteString(fmt.Sprintf("Required fields in \"extra\": %s\n", strings.Join(td.Predicates.Required, ", ")))
	}
	if len(td.Predicates.Optional) > 0 {
		b.WriteString(fmt.Sprintf("Optional fields in \"extra\": %s\n", strings.Join(td.Predicates.Optional, ", ")))
	}
	if td.Structure.ParentType != "" {
		b.WriteString(fmt.Sprintf("Parent node must conform to type %q.\n", td.Structure.ParentType))
	}

	// Build an example extra object.
	example := make(map[string]string)
	for _, f := range td.Predicates.Required {
		example[f] = "<value>"
	}
	if len(example) > 0 {
		parts := make([]string, 0, len(example))
		for k, v := range example {
			parts = append(parts, fmt.Sprintf("%q: %q", k, v))
		}
		b.WriteString(fmt.Sprintf("Example: {\"extra\": {%s}}", strings.Join(parts, ", ")))
	}
	return b.String()
}

// renderTypeSuggestionHint tells the LLM what type the suggested nodes
// should eventually conform to, so rationales can account for type constraints.
func renderTypeSuggestionHint(td *TypeDef) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Suggested nodes should be designed to conform to type %q.\n", td.Name))
	if len(td.Predicates.Required) > 0 {
		b.WriteString(fmt.Sprintf("This type requires: %s\n", strings.Join(td.Predicates.Required, ", ")))
	}
	b.WriteString("Consider type constraints in your rationale for each suggestion.")
	return b.String()
}
