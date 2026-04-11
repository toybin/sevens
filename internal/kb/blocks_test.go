package kb_test

import (
	"context"
	"sort"
	"testing"

	"sevens/internal/kb"
	"sevens/internal/triple"
)

// assertBlockTriples creates block triples directly in the store,
// mimicking what projection/md.Sync would write.
func assertBlockTriples(t *testing.T, k *kb.KB, root, nodeTitle, path, kind, content, scope string) {
	t.Helper()
	ctx := context.Background()
	nodeSubj := kb.NodeSubject(root, nodeTitle)
	blockSubj := kb.BlockSubject(root, nodeTitle, path)

	triples := []triple.Triple{
		{Subject: blockSubj, Predicate: kb.PredBlockNode, Object: nodeSubj},
		{Subject: blockSubj, Predicate: kb.PredBlockRoot, Object: root},
		{Subject: blockSubj, Predicate: kb.PredBlockPath, Object: path},
		{Subject: blockSubj, Predicate: kb.PredBlockKind, Object: kind},
		{Subject: blockSubj, Predicate: kb.PredBlockContent, Object: content},
	}
	if scope != "" {
		triples = append(triples, triple.Triple{Subject: blockSubj, Predicate: kb.PredBlockScope, Object: scope})
	}
	if err := k.Graph().Store().AssertBatch(ctx, triples); err != nil {
		t.Fatal(err)
	}
}

func TestListBlocks(t *testing.T) {
	k := testKB(t)

	// Create a node
	k.CreateNode(ctx(), testRoot, "My Note", "## Intro\n\nSome text\n\n- item one", nil)

	// Assert block triples (as sync would)
	assertBlockTriples(t, k, testRoot, "My Note", "0", "heading", "Intro", "")
	assertBlockTriples(t, k, testRoot, "My Note", "1", "paragraph", "Some text", "Intro")
	assertBlockTriples(t, k, testRoot, "My Note", "2.0", "list-item", "item one", "Intro")

	blocks, err := k.ListBlocks(ctx(), testRoot, "My Note")
	if err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}

	// Verify ordering by path
	if blocks[0].Path != "0" || blocks[1].Path != "1" || blocks[2].Path != "2.0" {
		t.Fatalf("unexpected block paths: %s, %s, %s", blocks[0].Path, blocks[1].Path, blocks[2].Path)
	}

	// Verify content
	if blocks[0].Kind != "heading" || blocks[0].Content != "Intro" {
		t.Fatalf("unexpected first block: kind=%q content=%q", blocks[0].Kind, blocks[0].Content)
	}
	if blocks[1].Kind != "paragraph" || blocks[1].Content != "Some text" {
		t.Fatalf("unexpected second block: kind=%q content=%q", blocks[1].Kind, blocks[1].Content)
	}
	if blocks[2].Kind != "list-item" || blocks[2].Content != "item one" {
		t.Fatalf("unexpected third block: kind=%q content=%q", blocks[2].Kind, blocks[2].Content)
	}

	// Verify scope
	if blocks[1].Scope != "Intro" {
		t.Fatalf("expected scope 'Intro' for paragraph, got %q", blocks[1].Scope)
	}
}

func TestListBlocksEmpty(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Empty Note", "", nil)

	blocks, err := k.ListBlocks(ctx(), testRoot, "Empty Note")
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks for empty node, got %d", len(blocks))
	}
}

func TestResolveBlock(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Note", "content", nil)

	assertBlockTriples(t, k, testRoot, "Note", "0", "heading", "Section A", "")
	assertBlockTriples(t, k, testRoot, "Note", "1", "paragraph", "Paragraph text", "Section A")

	// Resolve existing block
	block, err := k.ResolveBlock(ctx(), testRoot, "Note", "0")
	if err != nil {
		t.Fatal(err)
	}
	if block == nil {
		t.Fatal("expected non-nil block")
	}
	if block.Kind != "heading" || block.Content != "Section A" {
		t.Fatalf("unexpected block: kind=%q content=%q", block.Kind, block.Content)
	}

	// Resolve non-existent block
	block, err = k.ResolveBlock(ctx(), testRoot, "Note", "99")
	if err != nil {
		t.Fatal(err)
	}
	if block != nil {
		t.Fatal("expected nil for non-existent block")
	}
}

func TestInboxOverview(t *testing.T) {
	k := testKB(t)

	// Create inbox with children
	k.CreateNode(ctx(), testRoot, "inbox", "", nil)
	p := "inbox"
	k.CreateNode(ctx(), testRoot, "Quick Note", "Some quick note text", &p)
	k.CreateNode(ctx(), testRoot, "Discussion: Ideas", "let's discuss", &p)
	k.CreateNode(ctx(), testRoot, "Empty Thing", "", &p)
	k.CreateNode(ctx(), testRoot, "2024-01-15", "daily entry", &p)

	// Add block triples for "Quick Note" to give it bullet blocks
	assertBlockTriples(t, k, testRoot, "Quick Note", "0", "list-item", "first item", "")
	assertBlockTriples(t, k, testRoot, "Quick Note", "1", "task", "do this", "")

	items, err := k.InboxOverview(ctx(), testRoot, "inbox")
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}

	// Items should be sorted by title (case-insensitive)
	titles := make([]string, len(items))
	for i, item := range items {
		titles[i] = item.Title
	}
	sorted := make([]string, len(titles))
	copy(sorted, titles)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Find each item by title and verify classification
	byTitle := make(map[string]kb.InboxItem)
	for _, item := range items {
		byTitle[item.Title] = item
	}

	if byTitle["Quick Note"].Kind != "capture" {
		t.Fatalf("expected 'Quick Note' kind=capture, got %q", byTitle["Quick Note"].Kind)
	}
	if byTitle["Discussion: Ideas"].Kind != "discussion" {
		t.Fatalf("expected 'Discussion: Ideas' kind=discussion, got %q", byTitle["Discussion: Ideas"].Kind)
	}
	if byTitle["Empty Thing"].Kind != "empty" {
		t.Fatalf("expected 'Empty Thing' kind=empty, got %q", byTitle["Empty Thing"].Kind)
	}
	if byTitle["2024-01-15"].Kind != "date" {
		t.Fatalf("expected '2024-01-15' kind=date, got %q", byTitle["2024-01-15"].Kind)
	}
}

func TestInboxOverviewDefaultTitle(t *testing.T) {
	k := testKB(t)

	k.CreateNode(ctx(), testRoot, "inbox", "", nil)
	p := "inbox"
	k.CreateNode(ctx(), testRoot, "Child", "some text", &p)

	// Pass empty string -- should default to "inbox"
	items, err := k.InboxOverview(ctx(), testRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Child" {
		t.Fatalf("expected 1 item 'Child', got %v", items)
	}
}
