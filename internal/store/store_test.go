package store

import (
	"database/sql"
	"sort"
	"testing"

	_ "turso.tech/database/tursogo"
)

// testDB opens an in-memory SQLite database, initialises the triples schema,
// and registers a cleanup to close it when the test finishes.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := InitTriplesSchema(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// sortStrings returns a sorted copy of ss (does not mutate the original).
func sortStrings(ss []string) []string {
	out := make([]string, len(ss))
	copy(out, ss)
	sort.Strings(out)
	return out
}

// ---- InitTriplesSchema -------------------------------------------------------

func TestInitTriplesSchema_CreatesTable(t *testing.T) {
	db := testDB(t)

	// If the schema was created we should be able to insert without error.
	_, err := db.Exec(`INSERT INTO triples (subject, predicate, object) VALUES ('s','p','o')`)
	if err != nil {
		t.Fatalf("insert after init: %v", err)
	}
}

func TestInitTriplesSchema_Idempotent(t *testing.T) {
	db := testDB(t) // first call inside testDB
	// Second call must not error.
	if err := InitTriplesSchema(db); err != nil {
		t.Fatalf("second InitTriplesSchema: %v", err)
	}
}

// ---- InsertTriple -----------------------------------------------------------

func TestInsertTriple_Basic(t *testing.T) {
	db := testDB(t)

	tr := Triple{Subject: "doc/a", Predicate: "node/title", Object: "Hello"}
	if err := InsertTriple(db, tr); err != nil {
		t.Fatalf("InsertTriple: %v", err)
	}

	got, err := GetObject(db, "doc/a", "node/title")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if got != "Hello" {
		t.Errorf("want 'Hello', got %q", got)
	}
}

func TestInsertTriple_Upsert(t *testing.T) {
	db := testDB(t)

	tr := Triple{Subject: "doc/a", Predicate: "node/content", Object: "v1"}
	if err := InsertTriple(db, tr); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Insert the same triple again — should not error (REPLACE semantics).
	if err := InsertTriple(db, tr); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Confirm exactly one row exists.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM triples WHERE subject='doc/a' AND predicate='node/content'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after upsert, got %d", count)
	}
}

// ---- SetTriple --------------------------------------------------------------

func TestSetTriple_SetsValue(t *testing.T) {
	db := testDB(t)

	if err := SetTriple(db, "doc/a", "node/status", "draft"); err != nil {
		t.Fatalf("SetTriple: %v", err)
	}

	got, err := GetObject(db, "doc/a", "node/status")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if got != "draft" {
		t.Errorf("want 'draft', got %q", got)
	}
}

func TestSetTriple_ReplacesExistingValue(t *testing.T) {
	db := testDB(t)

	_ = InsertTriple(db, Triple{Subject: "doc/a", Predicate: "node/status", Object: "pending"})

	if err := SetTriple(db, "doc/a", "node/status", "resolved"); err != nil {
		t.Fatalf("SetTriple: %v", err)
	}

	// Confirm exactly one row exists with the new value.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM triples WHERE subject='doc/a' AND predicate='node/status'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after SetTriple, got %d", count)
	}

	got, err := GetObject(db, "doc/a", "node/status")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if got != "resolved" {
		t.Errorf("want 'resolved', got %q", got)
	}
}

func TestSetTriple_DoesNotAffectOtherPredicates(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{Subject: "doc/a", Predicate: "node/status", Object: "pending"},
		{Subject: "doc/a", Predicate: "node/root", Object: "wiki"},
	})

	if err := SetTriple(db, "doc/a", "node/status", "resolved"); err != nil {
		t.Fatalf("SetTriple: %v", err)
	}

	// The other predicate should be untouched.
	got, err := GetObject(db, "doc/a", "node/root")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if got != "wiki" {
		t.Errorf("node/root should still be 'wiki', got %q", got)
	}
}

// ---- InsertTriples ----------------------------------------------------------

