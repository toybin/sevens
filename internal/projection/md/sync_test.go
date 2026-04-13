package md_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sevens/internal/graphops"
	"sevens/internal/kb"
	"sevens/internal/projection"
	"sevens/internal/projection/md"
	"sevens/internal/triple"

	_ "turso.tech/database/tursogo"
)

func ctx() context.Context { return context.Background() }

// walkNodeTitles extracts titles from a slice of WalkNode.
func walkNodeTitles(nodes []kb.WalkNode) []string {
	var titles []string
	for _, n := range nodes {
		titles = append(titles, n.Title)
	}
	return titles
}

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
	proj := md.New(k)

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
	w, err := k.Walk(ctx(), dir, "The Commons", kb.GatherNeighborhood)
	if err != nil {
		t.Fatal(err)
	}
	if w.Target.Content != "A neighborhood commons idea." {
		t.Fatalf("unexpected content: %q", w.Target.Content)
	}
	childTitles := walkNodeTitles(w.Children)
	if len(childTitles) != 1 || childTitles[0] != "Governance" {
		t.Fatalf("expected children [Governance], got %v", childTitles)
	}

	// Child's parent
	w, _ = k.Walk(ctx(), dir, "Governance", kb.GatherNeighborhood)
	if w.Parent == nil || w.Parent.Title != "The Commons" {
		t.Fatalf("expected parent 'The Commons', got %v", w.Parent)
	}
}

