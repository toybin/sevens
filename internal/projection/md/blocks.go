package md

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

var taskSignifierRe = regexp.MustCompile(`^\[([^\]]*)\]\s*(.*)$`)

// ExtractBlocks parses markdown body text into structural blocks.
func ExtractBlocks(body string) []ParsedBlock {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	source := []byte(body)
	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader(source))

	var blocks []ParsedBlock
	var walk func(node gast.Node, path []int)
	walk = func(node gast.Node, path []int) {
		switch n := node.(type) {
		case *gast.Heading:
			block := newBlock(path, "heading", plainText(n, source))
			block.Level = n.Level
			blocks = append(blocks, block)
		case *gast.Paragraph:
			if _, inListItem := node.Parent().(*gast.ListItem); inListItem {
				break
			}
			blocks = append(blocks, newBlock(path, "paragraph", plainText(n, source)))
		case *gast.ListItem:
			blocks = append(blocks, extractListItemBlock(n, source, path))
		}

		i := 0
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			walk(child, append(append([]int(nil), path...), i))
			i++
		}
	}

	i := 0
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		walk(child, []int{i})
		i++
	}
	return annotateBlocks(blocks)
}

func newBlock(path []int, kind, text string) ParsedBlock {
	return ParsedBlock{
		Path: formatBlockPath(path),
		Kind: kind,
		Text: text,
	}
}

func extractListItemBlock(item *gast.ListItem, source []byte, path []int) ParsedBlock {
	text := plainText(item, source)
	m := taskSignifierRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return newBlock(path, "list-item", text)
	}
	signifier := strings.TrimSpace(m[1])
	content := strings.TrimSpace(m[2])
	block := newBlock(path, "task", content)
	block.Signifier = signifier
	return block
}

func formatBlockPath(path []int) string {
	if len(path) == 0 {
		return "root"
	}
	parts := make([]string, len(path))
	for i, n := range path {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

func plainText(n gast.Node, source []byte) string {
	var sb strings.Builder
	_ = gast.Walk(n, func(node gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		switch v := node.(type) {
		case *gast.Text:
			sb.Write(v.Text(source))
			if v.HardLineBreak() || v.SoftLineBreak() {
				sb.WriteByte(' ')
			}
		case *gast.String:
			sb.Write(v.Value)
		}
		return gast.WalkContinue, nil
	})
	return strings.TrimSpace(sb.String())
}

// annotateBlocks fills in HeadingChain for each block based on
// heading nesting.
func annotateBlocks(blocks []ParsedBlock) []ParsedBlock {
	var headingChain []string
	for i := range blocks {
		block := &blocks[i]
		if block.Kind == "heading" {
			level := block.Level
			if level <= 0 {
				level = 1
			}
			if level-1 < len(headingChain) {
				headingChain = headingChain[:level-1]
			}
			headingChain = append(headingChain, block.Text)
			block.HeadingChain = append([]string(nil), headingChain...)
		} else {
			block.HeadingChain = append([]string(nil), headingChain...)
		}
	}
	return blocks
}

// VisibleBlockScope returns the heading scope that should be displayed
// for a block. For headings, removes the last element if it matches
// the block's own text (since showing "Heading > Heading" is redundant).
func VisibleBlockScope(kind, text string, scope []string) []string {
	if kind != "heading" || len(scope) == 0 {
		return append([]string(nil), scope...)
	}
	last := strings.TrimSpace(scope[len(scope)-1])
	if strings.EqualFold(last, strings.TrimSpace(text)) {
		return append([]string(nil), scope[:len(scope)-1]...)
	}
	return append([]string(nil), scope...)
}

// ScopeString joins a heading chain into a display string.
func ScopeString(scope []string) string {
	if len(scope) == 0 {
		return ""
	}
	return strings.Join(scope, " > ")
}

// RenderBlockMarkdown renders a block back to markdown syntax.
func RenderBlockMarkdown(block ParsedBlock, baseHeadingLevel int) string {
	switch block.Kind {
	case "paragraph":
		return block.Text
	case "list-item":
		return "- " + block.Text
	case "task":
		if block.Signifier == "" {
			return "- [ ] " + block.Text
		}
		return "- [" + block.Signifier + "] " + block.Text
	case "heading":
		level := block.Level
		if baseHeadingLevel > 0 {
			level = level - baseHeadingLevel + 1
			if level < 2 {
				level = 2
			}
		}
		if level < 1 {
			level = 1
		}
		return strings.Repeat("#", level) + " " + block.Text
	default:
		return block.Text
	}
}

// FindBlockByPath finds a block by its dotted path in a block list.
func FindBlockByPath(blocks []ParsedBlock, blockPath string) (ParsedBlock, int, error) {
	for i, block := range blocks {
		if block.Path == blockPath {
			return block, i, nil
		}
	}
	return ParsedBlock{}, -1, fmt.Errorf("block path not found: %s", blockPath)
}

// SelectExtractedBlocks selects a block and all blocks under it
// (for headings, includes all blocks under the same heading scope).
func SelectExtractedBlocks(blocks []ParsedBlock, idx int) []ParsedBlock {
	target := blocks[idx]
	if target.Kind != "heading" {
		return []ParsedBlock{target}
	}

	prefix := target.HeadingChain
	selected := []ParsedBlock{target}
	for i := idx + 1; i < len(blocks); i++ {
		if !hasHeadingPrefix(blocks[i].HeadingChain, prefix) {
			break
		}
		selected = append(selected, blocks[i])
	}
	return selected
}

func hasHeadingPrefix(chain, prefix []string) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(chain) < len(prefix) {
		return false
	}
	for i := range prefix {
		if chain[i] != prefix[i] {
			return false
		}
	}
	return true
}

// RenderExtractedContent renders content for a newly extracted node.
func RenderExtractedContent(sourceTitle string, target ParsedBlock, selected []ParsedBlock) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Source: [[%s]] (block %s)\n", sourceTitle, target.Path))
	if len(target.HeadingChain) > 0 {
		sb.WriteString(fmt.Sprintf("Source scope: %s\n", ScopeString(target.HeadingChain)))
	}
	sb.WriteString("\n")

	if target.Kind == "heading" {
		baseLevel := target.Level
		for i, block := range selected {
			if i == 0 {
				continue
			}
			rendered := RenderBlockMarkdown(block, baseLevel)
			if rendered == "" {
				continue
			}
			sb.WriteString(rendered)
			sb.WriteString("\n\n")
		}
		if len(selected) == 1 {
			sb.WriteString("_Section extracted from source._\n")
		}
		return strings.TrimRight(sb.String(), "\n") + "\n"
	}

	sb.WriteString(RenderBlockMarkdown(target, 0))
	sb.WriteString("\n")
	return sb.String()
}
