package graph

import (
	"reflect"
	"testing"
)

func TestDiffParsedBlocks_InsertBeforeListKeepsTaskIdentity(t *testing.T) {
	oldBlocks := extractBlocks("# Intro\n\n- [x] done item\n- plain item\n")
	newBlocks := extractBlocks("# Intro\n\nInserted note.\n\n- [x] done item\n- plain item\n")

	diff := DiffParsedBlocks(oldBlocks, newBlocks)

	if !reflect.DeepEqual(diff.Inserted, []string{"1"}) {
		t.Fatalf("Inserted = %v, want %v", diff.Inserted, []string{"1"})
	}
	if !reflect.DeepEqual(diff.Unchanged, []ParsedBlockChange{
		{OldPath: "0", NewPath: "0"},
		{OldPath: "1.0", NewPath: "2.0"},
		{OldPath: "1.1", NewPath: "2.1"},
	}) {
		t.Fatalf("Unchanged = %v", diff.Unchanged)
	}
}

func TestDiffParsedBlocks_MoveTaskUnderNewHeading(t *testing.T) {
	oldBlocks := extractBlocks("# Todo\n\n- [x] done item\n\n# Later\n")
	newBlocks := extractBlocks("# Todo\n\n# Later\n\n- [x] done item\n")

	diff := DiffParsedBlocks(oldBlocks, newBlocks)

	if !reflect.DeepEqual(diff.ScopeChanged, []ParsedBlockChange{
		{OldPath: "1.0", NewPath: "2.0"},
	}) {
		t.Fatalf("ScopeChanged = %v", diff.ScopeChanged)
	}
}

func TestDiffParsedBlocks_SmallParagraphEditKeepsIdentity(t *testing.T) {
	oldBlocks := extractBlocks("# Intro\n\nSome body text under the heading.\n")
	newBlocks := extractBlocks("# Intro\n\nSome extra body text under the heading.\n")

	diff := DiffParsedBlocks(oldBlocks, newBlocks)

	if len(diff.Inserted) != 0 || len(diff.Deleted) != 0 {
		t.Fatalf("expected edit match, got inserted=%v deleted=%v", diff.Inserted, diff.Deleted)
	}
	if !reflect.DeepEqual(diff.Edited, []ParsedBlockChange{
		{OldPath: "1", NewPath: "1"},
	}) {
		t.Fatalf("Edited = %v", diff.Edited)
	}
}

func TestDiffParsedBlocks_OnlyFlagsActualRelativeReorder(t *testing.T) {
	oldBlocks := extractBlocks("# Intro\n\n- first\n- second\n- third\n")
	newBlocks := extractBlocks("# Intro\n\n- second\n- first\n- third\n")

	diff := DiffParsedBlocks(oldBlocks, newBlocks)

	if len(diff.Inserted) != 0 || len(diff.Deleted) != 0 {
		t.Fatalf("expected pure reorder, got inserted=%v deleted=%v", diff.Inserted, diff.Deleted)
	}
	if len(diff.Reordered) == 0 {
		t.Fatalf("expected reordered blocks, got none")
	}
}