func TestSyncWikiLinks(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"a.md": "---\ntitle: A\n---\n\nSee [[B]] for details.",
		"b.md": "---\ntitle: B\n---\n\nSee [[A]] too.",
	})

	proj.Sync(ctx(), dir)

	w, _ := k.Walk(ctx(), dir, "A", kb.GatherNeighborhood)
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

	w, _ := k.Walk(ctx(), dir, "Note", kb.GatherMinimal)
	if w.Target.Content != "Version 2." {
		t.Fatalf("expected 'Version 2.' after resync, got %q", w.Target.Content)
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

	w, _ := k.Walk(ctx(), dir, "Analysis", kb.GatherNeighborhood)
	ct := walkNodeTitles(w.Children)
	sort.Strings(ct)
	if len(ct) != 2 {
		t.Fatalf("expected 2 children, got %v", ct)
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

func TestApplyOpsEdit(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		".sevens.edn": `{:path "` + t.TempDir() + `" :alias "test"}`,
		"note.md":     "---\ntitle: Note\n---\n\nHello world and stuff.\n",
	})

	// Sync to populate triples (so editFile can look up node/file)
	_, err := proj.Sync(ctx(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the node/file triple exists
	subj := kb.NodeSubject(dir, "Note")
	filePath, ok, _ := k.Graph().Lookup(ctx(), subj, kb.PredNodeFile)
	t.Logf("node/file: ok=%v path=%q", ok, filePath)

	ops := []projection.FileOp{{
		Action:  "edit",
		File:    "Note",
		OldText: "Hello world",
		NewText: "Goodbye world",
	}}
	result, err := proj.ApplyOps(ctx(), dir, ops)
	if err != nil {
		t.Fatalf("ApplyOps: %v", err)
	}
	t.Logf("edited: %v", result.FilesEdited)

	content, _ := os.ReadFile(filepath.Join(dir, "note.md"))
	t.Logf("content: %s", string(content))
	if !strings.Contains(string(content), "Goodbye world") {
		t.Fatal("edit was not applied to file")
	}
}

func TestSyncExtraFrontmatter(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"task.md": "---\ntitle: My Task\nstatus: todo\ndeadline: 2026-05-01\npriority: high\n---\n\nDo the thing.",
	})

	result, err := proj.Sync(ctx(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.NodesScanned != 1 {
		t.Fatalf("expected 1 node, got %d", result.NodesScanned)
	}

	// Verify meta/* predicates were created.
	subj := kb.NodeSubject(dir, "My Task")
	allTriples, _ := k.Graph().Store().BySubject(ctx(), subj)

	metaMap := make(map[string]string)
	for _, tr := range allTriples {
		if strings.HasPrefix(tr.Predicate, "meta/") {
			key := strings.TrimPrefix(tr.Predicate, "meta/")
			metaMap[key] = tr.Object
		}
	}
	if metaMap["status"] != "todo" {
		t.Fatalf("expected meta/status=todo, got %q", metaMap["status"])
	}
	if metaMap["deadline"] != "2026-05-01" {
		t.Fatalf("expected meta/deadline=2026-05-01, got %q", metaMap["deadline"])
	}
	if metaMap["priority"] != "high" {
		t.Fatalf("expected meta/priority=high, got %q", metaMap["priority"])
	}
}

func TestWriteExtraFrontmatter(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"task.md": "---\ntitle: My Task\nstatus: todo\ndeadline: 2026-05-01\n---\n\nDo the thing.",
	})

	// Sync to populate KB.
	if _, err := proj.Sync(ctx(), dir); err != nil {
		t.Fatal(err)
	}

	// Write back to file.
	if err := proj.Write(ctx(), dir, "My Task"); err != nil {
		t.Fatal(err)
	}

	// Read the file and verify extra fields round-tripped.
	data, err := os.ReadFile(filepath.Join(dir, "my-task.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "deadline: 2026-05-01") {
		t.Fatalf("expected deadline in rendered frontmatter, got:\n%s", content)
	}
	if !strings.Contains(content, "status: todo") {
		t.Fatalf("expected status in rendered frontmatter, got:\n%s", content)
	}

	// Also verify: re-parse should recover the extra fields.
	fm, _ := md.ParseFrontmatter(content)
	if fm.Extra["status"] != "todo" {
		t.Fatalf("expected Extra[status]=todo, got %q", fm.Extra["status"])
	}
	if fm.Extra["deadline"] != "2026-05-01" {
		t.Fatalf("expected Extra[deadline]=2026-05-01, got %q", fm.Extra["deadline"])
	}

	_ = k // silence unused
}

func TestSyncOrthographyPropertyList(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"task.md": "---\ntitle: Task\n---\n\n## Review PR (status open | @julian)\n\nDetails here.",
	})

	_, err := proj.Sync(ctx(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// Find the heading block subject. ExtractBlocks assigns paths; the heading
	// is the first block so typically path "0".
	blocks := md.ExtractBlocks("## Review PR (status open | @julian)\n\nDetails here.")
	if len(blocks) == 0 {
		t.Fatal("expected at least one block")
	}
	blockSubj := kb.BlockSubject(dir, "Task", blocks[0].Path)

	store := k.Graph().Store()
	triples, _ := store.BySubject(ctx(), blockSubj)
	metaMap := make(map[string]string)
	for _, tr := range triples {
		if strings.HasPrefix(tr.Predicate, "meta/") {
			metaMap[tr.Predicate] = tr.Object
		}
	}
	if metaMap["meta/status"] != "open" {
		t.Fatalf("expected meta/status=open, got %q", metaMap["meta/status"])
	}
	// With orthography bindings loaded, "@" resolves to "assignee".
	// Without bindings, it falls back to the raw signifier "@".
	assigneeVal := metaMap["meta/assignee"]
	if assigneeVal == "" {
		assigneeVal = metaMap["meta/@"]
	}
	if assigneeVal != "julian" {
		t.Fatalf("expected meta/assignee or meta/@=julian, got assignee=%q @=%q",
			metaMap["meta/assignee"], metaMap["meta/@"])
	}
}

func TestSyncOrthographyInlineAtom(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"note.md": "---\ntitle: Note\n---\n\n## Section\n\nTalk to @julian about #research.",
	})

	_, err := proj.Sync(ctx(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// The paragraph block containing @julian and #research.
	blocks := md.ExtractBlocks("## Section\n\nTalk to @julian about #research.")
	var paraBlock md.ParsedBlock
	for _, b := range blocks {
		if b.Kind == "paragraph" {
			paraBlock = b
			break
		}
	}
	blockSubj := kb.BlockSubject(dir, "Note", paraBlock.Path)

	store := k.Graph().Store()
	triples, _ := store.BySubject(ctx(), blockSubj)
	refMap := make(map[string][]string)
	for _, tr := range triples {
		if strings.HasPrefix(tr.Predicate, "ref/") {
			refMap[tr.Predicate] = append(refMap[tr.Predicate], tr.Object)
		}
	}
	if len(refMap["ref/@"]) != 1 || refMap["ref/@"][0] != "julian" {
		t.Fatalf("expected ref/@=[julian], got %v", refMap["ref/@"])
	}
	if len(refMap["ref/#"]) != 1 || refMap["ref/#"][0] != "research" {
		t.Fatalf("expected ref/#=[research], got %v", refMap["ref/#"])
	}
}

func TestSyncOrthographyParagraphParensNoFalsePositive(t *testing.T) {
	proj, k, dir := setup(t, map[string]string{
		"note.md": "---\ntitle: Note\n---\n\nThis is a paragraph (with parenthesized text) in prose.",
	})

	_, err := proj.Sync(ctx(), dir)
	if err != nil {
		t.Fatal(err)
	}

	// Paragraphs are not attachment lines, so FindPropertyLists should not
	// produce meta/* triples. Check all paragraph blocks.
	blocks := md.ExtractBlocks("This is a paragraph (with parenthesized text) in prose.")
	for _, b := range blocks {
		blockSubj := kb.BlockSubject(dir, "Note", b.Path)
		triples, _ := k.Graph().Store().BySubject(ctx(), blockSubj)
		for _, tr := range triples {
			if strings.HasPrefix(tr.Predicate, "meta/") {
				t.Fatalf("unexpected meta triple on paragraph block: %v", tr)
			}
		}
	}
}

func TestParseFrontmatterExtraFields(t *testing.T) {
	input := "---\ntitle: Test\nparent: \"[[Root]]\"\nstatus: active\ntags: research\n---\n\nBody text."
	fm, body := md.ParseFrontmatter(input)

	if fm.Title != "Test" {
		t.Fatalf("expected title 'Test', got %q", fm.Title)
	}
	if fm.Parent != "Root" {
		t.Fatalf("expected parent 'Root', got %q", fm.Parent)
	}
	if fm.Extra["status"] != "active" {
		t.Fatalf("expected Extra[status]='active', got %q", fm.Extra["status"])
	}
	if fm.Extra["tags"] != "research" {
		t.Fatalf("expected Extra[tags]='research', got %q", fm.Extra["tags"])
	}
	if body != "Body text." {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestRenderFrontmatterExtraFields(t *testing.T) {
	fm := md.Frontmatter{
		Title: "Test",
		Extra: map[string]string{
			"status":   "done",
			"deadline": "2026-05-01",
		},
	}
	rendered := md.RenderFrontmatter(fm)

	if !strings.Contains(rendered, "deadline: 2026-05-01") {
		t.Fatalf("expected deadline in output:\n%s", rendered)
	}
	if !strings.Contains(rendered, "status: done") {
		t.Fatalf("expected status in output:\n%s", rendered)
	}
	// Extra fields should come after known fields and be sorted.
	lines := strings.Split(rendered, "\n")
	var extraLines []string
	for _, l := range lines {
		if strings.HasPrefix(l, "deadline:") || strings.HasPrefix(l, "status:") {
			extraLines = append(extraLines, l)
		}
	}
	if len(extraLines) != 2 {
		t.Fatalf("expected 2 extra lines, got %d", len(extraLines))
	}
	// deadline should come before status (alphabetical).
	if !strings.HasPrefix(extraLines[0], "deadline:") {
		t.Fatalf("expected deadline first (sorted), got %v", extraLines)
	}
}
