package graph

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	_ "turso.tech/database/tursogo"

	"sevens/internal/store"
)

// testDB creates an in-memory SQLite database with the triples schema initialized.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitTriplesSchema(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testNodeSubject(root, title string) string {
	return store.NodeSubject(root, title)
}

func testNodeTriples(root, title, parent, content string) []store.Triple {
	subject := testNodeSubject(root, title)
	triples := []store.Triple{
		{Subject: subject, Predicate: "node/title", Object: title},
		{Subject: subject, Predicate: "node/root", Object: root},
		{Subject: subject, Predicate: "node/content", Object: content},
	}
	if parent != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "node/parent", Object: testNodeSubject(root, parent)})
	}
	return triples
}

// --- ExpandTilde ---

func TestExpandTilde_WithTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := ExpandTilde("~/foo/bar")
	want := filepath.Join(home, "foo/bar")
	if got != want {
		t.Errorf("ExpandTilde(~/foo/bar) = %q, want %q", got, want)
	}
}

func TestExpandTilde_WithoutTilde(t *testing.T) {
	input := "/absolute/path/no/tilde"
	got := ExpandTilde(input)
	if got != input {
		t.Errorf("ExpandTilde(%q) = %q, want %q", input, got, input)
	}
}

func TestExpandTilde_JustTilde(t *testing.T) {
	// "~" alone does NOT have the prefix "~/" so it should be returned as-is.
	got := ExpandTilde("~")
	if got != "~" {
		t.Errorf("ExpandTilde(~) = %q, want %q", got, "~")
	}
}

func TestExpandTilde_RelativePath(t *testing.T) {
	got := ExpandTilde("relative/path")
	if got != "relative/path" {
		t.Errorf("ExpandTilde(relative/path) = %q, want %q", got, "relative/path")
	}
}

// --- NodeToTriples ---

func TestNodeToTriples_WithParent(t *testing.T) {
	parent := "Parent Node"
	node := ParsedNode{
		Title:    "Child Node",
		Parent:   &parent,
		FilePath: "/tmp/child.md",
		Content:  "Some content",
	}
	triples := NodeToTriples(node, "my-root")

	predicates := triplePredicateMap(triples)

	if predicates["node/root"] != "my-root" {
		t.Errorf("expected node/root = %q, got %q", "my-root", predicates["node/root"])
	}
	if predicates["node/title"] != "Child Node" {
		t.Errorf("expected node/title = %q, got %q", "Child Node", predicates["node/title"])
	}
	if predicates["node/parent"] != store.NodeSubject("my-root", "Parent Node") {
		t.Errorf("expected node/parent = %q, got %q", store.NodeSubject("my-root", "Parent Node"), predicates["node/parent"])
	}
	if predicates["node/file-path"] != "/tmp/child.md" {
		t.Errorf("expected node/file-path = %q, got %q", "/tmp/child.md", predicates["node/file-path"])
	}
	if predicates["node/content"] != "Some content" {
		t.Errorf("expected node/content = %q, got %q", "Some content", predicates["node/content"])
	}
}

func TestNodeToTriples_WithoutParent(t *testing.T) {
	node := ParsedNode{
		Title:    "Root Node",
		Parent:   nil,
		FilePath: "/tmp/root.md",
		Content:  "Root content",
	}
	triples := NodeToTriples(node, "my-root")

	for _, tr := range triples {
		if tr.Predicate == "node/parent" {
			t.Errorf("expected no node/parent triple, got one with object %q", tr.Object)
		}
	}

	predicates := triplePredicateMap(triples)
	if predicates["node/root"] != "my-root" {
		t.Errorf("expected node/root = %q, got %q", "my-root", predicates["node/root"])
	}
}

func TestNodeToTriples_WithCrossRefs(t *testing.T) {
	node := ParsedNode{
		Title:     "Node With Refs",
		FilePath:  "/tmp/refs.md",
		Content:   "Content [[Link A]] and [[Link B]]",
		CrossRefs: []string{"Link A", "Link B"},
	}
	triples := NodeToTriples(node, "root")

	wikiLinks := collectObjects(triples, "ref/wiki-link")
	sort.Strings(wikiLinks)
	if len(wikiLinks) != 2 || wikiLinks[0] != "Link A" || wikiLinks[1] != "Link B" {
		t.Errorf("expected wiki links [Link A, Link B], got %v", wikiLinks)
	}
}

func TestNodeToTriples_WithSiblingRole(t *testing.T) {
	node := ParsedNode{
		Title:       "Sibling Node",
		FilePath:    "/tmp/sib.md",
		Content:     "Content",
		SiblingRole: "executor",
	}
	triples := NodeToTriples(node, "root")

	predicates := triplePredicateMap(triples)
	if predicates["sibling/role"] != "executor" {
		t.Errorf("expected sibling/role = %q, got %q", "executor", predicates["sibling/role"])
	}
}

func TestNodeToTriples_WithMaxChars(t *testing.T) {
	maxChars := 500
	node := ParsedNode{
		Title:    "Capped Node",
		FilePath: "/tmp/capped.md",
		Content:  "Content",
		MaxChars: &maxChars,
	}
	triples := NodeToTriples(node, "root")

	predicates := triplePredicateMap(triples)
	if predicates["node/max-chars"] != "500" {
		t.Errorf("expected node/max-chars = %q, got %q", "500", predicates["node/max-chars"])
	}
}

func TestNodeToTriples_NoSiblingRole(t *testing.T) {
	node := ParsedNode{
		Title:    "Plain Node",
		FilePath: "/tmp/plain.md",
		Content:  "Plain content",
	}
	triples := NodeToTriples(node, "root")

	for _, tr := range triples {
		if tr.Predicate == "sibling/role" {
			t.Errorf("expected no sibling/role triple, got one with object %q", tr.Object)
		}
	}
}

