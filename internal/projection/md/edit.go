package md

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sevens/internal/kb"
)

// NodeEdit represents a file edit (old text -> new text).
type NodeEdit struct {
	FilePath string
	OldText  string
	NewText  string
}

// PrepareAppend computes a NodeEdit that appends markdown to a node's file.
func PrepareAppend(ctx context.Context, k *kb.KB, root, nodeTitle, markdown string) (NodeEdit, error) {
	subject := kb.NodeSubject(root, nodeTitle)
	filePath, ok, _ := k.Graph().Lookup(ctx, subject, kb.PredNodeFile)
	if !ok {
		filePath = filepath.Join(root, SanitizeFilename(nodeTitle))
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return NodeEdit{}, fmt.Errorf("reading %s: %w", filePath, err)
	}

	raw := string(data)
	_, body := ParseFrontmatter(raw)
	insertion := strings.TrimSpace(markdown)
	if insertion == "" {
		return NodeEdit{}, fmt.Errorf("empty content to append")
	}

	newBody := strings.TrimRight(body, "\n")
	if newBody != "" {
		newBody += "\n\n"
	}
	newBody += insertion + "\n"

	var newText string
	if body == "" {
		newText = strings.TrimRight(raw, "\n")
		if newText != "" {
			newText += "\n\n"
		}
		newText += insertion + "\n"
	} else {
		newText = strings.Replace(raw, body, newBody, 1)
	}

	return NodeEdit{FilePath: filePath, OldText: raw, NewText: newText}, nil
}

// PrepareInsertUnderHeading computes a NodeEdit that inserts markdown
// under a named heading in a node's file.
func PrepareInsertUnderHeading(ctx context.Context, k *kb.KB, root, nodeTitle, heading string, createIfMissing bool, markdown string) (NodeEdit, error) {
	subject := kb.NodeSubject(root, nodeTitle)
	filePath, ok, _ := k.Graph().Lookup(ctx, subject, kb.PredNodeFile)
	if !ok {
		filePath = filepath.Join(root, SanitizeFilename(nodeTitle))
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return NodeEdit{}, fmt.Errorf("reading %s: %w", filePath, err)
	}

	raw := string(data)
	_, body := ParseFrontmatter(raw)
	insertion := strings.TrimSpace(markdown)
	if insertion == "" {
		return NodeEdit{}, fmt.Errorf("empty content to insert")
	}

	lines := strings.Split(body, "\n")

	// Find the heading line
	headingIdx := -1
	headingLevel := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for _, c := range trimmed {
				if c == '#' {
					level++
				} else {
					break
				}
			}
			text := strings.TrimSpace(trimmed[level:])
			if strings.EqualFold(text, heading) {
				headingIdx = i
				headingLevel = level
				break
			}
		}
	}

	if headingIdx == -1 && !createIfMissing {
		return NodeEdit{}, fmt.Errorf("heading %q not found", heading)
	}

	var newBody string
	if headingIdx == -1 {
		// Create the heading at the end
		newBody = strings.TrimRight(body, "\n")
		if newBody != "" {
			newBody += "\n\n"
		}
		newBody += "## " + heading + "\n\n" + insertion + "\n"
	} else {
		// Find the end of this heading's section
		endIdx := len(lines)
		for i := headingIdx + 1; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "#") {
				level := 0
				for _, c := range trimmed {
					if c == '#' {
						level++
					} else {
						break
					}
				}
				if level <= headingLevel {
					endIdx = i
					break
				}
			}
		}

		// Insert before the next heading (or at end)
		before := strings.Join(lines[:endIdx], "\n")
		before = strings.TrimRight(before, "\n") + "\n\n" + insertion + "\n"
		if endIdx < len(lines) {
			before += "\n" + strings.Join(lines[endIdx:], "\n")
		}
		newBody = before
	}

	// Reconstruct full file
	fmStr := ""
	fmIdx := strings.Index(raw, body)
	if fmIdx > 0 {
		fmStr = raw[:fmIdx]
	}
	newText := fmStr + newBody

	return NodeEdit{FilePath: filePath, OldText: raw, NewText: newText}, nil
}
