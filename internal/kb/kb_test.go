package kb_test

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"

	"sevens/internal/graphops"
	"sevens/internal/kb"
	"sevens/internal/triple"

	_ "turso.tech/database/tursogo"
)

const testRoot = "/tmp/test-root"

// walkNodeTitles extracts titles from a slice of WalkNode.
func walkNodeTitles(nodes []kb.WalkNode) []string {
	var titles []string
	for _, n := range nodes {
		titles = append(titles, n.Title)
	}
	return titles
}

func testKB(t *testing.T) *kb.KB {
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
	return kb.New(graph)
}

func ctx() context.Context { return context.Background() }

// --- Identity ---

func TestNodeSubject(t *testing.T) {
	s1 := kb.NodeSubject("/root1", "My Note")
	s2 := kb.NodeSubject("/root2", "My Note")
	if s1 == s2 {
		t.Fatal("subjects for different roots should differ")
	}
	// Same root+title should be deterministic
	s3 := kb.NodeSubject("/root1", "My Note")
	if s1 != s3 {
		t.Fatal("same root+title should produce same subject")
	}
}

func TestBlockSubject(t *testing.T) {
	s := kb.BlockSubject("/root", "My Note", "0.1.2")
	if s == "" {
		t.Fatal("block subject should not be empty")
	}
	// Should differ from node subject
	ns := kb.NodeSubject("/root", "My Note")
	if s == ns {
		t.Fatal("block and node subjects should differ")
	}
}

// --- CreateNode ---

func TestCreateNode(t *testing.T) {
	k := testKB(t)

	parent := "Parent Note"
	subj, err := k.CreateNode(ctx(), testRoot, "Child Note", "hello world", &parent)
	if err != nil {
		t.Fatal(err)
	}
	if subj == "" {
		t.Fatal("expected non-empty subject")
	}

	// Verify via Walk
	w, err := k.Walk(ctx(), testRoot, "Child Note", kb.GatherNeighborhood)
	if err != nil {
		t.Fatal(err)
	}
	if w.Target.Title != "Child Note" {
		t.Fatalf("expected title 'Child Note', got %q", w.Target.Title)
	}
	if w.Target.Content != "hello world" {
		t.Fatalf("expected content 'hello world', got %q", w.Target.Content)
	}
}

func TestCreateNodeNoParent(t *testing.T) {
	k := testKB(t)

	_, err := k.CreateNode(ctx(), testRoot, "Root Note", "I am root", nil)
	if err != nil {
		t.Fatal(err)
	}

	w, err := k.Walk(ctx(), testRoot, "Root Note", kb.GatherNeighborhood)
	if err != nil {
		t.Fatal(err)
	}
	if w.Parent != nil {
		t.Fatalf("expected nil parent, got %v", w.Parent)
	}
}

// --- Tree navigation ---

func TestWalkParentAndChildren(t *testing.T) {
	k := testKB(t)

	// Create a small tree: Root -> [A, B, C]
	k.CreateNode(ctx(), testRoot, "Root", "root content", nil)
	p := "Root"
	k.CreateNode(ctx(), testRoot, "A", "a content", &p)
	k.CreateNode(ctx(), testRoot, "B", "b content", &p)
	k.CreateNode(ctx(), testRoot, "C", "c content", &p)

	// Walk Root -- should see 3 children
	w, _ := k.Walk(ctx(), testRoot, "Root", kb.GatherNeighborhood)
	childTitles := walkNodeTitles(w.Children)
	sort.Strings(childTitles)
	if len(childTitles) != 3 || childTitles[0] != "A" || childTitles[1] != "B" || childTitles[2] != "C" {
		t.Fatalf("expected children [A B C], got %v", childTitles)
	}

	// Walk A -- should see Root as parent
	w, _ = k.Walk(ctx(), testRoot, "A", kb.GatherNeighborhood)
	if w.Parent == nil || w.Parent.Title != "Root" {
		t.Fatalf("expected parent 'Root', got %v", w.Parent)
	}
}

