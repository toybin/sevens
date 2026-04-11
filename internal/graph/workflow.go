package graph

import (
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"sevens/internal/store"
)

var dateTitleRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type InboxItemSummary struct {
	Title        string `edn:"title"`
	FilePath     string `edn:"file-path,omitempty"`
	Kind         string `edn:"kind"`
	CharCount    int    `edn:"char-count"`
	BlockCount   int    `edn:"block-count"`
	HeadingCount int    `edn:"heading-count"`
	BulletCount  int    `edn:"bullet-count"`
	Empty        bool   `edn:"empty"`
	Error        string `edn:"error,omitempty"`
}

type InboxOverview struct {
	NodeTitle string             `edn:"node-title"`
	Items     []InboxItemSummary `edn:"items"`
}

type BlockListEntry struct {
	Path      string   `edn:"path"`
	Kind      string   `edn:"kind"`
	Text      string   `edn:"text"`
	Level     int      `edn:"level,omitempty"`
	Signifier string   `edn:"signifier,omitempty"`
	Scope     []string `edn:"scope,omitempty"`
}

type BlockListOutput struct {
	NodeTitle string           `edn:"node-title"`
	FilePath  string           `edn:"file-path"`
	Blocks    []BlockListEntry `edn:"blocks"`
}

type BlockTarget struct {
	Subject   string   `edn:"subject"`
	NodeTitle string   `edn:"node-title"`
	Path      string   `edn:"path"`
	Kind      string   `edn:"kind"`
	Text      string   `edn:"text"`
	Markdown  string   `edn:"markdown"`
	Level     int      `edn:"level,omitempty"`
	Signifier string   `edn:"signifier,omitempty"`
	Scope     []string `edn:"scope,omitempty"`
}

type NodeEdit struct {
	NodeTitle string `edn:"node-title"`
	FilePath  string `edn:"file-path"`
	OldText   string `edn:"old-text"`
	NewText   string `edn:"new-text"`
}

type ExtractedNode struct {
	Title       string   `edn:"title"`
	ParentTitle string   `edn:"parent-title"`
	SourceTitle string   `edn:"source-title"`
	SourcePath  string   `edn:"source-path"`
	SourceKind  string   `edn:"source-kind"`
	SourceScope []string `edn:"source-scope,omitempty"`
	Content     string   `edn:"content"`
}

func visibleBlockScope(kind, text string, scope []string) []string {
	if kind != "heading" || len(scope) == 0 {
		return append([]string(nil), scope...)
	}
	last := strings.TrimSpace(scope[len(scope)-1])
	if strings.EqualFold(last, strings.TrimSpace(text)) {
		return append([]string(nil), scope[:len(scope)-1]...)
	}
	return append([]string(nil), scope...)
}

func BuildBlockList(db *sql.DB, root, nodeTitle string) (BlockListOutput, error) {
	node, filePath, canonical, err := loadCurrentParsedNode(db, root, nodeTitle)
	if err != nil {
		return BlockListOutput{}, err
	}
	if node == nil {
		return BlockListOutput{}, fmt.Errorf("current file has no titled node: %s", filePath)
	}
	output := BlockListOutput{
		NodeTitle: canonical,
		FilePath:  filePath,
		Blocks:    make([]BlockListEntry, 0, len(node.Blocks)),
	}
	for _, block := range node.Blocks {
		output.Blocks = append(output.Blocks, BlockListEntry{
			Path:      block.Path,
			Kind:      block.Kind,
			Text:      block.Text,
			Level:     block.Level,
			Signifier: block.Signifier,
			Scope:     visibleBlockScope(block.Kind, block.Text, block.HeadingChain),
		})
	}
	return output, nil
}

func ResolveBlockTarget(db *sql.DB, root, nodeTitle, blockPath string) (*BlockTarget, error) {
	nodeSubject, canonical := store.ResolveNode(db, nodeTitle, root)
	if nodeSubject == "" {
		return nil, fmt.Errorf("node not found: %s", nodeTitle)
	}
	var subject string
	err := db.QueryRow(`
		SELECT t1.subject FROM triples t1
		JOIN triples t2 ON t1.subject = t2.subject
		WHERE t1.predicate = 'block/path' AND t1.object = ?
		AND t2.predicate = 'block/node' AND t2.object = ?
		LIMIT 1
	`, blockPath, nodeSubject).Scan(&subject)
	if err != nil {
		return nil, fmt.Errorf("block path not found: %s", blockPath)
	}
	return resolveBlockTargetSubject(db, subject, canonical)
}

