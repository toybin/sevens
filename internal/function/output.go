package function

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OutputEnvelope is the universal response format from LLM calls.
// The LLM always returns JSON conforming to one of the sevens output schemas.
// Exactly one of Text, Suggestions, or Ops will be populated.
type OutputEnvelope struct {
	Text        string       `json:"text,omitempty"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
	Ops         []FileOp     `json:"ops,omitempty"`
}

// Suggestion is a proposed child node from decompose-style functions.
type Suggestion struct {
	Title     string `json:"title"`
	Rationale string `json:"rationale"`
}

// PrimitiveTypeName maps an OutputShape to the corresponding primitive type name.
func PrimitiveTypeName(shape OutputShape) string {
	switch shape {
	case ShapeText:
		return "text"
	case ShapeFileOps:
		return "create" // default for ops; edit can be selected via output-type
	case ShapeStructured:
		return "suggestion"
	default:
		return "text"
	}
}

// ParseOutput parses raw LLM output into an OutputEnvelope.
// It tries JSON first, then falls back based on the expected shape.
func ParseOutput(raw string, shape OutputShape) (*OutputEnvelope, error) {
	raw = strings.TrimSpace(raw)

	// Strip markdown code fences if present.
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) >= 2 {
			// Remove first line (```json) and last line (```)
			end := len(lines) - 1
			for end > 0 && strings.TrimSpace(lines[end]) == "" {
				end--
			}
			if strings.TrimSpace(lines[end]) == "```" {
				raw = strings.Join(lines[1:end], "\n")
			}
		}
	}

	// Try parsing as the envelope format.
	var env OutputEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err == nil {
		// Check which field is populated.
		if env.Text != "" || len(env.Suggestions) > 0 || len(env.Ops) > 0 {
			return &env, nil
		}
	}

	// Fallback: try parsing as a bare array (legacy ops or suggestions format).
	switch shape {
	case ShapeFileOps:
		ops, err := ParseOps(raw)
		if err == nil && len(ops) > 0 {
			return &OutputEnvelope{Ops: ops}, nil
		}
	case ShapeStructured:
		var suggestions []Suggestion
		if err := json.Unmarshal([]byte(raw), &suggestions); err == nil && len(suggestions) > 0 {
			return &OutputEnvelope{Suggestions: suggestions}, nil
		}
	case ShapeText:
		// Not JSON — treat as raw text.
		return &OutputEnvelope{Text: raw}, nil
	}

	// Last resort: treat as text.
	return &OutputEnvelope{Text: raw}, fmt.Errorf("could not parse as %v, treating as text", shape)
}