func TestWalkSiblings(t *testing.T) {
	k := testKB(t)

	k.CreateNode(ctx(), testRoot, "Root", "", nil)
	p := "Root"
	k.CreateNode(ctx(), testRoot, "A", "", &p)
	k.CreateNode(ctx(), testRoot, "B", "", &p)
	k.CreateNode(ctx(), testRoot, "C", "", &p)

	w, _ := k.Walk(ctx(), testRoot, "B", kb.GatherNeighborhood)
	sibTitles := walkNodeTitles(w.Siblings)
	sort.Strings(sibTitles)
	if len(sibTitles) != 2 || sibTitles[0] != "A" || sibTitles[1] != "C" {
		t.Fatalf("expected siblings [A C], got %v", sibTitles)
	}
}

func TestWalkCrossRefs(t *testing.T) {
	k := testKB(t)

	k.CreateNode(ctx(), testRoot, "A", "", nil)
	k.CreateNode(ctx(), testRoot, "B", "", nil)
	k.LinkNodes(ctx(), testRoot, "A", "B")

	w, _ := k.Walk(ctx(), testRoot, "A", kb.GatherNeighborhood)
	if len(w.CrossRefs) != 1 || w.CrossRefs[0] != "B" {
		t.Fatalf("expected cross-refs [B], got %v", w.CrossRefs)
	}
}

func TestWalkRoles(t *testing.T) {
	k := testKB(t)

	k.CreateNode(ctx(), testRoot, "Root", "", nil)
	p := "Root"
	k.CreateNode(ctx(), testRoot, "Pro", "", &p)
	k.CreateNode(ctx(), testRoot, "Con", "", &p)
	k.SetRole(ctx(), testRoot, "Pro", "support")
	k.SetRole(ctx(), testRoot, "Con", "counterexample")

	w, _ := k.Walk(ctx(), testRoot, "Root", kb.GatherNeighborhood)
	if w.ChildRoles["Pro"] != "support" {
		t.Fatalf("expected Pro role 'support', got %q", w.ChildRoles["Pro"])
	}
	if w.ChildRoles["Con"] != "counterexample" {
		t.Fatalf("expected Con role 'counterexample', got %q", w.ChildRoles["Con"])
	}
}

// --- Queries ---

func TestChildren(t *testing.T) {
	k := testKB(t)

	k.CreateNode(ctx(), testRoot, "Root", "", nil)
	p := "Root"
	k.CreateNode(ctx(), testRoot, "X", "", &p)
	k.CreateNode(ctx(), testRoot, "Y", "", &p)

	children, _ := k.Children(ctx(), testRoot, "Root")
	sort.Strings(children)
	if len(children) != 2 || children[0] != "X" || children[1] != "Y" {
		t.Fatalf("expected [X Y], got %v", children)
	}
}

func TestParent(t *testing.T) {
	k := testKB(t)

	k.CreateNode(ctx(), testRoot, "Root", "", nil)
	p := "Root"
	k.CreateNode(ctx(), testRoot, "Child", "", &p)

	parent, _ := k.Parent(ctx(), testRoot, "Child")
	if parent == nil || *parent != "Root" {
		t.Fatalf("expected parent 'Root', got %v", parent)
	}

	// Root has no parent
	parent, _ = k.Parent(ctx(), testRoot, "Root")
	if parent != nil {
		t.Fatalf("expected nil parent for root, got %v", parent)
	}
}

func TestSiblings(t *testing.T) {
	k := testKB(t)

	k.CreateNode(ctx(), testRoot, "Root", "", nil)
	p := "Root"
	k.CreateNode(ctx(), testRoot, "A", "", &p)
	k.CreateNode(ctx(), testRoot, "B", "", &p)

	sibs, _ := k.Siblings(ctx(), testRoot, "A")
	if len(sibs) != 1 || sibs[0] != "B" {
		t.Fatalf("expected [B], got %v", sibs)
	}
}

