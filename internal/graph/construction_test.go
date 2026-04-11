package graph

import (
	"reflect"
	"testing"
)

func TestDiffConstruction_StableAndInsertedSibling(t *testing.T) {
	oldNodes := []ConstructionNode{
		{ID: "section", ParentID: "doc", InboundID: "make-section"},
		{ID: "task-a", ParentID: "section", InboundID: "brainstorm"},
		{ID: "task-b", ParentID: "section", PrevID: "task-a", InboundID: "brainstorm"},
	}
	newNodes := []ConstructionNode{
		{ID: "section", ParentID: "doc", InboundID: "make-section"},
		{ID: "task-x", ParentID: "section", InboundID: "brainstorm"},
		{ID: "task-a", ParentID: "section", PrevID: "task-x", InboundID: "brainstorm"},
		{ID: "task-b", ParentID: "section", PrevID: "task-a", InboundID: "brainstorm"},
	}

	diff, err := DiffConstruction(oldNodes, newNodes)
	if err != nil {
		t.Fatalf("DiffConstruction returned error: %v", err)
	}

	if !reflect.DeepEqual(diff.Unchanged, []string{"section", "task-b"}) {
		t.Errorf("Unchanged = %v, want %v", diff.Unchanged, []string{"section", "task-b"})
	}
	if !reflect.DeepEqual(diff.Inserted, []string{"task-x"}) {
		t.Errorf("Inserted = %v, want %v", diff.Inserted, []string{"task-x"})
	}
	if !reflect.DeepEqual(diff.Reordered, []string{"task-a"}) {
		t.Errorf("Reordered = %v, want %v", diff.Reordered, []string{"task-a"})
	}
}

func TestDiffConstruction_ReparentAndInboundChange(t *testing.T) {
	oldNodes := []ConstructionNode{
		{ID: "section-a", ParentID: "doc", InboundID: "make-section"},
		{ID: "section-b", ParentID: "doc", PrevID: "section-a", InboundID: "make-section"},
		{ID: "idea", ParentID: "section-a", InboundID: "decompose"},
	}
	newNodes := []ConstructionNode{
		{ID: "section-a", ParentID: "doc", InboundID: "make-section"},
		{ID: "section-b", ParentID: "doc", PrevID: "section-a", InboundID: "make-section"},
		{ID: "idea", ParentID: "section-b", InboundID: "refine"},
	}

	diff, err := DiffConstruction(oldNodes, newNodes)
	if err != nil {
		t.Fatalf("DiffConstruction returned error: %v", err)
	}

	if !reflect.DeepEqual(diff.Reparented, []string{"idea"}) {
		t.Errorf("Reparented = %v, want %v", diff.Reparented, []string{"idea"})
	}
	if !reflect.DeepEqual(diff.InboundChanged, []string{"idea"}) {
		t.Errorf("InboundChanged = %v, want %v", diff.InboundChanged, []string{"idea"})
	}
}

func TestDiffConstruction_Deletion(t *testing.T) {
	oldNodes := []ConstructionNode{
		{ID: "section", ParentID: "doc", InboundID: "make-section"},
		{ID: "obsolete", ParentID: "section", InboundID: "brainstorm"},
	}
	newNodes := []ConstructionNode{
		{ID: "section", ParentID: "doc", InboundID: "make-section"},
	}

	diff, err := DiffConstruction(oldNodes, newNodes)
	if err != nil {
		t.Fatalf("DiffConstruction returned error: %v", err)
	}

	if !reflect.DeepEqual(diff.Deleted, []string{"obsolete"}) {
		t.Errorf("Deleted = %v, want %v", diff.Deleted, []string{"obsolete"})
	}
	if !reflect.DeepEqual(diff.Unchanged, []string{"section"}) {
		t.Errorf("Unchanged = %v, want %v", diff.Unchanged, []string{"section"})
	}
}

func TestDiffConstruction_DuplicateID(t *testing.T) {
	_, err := DiffConstruction(
		[]ConstructionNode{{ID: "dup"}, {ID: "dup"}},
		[]ConstructionNode{{ID: "ok"}},
	)
	if err == nil {
		t.Fatal("expected duplicate ID error")
	}
}
