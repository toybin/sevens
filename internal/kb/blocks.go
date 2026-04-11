package kb

import (
	"context"
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

// ChildSummary describes a child node's basic metrics. No domain
// classification -- the KB doesn't know what an "inbox" or "capture" is.
type ChildSummary struct {
	Title     string
	CharCount int
	Empty     bool
}

// ChildrenSummary returns basic metrics for each child of a node.
func (k *KB) ChildrenSummary(ctx context.Context, root, nodeTitle string) ([]ChildSummary, error) {
	children, err := k.Children(ctx, root, nodeTitle)
	if err != nil {
		return nil, err
	}

	var items []ChildSummary
	for _, childTitle := range children {
		subject := NodeSubject(root, childTitle)
		charCount := 0
		if cc, ok, _ := k.graph.Lookup(ctx, subject, PredNodeCharCount); ok {
			charCount, _ = strconv.Atoi(cc)
		}
		content, _, _ := k.graph.Lookup(ctx, subject, PredNodeContent)
		items = append(items, ChildSummary{
			Title:     childTitle,
			CharCount: charCount,
			Empty:     strings.TrimSpace(content) == "",
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})

	return items, nil
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
