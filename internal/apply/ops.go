package apply

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sevens/internal/store"
	"sevens/internal/ui"
)

// ExecuteOps runs each FileOp in order, returning the lists of created and
// edited filenames. It stops and returns on the first error.
func ExecuteOps(ops []FileOp, root string, db *sql.DB) (filesCreated []string, filesEdited []string, err error) {
	for _, op := range ops {
		switch op.Action {
		case "create":
			name, err := createFile(op, root)
			if err != nil {
				return filesCreated, filesEdited, fmt.Errorf("create %q: %w", op.Title, err)
			}
			filesCreated = append(filesCreated, name)
		case "edit":
			name, err := editFile(op, root, db)
			if err != nil {
				return filesCreated, filesEdited, fmt.Errorf("edit %q: %w", op.File, err)
			}
			filesEdited = append(filesEdited, name)
		default:
			return filesCreated, filesEdited, fmt.Errorf("unknown action %q", op.Action)
		}
	}
	return filesCreated, filesEdited, nil
}

// stripContentFrontmatter removes any leading YAML frontmatter from LLM-generated
// content so that the system-generated frontmatter in createFile is always authoritative.
func stripContentFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	// Find the closing ---
	rest := content[4:] // skip opening "---\n"
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return content // malformed, leave as-is
	}
	after := rest[idx+4:] // skip "\n---"
	// Skip optional newline after closing ---
	if strings.HasPrefix(after, "\n") {
		after = after[1:]
	}
	return strings.TrimLeft(after, "\n")
}

func createFile(op FileOp, root string) (string, error) {
	content := stripContentFrontmatter(op.Content)

	var sb strings.Builder
	sb.WriteString("---\n")
	// Quote title if it contains YAML-special characters
	if strings.ContainsAny(op.Title, ":{}[]#&*!|>'\"%@`") {
		sb.WriteString("title: \"" + strings.ReplaceAll(op.Title, "\"", "\\\"") + "\"\n")
	} else {
		sb.WriteString("title: " + op.Title + "\n")
	}
	if op.Parent != "" {
		sb.WriteString("parent: \"[[" + op.Parent + "]]\"\n")
	}
	for k, v := range op.ExtraFrontmatter {
		sb.WriteString(k + ": " + v + "\n")
	}
	sb.WriteString("---\n\n")
	sb.WriteString(content)

	filename := SanitizeFilename(op.Title)
	path := filepath.Join(root, filename)

	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("file already exists: %s", filename)
	}

	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "%s Created: %s (%s)\n", ui.Success.Render("[apply]"), op.Title, filename)
	return filename, nil
}

func editFile(op FileOp, root string, db *sql.DB) (string, error) {
	subject, _ := store.ResolveNode(db, op.File, root)
	filePath, err := store.GetObject(db, subject, "node/file-path")
	if err != nil {
		return "", fmt.Errorf("query node file-path: %w", err)
	}
	if filePath == "" {
		return "", fmt.Errorf("node %q not found in database", op.File)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	content := string(data)

	if !strings.Contains(content, op.OldText) {
		// Try fuzzy match — find the most similar substring
		bestMatch, score := findBestMatch(content, op.OldText)
		if score < 0.6 || bestMatch == "" {
			return "", fmt.Errorf("old_text not found in %q (no close match): %q", op.File, truncate(op.OldText, 80))
		}

		fmt.Fprintf(os.Stderr, "%s Exact match failed for %q. Best fuzzy match (%s similar):\n",
			ui.Warning.Render("[edit]"), op.File, ui.Label.Render(fmt.Sprintf("%.0f%%", score*100)))
		fmt.Fprintf(os.Stderr, "  %s %s\n", ui.Dim.Render("Expected:"), truncate(op.OldText, 120))
		fmt.Fprintf(os.Stderr, "  %s    %s\n", ui.Dim.Render("Found:"), truncate(bestMatch, 120))
		fmt.Fprintf(os.Stderr, "  %s [y/N] ", ui.Label.Render("Use fuzzy match?"))

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			return "", fmt.Errorf("old_text not found in %q (fuzzy match rejected)", op.File)
		}

		// Use the fuzzy match
		op.OldText = bestMatch
	}

	updated := strings.Replace(content, op.OldText, op.NewText, 1)
	if err := os.WriteFile(filePath, []byte(updated), 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "%s Edited: %s\n", ui.Success.Render("[apply]"), op.File)
	return filepath.Base(filePath), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// findBestMatch finds the substring in content most similar to target.
// Uses a sliding window approach with simple similarity scoring.
func findBestMatch(content, target string) (string, float64) {
	if len(target) == 0 {
		return "", 0
	}

	targetLines := strings.Split(strings.TrimSpace(target), "\n")
	contentLines := strings.Split(content, "\n")
	targetLen := len(targetLines)

	if targetLen == 0 || len(contentLines) == 0 {
		return "", 0
	}

	bestScore := 0.0
	bestStart := 0
	bestEnd := 0

	// Slide a window of similar size to target over content
	for windowSize := max(1, targetLen-2); windowSize <= min(len(contentLines), targetLen+2); windowSize++ {
		for start := 0; start <= len(contentLines)-windowSize; start++ {
			end := start + windowSize
			window := strings.Join(contentLines[start:end], "\n")
			score := similarity(target, window)
			if score > bestScore {
				bestScore = score
				bestStart = start
				bestEnd = end
			}
		}
	}

	if bestScore < 0.5 {
		return "", bestScore
	}

	return strings.Join(contentLines[bestStart:bestEnd], "\n"), bestScore
}

// similarity computes a simple ratio of matching lines.
func similarity(a, b string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	// Count matching lines
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")

	matches := 0
	for _, al := range aLines {
		al = strings.TrimSpace(al)
		for _, bl := range bLines {
			if strings.TrimSpace(bl) == al && al != "" {
				matches++
				break
			}
		}
	}

	maxLines := len(aLines)
	if len(bLines) > maxLines {
		maxLines = len(bLines)
	}

	return float64(matches) / float64(maxLines)
}
