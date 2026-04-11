// Package md implements the Projection contract for Markdown files.
//
// Owns: markdown parsing, rendering, frontmatter, block identity
// tracking, git operations, and file I/O for the markdown surface.
package md

import (
	"bufio"
	"strings"
)

// ParsedBlock is a structural element within a markdown document.
type ParsedBlock struct {
	Path         string   // dotted path e.g. "0.1.2"
	Kind         string   // heading, paragraph, list-item, task
	Text         string   // plain-text content of the block
	Level        int      // heading level (1-6), 0 for non-headings
	Signifier    string   // task signifier e.g. "x", "!!"
	HeadingChain []string // scope: list of headings above this block
}

// ParsedNode is a fully parsed markdown file.
type ParsedNode struct {
	Title        string
	Parent       *string
	FilePath     string
	Content      string // body after frontmatter
	CrossRefs    []string
	SiblingRole  string
	IncludeGroup bool
	Blocks       []ParsedBlock
}

// Frontmatter is the YAML header of a markdown file.
type Frontmatter struct {
	Title        string
	Parent       string
	SiblingRole  string
	IncludeGroup bool
}

// ParseFrontmatter extracts frontmatter fields from the YAML block
// between --- delimiters. Returns the frontmatter and the body
// (everything after the closing ---).
//
// This is a simplified parser that handles the common case.
// The current code uses goldmark/frontmatter for full YAML parsing.
func ParseFrontmatter(content string) (Frontmatter, string) {
	var fm Frontmatter
	lines := strings.Split(content, "\n")

	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, content
	}

	// Find closing ---
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return fm, content
	}

	// Parse key: value pairs
	for _, line := range lines[1:closeIdx] {
		key, val := parseYAMLLine(line)
		switch key {
		case "title":
			fm.Title = val
		case "parent":
			fm.Parent = stripWikiLink(val)
		case "sibling-role":
			fm.SiblingRole = val
		case "include-group":
			fm.IncludeGroup = val == "true"
		}
	}

	body := strings.Join(lines[closeIdx+1:], "\n")
	body = strings.TrimLeft(body, "\n")
	return fm, body
}

// RenderFrontmatter produces a YAML frontmatter block.
func RenderFrontmatter(fm Frontmatter) string {
	var b strings.Builder
	b.WriteString("---\n")
	if fm.Title != "" {
		b.WriteString("title: " + fm.Title + "\n")
	}
	if fm.Parent != "" {
		b.WriteString("parent: \"[[" + fm.Parent + "]]\"\n")
	}
	if fm.SiblingRole != "" {
		b.WriteString("sibling-role: " + fm.SiblingRole + "\n")
	}
	if fm.IncludeGroup {
		b.WriteString("include-group: true\n")
	}
	b.WriteString("---\n")
	return b.String()
}

// RenderNode produces a complete markdown file from frontmatter and body.
func RenderNode(fm Frontmatter, body string) string {
	header := RenderFrontmatter(fm)
	if body == "" {
		return header
	}
	return header + "\n" + body + "\n"
}

// ExtractWikiLinks finds all [[Target]] links in markdown text.
func ExtractWikiLinks(content string) []string {
	var links []string
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		for {
			start := strings.Index(line, "[[")
			if start == -1 {
				break
			}
			end := strings.Index(line[start+2:], "]]")
			if end == -1 {
				break
			}
			target := line[start+2 : start+2+end]
			if _, ok := seen[target]; !ok {
				seen[target] = struct{}{}
				links = append(links, target)
			}
			line = line[start+2+end+2:]
		}
	}
	return links
}

// SanitizeFilename converts a node title to a safe .md filename.
func SanitizeFilename(title string) string {
	title = strings.ToLower(title)
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			b.WriteRune('-')
		}
	}
	result := b.String()
	// Collapse multiple dashes
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if result == "" {
		result = "untitled"
	}
	return result + ".md"
}

// --- helpers ---

func parseYAMLLine(line string) (string, string) {
	idx := strings.Index(line, ":")
	if idx == -1 {
		return "", ""
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	// Strip quotes
	val = strings.Trim(val, "\"'")
	return key, val
}

func stripWikiLink(s string) string {
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	return s
}