func TestOverview(t *testing.T) {
	k := testKB(t)

	k.CreateNode(ctx(), testRoot, "Root", "root stuff", nil)
	p := "Root"
	k.CreateNode(ctx(), testRoot, "A", "aaa", &p)
	k.CreateNode(ctx(), testRoot, "B", "bb", &p)

	nodes, err := k.Overview(ctx(), testRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	// Find Root node
	var root *kb.OverviewNode
	for i := range nodes {
		if nodes[i].Title == "Root" {
			root = &nodes[i]
			break
		}
	}
	if root == nil {
		t.Fatal("Root not found in overview")
	}
	if root.ChildCount != 2 {
		t.Fatalf("expected 2 children for Root, got %d", root.ChildCount)
	}
}

func TestResolve(t *testing.T) {
	k := testKB(t)

	k.CreateNode(ctx(), testRoot, "My Note", "", nil)

	subj := k.Resolve(ctx(), testRoot, "My Note")
	if subj == "" {
		t.Fatal("expected to resolve 'My Note'")
	}
	if subj != kb.NodeSubject(testRoot, "My Note") {
		t.Fatalf("resolved subject doesn't match expected: %q", subj)
	}

	// Non-existent
	subj = k.Resolve(ctx(), testRoot, "Nope")
	if subj != "" {
		t.Fatalf("expected empty for non-existent, got %q", subj)
	}
}

// --- Mutations ---

func TestSetContent(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Note", "old", nil)

	k.SetContent(ctx(), testRoot, "Note", "new content")

	w, _ := k.Walk(ctx(), testRoot, "Note", kb.GatherMinimal)
	if w.Target.Content != "new content" {
		t.Fatalf("expected 'new content', got %q", w.Target.Content)
	}
}

func TestMoveNode(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "P1", "", nil)
	k.CreateNode(ctx(), testRoot, "P2", "", nil)
	p1 := "P1"
	k.CreateNode(ctx(), testRoot, "Child", "", &p1)

	k.MoveNode(ctx(), testRoot, "Child", "P2")

	parent, _ := k.Parent(ctx(), testRoot, "Child")
	if parent == nil || *parent != "P2" {
		t.Fatalf("expected parent P2 after move, got %v", parent)
	}
}

func TestDeleteNode(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Doomed", "goodbye", nil)

	err := k.DeleteNode(ctx(), testRoot, "Doomed")
	if err != nil {
		t.Fatal(err)
	}

	subj := k.Resolve(ctx(), testRoot, "Doomed")
	if subj != "" {
		t.Fatal("expected node to be gone after delete")
	}
}

func TestDeleteNodeRefusesWithChildren(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Parent", "", nil)
	p := "Parent"
	k.CreateNode(ctx(), testRoot, "Child", "", &p)

	err := k.DeleteNode(ctx(), testRoot, "Parent")
	if err == nil {
		t.Fatal("expected error deleting node with children")
	}
}

func TestCreateNodeDuplicateErrors(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Unique", "first", nil)

	_, err := k.CreateNode(ctx(), testRoot, "Unique", "second", nil)
	if err == nil {
		t.Fatal("expected error creating duplicate node")
	}
}

func TestMoveNodeCycleErrors(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "A", "", nil)
	p := "A"
	k.CreateNode(ctx(), testRoot, "B", "", &p)
	p2 := "B"
	k.CreateNode(ctx(), testRoot, "C", "", &p2)

	// Try to move A under C (A -> B -> C -> A would be a cycle)
	err := k.MoveNode(ctx(), testRoot, "A", "C")
	if err == nil {
		t.Fatal("expected error for cycle-creating move")
	}
}

