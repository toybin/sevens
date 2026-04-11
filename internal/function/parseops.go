package function

import (
	"encoding/json"
	"strings"
)

// ParseOps extracts []FileOp from LLM text output.
// Handles code fence wrappers and basic validation.
func ParseOps(raw string) ([]FileOp, error) {
	text := strings.TrimSpace(raw)

	// Strip code fences
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		// Remove first line (```json or ```)
		if len(lines) > 1 {
			lines = lines[1:]
		}
		// Remove last line if it's ```
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			lines = lines[:len(lines)-1]
		}
		text = strings.Join(lines, "\n")
	}

	text = strings.TrimSpace(text)

	// Try to parse as JSON array of FileOps
	var ops []FileOp
	if err := json.Unmarshal([]byte(text), &ops); err != nil {
		// Try wrapping in array if it's a single object
		if err2 := json.Unmarshal([]byte("["+text+"]"), &ops); err2 != nil {
			return nil, err
		}
	}

	// Validate
	var valid []FileOp
	for _, op := range ops {
		if op.Action == "" {
			continue
		}
		if op.Action == "create" && op.Title == "" {
			continue
		}
		if op.Action == "edit" && op.File == "" {
			continue
		}
		valid = append(valid, op)
	}

	return valid, nil
}