func ResolveBlockTargetBySubject(db *sql.DB, subject string) (*BlockTarget, error) {
	return resolveBlockTargetSubject(db, subject, "")
}

func PrepareAppendToNode(db *sql.DB, root, nodeTitle, markdown string) (NodeEdit, error) {
	node, filePath, canonical, err := loadCurrentParsedNode(db, root, nodeTitle)
	if err != nil {
		return NodeEdit{}, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return NodeEdit{}, fmt.Errorf("reading current file: %w", err)
	}
	raw := string(data)
	body := node.Content
	insertion := strings.TrimSpace(markdown)
	if insertion == "" {
		return NodeEdit{}, fmt.Errorf("empty template content")
	}
	newBody := strings.TrimRight(body, "\n")
	if newBody != "" {
		newBody += "\n\n"
	}
	newBody += insertion + "\n"
	newText := raw
	if body == "" {
		newText = strings.TrimRight(raw, "\n")
		if newText != "" {
			newText += "\n\n"
		}
		newText += insertion + "\n"
	} else {
		newText = strings.Replace(raw, body, newBody, 1)
	}
	return NodeEdit{
		NodeTitle: canonical,
		FilePath:  filePath,
		OldText:   raw,
		NewText:   newText,
	}, nil
}

func PrepareInsertUnderHeading(db *sql.DB, root, nodeTitle, heading string, requestedHeadingLevel int, createIfMissing bool, markdown string) (NodeEdit, error) {
	node, filePath, canonical, err := loadCurrentParsedNode(db, root, nodeTitle)
	if err != nil {
		return NodeEdit{}, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return NodeEdit{}, fmt.Errorf("reading current file: %w", err)
	}
	raw := string(data)
	body := node.Content
	if strings.TrimSpace(markdown) == "" {
		return NodeEdit{}, fmt.Errorf("empty template content")
	}
	lines := strings.Split(body, "\n")
	headingPath := parseHeadingRef(heading)
	targetIndex := -1
	targetLevel := 0
	var candidatePaths []string
	searchFrom := 0
	for _, block := range node.Blocks {
		if block.Kind != "heading" {
			continue
		}
		fullPath := ScopeString(block.HeadingChain)
		level := block.Level
		if level < 1 {
			level = 1
		}
		lineIndex := firstMatchingLineAfter(lines, strings.Repeat("#", level)+" "+block.Text, searchFrom)
		if lineIndex != -1 {
			searchFrom = lineIndex + 1
		}
		if matchesHeadingRef(block, headingPath) {
			candidatePaths = append(candidatePaths, fullPath)
			targetIndex = lineIndex
			targetLevel = block.Level
		}
	}
	if len(candidatePaths) > 1 && len(headingPath) == 1 {
		return NodeEdit{}, fmt.Errorf("heading %q is ambiguous; use scoped path like %q", heading, candidatePaths[0])
	}
	if targetIndex == -1 {
		if !createIfMissing {
			return NodeEdit{}, fmt.Errorf("heading not found: %q", heading)
		}
		if len(headingPath) > 1 {
			parentPath := headingPath[:len(headingPath)-1]
			if _, _, ok := findHeadingInsertionPoint(node, body, parentPath); !ok {
				return NodeEdit{}, fmt.Errorf("parent heading path not found: %q", ScopeString(parentPath))
			}
		}
		return prepareInsertWithCreatedHeading(node, canonical, filePath, raw, body, headingPath, requestedHeadingLevel, markdown), nil
	}
	insertAt := len(lines)
	for i := targetIndex + 1; i < len(lines); i++ {
		if level := headingLevel(lines[i]); level > 0 && level <= targetLevel {
			insertAt = i
			break
		}
	}

	var rebuilt []string
	rebuilt = append(rebuilt, lines[:insertAt]...)
	blockLines := strings.Split(strings.TrimSpace(markdown), "\n")
	if len(rebuilt) > 0 && strings.TrimSpace(rebuilt[len(rebuilt)-1]) != "" {
		rebuilt = append(rebuilt, "")
	}
	rebuilt = append(rebuilt, blockLines...)
	if insertAt < len(lines) && len(blockLines) > 0 && strings.TrimSpace(lines[insertAt]) != "" {
		rebuilt = append(rebuilt, "")
	}
	rebuilt = append(rebuilt, lines[insertAt:]...)
	newBody := strings.Join(rebuilt, "\n")
	if !strings.HasSuffix(newBody, "\n") {
		newBody += "\n"
	}
	return NodeEdit{
		NodeTitle: canonical,
		FilePath:  filePath,
		OldText:   raw,
		NewText:   strings.Replace(raw, body, newBody, 1),
	}, nil
}

