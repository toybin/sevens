package graph

import (
	"bytes"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/frontmatter"
	"olympos.io/encoding/edn"
	"sevens/internal/store"
	"sevens/internal/ui"
)

var wikiLinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
var taskSignifierRe = regexp.MustCompile(`^\[([^\]]*)\]\s*(.*)$`)
var tagRe = regexp.MustCompile(`(^|[[:space:]\(\[\{'"])#([A-Za-z][A-Za-z0-9_/-]*)`)

func stripWikiLink(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	return s
}

func ExpandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

func LoadConfig(root string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(root, ".sevens.edn"))
	if err != nil {
		return Config{}, fmt.Errorf("reading .sevens.edn in %s: %w", root, err)
	}
	var cfg Config
	if err := edn.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing .sevens.edn in %s: %w", root, err)
	}
	cfg.Path = ExpandTilde(cfg.Path)
	return cfg, nil
}

func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving path %s: %w", dir, err)
	}
	current := abs
	for {
		candidate := filepath.Join(current, ".sevens.edn")
		if _, err := os.Stat(candidate); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no .sevens.edn found in %s or any parent directory", abs)
		}
		current = parent
	}
}

func ScanFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning files in %s: %w", root, err)
	}
	return files, nil
}

func extractBody(data []byte) string {
	if !bytes.HasPrefix(data, []byte("---")) {
		return strings.TrimSpace(string(data))
	}
	rest := data[3:]
	newline := bytes.IndexByte(rest, '\n')
	if newline == -1 {
		return strings.TrimSpace(string(data))
	}
	rest = rest[newline+1:]

	closing := []byte("\n---")
	idx := bytes.Index(rest, closing)
	if idx == -1 {
		if bytes.HasPrefix(rest, []byte("---")) {
			after := rest[3:]
			nl := bytes.IndexByte(after, '\n')
			if nl == -1 {
				return ""
			}
			return strings.TrimSpace(string(after[nl+1:]))
		}
		return strings.TrimSpace(string(data))
	}

	after := rest[idx+len(closing):]
	nl := bytes.IndexByte(after, '\n')
	if nl == -1 {
		return ""
	}
	return strings.TrimSpace(string(after[nl+1:]))
}

func extractWikiLinks(body string) []string {
	matches := wikiLinkRe.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool)
	result := []string{}
	for _, m := range matches {
		link := m[1]
		if !seen[link] {
			seen[link] = true
			result = append(result, link)
		}
	}
	return result
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

func extractTags(text string) []string {
	matches := tagRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var tags []string
	for _, m := range matches {
		tag := m[2]
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

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
		fillBlockIdentity(block)
	}
	return blocks
}

