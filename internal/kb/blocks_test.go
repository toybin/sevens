package kb_test

import (
	"context"
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

func TestChildrenSummary(t *testing.T) {
	k := testKB(t)

	// Create container with children
	k.CreateNode(ctx(), testRoot, "Container", "", nil)
	p := "Container"
	k.CreateNode(ctx(), testRoot, "Note A", "some text", &p)
	k.CreateNode(ctx(), testRoot, "Note B", "", &p)

	items, err := k.ChildrenSummary(ctx(), testRoot, "Container")
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Should be sorted by title
	if items[0].Title != "Note A" || items[1].Title != "Note B" {
		t.Fatalf("expected [Note A, Note B], got [%s, %s]", items[0].Title, items[1].Title)
	}

	// Note A has content, Note B is empty
	if items[0].Empty {
		t.Fatal("Note A should not be empty")
	}
	if !items[1].Empty {
		t.Fatal("Note B should be empty")
	}
	if items[0].CharCount == 0 {
		t.Fatal("Note A should have non-zero char count")
	}
}
