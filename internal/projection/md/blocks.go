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
			block.SourceStart, block.SourceStop = headingSourceRange(n, source)
			blocks = append(blocks, block)
		case *gast.Paragraph:
			if _, inListItem := node.Parent().(*gast.ListItem); inListItem {
				break
			}
			block := newBlock(path, "paragraph", plainText(n, source))
			block.SourceStart, block.SourceStop = linesSourceRange(n.Lines(), source)
			blocks = append(blocks, block)
		case *gast.ListItem:
			block := extractListItemBlock(n, source, path)
			block.SourceStart, block.SourceStop = listItemSourceRange(n, source)
			blocks = append(blocks, block)
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

// linesSourceRange returns the byte range [start, stop) for a block whose
// content is described by a set of text segments (e.g. paragraphs). stop is
// one past the trailing newline if one is present in the source.
func linesSourceRange(segs *text.Segments, source []byte) (start, stop int) {
	if segs == nil || segs.Len() == 0 {
		return 0, 0
	}
	start = segs.At(0).Start
	stop = segs.At(segs.Len() - 1).Stop
	// stop points at the newline byte; include it.
	if stop < len(source) && source[stop] == '\n' {
		stop++
	}
	return start, stop
}

// headingSourceRange returns the byte range [start, stop) for a heading node.
// Goldmark heading Lines() cover only the text content (after "## "); we walk
// backward to find the '#' that opens the line.
func headingSourceRange(n *gast.Heading, source []byte) (start, stop int) {
	segs := n.Lines()
	if segs == nil || segs.Len() == 0 {
		return 0, 0
	}
	contentStart := segs.At(0).Start
	// Walk backward past the heading text prefix "# " to find the opening '#'.
	lineStart := contentStart
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	stop = segs.At(segs.Len() - 1).Stop
	if stop < len(source) && source[stop] == '\n' {
		stop++
	}
	return lineStart, stop
}

// listItemSourceRange returns the byte range [start, stop) for a list item.
// ListItem nodes have no Lines() of their own; we use node.Pos() for the
// start and the child TextBlock's last line for the stop.
func listItemSourceRange(n *gast.ListItem, source []byte) (start, stop int) {
	start = n.Pos()
	// The list item's content lives in a TextBlock child; use its last line.
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		segs := child.Lines()
		if segs != nil && segs.Len() > 0 {
			s := segs.At(segs.Len() - 1).Stop
			if s > stop {
				stop = s
			}
		}
	}
	if stop < len(source) && source[stop] == '\n' {
		stop++
	}
	if stop == 0 {
		stop = start
	}
	return start, stop
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

// SourceRangeText returns the exact source bytes that span all selected blocks,
// including any blank-line separator that follows the last block. This is used
// to build the OldText for a file-edit operation so that the removal matches
// the original source precisely instead of relying on re-rendered text.
//
// If any block lacks source position data (SourceStart == SourceStop == 0),
// the function returns an empty string so the caller can skip the removal.
func SourceRangeText(body string, selected []ParsedBlock) string {
	if len(selected) == 0 {
		return ""
	}
	start := selected[0].SourceStart
	stop := selected[len(selected)-1].SourceStop
	if start == 0 && stop == 0 {
		return ""
	}
	if stop > len(body) {
		stop = len(body)
	}
	// Include the blank line that separates the last block from the next
	// section so removal leaves a clean result.
	if stop < len(body) && body[stop] == '\n' {
		stop++
	}
	return body[start:stop]
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