func TestInsertTriples_Batch(t *testing.T) {
	db := testDB(t)

	triples := []Triple{
		{Subject: "doc/a", Predicate: "node/root", Object: "wiki"},
		{Subject: "doc/b", Predicate: "node/root", Object: "wiki"},
		{Subject: "doc/c", Predicate: "node/root", Object: "wiki"},
	}
	if err := InsertTriples(db, triples); err != nil {
		t.Fatalf("InsertTriples: %v", err)
	}

	subjects, err := GetSubjects(db, "node/root", "wiki")
	if err != nil {
		t.Fatalf("GetSubjects: %v", err)
	}
	if len(subjects) != 3 {
		t.Errorf("expected 3 subjects, got %d: %v", len(subjects), subjects)
	}
}

func TestInsertTriples_EmptySlice(t *testing.T) {
	db := testDB(t)

	if err := InsertTriples(db, []Triple{}); err != nil {
		t.Fatalf("InsertTriples with empty slice: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM triples`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}
}

// ---- GetObject --------------------------------------------------------------

func TestGetObject_Found(t *testing.T) {
	db := testDB(t)

	_ = InsertTriple(db, Triple{"node/1", "node/content", "some content"})

	got, err := GetObject(db, "node/1", "node/content")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if got != "some content" {
		t.Errorf("want 'some content', got %q", got)
	}
}

func TestGetObject_NotFound(t *testing.T) {
	db := testDB(t)

	got, err := GetObject(db, "missing", "node/content")
	if err != nil {
		t.Fatalf("GetObject on missing key: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string for missing key, got %q", got)
	}
}

// ---- GetObjects -------------------------------------------------------------

func TestGetObjects_Multiple(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "ref/wiki-link", "doc/b"},
		{"doc/a", "ref/wiki-link", "doc/c"},
		{"doc/a", "ref/wiki-link", "doc/d"},
	})

	objs, err := GetObjects(db, "doc/a", "ref/wiki-link")
	if err != nil {
		t.Fatalf("GetObjects: %v", err)
	}
	if len(objs) != 3 {
		t.Errorf("expected 3 objects, got %d: %v", len(objs), objs)
	}
}

func TestGetObjects_None(t *testing.T) {
	db := testDB(t)

	objs, err := GetObjects(db, "doc/a", "ref/wiki-link")
	if err != nil {
		t.Fatalf("GetObjects: %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("expected empty slice, got %v", objs)
	}
}

// ---- GetSubjects ------------------------------------------------------------

func TestGetSubjects_InverseLookup(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/parent", "doc/root"},
		{"doc/b", "node/parent", "doc/root"},
		{"doc/c", "node/parent", "other"},
	})

	children, err := GetSubjects(db, "node/parent", "doc/root")
	if err != nil {
		t.Fatalf("GetSubjects: %v", err)
	}
	got := sortStrings(children)
	want := []string{"doc/a", "doc/b"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestGetSubjects_None(t *testing.T) {
	db := testDB(t)

	subjects, err := GetSubjects(db, "node/parent", "nonexistent")
	if err != nil {
		t.Fatalf("GetSubjects: %v", err)
	}
	if len(subjects) != 0 {
		t.Errorf("expected empty, got %v", subjects)
	}
}

// ---- GetSubjectTriples ------------------------------------------------------

func TestGetSubjectTriples_All(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/root", "wiki"},
		{"doc/a", "node/content", "hello world"},
		{"doc/a", "node/parent", "doc/root"},
		{"doc/b", "node/root", "wiki"}, // different subject — should not appear
	})

	triples, err := GetSubjectTriples(db, "doc/a")
	if err != nil {
		t.Fatalf("GetSubjectTriples: %v", err)
	}
	if len(triples) != 3 {
		t.Errorf("expected 3 triples for doc/a, got %d: %v", len(triples), triples)
	}
	for _, tr := range triples {
		if tr.Subject != "doc/a" {
			t.Errorf("unexpected subject %q in result", tr.Subject)
		}
	}
}