func prepareInsertWithCreatedHeading(node *ParsedNode, canonical, filePath, raw, body string, headingPath []string, requestedHeadingLevel int, markdown string) NodeEdit {
	targetHeading := headingPath[len(headingPath)-1]
	level := inferHeadingLevel(node, requestedHeadingLevel)
	insertAt := len(strings.Split(body, "\n"))
	if len(headingPath) > 1 {
		parentPath := headingPath[:len(headingPath)-1]
		parentIndex, parentLevel, ok := findHeadingInsertionPoint(node, body, parentPath)
		if ok {
			level = parentLevel + 1
			insertAt = parentIndex
		}
	}
	headerLine := strings.Repeat("#", level) + " " + strings.TrimSpace(targetHeading)
	insertionParts := []string{headerLine, "", strings.TrimSpace(markdown)}
	insertion := strings.Join(insertionParts, "\n") + "\n"

	lines := strings.Split(body, "\n")
	newBody := strings.TrimRight(body, "\n")
	if insertAt < len(lines) {
		var rebuilt []string
		rebuilt = append(rebuilt, lines[:insertAt]...)
		if len(rebuilt) > 0 && strings.TrimSpace(rebuilt[len(rebuilt)-1]) != "" {
			rebuilt = append(rebuilt, "")
		}
		rebuilt = append(rebuilt, strings.Split(strings.TrimRight(insertion, "\n"), "\n")...)
		if insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) != "" {
			rebuilt = append(rebuilt, "")
		}
		rebuilt = append(rebuilt, lines[insertAt:]...)
		newBody = strings.Join(rebuilt, "\n")
		if !strings.HasSuffix(newBody, "\n") {
			newBody += "\n"
		}
	} else {
		if newBody != "" {
			newBody += "\n\n"
		}
		newBody += insertion
	}
	if body == "" {
		newText := strings.TrimRight(raw, "\n")
		if newText != "" {
			newText += "\n\n"
		}
		newText += insertion
		return NodeEdit{
			NodeTitle: canonical,
			FilePath:  filePath,
			OldText:   raw,
			NewText:   newText,
		}
	}
	return NodeEdit{
		NodeTitle: canonical,
		FilePath:  filePath,
		OldText:   raw,
		NewText:   strings.Replace(raw, body, newBody, 1),
	}
}

func parseHeadingRef(heading string) []string {
	parts := strings.Split(heading, ">")
	var path []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			path = append(path, part)
		}
	}
	if len(path) == 0 {
		return []string{strings.TrimSpace(heading)}
	}
	return path
}

func matchesHeadingRef(block ParsedBlock, headingPath []string) bool {
	if len(headingPath) == 1 {
		return strings.EqualFold(strings.TrimSpace(block.Text), headingPath[0])
	}
	if len(block.HeadingChain) != len(headingPath) {
		return false
	}
	for i := range headingPath {
		if !strings.EqualFold(strings.TrimSpace(block.HeadingChain[i]), headingPath[i]) {
			return false
		}
	}
	return true
}

func findHeadingInsertionPoint(node *ParsedNode, body string, parentPath []string) (insertAt int, parentLevel int, ok bool) {
	lines := strings.Split(body, "\n")
	targetIndex := -1
	for _, block := range node.Blocks {
		if block.Kind != "heading" || !matchesHeadingRef(block, parentPath) {
			continue
		}
		level := block.Level
		if level < 1 {
			level = 1
		}
		targetIndex = firstMatchingLine(lines, strings.Repeat("#", level)+" "+block.Text)
		parentLevel = block.Level
		break
	}
	if targetIndex == -1 {
		return 0, 0, false
	}
	insertAt = len(lines)
	for i := targetIndex + 1; i < len(lines); i++ {
		if level := headingLevel(lines[i]); level > 0 && level <= parentLevel {
			insertAt = i
			break
		}
	}
	return insertAt, parentLevel, true
}

func inferHeadingLevel(node *ParsedNode, requested int) int {
	if requested >= 1 && requested <= 6 {
		return requested
	}

	titleLikeLevel := 0
	for _, block := range node.Blocks {
		if block.Kind != "heading" || block.Level < 1 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(block.Text), strings.TrimSpace(node.Title)) && titleLikeLevel == 0 {
			titleLikeLevel = block.Level
			continue
		}
		return block.Level
	}
	if titleLikeLevel > 0 && titleLikeLevel < 6 {
		return titleLikeLevel + 1
	}
	return 2
}