func TestUnlinkNodes(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "A", "", nil)
	k.CreateNode(ctx(), testRoot, "B", "", nil)
	k.LinkNodes(ctx(), testRoot, "A", "B")

	k.UnlinkNodes(ctx(), testRoot, "A", "B")

	w, _ := k.Walk(ctx(), testRoot, "A", kb.GatherNeighborhood)
	if len(w.CrossRefs) != 0 {
		t.Fatalf("expected no cross-refs after unlink, got %v", w.CrossRefs)
	}
}

func TestClearRoot(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "A", "aaa", nil)
	k.CreateNode(ctx(), testRoot, "B", "bbb", nil)

	otherRoot := "/other"
	k.CreateNode(ctx(), otherRoot, "C", "ccc", nil)

	k.ClearRoot(ctx(), testRoot)

	if subj := k.Resolve(ctx(), testRoot, "A"); subj != "" {
		t.Fatal("expected A to be cleared")
	}
	if subj := k.Resolve(ctx(), testRoot, "B"); subj != "" {
		t.Fatal("expected B to be cleared")
	}
	if subj := k.Resolve(ctx(), otherRoot, "C"); subj == "" {
		t.Fatal("expected C in other root to survive")
	}
}

// --- Validation ---

func TestValidateOverflow(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Root", "", nil)
	p := "Root"
	for i := 0; i < 5; i++ {
		k.CreateNode(ctx(), testRoot, fmt.Sprintf("Child%d", i), "", &p)
	}

	violations, err := k.Validate(ctx(), testRoot, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range violations {
		if v.Kind == "overflow" && v.Title == "Root" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected overflow violation for Root with 5 children (max 3)")
	}
}

func TestValidateOrphans(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Root", "", nil)
	k.CreateNode(ctx(), testRoot, "Orphan", "lost", nil)

	violations, err := k.Validate(ctx(), testRoot, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range violations {
		if v.Kind == "orphan" && v.Title == "Orphan" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected orphan violation for 'Orphan'")
	}
}

func TestValidateNoCycleInSimpleTree(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Root", "", nil)
	p := "Root"
	k.CreateNode(ctx(), testRoot, "A", "", &p)
	k.CreateNode(ctx(), testRoot, "B", "", &p)
	k.CreateNode(ctx(), testRoot, "C", "", &p)

	violations, err := k.Validate(ctx(), testRoot, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		if v.Kind == "cycle" {
			t.Fatalf("unexpected cycle violation: %s — %s", v.Title, v.Detail)
		}
	}
}

func TestValidateDetectsRealCycle(t *testing.T) {
	k := testKB(t)
	// Create two nodes, then inject a cycle via raw triples
	// (MoveNode refuses cycles, so we go under the hood)
	k.CreateNode(ctx(), testRoot, "A", "", nil)
	pA := "A"
	k.CreateNode(ctx(), testRoot, "B", "", &pA)

	// Force A's parent to B, creating A -> B -> A
	subjA := kb.NodeSubject(testRoot, "A")
	subjB := kb.NodeSubject(testRoot, "B")
	k.Graph().Set(ctx(), subjA, "node/parent", subjB)

	violations, err := k.Validate(ctx(), testRoot, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var cycleCount int
	for _, v := range violations {
		if v.Kind == "cycle" {
			cycleCount++
		}
	}
	if cycleCount == 0 {
		t.Fatal("expected cycle violation for A->B->A cycle")
	}
}

func TestValidateOverlength(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Verbose", "x]x]x]x]x]x]x]x]x]x]", nil) // 22 chars

	violations, _ := k.Validate(ctx(), testRoot, 0, 10)
	var found bool
	for _, v := range violations {
		if v.Kind == "overlength" && v.Title == "Verbose" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected overlength violation")
	}
}

// --- Log ---