func TestGetSubjectTriples_Empty(t *testing.T) {
	db := testDB(t)

	triples, err := GetSubjectTriples(db, "doc/missing")
	if err != nil {
		t.Fatalf("GetSubjectTriples: %v", err)
	}
	if len(triples) != 0 {
		t.Errorf("expected empty, got %v", triples)
	}
}

// ---- GetPredicateTriples ----------------------------------------------------

func TestGetPredicateTriples_All(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/root", "wiki"},
		{"doc/b", "node/root", "wiki"},
		{"doc/c", "node/root", "notes"},
		{"doc/a", "node/content", "hello"}, // different predicate
	})

	triples, err := GetPredicateTriples(db, "node/root")
	if err != nil {
		t.Fatalf("GetPredicateTriples: %v", err)
	}
	if len(triples) != 3 {
		t.Errorf("expected 3 triples for node/root, got %d", len(triples))
	}
	for _, tr := range triples {
		if tr.Predicate != "node/root" {
			t.Errorf("unexpected predicate %q", tr.Predicate)
		}
	}
}

func TestGetPredicateTriples_None(t *testing.T) {
	db := testDB(t)

	triples, err := GetPredicateTriples(db, "no/such/predicate")
	if err != nil {
		t.Fatalf("GetPredicateTriples: %v", err)
	}
	if len(triples) != 0 {
		t.Errorf("expected empty, got %v", triples)
	}
}

// ---- SearchContent ----------------------------------------------------------

func TestSearchContent_Found(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/root", "wiki"},
		{"doc/a", "node/content", "The quick brown fox"},
		{"doc/b", "node/root", "wiki"},
		{"doc/b", "node/content", "jumped over the lazy dog"},
		{"doc/c", "node/root", "other"}, // different root — should not match
		{"doc/c", "node/content", "quick notes here"},
	})

	results, err := SearchContent(db, "quick", "wiki")
	if err != nil {
		t.Fatalf("SearchContent: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0] != "doc/a" {
		t.Errorf("expected doc/a, got %q", results[0])
	}
}

func TestSearchContent_None(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/root", "wiki"},
		{"doc/a", "node/content", "nothing matches"},
	})

	results, err := SearchContent(db, "xyzzy", "wiki")
	if err != nil {
		t.Fatalf("SearchContent: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty, got %v", results)
	}
}

// ---- SearchTitles -----------------------------------------------------------

func TestSearchTitles_Found(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"wiki/GoLang Tips", "node/root", "wiki"},
		{"wiki/Go Patterns", "node/root", "wiki"},
		{"wiki/Python Notes", "node/root", "wiki"},
		{"wiki/Go Snippets", "node/root", "notes"}, // different root
	})

	results, err := SearchTitles(db, "Go", "wiki")
	if err != nil {
		t.Fatalf("SearchTitles: %v", err)
	}
	got := sortStrings(results)
	want := sortStrings([]string{"wiki/GoLang Tips", "wiki/Go Patterns"})
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestSearchTitles_None(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"wiki/Hello World", "node/root", "wiki"},
	})

	results, err := SearchTitles(db, "Rust", "wiki")
	if err != nil {
		t.Fatalf("SearchTitles: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty, got %v", results)
	}
}

// ---- GetRootNodeData --------------------------------------------------------

func TestGetRootNodeData_BatchFetch(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/root", "wiki"},
		{"doc/a", "node/content", "alpha content"},
		{"doc/a", "node/title", "Alpha"},
		{"doc/b", "node/root", "wiki"},
		{"doc/b", "node/content", "beta content"},
		{"doc/c", "node/root", "notes"}, // different root
		{"doc/c", "node/content", "gamma content"},
	})

	data, err := GetRootNodeData(db, "wiki", []string{"node/content", "node/title"})
	if err != nil {
		t.Fatalf("GetRootNodeData: %v", err)
	}

	// Alpha and doc/b should appear; doc/c should not.
	if _, ok := data["Alpha"]; !ok {
		t.Error("expected Alpha in result")
	}
	if _, ok := data["doc/b"]; !ok {
		t.Error("expected doc/b in result")
	}
	if _, ok := data["doc/c"]; ok {
		t.Error("doc/c should not appear (different root)")
	}

	if v := data["Alpha"]["node/content"]; len(v) == 0 || v[0] != "alpha content" {
		t.Errorf("unexpected node/content for Alpha: %v", v)
	}
	if v := data["Alpha"]["node/title"]; len(v) == 0 || v[0] != "Alpha" {
		t.Errorf("unexpected node/title for Alpha: %v", v)
	}
}