func TestNodeToTriples_WithBlocks(t *testing.T) {
	node := ParsedNode{
		Title:    "Block Node",
		FilePath: "/tmp/block.md",
		Content:  "# Intro #proj/demo\n\n- [x] done item #work/today",
		Blocks: []ParsedBlock{
			{Path: "0", Kind: "heading", Text: "Intro #proj/demo", Level: 1, Tags: []string{"proj/demo"}, HeadingChain: []string{"Intro #proj/demo"}, AnchorHashes: anchorHashesForText("Intro #proj/demo")},
			{Path: "1.0", Kind: "task", Text: "done item #work/today", Signifier: "x", Tags: []string{"work/today"}, HeadingChain: []string{"Intro #proj/demo"}, AnchorHashes: anchorHashesForText("done item #work/today")},
		},
	}

	triples := NodeToTriples(node, "root", map[string]string{
		"0":   store.BlockSubject("root", "Block Node", "0"),
		"1.0": store.BlockSubject("root", "Block Node", "1.0"),
	})

	heading := tripleSubjectPredicateMap(triples, store.BlockSubject("root", "Block Node", "0"))
	if heading["block/root"] != "root" {
		t.Errorf("heading block/root = %q, want %q", heading["block/root"], "root")
	}
	if heading["block/node"] != store.NodeSubject("root", "Block Node") {
		t.Errorf("heading block/node = %q, want %q", heading["block/node"], store.NodeSubject("root", "Block Node"))
	}
	if heading["block/kind"] != "heading" {
		t.Errorf("heading block/kind = %q, want %q", heading["block/kind"], "heading")
	}
	if heading["block/text"] != "Intro #proj/demo" {
		t.Errorf("heading block/text = %q, want %q", heading["block/text"], "Intro #proj/demo")
	}
	if heading["block/heading-level"] != "1" {
		t.Errorf("heading block/heading-level = %q, want %q", heading["block/heading-level"], "1")
	}
	if heading["block/id"] != store.BlockSubject("root", "Block Node", "0") {
		t.Errorf("heading block/id = %q, want %q", heading["block/id"], store.BlockSubject("root", "Block Node", "0"))
	}
	if heading["block/heading-chain"] != "Intro #proj/demo" {
		t.Errorf("heading block/heading-chain = %q, want %q", heading["block/heading-chain"], "Intro #proj/demo")
	}
	if !reflect.DeepEqual(collectSubjectObjects(triples, store.BlockSubject("root", "Block Node", "0"), "block/tag"), []string{"proj/demo"}) {
		t.Errorf("heading block/tag = %v, want %v", collectSubjectObjects(triples, store.BlockSubject("root", "Block Node", "0"), "block/tag"), []string{"proj/demo"})
	}
	if len(collectSubjectObjects(triples, store.BlockSubject("root", "Block Node", "0"), "block/anchor-hash")) == 0 {
		t.Errorf("expected heading block/anchor-hash values")
	}

	task := tripleSubjectPredicateMap(triples, store.BlockSubject("root", "Block Node", "1.0"))
	if task["block/kind"] != "task" {
		t.Errorf("task block/kind = %q, want %q", task["block/kind"], "task")
	}
	if task["block/text"] != "done item #work/today" {
		t.Errorf("task block/text = %q, want %q", task["block/text"], "done item #work/today")
	}
	if task["block/signifier"] != "x" {
		t.Errorf("task block/signifier = %q, want %q", task["block/signifier"], "x")
	}
	if !reflect.DeepEqual(collectSubjectObjects(triples, store.BlockSubject("root", "Block Node", "1.0"), "block/tag"), []string{"work/today"}) {
		t.Errorf("task block/tag = %v, want %v", collectSubjectObjects(triples, store.BlockSubject("root", "Block Node", "1.0"), "block/tag"), []string{"work/today"})
	}
}

// --- RootConfigToTriples ---

func TestRootConfigToTriples_WithAlias(t *testing.T) {
	cfg := Config{
		Path:  "/some/path",
		Alias: "myalias",
	}
	triples := RootConfigToTriples(cfg, "/root/path")

	predicates := triplePredicateMap(triples)
	if predicates["root/path"] != "/some/path" {
		t.Errorf("expected root/path = %q, got %q", "/some/path", predicates["root/path"])
	}
	if predicates["root/alias"] != "myalias" {
		t.Errorf("expected root/alias = %q, got %q", "myalias", predicates["root/alias"])
	}
}

func TestRootConfigToTriples_WithMaxChars(t *testing.T) {
	maxChars := 1000
	cfg := Config{
		Path:     "/some/path",
		MaxChars: &maxChars,
	}
	triples := RootConfigToTriples(cfg, "/root/path")

	predicates := triplePredicateMap(triples)
	if predicates["root/max-chars"] != "1000" {
		t.Errorf("expected root/max-chars = %q, got %q", "1000", predicates["root/max-chars"])
	}
}

func TestRootConfigToTriples_NoAlias(t *testing.T) {
	cfg := Config{
		Path: "/some/path",
	}
	triples := RootConfigToTriples(cfg, "/root/path")

	for _, tr := range triples {
		if tr.Predicate == "root/alias" {
			t.Errorf("expected no root/alias triple, got one with object %q", tr.Object)
		}
	}
}

// --- FindRoot ---

func TestFindRoot_DirectDir(t *testing.T) {
	dir := t.TempDir()
	ednPath := filepath.Join(dir, ".sevens.edn")
	if err := os.WriteFile(ednPath, []byte(`{:path "/tmp"}`), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := FindRoot(dir)
	if err != nil {
		t.Fatalf("FindRoot returned error: %v", err)
	}
	if got != dir {
		t.Errorf("FindRoot = %q, want %q", got, dir)
	}
}

func TestFindRoot_WalkUpFromSubdir(t *testing.T) {
	root := t.TempDir()
	ednPath := filepath.Join(root, ".sevens.edn")
	if err := os.WriteFile(ednPath, []byte(`{:path "/tmp"}`), 0644); err != nil {
		t.Fatal(err)
	}

	subdir := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	got, err := FindRoot(subdir)
	if err != nil {
		t.Fatalf("FindRoot returned error: %v", err)
	}
	if got != root {
		t.Errorf("FindRoot = %q, want %q", got, root)
	}
}

func TestFindRoot_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := FindRoot(dir)
	if err == nil {
		t.Error("expected error when .sevens.edn not found, got nil")
	}
}

// --- LoadConfig ---

func TestLoadConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	content := `{:path "/some/notes" :alias "mynotes"}`
	if err := os.WriteFile(filepath.Join(dir, ".sevens.edn"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Path != "/some/notes" {
		t.Errorf("cfg.Path = %q, want %q", cfg.Path, "/some/notes")
	}
	if cfg.Alias != "mynotes" {
		t.Errorf("cfg.Alias = %q, want %q", cfg.Alias, "mynotes")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadConfig(dir)
	if err == nil {
		t.Error("expected error for missing .sevens.edn, got nil")
	}
}

func TestLoadConfig_ExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	content := `{:path "~/my-notes"}`
	if err := os.WriteFile(filepath.Join(dir, ".sevens.edn"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	want := filepath.Join(home, "my-notes")
	if cfg.Path != want {
		t.Errorf("cfg.Path = %q, want %q", cfg.Path, want)
	}
}

// --- ScanFiles ---

func TestScanFiles_FindsMdFiles(t *testing.T) {
	dir := t.TempDir()

	files := []string{"alpha.md", "beta.md", "notes.txt", "gamma.md"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ScanFiles(dir)
	if err != nil {
		t.Fatalf("ScanFiles returned error: %v", err)
	}

	mdFiles := []string{}
	for _, p := range got {
		if strings.HasSuffix(p, ".md") {
			mdFiles = append(mdFiles, p)
		}
	}

	if len(mdFiles) != 3 {
		t.Errorf("expected 3 .md files, got %d: %v", len(mdFiles), mdFiles)
	}
}

func TestScanFiles_SkipsGitDir(t *testing.T) {
	dir := t.TempDir()

	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	// .md file inside .git — should be skipped
	if err := os.WriteFile(filepath.Join(gitDir, "COMMIT_EDITMSG.md"), []byte("git stuff"), 0644); err != nil {
		t.Fatal(err)
	}
	// .md file at root level — should be included
	if err := os.WriteFile(filepath.Join(dir, "real.md"), []byte("real content"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ScanFiles(dir)
	if err != nil {
		t.Fatalf("ScanFiles returned error: %v", err)
	}

	for _, p := range got {
		if strings.Contains(p, ".git") {
			t.Errorf("ScanFiles returned a file inside .git: %q", p)
		}
	}

	if len(got) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(got), got)
	}
}

func TestScanFiles_NestedDirs(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.md"), []byte("top"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.md"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ScanFiles(dir)
	if err != nil {
		t.Fatalf("ScanFiles returned error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(got), got)
	}
}

// --- ParseAllFiles ---

func TestParseAllFiles_ParsesFrontmatter(t *testing.T) {
	dir := t.TempDir()

	content := "---\ntitle: My Node\nparent: \"[[Parent Node]]\"\n---\n\nBody content here.\n"
	path := filepath.Join(dir, "node.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, dups := ParseAllFiles([]string{path})
	if len(dups) != 0 {
		t.Errorf("expected no duplicates, got %v", dups)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	n := nodes[0]
	if n.Title != "My Node" {
		t.Errorf("Title = %q, want %q", n.Title, "My Node")
	}
	if n.Parent == nil || *n.Parent != "Parent Node" {
		t.Errorf("Parent = %v, want %q", n.Parent, "Parent Node")
	}
	if n.FilePath != path {
		t.Errorf("FilePath = %q, want %q", n.FilePath, path)
	}
}

func TestParseAllFiles_SkipsNoTitle(t *testing.T) {
	dir := t.TempDir()

	content := "---\nauthor: someone\n---\n\nNo title here.\n"
	path := filepath.Join(dir, "no-title.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for file without title, got %d", len(nodes))
	}
}

func TestParseAllFiles_DuplicateTitles(t *testing.T) {
	dir := t.TempDir()

	content1 := "---\ntitle: Duplicate\n---\n\nFirst.\n"
	content2 := "---\ntitle: Duplicate\n---\n\nSecond.\n"

	path1 := filepath.Join(dir, "first.md")
	path2 := filepath.Join(dir, "second.md")

	if err := os.WriteFile(path1, []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2, []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, dups := ParseAllFiles([]string{path1, path2})
	if len(nodes) != 1 {
		t.Errorf("expected 1 node (first wins), got %d", len(nodes))
	}
	if len(dups) != 1 || dups[0] != "Duplicate" {
		t.Errorf("expected duplicates = [Duplicate], got %v", dups)
	}
}

func TestParseAllFiles_ExtractsWikiLinks(t *testing.T) {
	dir := t.TempDir()

	content := "---\ntitle: Linker\n---\n\nSee [[Alpha]] and also [[Beta]].\n"
	path := filepath.Join(dir, "linker.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	refs := nodes[0].CrossRefs
	sort.Strings(refs)
	if len(refs) != 2 || refs[0] != "Alpha" || refs[1] != "Beta" {
		t.Errorf("CrossRefs = %v, want [Alpha Beta]", refs)
	}
}

func TestParseAllFiles_ExtractsBlocks(t *testing.T) {
	dir := t.TempDir()

	content := strings.Join([]string{
		"---",
		"title: Blocky",
		"---",
		"",
		"# Intro #proj/demo",
		"",
		"- [x] done item #work/today",
		"- plain item",
		"- [!!] urgent item #work/high",
		"",
	}, "\n")
	path := filepath.Join(dir, "blocky.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	blocks := nodes[0].Blocks
	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d: %#v", len(blocks), blocks)
	}

	assertParsedBlock(t, blocks[0], ParsedBlock{Path: "0", Kind: "heading", Text: "Intro #proj/demo", Level: 1, Tags: []string{"proj/demo"}}, []string{"Intro #proj/demo"})
	assertParsedBlock(t, blocks[1], ParsedBlock{Path: "1.0", Kind: "task", Text: "done item #work/today", Signifier: "x", Tags: []string{"work/today"}}, []string{"Intro #proj/demo"})
	assertParsedBlock(t, blocks[2], ParsedBlock{Path: "1.1", Kind: "list-item", Text: "plain item"}, []string{"Intro #proj/demo"})
	assertParsedBlock(t, blocks[3], ParsedBlock{Path: "1.2", Kind: "task", Text: "urgent item #work/high", Signifier: "!!", Tags: []string{"work/high"}}, []string{"Intro #proj/demo"})
}

// --- PopulateTriples ---

func TestPopulateTriples_InsertsTriples(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	cfg := Config{Path: root, Alias: "test"}

	parent := "Parent"
	nodes := []ParsedNode{
		{
			Title:    "Parent",
			FilePath: "/tmp/parent.md",
			Content:  "Parent content",
		},
		{
			Title:    "Child",
			Parent:   &parent,
			FilePath: "/tmp/child.md",
			Content:  "Child content",
		},
	}

	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatalf("PopulateTriples returned error: %v", err)
	}

	// Verify parent node/root triple exists
	obj, err := store.GetObject(db, store.NodeSubject(root, "Parent"), "node/root")
	if err != nil {
		t.Fatalf("GetObject error: %v", err)
	}
	if obj != root {
		t.Errorf("node/root for Parent = %q, want %q", obj, root)
	}

	// Verify child's parent triple
	parentObj, err := store.GetObject(db, store.NodeSubject(root, "Child"), "node/parent")
	if err != nil {
		t.Fatalf("GetObject error: %v", err)
	}
	if parentObj != store.NodeSubject(root, "Parent") {
		t.Errorf("node/parent for Child = %q, want %q", parentObj, store.NodeSubject(root, "Parent"))
	}
}

func TestPopulateTriples_ClearsOnRepopulate(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	cfg := Config{Path: root}

	nodes1 := []ParsedNode{
		{Title: "OldNode", FilePath: "/tmp/old.md", Content: "old"},
	}
	if err := PopulateTriples(db, root, nodes1, cfg); err != nil {
		t.Fatal(err)
	}

	// Second call should clear and replace
	nodes2 := []ParsedNode{
		{Title: "NewNode", FilePath: "/tmp/new.md", Content: "new"},
	}
	if err := PopulateTriples(db, root, nodes2, cfg); err != nil {
		t.Fatal(err)
	}

	// OldNode should be gone
	obj, err := store.GetObject(db, store.NodeSubject(root, "OldNode"), "node/root")
	if err != nil {
		t.Fatal(err)
	}
	if obj != "" {
		t.Errorf("expected OldNode to be cleared, but node/root = %q", obj)
	}

	// NewNode should exist
	obj, err = store.GetObject(db, store.NodeSubject(root, "NewNode"), "node/root")
	if err != nil {
		t.Fatal(err)
	}
	if obj != root {
		t.Errorf("NewNode node/root = %q, want %q", obj, root)
	}
}

func TestPopulateTriples_InsertsBlockTriples(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	cfg := Config{Path: root}
	nodes := []ParsedNode{
		{
			Title:    "Blocky",
			FilePath: "/tmp/blocky.md",
			Content:  "# Intro #proj/demo\n\n- [x] done item #work/today",
			Blocks: []ParsedBlock{
				{Path: "0", Kind: "heading", Text: "Intro #proj/demo", Level: 1, Tags: []string{"proj/demo"}},
				{Path: "1.0", Kind: "task", Text: "done item #work/today", Signifier: "x", Tags: []string{"work/today"}},
			},
		},
	}

	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatalf("PopulateTriples returned error: %v", err)
	}

	headingSubject := store.BlockSubject(root, "Blocky", "0")
	headingKind, err := store.GetObject(db, headingSubject, "block/kind")
	if err != nil {
		t.Fatalf("GetObject error: %v", err)
	}
	if headingKind != "heading" {
		t.Errorf("heading block/kind = %q, want %q", headingKind, "heading")
	}

	taskSubject := store.BlockSubject(root, "Blocky", "1.0")
	signifier, err := store.GetObject(db, taskSubject, "block/signifier")
	if err != nil {
		t.Fatalf("GetObject error: %v", err)
	}
	if signifier != "x" {
		t.Errorf("task block/signifier = %q, want %q", signifier, "x")
	}

	headingTags, err := store.GetObjects(db, headingSubject, "block/tag")
	if err != nil {
		t.Fatalf("GetObjects error: %v", err)
	}
	if !reflect.DeepEqual(headingTags, []string{"proj/demo"}) {
		t.Errorf("heading block/tag = %v, want %v", headingTags, []string{"proj/demo"})
	}
}

func TestPopulateTriples_ReusesBlockSubjectAcrossPathShift(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	cfg := Config{Path: root}

	nodes1 := []ParsedNode{
		{
			Title:    "Blocky",
			FilePath: "/tmp/blocky.md",
			Content:  "# Intro\n\n- [x] done item",
			Blocks: []ParsedBlock{
				{Path: "0", Kind: "heading", Text: "Intro", Level: 1, HeadingChain: []string{"Intro"}, AnchorHashes: anchorHashesForText("Intro")},
				{Path: "1.0", Kind: "task", Text: "done item", Signifier: "x", HeadingChain: []string{"Intro"}, AnchorHashes: anchorHashesForText("done item")},
			},
		},
	}
	if err := PopulateTriples(db, root, nodes1, cfg); err != nil {
		t.Fatal(err)
	}

	oldTaskSubject := store.BlockSubject(root, "Blocky", "1.0")

	nodes2 := []ParsedNode{
		{
			Title:    "Blocky",
			FilePath: "/tmp/blocky.md",
			Content:  "# Intro\n\nInserted note.\n\n- [x] done item",
			Blocks: []ParsedBlock{
				{Path: "0", Kind: "heading", Text: "Intro", Level: 1, HeadingChain: []string{"Intro"}, AnchorHashes: anchorHashesForText("Intro")},
				{Path: "1", Kind: "paragraph", Text: "Inserted note.", HeadingChain: []string{"Intro"}, AnchorHashes: anchorHashesForText("Inserted note.")},
				{Path: "2.0", Kind: "task", Text: "done item", Signifier: "x", HeadingChain: []string{"Intro"}, AnchorHashes: anchorHashesForText("done item")},
			},
		},
	}
	if err := PopulateTriples(db, root, nodes2, cfg); err != nil {
		t.Fatal(err)
	}

	path, err := store.GetObject(db, oldTaskSubject, "block/path")
	if err != nil {
		t.Fatal(err)
	}
	if path != "2.0" {
		t.Fatalf("block/path after shift = %q, want %q", path, "2.0")
	}

	id, err := store.GetObject(db, oldTaskSubject, "block/id")
	if err != nil {
		t.Fatal(err)
	}
	if id != oldTaskSubject {
		t.Fatalf("block/id = %q, want %q", id, oldTaskSubject)
	}
}

func TestBuildBlockDiff_CurrentFileVsSyncedSnapshot(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}
	path := filepath.Join(root, "blocky.md")

	original := strings.Join([]string{
		"---",
		"title: Blocky",
		"---",
		"",
		"# Today",
		"",
		"- [!!] decide stable identity #sevens/identity",
		"",
		"## Open Questions",
		"Stable block identity probably needs content anchors.",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	updated := strings.Join([]string{
		"---",
		"title: Blocky",
		"---",
		"",
		"Intro note before everything.",
		"",
		"# Today",
		"",
		"## Blocked",
		"- [!!] decide stable identity #sevens/identity",
		"",
		"Stable block identity probably needs deterministic content anchors.",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		t.Fatal(err)
	}

	output, err := BuildBlockDiff(db, root, "Blocky")
	if err != nil {
		t.Fatalf("BuildBlockDiff returned error: %v", err)
	}

	if len(output.Inserted) == 0 {
		t.Fatalf("expected inserted blocks, got none: %#v", output)
	}
	if len(output.ScopeChanged) == 0 {
		t.Fatalf("expected scope-changed blocks, got none: %#v", output)
	}
	if len(output.Edited) == 0 {
		t.Fatalf("expected edited blocks, got none: %#v", output)
	}

	foundTaskScopeChange := false
	for _, entry := range output.ScopeChanged {
		if strings.Contains(entry.NewText, "decide stable identity") {
			foundTaskScopeChange = true
			if ScopeString(entry.OldScope) != "Today" {
				t.Fatalf("old scope = %q, want %q", ScopeString(entry.OldScope), "Today")
			}
			if ScopeString(entry.NewScope) != "Today > Blocked" {
				t.Fatalf("new scope = %q, want %q", ScopeString(entry.NewScope), "Today > Blocked")
			}
		}
	}
	if !foundTaskScopeChange {
		t.Fatalf("expected task scope change entry, got %#v", output.ScopeChanged)
	}
}

func TestBuildInboxOverview(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}

	files := map[string]string{
		"inbox.md": strings.Join([]string{
			"---",
			"title: inbox",
			"---",
			"",
		}, "\n"),
		"2026-04-08.md": strings.Join([]string{
			"---",
			"title: 2026-04-08",
			"parent: \"[[inbox]]\"",
			"---",
			"",
		}, "\n"),
		"capture.md": strings.Join([]string{
			"---",
			"title: Vijay/Julian - EHE Modernization Sync",
			"parent: \"[[inbox]]\"",
			"---",
			"",
			"- not sure where dates are coming from",
			"- need to set milestones",
			"",
		}, "\n"),
		"discussion.md": strings.Join([]string{
			"---",
			"title: \"Discussion: Braindump\"",
			"parent: \"[[inbox]]\"",
			"---",
			"",
			"# Discussion",
			"",
			"Some threaded content.",
			"",
		}, "\n"),
	}
	var paths []string
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}

	nodes, _ := ParseAllFiles(paths)
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	output, err := BuildInboxOverview(db, root, "inbox")
	if err != nil {
		t.Fatalf("BuildInboxOverview returned error: %v", err)
	}
	if output.NodeTitle != "inbox" {
		t.Fatalf("NodeTitle = %q, want %q", output.NodeTitle, "inbox")
	}
	if len(output.Items) != 3 {
		t.Fatalf("expected 3 inbox items, got %d: %#v", len(output.Items), output.Items)
	}

	byTitle := make(map[string]InboxItemSummary, len(output.Items))
	for _, item := range output.Items {
		byTitle[item.Title] = item
	}

	if byTitle["2026-04-08"].Kind != "empty-date" {
		t.Fatalf("date kind = %q, want %q", byTitle["2026-04-08"].Kind, "empty-date")
	}
	if byTitle["Vijay/Julian - EHE Modernization Sync"].Kind != "capture" {
		t.Fatalf("capture kind = %q, want %q", byTitle["Vijay/Julian - EHE Modernization Sync"].Kind, "capture")
	}
	if byTitle["Vijay/Julian - EHE Modernization Sync"].BulletCount != 2 {
		t.Fatalf("capture bullet count = %d, want %d", byTitle["Vijay/Julian - EHE Modernization Sync"].BulletCount, 2)
	}
	if byTitle["Discussion: Braindump"].Kind != "discussion" {
		t.Fatalf("discussion kind = %q, want %q", byTitle["Discussion: Braindump"].Kind, "discussion")
	}
}

func TestBuildBlockList(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}
	path := filepath.Join(root, "note.md")

	content := strings.Join([]string{
		"---",
		"title: Blocky",
		"---",
		"",
		"# Today",
		"",
		"- [!!] unblock CI/CD dates",
		"- plain bullet",
		"",
		"Loose paragraph.",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	output, err := BuildBlockList(db, root, "Blocky")
	if err != nil {
		t.Fatalf("BuildBlockList returned error: %v", err)
	}
	if output.NodeTitle != "Blocky" {
		t.Fatalf("NodeTitle = %q, want %q", output.NodeTitle, "Blocky")
	}
	if len(output.Blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d: %#v", len(output.Blocks), output.Blocks)
	}
	if output.Blocks[0].Kind != "heading" || output.Blocks[0].Path != "0" {
		t.Fatalf("first block = %#v, want heading path 0", output.Blocks[0])
	}
	if output.Blocks[1].Kind != "task" || output.Blocks[1].Signifier != "!!" {
		t.Fatalf("second block = %#v, want task signifier !!", output.Blocks[1])
	}
	if ScopeString(output.Blocks[3].Scope) != "Today" {
		t.Fatalf("paragraph scope = %q, want %q", ScopeString(output.Blocks[3].Scope), "Today")
	}
}

func TestResolveBlockTarget(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}
	path := filepath.Join(root, "note.md")

	content := strings.Join([]string{
		"---",
		"title: Blocky",
		"---",
		"",
		"# Today",
		"",
		"- [!!] unblock CI/CD dates",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	target, err := ResolveBlockTarget(db, root, "Blocky", "1.0")
	if err != nil {
		t.Fatalf("ResolveBlockTarget returned error: %v", err)
	}
	if target.Label() != "Blocky#1.0" {
		t.Fatalf("Label = %q, want %q", target.Label(), "Blocky#1.0")
	}
	if target.Kind != "task" || target.Signifier != "!!" {
		t.Fatalf("target = %#v, want task signifier !!", target)
	}
	if target.Markdown != "- [!!] unblock CI/CD dates" {
		t.Fatalf("Markdown = %q", target.Markdown)
	}

	byID, err := ResolveBlockTargetBySubject(db, target.Subject)
	if err != nil {
		t.Fatalf("ResolveBlockTargetBySubject returned error: %v", err)
	}
	if byID.Path != "1.0" || byID.NodeTitle != "Blocky" {
		t.Fatalf("resolved by id = %#v", byID)
	}
}

func TestPrepareAppendToNode_AppendsToBodyAndEmptyNode(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}

	files := map[string]string{
		"work.md": strings.Join([]string{
			"---",
			"title: Work",
			"---",
			"",
			"Existing paragraph.",
			"",
		}, "\n"),
		"empty.md": strings.Join([]string{
			"---",
			"title: Empty",
			"---",
			"",
		}, "\n"),
	}
	var paths []string
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}

	nodes, _ := ParseAllFiles(paths)
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	edit, err := PrepareAppendToNode(db, root, "Work", "### 2026-04-09\n\nCapture text.")
	if err != nil {
		t.Fatalf("PrepareAppendToNode returned error: %v", err)
	}
	if !strings.Contains(edit.NewText, "Existing paragraph.\n\n### 2026-04-09\n\nCapture text.\n") {
		t.Fatalf("updated body missing appended text:\n%s", edit.NewText)
	}

	emptyEdit, err := PrepareAppendToNode(db, root, "Empty", "### 2026-04-09\n\nCapture text.")
	if err != nil {
		t.Fatalf("PrepareAppendToNode empty returned error: %v", err)
	}
	if !strings.Contains(emptyEdit.NewText, "title: Empty") || !strings.Contains(emptyEdit.NewText, "### 2026-04-09") {
		t.Fatalf("empty append did not preserve frontmatter and append content:\n%s", emptyEdit.NewText)
	}
	if strings.HasPrefix(emptyEdit.NewText, "### 2026-04-09") {
		t.Fatalf("empty append inserted content before frontmatter:\n%s", emptyEdit.NewText)
	}
}

func TestPrepareInsertUnderHeading_InsertsBeforeNextPeerHeading(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}
	path := filepath.Join(root, "note.md")

	content := strings.Join([]string{
		"---",
		"title: Plan",
		"---",
		"",
		"# Today",
		"",
		"Existing paragraph.",
		"",
		"# Later",
		"",
		"- something else",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	edit, err := PrepareInsertUnderHeading(db, root, "Plan", "Today", 0, false, "- inserted bullet")
	if err != nil {
		t.Fatalf("PrepareInsertUnderHeading returned error: %v", err)
	}
	if !strings.Contains(edit.NewText, "Existing paragraph.\n\n- inserted bullet\n\n# Later") {
		t.Fatalf("inserted text not placed before next heading:\n%s", edit.NewText)
	}
}

func TestPrepareInsertUnderHeading_CreatesMissingHeadingWhenRequested(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}
	path := filepath.Join(root, "note.md")

	content := strings.Join([]string{
		"---",
		"title: Plan",
		"---",
		"",
		"# Plan",
		"",
		"Existing paragraph.",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	edit, err := PrepareInsertUnderHeading(db, root, "Plan", "Questions", 0, true, "- what is blocked?")
	if err != nil {
		t.Fatalf("PrepareInsertUnderHeading returned error: %v", err)
	}
	if !strings.Contains(edit.NewText, "Existing paragraph.\n\n## Questions\n\n- what is blocked?\n") {
		t.Fatalf("missing created heading insertion:\n%s", edit.NewText)
	}
}

func TestPrepareInsertUnderHeading_ErrorsOnAmbiguousHeadingName(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}
	path := filepath.Join(root, "note.md")

	content := strings.Join([]string{
		"---",
		"title: Plan",
		"---",
		"",
		"# Work",
		"",
		"## Notes",
		"",
		"# Personal",
		"",
		"## Notes",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	_, err := PrepareInsertUnderHeading(db, root, "Plan", "Notes", 0, false, "- inserted bullet")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous heading error, got %v", err)
	}
}

func TestPrepareInsertUnderHeading_UsesScopedHeadingPath(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}
	path := filepath.Join(root, "note.md")

	content := strings.Join([]string{
		"---",
		"title: Plan",
		"---",
		"",
		"# Work",
		"",
		"## Notes",
		"",
		"# Personal",
		"",
		"## Notes",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path})
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	edit, err := PrepareInsertUnderHeading(db, root, "Plan", "Personal > Notes", 0, false, "- inserted bullet")
	if err != nil {
		t.Fatalf("PrepareInsertUnderHeading returned error: %v", err)
	}
	if !strings.Contains(edit.NewText, "# Personal\n\n## Notes\n\n- inserted bullet") {
		t.Fatalf("scoped insertion not placed under Personal > Notes:\n%s", edit.NewText)
	}
}

func TestPrepareBlockExtraction_ListItem(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}
	path := filepath.Join(root, "capture.md")

	content := strings.Join([]string{
		"---",
		"title: Sonal/Julian - Modernization Chat",
		"parent: \"[[inbox]]\"",
		"---",
		"",
		"- need to understand dependencies",
		"- protect via definition of done",
		"",
	}, "\n")
	parent := strings.Join([]string{
		"---",
		"title: Braindump",
		"---",
		"",
		"# work",
		"",
	}, "\n")
	inbox := strings.Join([]string{
		"---",
		"title: inbox",
		"---",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Braindump.md"), []byte(parent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inbox.md"), []byte(inbox), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path, filepath.Join(root, "Braindump.md"), filepath.Join(root, "inbox.md")})
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	out, err := PrepareBlockExtraction(db, root, "Sonal/Julian - Modernization Chat", "0.1", "Definition of Done", "Braindump")
	if err != nil {
		t.Fatalf("PrepareBlockExtraction returned error: %v", err)
	}
	if out.ParentTitle != "Braindump" {
		t.Fatalf("ParentTitle = %q, want %q", out.ParentTitle, "Braindump")
	}
	if out.SourceKind != "list-item" {
		t.Fatalf("SourceKind = %q, want %q", out.SourceKind, "list-item")
	}
	if !strings.Contains(out.Content, "Source: [[Sonal/Julian - Modernization Chat]] (block 0.1)") {
		t.Fatalf("content missing source line: %q", out.Content)
	}
	if !strings.Contains(out.Content, "- protect via definition of done") {
		t.Fatalf("content missing rendered list item: %q", out.Content)
	}
}

func TestPrepareBlockExtraction_HeadingSection(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	cfg := Config{Path: root}
	path := filepath.Join(root, "discussion.md")

	content := strings.Join([]string{
		"---",
		"title: \"Discussion: Braindump\"",
		"parent: \"[[Braindump]]\"",
		"---",
		"",
		"# Discussion",
		"",
		"## Program Structure",
		"",
		"This is the first paragraph.",
		"",
		"### Subpoint",
		"",
		"Nested detail.",
		"",
		"## Another Thread",
		"",
		"Different content.",
		"",
	}, "\n")
	parent := strings.Join([]string{
		"---",
		"title: Braindump",
		"---",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Braindump.md"), []byte(parent), 0644); err != nil {
		t.Fatal(err)
	}

	nodes, _ := ParseAllFiles([]string{path, filepath.Join(root, "Braindump.md")})
	if err := PopulateTriples(db, root, nodes, cfg); err != nil {
		t.Fatal(err)
	}

	out, err := PrepareBlockExtraction(db, root, "Discussion: Braindump", "1", "", "")
	if err != nil {
		t.Fatalf("PrepareBlockExtraction returned error: %v", err)
	}
	if out.Title != "Program Structure" {
		t.Fatalf("Title = %q, want %q", out.Title, "Program Structure")
	}
	if out.ParentTitle != "Discussion: Braindump" {
		t.Fatalf("ParentTitle = %q, want %q", out.ParentTitle, "Discussion: Braindump")
	}
	if strings.Contains(out.Content, "Another Thread") {
		t.Fatalf("content should not include following sibling section: %q", out.Content)
	}
	if !strings.Contains(out.Content, "This is the first paragraph.") {
		t.Fatalf("content missing section paragraph: %q", out.Content)
	}
	if !strings.Contains(out.Content, "## Subpoint") {
		t.Fatalf("content missing relative subheading: %q", out.Content)
	}
}

// --- BuildWalk ---

func setupSmallGraph(t *testing.T, db *sql.DB, root string) {
	t.Helper()
	rootSubj := testNodeSubject(root, "Root")
	childASubj := testNodeSubject(root, "Child A")
	childBSubj := testNodeSubject(root, "Child B")
	triples := []store.Triple{
		{Subject: rootSubj, Predicate: "node/title", Object: "Root"},
		{Subject: rootSubj, Predicate: "node/root", Object: root},
		{Subject: rootSubj, Predicate: "node/content", Object: "Root content"},
		{Subject: rootSubj, Predicate: "node/file-path", Object: "/tmp/root.md"},
		{Subject: childASubj, Predicate: "node/title", Object: "Child A"},
		{Subject: childASubj, Predicate: "node/root", Object: root},
		{Subject: childASubj, Predicate: "node/parent", Object: rootSubj},
		{Subject: childASubj, Predicate: "node/content", Object: "Child A content"},
		{Subject: childASubj, Predicate: "node/file-path", Object: "/tmp/child-a.md"},
		{Subject: childBSubj, Predicate: "node/title", Object: "Child B"},
		{Subject: childBSubj, Predicate: "node/root", Object: root},
		{Subject: childBSubj, Predicate: "node/parent", Object: rootSubj},
		{Subject: childBSubj, Predicate: "node/content", Object: "Child B content"},
		{Subject: childBSubj, Predicate: "node/file-path", Object: "/tmp/child-b.md"},
	}
	if err := store.InsertTriples(db, triples); err != nil {
		t.Fatalf("InsertTriples: %v", err)
	}
}

func TestBuildWalk_RootNode(t *testing.T) {
	db := testDB(t)
	root := "/test/walk"
	setupSmallGraph(t, db, root)

	out, err := BuildWalk(db, root, "Root", 1)
	if err != nil {
		t.Fatalf("BuildWalk returned error: %v", err)
	}
	if out.Node.Title != "Root" {
		t.Errorf("Node.Title = %q, want %q", out.Node.Title, "Root")
	}
	if out.Node.Parent != nil {
		t.Errorf("expected no parent for Root, got %q", *out.Node.Parent)
	}
	if len(out.Node.Children) != 2 {
		t.Errorf("expected 2 children, got %d: %v", len(out.Node.Children), out.Node.Children)
	}
	if out.Node.Content != "Root content" {
		t.Errorf("Content = %q, want %q", out.Node.Content, "Root content")
	}
}

func TestBuildWalk_ChildNode(t *testing.T) {
	db := testDB(t)
	root := "/test/walk"
	setupSmallGraph(t, db, root)

	out, err := BuildWalk(db, root, "Child A", 1)
	if err != nil {
		t.Fatalf("BuildWalk returned error: %v", err)
	}
	if out.Node.Parent == nil || *out.Node.Parent != "Root" {
		t.Errorf("expected Parent = Root, got %v", out.Node.Parent)
	}
	// Child A has sibling Child B
	if len(out.Node.Siblings) != 1 || out.Node.Siblings[0] != "Child B" {
		t.Errorf("expected siblings = [Child B], got %v", out.Node.Siblings)
	}
}

func TestBuildWalk_NodeNotFound(t *testing.T) {
	db := testDB(t)
	root := "/test/walk"
	setupSmallGraph(t, db, root)

	_, err := BuildWalk(db, root, "Nonexistent", 1)
	if err == nil {
		t.Error("expected error for nonexistent node, got nil")
	}
}

func TestBuildWalk_UnwalkedExcludesCurrentNode(t *testing.T) {
	db := testDB(t)
	root := "/test/walk"
	setupSmallGraph(t, db, root)

	out, err := BuildWalk(db, root, "Root", 1)
	if err != nil {
		t.Fatalf("BuildWalk returned error: %v", err)
	}
	for _, u := range out.Unwalked {
		if u == "Root" {
			t.Error("Unwalked should not include the current node 'Root'")
		}
	}
}

// --- BuildOverview ---

func TestBuildOverview_ReturnsAllNodes(t *testing.T) {
	db := testDB(t)
	root := "/test/overview"
	setupSmallGraph(t, db, root)

	cfg := Config{Path: root}
	out, err := BuildOverview(db, root, cfg)
	if err != nil {
		t.Fatalf("BuildOverview returned error: %v", err)
	}

	if len(out.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(out.Nodes))
	}
}

func TestBuildOverview_TreeStructure(t *testing.T) {
	db := testDB(t)
	root := "/test/overview"
	setupSmallGraph(t, db, root)

	cfg := Config{Path: root}
	out, err := BuildOverview(db, root, cfg)
	if err != nil {
		t.Fatalf("BuildOverview returned error: %v", err)
	}

	// Find Root node in output
	var rootNode *OverviewNode
	for i := range out.Nodes {
		if out.Nodes[i].Title == "Root" {
			rootNode = &out.Nodes[i]
			break
		}
	}
	if rootNode == nil {
		t.Fatal("Root node not found in overview output")
	}

	if len(rootNode.Children) != 2 {
		t.Errorf("Root.Children count = %d, want 2: %v", len(rootNode.Children), rootNode.Children)
	}
	if rootNode.ChildCount != 2 {
		t.Errorf("Root.ChildCount = %d, want 2", rootNode.ChildCount)
	}
	if rootNode.Parent != nil {
		t.Errorf("Root.Parent should be nil, got %q", *rootNode.Parent)
	}
}

func TestBuildOverview_ChildParentPointers(t *testing.T) {
	db := testDB(t)
	root := "/test/overview"
	setupSmallGraph(t, db, root)

	cfg := Config{Path: root}
	out, err := BuildOverview(db, root, cfg)
	if err != nil {
		t.Fatalf("BuildOverview returned error: %v", err)
	}

	for _, n := range out.Nodes {
		if n.Title == "Child A" || n.Title == "Child B" {
			if n.Parent == nil || *n.Parent != "Root" {
				t.Errorf("%s.Parent = %v, want Root", n.Title, n.Parent)
			}
		}
	}
}

// --- Validate ---

func TestValidate_Orphans(t *testing.T) {
	db := testDB(t)
	root := "/test/validate"

	// Insert an orphan node: not a parent of anything, not a child of anything
	triples := []store.Triple{
		{Subject: testNodeSubject(root, "Orphan"), Predicate: "node/title", Object: "Orphan"},
		{Subject: testNodeSubject(root, "Orphan"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "Orphan"), Predicate: "node/content", Object: "short"},
		// no node/parent, and nobody has Orphan as their parent
	}
	if err := store.InsertTriples(db, triples); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Path: root}
	report, err := Validate(db, root, cfg)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	found := false
	for _, o := range report.Orphans {
		if o == "Orphan" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Orphan' in report.Orphans, got %v", report.Orphans)
	}
}

func TestValidate_MissingParent(t *testing.T) {
	db := testDB(t)
	root := "/test/validate"

	// Child references a parent that doesn't exist in this root
	triples := []store.Triple{
		{Subject: testNodeSubject(root, "Child"), Predicate: "node/title", Object: "Child"},
		{Subject: testNodeSubject(root, "Child"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "Child"), Predicate: "node/parent", Object: testNodeSubject(root, "GhostParent")},
		{Subject: testNodeSubject(root, "Child"), Predicate: "node/content", Object: "content"},
		// GhostParent is NOT in this root
	}
	if err := store.InsertTriples(db, triples); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Path: root}
	report, err := Validate(db, root, cfg)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	found := false
	for _, m := range report.MissingParents {
		if m == "Child" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Child' in report.MissingParents, got %v", report.MissingParents)
	}
}

func TestValidate_Overflow(t *testing.T) {
	db := testDB(t)
	root := "/test/validate"

	// Insert a parent with 10 children (>9 triggers overflow)
	var triples []store.Triple
	triples = append(triples,
		store.Triple{Subject: testNodeSubject(root, "BigParent"), Predicate: "node/title", Object: "BigParent"},
		store.Triple{Subject: testNodeSubject(root, "BigParent"), Predicate: "node/root", Object: root},
		store.Triple{Subject: testNodeSubject(root, "BigParent"), Predicate: "node/content", Object: "Big parent"},
	)
	for i := 0; i < 10; i++ {
		child := strings.Repeat("X", i+1) // unique names
		triples = append(triples,
			store.Triple{Subject: testNodeSubject(root, child), Predicate: "node/title", Object: child},
			store.Triple{Subject: testNodeSubject(root, child), Predicate: "node/root", Object: root},
			store.Triple{Subject: testNodeSubject(root, child), Predicate: "node/parent", Object: testNodeSubject(root, "BigParent")},
			store.Triple{Subject: testNodeSubject(root, child), Predicate: "node/content", Object: "child content"},
		)
	}
	if err := store.InsertTriples(db, triples); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Path: root}
	report, err := Validate(db, root, cfg)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	found := false
	for _, o := range report.Overflow {
		if o == "BigParent" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'BigParent' in report.Overflow, got %v", report.Overflow)
	}
}

func TestValidate_LengthViolation(t *testing.T) {
	db := testDB(t)
	root := "/test/validate"

	// Node with content that exceeds max-chars
	longContent := strings.Repeat("a", 200)
	triples := []store.Triple{
		{Subject: testNodeSubject(root, "LongNode"), Predicate: "node/title", Object: "LongNode"},
		{Subject: testNodeSubject(root, "LongNode"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "LongNode"), Predicate: "node/content", Object: longContent},
		{Subject: testNodeSubject(root, "LongNode"), Predicate: "node/max-chars", Object: "50"},
		{Subject: testNodeSubject(root, "LongNode"), Predicate: "content/char-count", Object: "200"},
	}
	if err := store.InsertTriples(db, triples); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Path: root}
	report, err := Validate(db, root, cfg)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	found := false
	for _, v := range report.LengthViolations {
		if strings.Contains(v, "LongNode") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'LongNode' in report.LengthViolations, got %v", report.LengthViolations)
	}
}

func TestValidate_NoIssues(t *testing.T) {
	db := testDB(t)
	root := "/test/validate"
	setupSmallGraph(t, db, root)

	cfg := Config{Path: root}
	report, err := Validate(db, root, cfg)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	// Root has both children, so no orphans. Children have valid parents.
	// Root itself has substantial content, so not an orphan.
	if len(report.MissingParents) != 0 {
		t.Errorf("expected no missing parents, got %v", report.MissingParents)
	}
	if len(report.Overflow) != 0 {
		t.Errorf("expected no overflow, got %v", report.Overflow)
	}
}

// --- Helpers ---

// triplePredicateMap builds a map of predicate → object from a list of triples.
// For multi-valued predicates, only the last value is stored; use collectObjects for those.
func triplePredicateMap(triples []store.Triple) map[string]string {
	m := make(map[string]string)
	for _, tr := range triples {
		m[tr.Predicate] = tr.Object
	}
	return m
}

func tripleSubjectPredicateMap(triples []store.Triple, subject string) map[string]string {
	m := make(map[string]string)
	for _, tr := range triples {
		if tr.Subject == subject {
			m[tr.Predicate] = tr.Object
		}
	}
	return m
}

// collectObjects returns all objects for a given predicate from a list of triples.
func collectObjects(triples []store.Triple, predicate string) []string {
	var result []string
	for _, tr := range triples {
		if tr.Predicate == predicate {
			result = append(result, tr.Object)
		}
	}
	return result
}

func collectSubjectObjects(triples []store.Triple, subject, predicate string) []string {
	var result []string
	for _, tr := range triples {
		if tr.Subject == subject && tr.Predicate == predicate {
			result = append(result, tr.Object)
		}
	}
	return result
}

func assertParsedBlock(t *testing.T, got, want ParsedBlock, wantHeadingChain []string) {
	t.Helper()
	if got.Path != want.Path || got.Kind != want.Kind || got.Text != want.Text || got.Level != want.Level || got.Signifier != want.Signifier {
		t.Fatalf("parsed block = %#v, want core fields %#v", got, want)
	}
	if !reflect.DeepEqual(got.Tags, want.Tags) {
		t.Fatalf("parsed block tags = %v, want %v", got.Tags, want.Tags)
	}
	if !reflect.DeepEqual(got.HeadingChain, wantHeadingChain) {
		t.Fatalf("parsed block heading chain = %v, want %v", got.HeadingChain, wantHeadingChain)
	}
	if len(got.AnchorHashes) == 0 {
		t.Fatalf("parsed block anchor hashes should not be empty: %#v", got)
	}
}

// --- ResolveGroup ---

// setupGroupGraph creates:
//
//	Root
//	├── A
//	│   ├── A1
//	│   └── A2
//	├── B
//	│   └── B1
//	└── C
func setupGroupGraph(t *testing.T, db *sql.DB, root string) {
	t.Helper()
	triples := []store.Triple{
		{Subject: testNodeSubject(root, "Root"), Predicate: "node/title", Object: "Root"},
		{Subject: testNodeSubject(root, "Root"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "Root"), Predicate: "node/content", Object: "root"},
		{Subject: testNodeSubject(root, "A"), Predicate: "node/title", Object: "A"},
		{Subject: testNodeSubject(root, "A"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "A"), Predicate: "node/parent", Object: testNodeSubject(root, "Root")},
		{Subject: testNodeSubject(root, "A"), Predicate: "node/content", Object: "a"},
		{Subject: testNodeSubject(root, "A1"), Predicate: "node/title", Object: "A1"},
		{Subject: testNodeSubject(root, "A1"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "A1"), Predicate: "node/parent", Object: testNodeSubject(root, "A")},
		{Subject: testNodeSubject(root, "A1"), Predicate: "node/content", Object: "a1"},
		{Subject: testNodeSubject(root, "A2"), Predicate: "node/title", Object: "A2"},
		{Subject: testNodeSubject(root, "A2"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "A2"), Predicate: "node/parent", Object: testNodeSubject(root, "A")},
		{Subject: testNodeSubject(root, "A2"), Predicate: "node/content", Object: "a2"},
		{Subject: testNodeSubject(root, "B"), Predicate: "node/title", Object: "B"},
		{Subject: testNodeSubject(root, "B"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "B"), Predicate: "node/parent", Object: testNodeSubject(root, "Root")},
		{Subject: testNodeSubject(root, "B"), Predicate: "node/content", Object: "b"},
		{Subject: testNodeSubject(root, "B1"), Predicate: "node/title", Object: "B1"},
		{Subject: testNodeSubject(root, "B1"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "B1"), Predicate: "node/parent", Object: testNodeSubject(root, "B")},
		{Subject: testNodeSubject(root, "B1"), Predicate: "node/content", Object: "b1"},
		{Subject: testNodeSubject(root, "C"), Predicate: "node/title", Object: "C"},
		{Subject: testNodeSubject(root, "C"), Predicate: "node/root", Object: root},
		{Subject: testNodeSubject(root, "C"), Predicate: "node/parent", Object: testNodeSubject(root, "Root")},
		{Subject: testNodeSubject(root, "C"), Predicate: "node/content", Object: "c"},
	}
	if err := store.InsertTriples(db, triples); err != nil {
		t.Fatal(err)
	}
}

func TestResolveGroup_AllDescendants(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	setupGroupGraph(t, db, root)

	titles, err := ResolveGroup(db, root, Group{Root: "Root"})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(titles)
	want := []string{"A", "A1", "A2", "B", "B1", "C", "Root"}
	if len(titles) != len(want) {
		t.Fatalf("got %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, titles[i], want[i])
		}
	}
}

func TestResolveGroup_ExcludeSubtree(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	setupGroupGraph(t, db, root)

	// Exclude A → should prune A, A1, A2
	titles, err := ResolveGroup(db, root, Group{Root: "Root", Exclude: []string{"A"}})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(titles)
	want := []string{"B", "B1", "C", "Root"}
	if len(titles) != len(want) {
		t.Fatalf("got %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, titles[i], want[i])
		}
	}
}

func TestResolveGroup_ExcludeLeaf(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	setupGroupGraph(t, db, root)

	// Exclude just A1
	titles, err := ResolveGroup(db, root, Group{Root: "Root", Exclude: []string{"A1"}})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(titles)
	want := []string{"A", "A2", "B", "B1", "C", "Root"}
	if len(titles) != len(want) {
		t.Fatalf("got %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, titles[i], want[i])
		}
	}
}

func TestResolveGroup_SingletonNodes(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	setupGroupGraph(t, db, root)

	// Root is B (includes B, B1) plus singleton "External"
	titles, err := ResolveGroup(db, root, Group{
		Root:  "B",
		Nodes: []string{"External"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(titles)
	want := []string{"B", "B1", "External"}
	if len(titles) != len(want) {
		t.Fatalf("got %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, titles[i], want[i])
		}
	}
}

func TestResolveGroup_SingletonDedup(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	setupGroupGraph(t, db, root)

	// Singleton that's already in the subtree should not duplicate
	titles, err := ResolveGroup(db, root, Group{
		Root:  "B",
		Nodes: []string{"B1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(titles)
	want := []string{"B", "B1"}
	if len(titles) != len(want) {
		t.Fatalf("got %v, want %v", titles, want)
	}
}

func TestResolveGroup_ExcludeRoot(t *testing.T) {
	db := testDB(t)
	root := "/test/root"
	setupGroupGraph(t, db, root)

	// Excluding the root itself should return nothing from the subtree
	titles, err := ResolveGroup(db, root, Group{Root: "Root", Exclude: []string{"Root"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 0 {
		t.Errorf("expected 0 titles when root is excluded, got %v", titles)
	}
}
