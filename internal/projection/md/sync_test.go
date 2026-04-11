package md_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"sevens/internal/graphops"
	"sevens/internal/kb"
	"sevens/internal/projection/md"
	"sevens/internal/triple"

	_ "turso.tech/database/tursogo"
)

func ctx() context.Context { return context.Background() }

// setup creates an in-memory KB and a temp directory with markdown files.
func setup(t *testing.T, files map[string]string) (*md.MarkdownProjection, *kb.KB, string) {
	t.Helper()

	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	store, err := triple.New(db)
	if err != nil {
		t.Fatal(err)
	}
	graph := graphops.New(store)
	k := kb.New(graph)
	proj := md.New(k, store)

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return proj, k, dir
}

func TestSyncBasic(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"root.md":  "---\ntitle: The Commons\n---\n\nA neighborhood commons idea.",
		"child.md": "---\ntitle: Governance\nparent: \"[[The Commons]]\"\n---\n\nHow decisions get made.",
	})

	result, err := proj.Sync(ctx(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.NodesScanned != 2 {
		t.Fatalf("expected 2 nodes scanned, got %d", result.NodesScanned)
	}

	// Verify graph state
	w, err := k.Walk(ctx(), dir, "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if w.Content != "A neighborhood commons idea." {
		t.Fatalf("unexpected content: %q", w.Content)
	}
	if len(w.Children) != 1 || w.Children[0] != "Governance" {
		t.Fatalf("expected children [Governance], got %v", w.Children)
	}

	// Child's parent
	w, _ = k.Walk(ctx(), dir, "Governance")
	if w.Parent == nil || *w.Parent != "The Commons" {
		t.Fatalf("expected parent 'The Commons', got %v", w.Parent)
	}
}

func TestSyncWikiLinks(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"a.md": "---\ntitle: A\n---\n\nSee [[B]] for details.",
		"b.md": "---\ntitle: B\n---\n\nSee [[A]] too.",
	})

	proj.Sync(ctx(), dir)

	w, _ := k.Walk(ctx(), dir, "A")
	if len(w.CrossRefs) != 1 || w.CrossRefs[0] != "B" {
		t.Fatalf("expected cross-ref [B], got %v", w.CrossRefs)
	}
}

func TestSyncSkipsNoTitle(t *testing.T) {
	proj, _, dir := setup(t, map[string]string{
		"good.md": "---\ntitle: Good\n---\n\nContent.",
		"bad.md":  "No frontmatter at all, just text.",
	})

	result, _ := proj.Sync(ctx(), dir)
	if result.NodesScanned != 1 {
		t.Fatalf("expected 1 node (skipped bad.md), got %d", result.NodesScanned)
	}
}

func TestSyncResyncs(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"note.md": "---\ntitle: Note\n---\n\nVersion 1.",
	})

	proj.Sync(ctx(), dir)

	// Edit the file
	os.WriteFile(filepath.Join(dir, "note.md"),
		[]byte("---\ntitle: Note\n---\n\nVersion 2."), 0644)

	proj.Sync(ctx(), dir)

	w, _ := k.Walk(ctx(), dir, "Note")
	if w.Content != "Version 2." {
		t.Fatalf("expected 'Version 2.' after resync, got %q", w.Content)
	}
}

func TestSyncClearsDeletedNodes(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"a.md": "---\ntitle: A\n---\n\nKeep.",
		"b.md": "---\ntitle: B\n---\n\nDelete me.",
	})

	proj.Sync(ctx(), dir)

	// Delete b.md
	os.Remove(filepath.Join(dir, "b.md"))

	proj.Sync(ctx(), dir)

	subj := k.Resolve(ctx(), dir, "B")
	if subj != "" {
		t.Fatal("expected B to be gone after resync without file")
	}
	subj = k.Resolve(ctx(), dir, "A")
	if subj == "" {
		t.Fatal("expected A to survive")
	}
}

func TestSyncWithRoles(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"root.md": "---\ntitle: Analysis\n---\n\nPros and cons.",
		"pro.md":  "---\ntitle: Pro\nparent: \"[[Analysis]]\"\nsibling-role: support\n---\n\nFor it.",
		"con.md":  "---\ntitle: Con\nparent: \"[[Analysis]]\"\nsibling-role: counterexample\n---\n\nAgainst it.",
	})

	proj.Sync(ctx(), dir)

	w, _ := k.Walk(ctx(), dir, "Analysis")
	sort.Strings(w.Children)
	if len(w.Children) != 2 {
		t.Fatalf("expected 2 children, got %v", w.Children)
	}
	if w.ChildRoles["Pro"] != "support" {
		t.Fatalf("expected Pro role 'support', got %q", w.ChildRoles["Pro"])
	}
	if w.ChildRoles["Con"] != "counterexample" {
		t.Fatalf("expected Con role 'counterexample', got %q", w.ChildRoles["Con"])
	}
}

func TestSyncTree(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"root.md":       "---\ntitle: Root\n---\n\nTop level.",
		"child1.md":     "---\ntitle: Child 1\nparent: \"[[Root]]\"\n---\n\nFirst.",
		"child2.md":     "---\ntitle: Child 2\nparent: \"[[Root]]\"\n---\n\nSecond.",
		"grandchild.md": "---\ntitle: Grandchild\nparent: \"[[Child 1]]\"\n---\n\nDeep.",
	})

	proj.Sync(ctx(), dir)

	nodes, _ := k.Overview(ctx(), dir)
	if len(nodes) != 4 {
		t.Fatalf("expected 4 nodes in overview, got %d", len(nodes))
	}

	children, _ := k.Children(ctx(), dir, "Root")
	sort.Strings(children)
	if len(children) != 2 || children[0] != "Child 1" || children[1] != "Child 2" {
		t.Fatalf("expected [Child 1, Child 2], got %v", children)
	}

	children, _ = k.Children(ctx(), dir, "Child 1")
	if len(children) != 1 || children[0] != "Grandchild" {
		t.Fatalf("expected [Grandchild], got %v", children)
	}
}