func TestGetRootNodeData_EmptyPredicates(t *testing.T) {
	db := testDB(t)

	data, err := GetRootNodeData(db, "wiki", []string{})
	if err != nil {
		t.Fatalf("GetRootNodeData with empty predicates: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil for empty predicates, got %v", data)
	}
}

// ---- Compose ----------------------------------------------------------------

func TestCompose_TwoStepTraversal(t *testing.T) {
	db := testDB(t)

	// A --node/parent--> B --node/root--> wiki
	// A --node/parent--> C --node/root--> notes
	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/parent", "doc/b"},
		{"doc/a", "node/parent", "doc/c"},
		{"doc/b", "node/root", "wiki"},
		{"doc/c", "node/root", "notes"},
	})

	results, err := Compose(db, "doc/a", "node/parent", "node/root")
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	got := sortStrings(results)
	want := []string{"notes", "wiki"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestCompose_NoPath(t *testing.T) {
	db := testDB(t)

	results, err := Compose(db, "doc/a", "node/parent", "node/root")
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty, got %v", results)
	}
}

// ---- ComposeInverse ---------------------------------------------------------

func TestComposeInverse_InverseTraversal(t *testing.T) {
	db := testDB(t)

	// sibling relationship via shared parent:
	// doc/a --node/parent--> doc/root
	// doc/b --node/parent--> doc/root
	// doc/c --node/parent--> doc/root
	// ComposeInverse(doc/a, node/parent, node/parent) => [doc/b, doc/c]
	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/parent", "doc/root"},
		{"doc/b", "node/parent", "doc/root"},
		{"doc/c", "node/parent", "doc/root"},
	})

	results, err := ComposeInverse(db, "doc/a", "node/parent", "node/parent")
	if err != nil {
		t.Fatalf("ComposeInverse: %v", err)
	}
	got := sortStrings(results)
	want := []string{"doc/b", "doc/c"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestComposeInverse_ExcludesSelf(t *testing.T) {
	db := testDB(t)

	_ = InsertTriple(db, Triple{"doc/a", "node/parent", "doc/root"})

	// Only doc/a points to doc/root; ComposeInverse should return nothing (self excluded).
	results, err := ComposeInverse(db, "doc/a", "node/parent", "node/parent")
	if err != nil {
		t.Fatalf("ComposeInverse: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty (self excluded), got %v", results)
	}
}

// ---- DeleteBySubject --------------------------------------------------------

func TestDeleteBySubject(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/root", "wiki"},
		{"doc/a", "node/content", "hello"},
		{"doc/b", "node/root", "wiki"},
	})

	if err := DeleteBySubject(db, "doc/a"); err != nil {
		t.Fatalf("DeleteBySubject: %v", err)
	}

	triples, err := GetSubjectTriples(db, "doc/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(triples) != 0 {
		t.Errorf("expected no triples for doc/a after delete, got %v", triples)
	}

	// doc/b must still exist.
	b, err := GetObject(db, "doc/b", "node/root")
	if err != nil || b != "wiki" {
		t.Errorf("doc/b should still exist, got %q, err %v", b, err)
	}
}

// ---- DeleteBySubjectPrefix --------------------------------------------------

func TestDeleteBySubjectPrefix(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"wiki/page-a", "node/root", "wiki"},
		{"wiki/page-b", "node/root", "wiki"},
		{"notes/page-c", "node/root", "notes"},
	})

	if err := DeleteBySubjectPrefix(db, "wiki/"); err != nil {
		t.Fatalf("DeleteBySubjectPrefix: %v", err)
	}

	remaining, err := GetPredicateTriples(db, "node/root")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Errorf("expected 1 triple remaining, got %d: %v", len(remaining), remaining)
	}
	if remaining[0].Subject != "notes/page-c" {
		t.Errorf("expected notes/page-c, got %q", remaining[0].Subject)
	}
}