func firstMatchingLine(lines []string, target string) int {
	return firstMatchingLineAfter(lines, target, 0)
}

func firstMatchingLineAfter(lines []string, target string, start int) int {
	if start < 0 {
		start = 0
	}
	for i, line := range lines {
		if i < start {
			continue
		}
		if strings.TrimSpace(line) == strings.TrimSpace(target) {
			return i
		}
	}
	return -1
}

func headingLevel(line string) int {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return 0
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level < len(trimmed) && trimmed[level] == ' ' {
		return level
	}
	return 0
}

func resolveBlockTargetSubject(db *sql.DB, subject string, canonicalNodeTitle string) (*BlockTarget, error) {
	triples, err := store.GetSubjectTriples(db, subject)
	if err != nil {
		return nil, fmt.Errorf("loading block %q: %w", subject, err)
	}
	if len(triples) == 0 {
		return nil, fmt.Errorf("block not found: %s", subject)
	}

	target := &BlockTarget{Subject: subject}
	var nodeSubject string
	for _, triple := range triples {
		switch triple.Predicate {
		case "block/node":
			nodeSubject = triple.Object
		case "block/path":
			target.Path = triple.Object
		case "block/kind":
			target.Kind = triple.Object
		case "block/text":
			target.Text = triple.Object
		case "block/heading-level":
			fmt.Sscanf(triple.Object, "%d", &target.Level)
		case "block/signifier":
			target.Signifier = triple.Object
		case "block/heading-chain":
			if triple.Object != "" {
				target.Scope = visibleBlockScope(target.Kind, target.Text, strings.Split(triple.Object, headingChainSeparator))
			}
		}
	}
	if nodeSubject == "" {
		return nil, fmt.Errorf("block %q has no node", subject)
	}
	if canonicalNodeTitle != "" {
		target.NodeTitle = canonicalNodeTitle
	} else {
		target.NodeTitle, err = store.NodeTitle(db, nodeSubject)
		if err != nil {
			return nil, fmt.Errorf("resolving block node title: %w", err)
		}
	}
	target.Markdown = renderBlockMarkdown(target.Kind, target.Text, target.Level, target.Signifier, 0)
	return target, nil
}

func (b BlockTarget) Label() string {
	return b.NodeTitle + "#" + b.Path
}

func BuildInboxOverview(db *sql.DB, root, nodeTitle string) (InboxOverview, error) {
	if nodeTitle == "" {
		nodeTitle = "inbox"
	}
	walk, err := BuildWalk(db, root, nodeTitle, 1)
	if err != nil {
		return InboxOverview{}, err
	}

	output := InboxOverview{NodeTitle: walk.Node.Title}
	for _, childTitle := range walk.Node.Children {
		summary, err := summarizeNodeForInbox(db, root, childTitle)
		if err != nil {
			return InboxOverview{}, err
		}
		output.Items = append(output.Items, summary)
	}
	sort.Slice(output.Items, func(i, j int) bool {
		return strings.ToLower(output.Items[i].Title) < strings.ToLower(output.Items[j].Title)
	})
	return output, nil
}

func summarizeNodeForInbox(db *sql.DB, root, nodeTitle string) (InboxItemSummary, error) {
	node, filePath, canonical, err := loadCurrentParsedNode(db, root, nodeTitle)
	if err != nil {
		return InboxItemSummary{}, err
	}

	summary := InboxItemSummary{
		Title:    canonical,
		FilePath: filePath,
	}
	if node == nil {
		summary.Kind = "empty"
		summary.Empty = true
		if isDiscussionTitle(canonical) {
			summary.Kind = "discussion"
		}
		if dateTitleRe.MatchString(canonical) {
			summary.Kind = "empty-date"
		}
		return summary, nil
	}

	content := strings.TrimSpace(node.Content)
	summary.CharCount = len(content)
	summary.BlockCount = len(node.Blocks)
	for _, block := range node.Blocks {
		switch block.Kind {
		case "heading":
			summary.HeadingCount++
		case "list-item", "task":
			summary.BulletCount++
		}
	}
	summary.Empty = summary.CharCount == 0
	summary.Kind = classifyInboxItem(canonical, summary)
	return summary, nil
}

func classifyInboxItem(title string, summary InboxItemSummary) string {
	switch {
	case summary.Error != "":
		return "error"
	case isDiscussionTitle(title):
		return "discussion"
	case dateTitleRe.MatchString(title) && summary.Empty:
		return "empty-date"
	case dateTitleRe.MatchString(title):
		return "date"
	case summary.Empty:
		return "empty"
	case summary.BulletCount > 0:
		return "capture"
	default:
		return "note"
	}
}

