package function

import (
	"context"
	"testing"

	_ "turso.tech/database/tursogo"
)

func TestEvalMatchChildExists(t *testing.T) {
	k, _, db := setupTestKB(t)
	defer db.Close()

	root := "/tmp/test"
	ctx := context.Background()

	// Create parent and child nodes.
	seedNode(t, k, root, "Project", "project content", nil)
	parent := "Project"
	seedNode(t, k, root, "Discussion - Project", "discussion", &parent)

	clauses := []MatchClause{
		{
			Predicate: Predicate{Kind: "child-exists", Args: []string{"Discussion - Project"}},
			Result:    "edit",
		},
		{
			Predicate: Predicate{Kind: "otherwise"},
			Result:    "create",
		},
	}

	result, err := EvalMatch(ctx, k, root, "Project", clauses)
	if err != nil {
		t.Fatalf("EvalMatch: %v", err)
	}
	if result != "edit" {
		t.Errorf("got %q, want 'edit'", result)
	}
}

func TestEvalMatchChildExistsFallthrough(t *testing.T) {
	k, _, db := setupTestKB(t)
	defer db.Close()

	root := "/tmp/test"
	ctx := context.Background()

	// Create parent with no matching child.
	seedNode(t, k, root, "Project", "content", nil)

	clauses := []MatchClause{
		{
			Predicate: Predicate{Kind: "child-exists", Args: []string{"Discussion - Project"}},
			Result:    "edit",
		},
		{
			Predicate: Predicate{Kind: "otherwise"},
			Result:    "create",
		},
	}

	result, err := EvalMatch(ctx, k, root, "Project", clauses)
	if err != nil {
		t.Fatalf("EvalMatch: %v", err)
	}
	if result != "create" {
		t.Errorf("got %q, want 'create'", result)
	}
}

func TestEvalMatchHasContent(t *testing.T) {
	k, _, db := setupTestKB(t)
	defer db.Close()

	root := "/tmp/test"
	ctx := context.Background()

	seedNode(t, k, root, "Full", "some content", nil)
	seedNode(t, k, root, "Empty", "", nil)

	// Node with content.
	clauses := []MatchClause{
		{
			Predicate: Predicate{Kind: "has-content"},
			Result:    "edit",
		},
		{
			Predicate: Predicate{Kind: "otherwise"},
			Result:    "create",
		},
	}

	result, err := EvalMatch(ctx, k, root, "Full", clauses)
	if err != nil {
		t.Fatalf("EvalMatch for Full: %v", err)
	}
	if result != "edit" {
		t.Errorf("Full: got %q, want 'edit'", result)
	}

	// Node with empty content.
	result, err = EvalMatch(ctx, k, root, "Empty", clauses)
	if err != nil {
		t.Fatalf("EvalMatch for Empty: %v", err)
	}
	if result != "create" {
		t.Errorf("Empty: got %q, want 'create'", result)
	}
}

func TestEvalMatchChildrenCount(t *testing.T) {
	k, _, db := setupTestKB(t)
	defer db.Close()

	root := "/tmp/test"
	ctx := context.Background()

	seedNode(t, k, root, "Parent", "p", nil)
	p := "Parent"
	seedNode(t, k, root, "Child1", "c1", &p)
	seedNode(t, k, root, "Child2", "c2", &p)

	// Exact count match.
	clauses := []MatchClause{
		{
			Predicate: Predicate{Kind: "children-count", Args: []string{"2"}},
			Result:    "full",
		},
		{
			Predicate: Predicate{Kind: "otherwise"},
			Result:    "partial",
		},
	}
	result, err := EvalMatch(ctx, k, root, "Parent", clauses)
	if err != nil {
		t.Fatalf("EvalMatch: %v", err)
	}
	if result != "full" {
		t.Errorf("got %q, want 'full'", result)
	}

	// gte operator.
	clauses = []MatchClause{
		{
			Predicate: Predicate{Kind: "children-count", Args: []string{"3", "gte"}},
			Result:    "many",
		},
		{
			Predicate: Predicate{Kind: "children-count", Args: []string{"1", "gte"}},
			Result:    "some",
		},
		{
			Predicate: Predicate{Kind: "otherwise"},
			Result:    "none",
		},
	}
	result, err = EvalMatch(ctx, k, root, "Parent", clauses)
	if err != nil {
		t.Fatalf("EvalMatch gte: %v", err)
	}
	if result != "some" {
		t.Errorf("gte: got %q, want 'some'", result)
	}
}

func TestEvalMatchOtherwise(t *testing.T) {
	k, _, db := setupTestKB(t)
	defer db.Close()

	root := "/tmp/test"
	ctx := context.Background()

	seedNode(t, k, root, "Node", "x", nil)

	clauses := []MatchClause{
		{
			Predicate: Predicate{Kind: "otherwise"},
			Result:    "text",
		},
	}

	result, err := EvalMatch(ctx, k, root, "Node", clauses)
	if err != nil {
		t.Fatalf("EvalMatch: %v", err)
	}
	if result != "text" {
		t.Errorf("got %q, want 'text'", result)
	}
}

func TestEvalMatchNoMatch(t *testing.T) {
	k, _, db := setupTestKB(t)
	defer db.Close()

	root := "/tmp/test"
	ctx := context.Background()

	seedNode(t, k, root, "Node", "", nil)

	// No otherwise clause, and the predicate doesn't match.
	clauses := []MatchClause{
		{
			Predicate: Predicate{Kind: "has-content"},
			Result:    "edit",
		},
	}

	_, err := EvalMatch(ctx, k, root, "Node", clauses)
	if err == nil {
		t.Error("expected error when no clause matches")
	}
}

func TestEvalMatchUnknownPredicate(t *testing.T) {
	k, _, db := setupTestKB(t)
	defer db.Close()

	root := "/tmp/test"
	ctx := context.Background()

	seedNode(t, k, root, "Node", "x", nil)

	clauses := []MatchClause{
		{
			Predicate: Predicate{Kind: "nonexistent"},
			Result:    "text",
		},
	}

	_, err := EvalMatch(ctx, k, root, "Node", clauses)
	if err == nil {
		t.Error("expected error for unknown predicate")
	}
}