func TestAppendAndReadLog(t *testing.T) {
	k := testKB(t)

	k.AppendLog(ctx(), kb.LogEntry{
		Event:     "apply",
		Root:      testRoot,
		Function:  "notice",
		Node:      "My Note",
		Timestamp: "2026-04-10T14:00:00Z",
		Result:    "found 3 gaps",
	})
	k.AppendLog(ctx(), kb.LogEntry{
		Event:     "apply",
		Root:      testRoot,
		Function:  "decompose",
		Node:      "My Note",
		Timestamp: "2026-04-10T15:00:00Z",
		Result:    "suggested 4 children",
	})

	entries, err := k.ReadLog(ctx(), testRoot, "My Note")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	// Should be ordered by timestamp
	if entries[0].Function != "notice" {
		t.Fatalf("expected first entry to be notice, got %q", entries[0].Function)
	}
	if entries[1].Function != "decompose" {
		t.Fatalf("expected second entry to be decompose, got %q", entries[1].Function)
	}
}

func TestLogWithRichFields(t *testing.T) {
	k := testKB(t)

	k.AppendLog(ctx(), kb.LogEntry{
		Event:        "applied",
		Root:         testRoot,
		Function:     "decompose",
		Node:         "Note",
		Timestamp:    "2026-04-10T14:00:00Z",
		Commit:       "abc123",
		Note:         "created 4 children",
		FilesCreated: []string{"child1.md", "child2.md"},
		FilesEdited:  []string{"note.md"},
	})

	entries, err := k.ReadLog(ctx(), testRoot, "Note")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Commit != "abc123" {
		t.Fatalf("expected commit 'abc123', got %q", e.Commit)
	}
	if e.Note != "created 4 children" {
		t.Fatalf("expected note, got %q", e.Note)
	}
	if len(e.FilesCreated) != 2 {
		t.Fatalf("expected 2 files created, got %d", len(e.FilesCreated))
	}
	if len(e.FilesEdited) != 1 {
		t.Fatalf("expected 1 file edited, got %d", len(e.FilesEdited))
	}
}

func TestReadLogFiltersByRoot(t *testing.T) {
	k := testKB(t)

	k.AppendLog(ctx(), kb.LogEntry{
		Event: "apply", Root: testRoot, Node: "Shared Title",
		Function: "notice", Timestamp: "2026-04-10T14:00:00Z",
	})
	k.AppendLog(ctx(), kb.LogEntry{
		Event: "apply", Root: "/other", Node: "Shared Title",
		Function: "elaborate", Timestamp: "2026-04-10T15:00:00Z",
	})

	entries, _ := k.ReadLog(ctx(), testRoot, "Shared Title")
	if len(entries) != 1 || entries[0].Function != "notice" {
		t.Fatalf("expected 1 entry for testRoot, got %d", len(entries))
	}
}

// --- Session ---

func TestSessionLifecycle(t *testing.T) {
	k := testKB(t)
	k.CreateNode(ctx(), testRoot, "Focus Note", "", nil)

	focusSubj := kb.NodeSubject(testRoot, "Focus Note")
	sess, err := k.StartSession(ctx(), focusSubj)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Focus != focusSubj {
		t.Fatalf("expected focus %q, got %q", focusSubj, sess.Focus)
	}
	if sess.Started == "" {
		t.Fatal("expected non-empty started timestamp")
	}

	// Add includes
	incl := kb.NodeSubject(testRoot, "Context Note")
	k.AddInclude(ctx(), sess.Subject, incl)

	// Reload
	loaded, err := k.LoadSession(ctx(), sess.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Includes) != 1 || loaded.Includes[0] != incl {
		t.Fatalf("expected 1 include, got %v", loaded.Includes)
	}

	// Change focus
	newFocus := kb.NodeSubject(testRoot, "Other Note")
	k.SetFocus(ctx(), sess.Subject, newFocus)
	loaded, _ = k.LoadSession(ctx(), sess.Subject)
	if loaded.Focus != newFocus {
		t.Fatalf("expected new focus, got %q", loaded.Focus)
	}

	// End session
	k.EndSession(ctx(), sess.Subject)
	loaded, _ = k.LoadSession(ctx(), sess.Subject)
	if loaded.Ended == "" {
		t.Fatal("expected non-empty ended timestamp")
	}
}