func PrepareBlockExtraction(db *sql.DB, root, sourceTitle, blockPath, newTitle, parentTitle string) (ExtractedNode, error) {
	node, _, canonical, err := loadCurrentParsedNode(db, root, sourceTitle)
	if err != nil {
		return ExtractedNode{}, err
	}
	if node == nil {
		return ExtractedNode{}, fmt.Errorf("node %q has no current file content", canonical)
	}

	block, idx, err := findBlockByPath(node.Blocks, blockPath)
	if err != nil {
		return ExtractedNode{}, err
	}

	if parentTitle == "" {
		parentTitle = canonical
	}
	parentSubject, parentCanonical := store.ResolveNode(db, parentTitle, root)
	if parentSubject == "" {
		return ExtractedNode{}, fmt.Errorf("parent node not found: %q", parentTitle)
	}
	parentTitle = parentCanonical

	if strings.TrimSpace(newTitle) == "" {
		if block.Kind == "heading" {
			newTitle = block.Text
		} else {
			return ExtractedNode{}, fmt.Errorf("title required for block %s (%s)", block.Path, block.Kind)
		}
	}

	selected := selectExtractedBlocks(node.Blocks, idx)
	content := renderExtractedNodeContent(canonical, block, selected)
	return ExtractedNode{
		Title:       strings.TrimSpace(newTitle),
		ParentTitle: parentTitle,
		SourceTitle: canonical,
		SourcePath:  block.Path,
		SourceKind:  block.Kind,
		SourceScope: append([]string(nil), block.HeadingChain...),
		Content:     content,
	}, nil
}

func loadCurrentParsedNode(db *sql.DB, root, nodeTitle string) (*ParsedNode, string, string, error) {
	nodeSubject, canonical := store.ResolveNode(db, nodeTitle, root)
	if nodeSubject == "" {
		return nil, "", "", fmt.Errorf("node not found: %s", nodeTitle)
	}
	filePath, err := store.GetObject(db, nodeSubject, "node/file-path")
	if err != nil {
		return nil, "", canonical, fmt.Errorf("loading file path: %w", err)
	}
	if filePath == "" {
		return nil, "", canonical, fmt.Errorf("node %q has no file path", canonical)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, filePath, canonical, fmt.Errorf("reading current file: %w", err)
	}
	node, err := parseFile(filePath, data)
	if err != nil {
		return nil, filePath, canonical, fmt.Errorf("parsing current file: %w", err)
	}
	return node, filePath, canonical, nil
}

func findBlockByPath(blocks []ParsedBlock, blockPath string) (ParsedBlock, int, error) {
	for i, block := range blocks {
		if block.Path == blockPath {
			return block, i, nil
		}
	}
	return ParsedBlock{}, -1, fmt.Errorf("block path not found: %s", blockPath)
}

func selectExtractedBlocks(blocks []ParsedBlock, idx int) []ParsedBlock {
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

func renderExtractedNodeContent(sourceTitle string, target ParsedBlock, selected []ParsedBlock) string {
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
			rendered := renderMarkdownBlock(block, baseLevel)
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

	sb.WriteString(renderMarkdownBlock(target, 0))
	sb.WriteString("\n")
	return sb.String()
}

func RenderBlockMarkdown(block BlockListEntry) string {
	return renderBlockMarkdown(block.Kind, block.Text, block.Level, block.Signifier, 0)
}

func renderMarkdownBlock(block ParsedBlock, baseHeadingLevel int) string {
	return renderBlockMarkdown(block.Kind, block.Text, block.Level, block.Signifier, baseHeadingLevel)
}

func renderBlockMarkdown(kind, text string, level int, signifier string, baseHeadingLevel int) string {
	switch kind {
	case "paragraph":
		return text
	case "list-item":
		return "- " + text
	case "task":
		if signifier == "" {
			return "- [ ] " + text
		}
		return "- [" + signifier + "] " + text
	case "heading":
		level := level
		if baseHeadingLevel > 0 {
			level = level - baseHeadingLevel + 1
			if level < 2 {
				level = 2
			}
		}
		if level < 1 {
			level = 1
		}
		return strings.Repeat("#", level) + " " + text
	default:
		return text
	}
}

func isDiscussionTitle(title string) bool {
	lower := strings.ToLower(title)
	return strings.HasPrefix(lower, "discussion:") || strings.HasPrefix(lower, "discussion -")
}
