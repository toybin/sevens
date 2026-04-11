package kb

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// BlockEntry represents a block within a node.
type BlockEntry struct {
	Subject string
	Path    string
	Kind    string
	Content string
	Scope   string
	Level   int
}

// ListBlocks returns all blocks for a node, ordered by path.
func (k *KB) ListBlocks(ctx context.Context, root, nodeTitle string) ([]BlockEntry, error) {
	nodeSubj := NodeSubject(root, nodeTitle)

	// Find all block subjects where block/node = nodeSubj
	blockSubjects, err := k.graph.Store().ByPredicateObject(ctx, PredBlockNode, nodeSubj)
	if err != nil {
		return nil, err
	}

	var blocks []BlockEntry
	for _, subj := range blockSubjects {
		entry := BlockEntry{Subject: subj}

		triples, err := k.graph.Store().BySubject(ctx, subj)
		if err != nil {
			continue
		}
		for _, t := range triples {
			switch t.Predicate {
			case PredBlockPath:
				entry.Path = t.Object
			case PredBlockKind:
				entry.Kind = t.Object
			case PredBlockContent:
				entry.Content = t.Object
			case PredBlockScope:
				entry.Scope = t.Object
			}
		}
		// Derive level from kind + content for headings
		if entry.Kind == "heading" {
			entry.Level = inferHeadingLevel(entry.Scope, entry.Content)
		}
		blocks = append(blocks, entry)
	}

	// Sort by path (lexicographic on dotted paths works for
	// single-digit indices; good enough for typical documents).
	sort.Slice(blocks, func(i, j int) bool {
		return compareDottedPaths(blocks[i].Path, blocks[j].Path) < 0
	})

	return blocks, nil
}

// ResolveBlock finds a specific block by node title and dotted path.
func (k *KB) ResolveBlock(ctx context.Context, root, nodeTitle, blockPath string) (*BlockEntry, error) {
	subject := BlockSubject(root, nodeTitle, blockPath)

	triples, err := k.graph.Store().BySubject(ctx, subject)
	if err != nil {
		return nil, err
	}
	if len(triples) == 0 {
		return nil, nil
	}

	entry := &BlockEntry{Subject: subject}
	for _, t := range triples {
		switch t.Predicate {
		case PredBlockPath:
			entry.Path = t.Object
		case PredBlockKind:
			entry.Kind = t.Object
		case PredBlockContent:
			entry.Content = t.Object
		case PredBlockScope:
			entry.Scope = t.Object
		}
	}
	if entry.Kind == "heading" {
		entry.Level = inferHeadingLevel(entry.Scope, entry.Content)
	}
	return entry, nil
}

// InboxItem describes a child node's classification for inbox display.
type InboxItem struct {
	Title        string
	Kind         string // "note", "capture", "discussion", "empty", "date", "empty-date"
	CharCount    int
	BlockCount   int
	HeadingCount int
	BulletCount  int
	Empty        bool
}

var dateTitleRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// InboxOverview classifies children of a node for inbox-style display.
func (k *KB) InboxOverview(ctx context.Context, root, nodeTitle string) ([]InboxItem, error) {
	if nodeTitle == "" {
		nodeTitle = "inbox"
	}

	children, err := k.Children(ctx, root, nodeTitle)
	if err != nil {
		return nil, err
	}

	var items []InboxItem
	for _, childTitle := range children {
		item := classifyChild(ctx, k, root, childTitle)
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})

	return items, nil
}

func classifyChild(ctx context.Context, k *KB, root, title string) InboxItem {
	subject := NodeSubject(root, title)
	content, _, _ := k.graph.Lookup(ctx, subject, PredNodeContent)
	charCount := 0
	if cc, ok, _ := k.graph.Lookup(ctx, subject, PredNodeCharCount); ok {
		charCount, _ = strconv.Atoi(cc)
	}

	item := InboxItem{
		Title:     title,
		CharCount: charCount,
		Empty:     strings.TrimSpace(content) == "",
	}

	// Count blocks by kind
	blockSubjects, _ := k.graph.Store().ByPredicateObject(ctx, PredBlockNode, subject)
	item.BlockCount = len(blockSubjects)
	for _, bs := range blockSubjects {
		kind, _, _ := k.graph.Lookup(ctx, bs, PredBlockKind)
		switch kind {
		case "heading":
			item.HeadingCount++
		case "list-item", "task":
			item.BulletCount++
		}
	}

	// Classify
	item.Kind = classifyInboxKind(title, item)
	return item
}

func classifyInboxKind(title string, item InboxItem) string {
	isDiscussion := strings.HasPrefix(strings.ToLower(title), "discussion:") ||
		strings.HasPrefix(strings.ToLower(title), "discussion -")
	isDate := dateTitleRe.MatchString(title)

	switch {
	case isDiscussion:
		return "discussion"
	case isDate && item.Empty:
		return "empty-date"
	case isDate:
		return "date"
	case item.Empty:
		return "empty"
	case item.BulletCount > 0:
		return "capture"
	default:
		return "note"
	}
}

// --- helpers ---

// inferHeadingLevel guesses the heading level from scope depth.
// The scope string is the visible scope (heading chain minus self for
// headings). Level = number of scope segments + 1.
func inferHeadingLevel(scope, content string) int {
	if scope == "" {
		return 1
	}
	// Count " > " separators to determine depth
	parts := strings.Split(scope, " > ")
	return len(parts) + 1
}

// compareDottedPaths compares two dotted numeric paths segment by segment.
func compareDottedPaths(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		ai, _ := strconv.Atoi(aParts[i])
		bi, _ := strconv.Atoi(bParts[i])
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	if len(aParts) < len(bParts) {
		return -1
	}
	if len(aParts) > len(bParts) {
		return 1
	}
	return 0
}
