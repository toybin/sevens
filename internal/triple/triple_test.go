package triple_test

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"sevens/internal/triple"

	_ "turso.tech/database/tursogo"
)

// testStore creates an in-memory triple store for testing.
func testStore(t *testing.T) *triple.Store {
	t.Helper()
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := triple.New(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ctx() context.Context { return context.Background() }

// --- Assert ---

func TestAssert(t *testing.T) {
	s := testStore(t)

	err := s.Assert(ctx(), triple.Triple{"alice", "knows", "bob"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.BySubject(ctx(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 triple, got %d", len(got))
	}
	if got[0].Subject != "alice" || got[0].Predicate != "knows" || got[0].Object != "bob" {
		t.Fatalf("unexpected triple: %+v", got[0])
	}
}

func TestAssertIdempotent(t *testing.T) {
	s := testStore(t)
	tr := triple.Triple{"alice", "knows", "bob"}

	s.Assert(ctx(), tr)
	s.Assert(ctx(), tr) // same triple again

	got, _ := s.BySubject(ctx(), "alice")
	if len(got) != 1 {
		t.Fatalf("expected 1 triple after duplicate assert, got %d", len(got))
	}
}

func TestAssertMultipleObjects(t *testing.T) {
	s := testStore(t)

	s.Assert(ctx(), triple.Triple{"alice", "knows", "bob"})
	s.Assert(ctx(), triple.Triple{"alice", "knows", "carol"})

	got, _ := s.BySubjectPredicate(ctx(), "alice", "knows")
	sort.Strings(got)
	if len(got) != 2 || got[0] != "bob" || got[1] != "carol" {
		t.Fatalf("expected [bob carol], got %v", got)
	}
}

// --- AssertBatch ---

func TestAssertBatch(t *testing.T) {
	s := testStore(t)

	triples := []triple.Triple{
		{"a", "r", "1"},
		{"a", "r", "2"},
		{"b", "r", "3"},
	}
	if err := s.AssertBatch(ctx(), triples); err != nil {
		t.Fatal(err)
	}

	got, _ := s.BySubjectPredicate(ctx(), "a", "r")
	sort.Strings(got)
	if len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("expected [1 2], got %v", got)
	}

	got, _ = s.BySubjectPredicate(ctx(), "b", "r")
	if len(got) != 1 || got[0] != "3" {
		t.Fatalf("expected [3], got %v", got)
	}
}

func TestAssertBatchEmpty(t *testing.T) {
	s := testStore(t)
	if err := s.AssertBatch(ctx(), nil); err != nil {
		t.Fatal(err)
	}
}

// --- Retract ---

func TestRetract(t *testing.T) {
	s := testStore(t)

	s.Assert(ctx(), triple.Triple{"alice", "knows", "bob"})
	s.Assert(ctx(), triple.Triple{"alice", "knows", "carol"})

	s.Retract(ctx(), triple.Triple{"alice", "knows", "bob"})

	got, _ := s.BySubjectPredicate(ctx(), "alice", "knows")
	if len(got) != 1 || got[0] != "carol" {
		t.Fatalf("expected [carol] after retract, got %v", got)
	}
}

func TestRetractNonexistent(t *testing.T) {
	s := testStore(t)
	// Should not error on retracting something that doesn't exist.
	err := s.Retract(ctx(), triple.Triple{"nobody", "knows", "nothing"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRetractBySubject(t *testing.T) {
	s := testStore(t)

	s.Assert(ctx(), triple.Triple{"alice", "knows", "bob"})
	s.Assert(ctx(), triple.Triple{"alice", "age", "30"})
	s.Assert(ctx(), triple.Triple{"bob", "knows", "carol"})

	s.RetractBySubject(ctx(), "alice")

	got, _ := s.BySubject(ctx(), "alice")
	if len(got) != 0 {
		t.Fatalf("expected 0 triples for alice, got %d", len(got))
	}

	// bob should be unaffected
	got, _ = s.BySubject(ctx(), "bob")
	if len(got) != 1 {
		t.Fatalf("expected 1 triple for bob, got %d", len(got))
	}
}

func TestRetractBySubjectPrefix(t *testing.T) {
	s := testStore(t)

	s.Assert(ctx(), triple.Triple{"node:abc:foo", "p", "1"})
	s.Assert(ctx(), triple.Triple{"node:abc:bar", "p", "2"})
	s.Assert(ctx(), triple.Triple{"node:xyz:baz", "p", "3"})

	s.RetractBySubjectPrefix(ctx(), "node:abc:")

	got, _ := s.ByPredicate(ctx(), "p")
	if len(got) != 1 || got[0].Subject != "node:xyz:baz" {
		t.Fatalf("expected only node:xyz:baz, got %+v", got)
	}
}

func TestRetractByPredicate(t *testing.T) {
	s := testStore(t)

	s.Assert(ctx(), triple.Triple{"a", "temp", "1"})
	s.Assert(ctx(), triple.Triple{"b", "temp", "2"})
	s.Assert(ctx(), triple.Triple{"a", "keep", "3"})

	s.RetractByPredicate(ctx(), "temp")

	got, _ := s.ByPredicate(ctx(), "temp")
	if len(got) != 0 {
		t.Fatalf("expected 0 triples for temp predicate, got %d", len(got))
	}
	got, _ = s.ByPredicate(ctx(), "keep")
	if len(got) != 1 {
		t.Fatalf("expected 1 triple for keep, got %d", len(got))
	}
}

// --- Queries ---

func TestByPredicateObject(t *testing.T) {
	s := testStore(t)

	s.Assert(ctx(), triple.Triple{"alice", "knows", "bob"})
	s.Assert(ctx(), triple.Triple{"carol", "knows", "bob"})
	s.Assert(ctx(), triple.Triple{"dave", "knows", "carol"})

	// Who knows bob?
	got, _ := s.ByPredicateObject(ctx(), "knows", "bob")
	sort.Strings(got)
	if len(got) != 2 || got[0] != "alice" || got[1] != "carol" {
		t.Fatalf("expected [alice carol], got %v", got)
	}
}

func TestBySubjectPredicateEmpty(t *testing.T) {
	s := testStore(t)

	got, err := s.BySubjectPredicate(ctx(), "nobody", "nothing")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %v", got)
	}
}

func TestSearch(t *testing.T) {
	s := testStore(t)

	s.Assert(ctx(), triple.Triple{"n1", "content", "The quick brown fox"})
	s.Assert(ctx(), triple.Triple{"n2", "content", "A lazy dog"})
	s.Assert(ctx(), triple.Triple{"n3", "content", "QUICK brown fox jumps"})

	// Case-insensitive search
	got, _ := s.Search(ctx(), "content", "quick")
	sort.Strings(got)
	if len(got) != 2 || got[0] != "n1" || got[1] != "n3" {
		t.Fatalf("expected [n1 n3], got %v", got)
	}
}

func TestSearchNoResults(t *testing.T) {
	s := testStore(t)

	s.Assert(ctx(), triple.Triple{"n1", "content", "hello"})

	got, _ := s.Search(ctx(), "content", "zzz")
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

// --- Schema idempotence ---

func TestNewIdempotent(t *testing.T) {
	db, _ := sql.Open("turso", ":memory:")
	defer db.Close()

	s1, err := triple.New(db)
	if err != nil {
		t.Fatal(err)
	}
	s1.Assert(ctx(), triple.Triple{"a", "b", "c"})

	// Creating a second Store on the same DB should not drop data.
	s2, err := triple.New(db)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s2.BySubject(ctx(), "a")
	if len(got) != 1 {
		t.Fatalf("expected 1 triple after re-init, got %d", len(got))
	}
}