// ---- DeleteByPredicate ------------------------------------------------------

func TestDeleteByPredicate(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/root", "wiki"},
		{"doc/b", "node/root", "wiki"},
		{"doc/a", "node/content", "hello"},
	})

	if err := DeleteByPredicate(db, "node/root"); err != nil {
		t.Fatalf("DeleteByPredicate: %v", err)
	}

	rootTriples, err := GetPredicateTriples(db, "node/root")
	if err != nil {
		t.Fatal(err)
	}
	if len(rootTriples) != 0 {
		t.Errorf("expected no node/root triples, got %v", rootTriples)
	}

	// node/content triple for doc/a must survive.
	content, err := GetObject(db, "doc/a", "node/content")
	if err != nil || content != "hello" {
		t.Errorf("node/content should survive, got %q, err %v", content, err)
	}
}

// ---- ClearRootTriples -------------------------------------------------------

func TestClearRootTriples(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/root", "wiki"},
		{"doc/a", "node/content", "alpha"},
		{"doc/b", "node/root", "wiki"},
		{"doc/b", "node/content", "beta"},
		{"block/a", "block/root", "wiki"},
		{"block/a", "block/text", "task a"},
		{"doc/c", "node/root", "notes"},
		{"doc/c", "node/content", "gamma"},
		{"block/c", "block/root", "notes"},
		{"block/c", "block/text", "task c"},
	})

	if err := ClearRootTriples(db, "wiki"); err != nil {
		t.Fatalf("ClearRootTriples: %v", err)
	}

	// wiki subjects must be gone.
	for _, subj := range []string{"doc/a", "doc/b", "block/a"} {
		ts, err := GetSubjectTriples(db, subj)
		if err != nil {
			t.Fatal(err)
		}
		if len(ts) != 0 {
			t.Errorf("%s should have been cleared, got %v", subj, ts)
		}
	}

	// notes subject must survive.
	ts, err := GetSubjectTriples(db, "doc/c")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 {
		t.Errorf("doc/c should have 2 triples, got %d: %v", len(ts), ts)
	}

	blockTriples, err := GetSubjectTriples(db, "block/c")
	if err != nil {
		t.Fatal(err)
	}
	if len(blockTriples) != 2 {
		t.Errorf("block/c should have 2 triples, got %d: %v", len(blockTriples), blockTriples)
	}
}

func TestClearRootTriples_NoMatch(t *testing.T) {
	db := testDB(t)

	// Should not error even when root has no associated subjects.
	if err := ClearRootTriples(db, "nonexistent-root"); err != nil {
		t.Fatalf("ClearRootTriples with no subjects: %v", err)
	}
}

// ---- RunQuery ---------------------------------------------------------------

func TestRunQuery_RawSQL(t *testing.T) {
	db := testDB(t)

	_ = InsertTriples(db, []Triple{
		{"doc/a", "node/root", "wiki"},
		{"doc/b", "node/root", "wiki"},
	})

	results, err := RunQuery(db, `SELECT subject, predicate, object FROM triples WHERE predicate = ?`, "node/root")
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}

	// results[0] is the header row.
	if len(results) < 1 {
		t.Fatal("expected at least a header row")
	}
	header := results[0]
	if len(header) != 3 || header[0] != "subject" || header[1] != "predicate" || header[2] != "object" {
		t.Errorf("unexpected header: %v", header)
	}

	dataRows := results[1:]
	if len(dataRows) != 2 {
		t.Errorf("expected 2 data rows, got %d", len(dataRows))
	}
}

func TestRunQuery_InvalidSQL(t *testing.T) {
	db := testDB(t)

	_, err := RunQuery(db, `SELECT * FROM nonexistent_table`)
	if err == nil {
		t.Error("expected error for query on nonexistent table")
	}
}