func newBlock(path []int, kind, text string) ParsedBlock {
	return ParsedBlock{
		Path: formatBlockPath(path),
		Kind: kind,
		Text: text,
		Tags: extractTags(text),
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

func extractBlocks(body string) []ParsedBlock {
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

func parseFile(filePath string, data []byte) (*ParsedNode, error) {
	md := goldmark.New(goldmark.WithExtensions(&frontmatter.Extender{}))
	ctx := parser.NewContext()
	var buf bytes.Buffer
	if err := md.Convert(data, &buf, parser.WithContext(ctx)); err != nil {
		return nil, fmt.Errorf("converting %s: %w", filePath, err)
	}

	var fm Frontmatter
	d := frontmatter.Get(ctx)
	if d != nil {
		if err := d.Decode(&fm); err != nil {
			return nil, fmt.Errorf("decoding frontmatter in %s: %w", filePath, err)
		}
	}

	if fm.Title == "" {
		return nil, nil
	}

	body := extractBody(data)
	crossRefs := extractWikiLinks(body)
	blocks := extractBlocks(body)

	var parent *string
	if fm.Parent != "" {
		p := stripWikiLink(fm.Parent)
		parent = &p
	}

	return &ParsedNode{
		Title:        fm.Title,
		Parent:       parent,
		FilePath:     filePath,
		Content:      body,
		MaxChars:     fm.MaxChars,
		ContextFiles: fm.ContextFiles,
		CrossRefs:    crossRefs,
		SiblingRole:  fm.SiblingRole,
		IncludeGroup: fm.IncludeGroup,
		Blocks:       blocks,
	}, nil
}

func ParseAllFiles(files []string) ([]ParsedNode, []string) {
	seen := make(map[string]bool)
	var nodes []ParsedNode
	duplicates := []string{}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s reading %s: %v\n", ui.Warning.Render("[warn]"), f, err)
			continue
		}
		node, err := parseFile(f, data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s parsing %s: %v\n", ui.Warning.Render("[warn]"), f, err)
			continue
		}
		if node == nil {
			continue
		}
		if seen[node.Title] {
			duplicates = append(duplicates, node.Title)
			continue
		}
		seen[node.Title] = true
		nodes = append(nodes, *node)
	}

	return nodes, duplicates
}

func resolveMaxChars(root, title string, db *sql.DB, config Config) *int {
	currentSubject, _ := store.ResolveNode(db, title, root)
	if currentSubject == "" {
		return config.MaxChars
	}
	visited := make(map[string]bool)
	for {
		if visited[currentSubject] {
			break
		}
		visited[currentSubject] = true

		// Check this node's max-chars
		mc, err := store.GetObject(db, currentSubject, "node/max-chars")
		if err == nil && mc != "" {
			v, _ := strconv.Atoi(mc)
			return &v
		}

		// Walk up to parent
		parent, err := store.GetObject(db, currentSubject, "node/parent")
		if err != nil || parent == "" {
			break
		}
		currentSubject = parent
	}
	return config.MaxChars
}

func Validate(db *sql.DB, root string, config Config) (ValidationReport, error) {
	report := ValidationReport{
		Orphans:          []string{},
		MissingParents:   []string{},
		DuplicateTitles:  []string{},
		Overflow:         []string{},
		LengthViolations: []string{},
	}

	titles, err := store.ListNodeTitles(db, root)
	if err != nil {
		return ValidationReport{}, fmt.Errorf("querying titles: %w", err)
	}
	nodeData, err := store.GetRootNodeData(db, root, []string{
		"node/parent", "content/char-count", "node/content",
	})
	if err != nil {
		return ValidationReport{}, fmt.Errorf("querying node data: %w", err)
	}
	subjectSet := make(map[string]bool, len(titles))
	titleToSubject := make(map[string]string, len(titles))
	parentChildCount := make(map[string]int)
	charCounts := make(map[string]int, len(titles))

	for _, title := range titles {
		subject, _ := store.ResolveNode(db, title, root)
		if subject == "" {
			continue
		}
		titleToSubject[title] = subject
		subjectSet[subject] = true

		if counts := nodeData[title]["content/char-count"]; len(counts) > 0 {
			charCounts[title], _ = strconv.Atoi(counts[0])
		} else if content := nodeData[title]["node/content"]; len(content) > 0 {
			charCounts[title] = len([]rune(content[0]))
		}
	}

	for _, title := range titles {
		if parents := nodeData[title]["node/parent"]; len(parents) > 0 {
			parentChildCount[parents[0]]++
			if !subjectSet[parents[0]] {
				report.MissingParents = append(report.MissingParents, title)
			}
		}
	}

	for _, title := range titles {
		subject := titleToSubject[title]
		hasParent := len(nodeData[title]["node/parent"]) > 0
		if !hasParent && parentChildCount[subject] == 0 && charCounts[title] <= 100 {
			report.Orphans = append(report.Orphans, title)
		}
		if limit := resolveMaxChars(root, title, db, config); limit != nil && charCounts[title] > *limit {
			report.LengthViolations = append(report.LengthViolations,
				fmt.Sprintf("%s (%d/%d chars)", title, charCounts[title], *limit))
		}
	}

	for parentSubject, count := range parentChildCount {
		if count > 9 {
			parent, _ := store.NodeTitle(db, parentSubject)
			report.Overflow = append(report.Overflow, parent)
		}
	}

	return report, nil
}

func PrintValidationReport(report ValidationReport, nodeCount int) {
	fmt.Fprintf(os.Stderr, "%s Loaded %d nodes\n", ui.Success.Render("[sync]"), nodeCount)

	if len(report.DuplicateTitles) > 0 {
		fmt.Fprintf(os.Stderr, "%s Duplicate titles: %s\n", ui.Warning.Render("[warn]"), formatList(report.DuplicateTitles))
	}
	if len(report.MissingParents) > 0 {
		fmt.Fprintf(os.Stderr, "%s Missing parents: %s\n", ui.Warning.Render("[warn]"), formatList(report.MissingParents))
	}
	if len(report.Orphans) > 0 {
		fmt.Fprintf(os.Stderr, "%s Orphans: %s\n", ui.Warning.Render("[warn]"), formatList(report.Orphans))
	}
	if len(report.Overflow) > 0 {
		fmt.Fprintf(os.Stderr, "%s Overflow (>9 children): %s\n", ui.Warning.Render("[warn]"), formatList(report.Overflow))
		fmt.Fprintf(os.Stderr, "%s\n", ui.Dim.Render("  hint: consider running `sevens apply decompose` on overflow nodes, or grouping children under intermediate nodes"))
	}
	if len(report.LengthViolations) > 0 {
		fmt.Fprintf(os.Stderr, "%s Length violations: %s\n", ui.Warning.Render("[warn]"), formatList(report.LengthViolations))
		fmt.Fprintf(os.Stderr, "%s\n", ui.Dim.Render("  hint: consider running `sevens apply decompose` on nodes exceeding their character limit"))
	}

	fmt.Fprintf(os.Stderr, "%s Done\n", ui.Success.Render("[sync]"))
}

func formatList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = fmt.Sprintf("%q", item)
	}
	return strings.Join(quoted, ", ")
}
