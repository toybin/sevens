package graphops_test

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"sevens/internal/graphops"
	"sevens/internal/triple"

	_ "turso.tech/database/tursogo"
)

func testGraph(t *testing.T) *graphops.Graph {
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
	return graphops.New(store)
}

func ctx() context.Context { return context.Background() }

// --- Set and Lookup ---

func TestSetAndLookup(t *testing.T) {
	g := testGraph(t)

	g.Set(ctx(), "alice", "name", "Alice")
	val, ok, err := g.Lookup(ctx(), "alice", "name")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || val != "Alice" {
		t.Fatalf("expected Alice, got %q (ok=%v)", val, ok)
	}
}

func TestSetReplaces(t *testing.T) {
	g := testGraph(t)

	g.Set(ctx(), "alice", "name", "Alice")
	g.Set(ctx(), "alice", "name", "Alicia") // replace

	val, ok, _ := g.Lookup(ctx(), "alice", "name")
	if !ok || val != "Alicia" {
		t.Fatalf("expected Alicia after replace, got %q", val)
	}

	// Should be exactly one value, not two
	vals, _ := g.Store().BySubjectPredicate(ctx(), "alice", "name")
	if len(vals) != 1 {
		t.Fatalf("expected 1 value after Set, got %d", len(vals))
	}
}

func TestLookupMissing(t *testing.T) {
	g := testGraph(t)

	_, ok, err := g.Lookup(ctx(), "nobody", "nothing")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected ok=false for missing lookup")
	}
}

// --- ParsePath ---

func TestParsePath(t *testing.T) {
	steps := graphops.ParsePath([]string{"parent", "parent~", "link"})
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if steps[0].Predicate != "parent" || steps[0].Inverse {
		t.Fatalf("step 0: expected parent forward, got %+v", steps[0])
	}
	if steps[1].Predicate != "parent" || !steps[1].Inverse {
		t.Fatalf("step 1: expected parent inverse, got %+v", steps[1])
	}
	if steps[2].Predicate != "link" || steps[2].Inverse {
		t.Fatalf("step 2: expected link forward, got %+v", steps[2])
	}
}

// --- Compose ---

func TestComposeSingleHop(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	// A --parent--> B
	s.Assert(ctx(), triple.Triple{"A", "parent", "B"})

	got, err := g.Compose(ctx(), "A", graphops.ParsePath([]string{"parent"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "B" {
		t.Fatalf("expected [B], got %v", got)
	}
}

func TestComposeInverse(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	// A --parent--> P
	// B --parent--> P
	// C --parent--> P
	s.Assert(ctx(), triple.Triple{"A", "parent", "P"})
	s.Assert(ctx(), triple.Triple{"B", "parent", "P"})
	s.Assert(ctx(), triple.Triple{"C", "parent", "P"})

	// Children of P = inverse of parent
	got, err := g.Compose(ctx(), "P", graphops.ParsePath([]string{"parent~"}))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Fatalf("expected [A B C], got %v", got)
	}
}

func TestComposeSiblings(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	// A, B, C all have parent P
	s.Assert(ctx(), triple.Triple{"A", "parent", "P"})
	s.Assert(ctx(), triple.Triple{"B", "parent", "P"})
	s.Assert(ctx(), triple.Triple{"C", "parent", "P"})

	// Siblings of A: go to parent, then find all children of parent
	got, err := g.Compose(ctx(), "A", graphops.ParsePath([]string{"parent", "parent~"}))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	// Includes self -- caller is responsible for filtering
	if len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Fatalf("expected [A B C], got %v", got)
	}
}

func TestComposeMultiHop(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	// A --parent--> B --parent--> C
	s.Assert(ctx(), triple.Triple{"A", "parent", "B"})
	s.Assert(ctx(), triple.Triple{"B", "parent", "C"})

	// Grandparent of A
	got, err := g.Compose(ctx(), "A", graphops.ParsePath([]string{"parent", "parent"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "C" {
		t.Fatalf("expected [C], got %v", got)
	}
}

func TestComposeEmptyPath(t *testing.T) {
	g := testGraph(t)

	got, err := g.Compose(ctx(), "A", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "A" {
		t.Fatalf("expected [A] for empty path, got %v", got)
	}
}

func TestComposeDeadEnd(t *testing.T) {
	g := testGraph(t)

	// No triples at all -- path goes nowhere
	got, err := g.Compose(ctx(), "A", graphops.ParsePath([]string{"parent"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result for dead-end path, got %v", got)
	}
}

func TestComposeDeduplicates(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	// Diamond: A --r--> B, A --r--> C, B --s--> D, C --s--> D
	s.Assert(ctx(), triple.Triple{"A", "r", "B"})
	s.Assert(ctx(), triple.Triple{"A", "r", "C"})
	s.Assert(ctx(), triple.Triple{"B", "s", "D"})
	s.Assert(ctx(), triple.Triple{"C", "s", "D"})

	got, err := g.Compose(ctx(), "A", graphops.ParsePath([]string{"r", "s"}))
	if err != nil {
		t.Fatal(err)
	}
	// D is reachable via two paths but should appear once
	if len(got) != 1 || got[0] != "D" {
		t.Fatalf("expected [D], got %v", got)
	}
}

// --- Reachable ---

func TestReachable(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	// Chain: A --r--> B --r--> C --r--> D
	s.Assert(ctx(), triple.Triple{"A", "r", "B"})
	s.Assert(ctx(), triple.Triple{"B", "r", "C"})
	s.Assert(ctx(), triple.Triple{"C", "r", "D"})

	got, err := g.Reachable(ctx(), "A", "r", false)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 4 || got[0] != "A" || got[1] != "B" || got[2] != "C" || got[3] != "D" {
		t.Fatalf("expected [A B C D], got %v", got)
	}
}

func TestReachableInverse(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	// A --parent--> R, B --parent--> R, C --parent--> A
	s.Assert(ctx(), triple.Triple{"A", "parent", "R"})
	s.Assert(ctx(), triple.Triple{"B", "parent", "R"})
	s.Assert(ctx(), triple.Triple{"C", "parent", "A"})

	// All descendants of R (follow parent inverse)
	got, err := g.Reachable(ctx(), "R", "parent", true)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	// R -> A, B (direct children), A -> C (grandchild)
	if len(got) != 4 || got[0] != "A" || got[1] != "B" || got[2] != "C" || got[3] != "R" {
		t.Fatalf("expected [A B C R], got %v", got)
	}
}

func TestReachableNoCycle(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	// Cycle: A --r--> B --r--> A
	s.Assert(ctx(), triple.Triple{"A", "r", "B"})
	s.Assert(ctx(), triple.Triple{"B", "r", "A"})

	got, err := g.Reachable(ctx(), "A", "r", false)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("expected [A B] (no infinite loop), got %v", got)
	}
}

// --- RetractSubgraph ---

func TestRetractSubgraph(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	// Two nodes in root "R1", one in "R2"
	s.Assert(ctx(), triple.Triple{"n1", "root", "R1"})
	s.Assert(ctx(), triple.Triple{"n1", "content", "hello"})
	s.Assert(ctx(), triple.Triple{"n2", "root", "R1"})
	s.Assert(ctx(), triple.Triple{"n2", "content", "world"})
	s.Assert(ctx(), triple.Triple{"n3", "root", "R2"})
	s.Assert(ctx(), triple.Triple{"n3", "content", "keep"})

	err := g.RetractSubgraph(ctx(), "root", "R1")
	if err != nil {
		t.Fatal(err)
	}

	// n1 and n2 should be gone (all their triples, not just root)
	got, _ := s.BySubject(ctx(), "n1")
	if len(got) != 0 {
		t.Fatalf("expected 0 triples for n1, got %d", len(got))
	}
	got, _ = s.BySubject(ctx(), "n2")
	if len(got) != 0 {
		t.Fatalf("expected 0 triples for n2, got %d", len(got))
	}

	// n3 should be untouched
	got, _ = s.BySubject(ctx(), "n3")
	if len(got) != 2 {
		t.Fatalf("expected 2 triples for n3, got %d", len(got))
	}
}

// --- RegisterPredicate + Spec ---

func TestRegisterAndRetrieveSpec(t *testing.T) {
	g := testGraph(t)

	g.RegisterPredicate(graphops.PredicateSpec{
		Name:         "parent",
		Multiplicity: graphops.Functional,
		Inverse:      "children",
	})

	spec := g.Spec("parent")
	if spec == nil {
		t.Fatal("expected spec for parent, got nil")
	}
	if spec.Multiplicity != graphops.Functional {
		t.Fatalf("expected Functional, got %v", spec.Multiplicity)
	}
	if spec.Inverse != "children" {
		t.Fatalf("expected children inverse, got %q", spec.Inverse)
	}
}

func TestSetRejectsRelational(t *testing.T) {
	g := testGraph(t)
	g.RegisterPredicate(graphops.PredicateSpec{
		Name:         "link",
		Multiplicity: graphops.Relational,
	})

	err := g.Set(ctx(), "A", "link", "B")
	if err == nil {
		t.Fatal("expected error calling Set on relational predicate")
	}
}

func TestSetAllowsUnregistered(t *testing.T) {
	g := testGraph(t)
	// Unregistered predicates are allowed (no metadata = no restriction)
	err := g.Set(ctx(), "A", "unknown-pred", "val")
	if err != nil {
		t.Fatalf("expected no error for unregistered predicate, got %v", err)
	}
}

func TestSubgraph(t *testing.T) {
	g := testGraph(t)
	s := g.Store()

	s.Assert(ctx(), triple.Triple{"n1", "root", "R1"})
	s.Assert(ctx(), triple.Triple{"n1", "content", "hello"})
	s.Assert(ctx(), triple.Triple{"n2", "root", "R1"})
	s.Assert(ctx(), triple.Triple{"n2", "content", "world"})
	s.Assert(ctx(), triple.Triple{"n3", "root", "R2"})

	triples, err := g.Subgraph(ctx(), "root", "R1")
	if err != nil {
		t.Fatal(err)
	}
	// n1 has 2 triples, n2 has 2 triples = 4 total
	if len(triples) != 4 {
		t.Fatalf("expected 4 triples in subgraph, got %d", len(triples))
	}
	// n3 should not appear
	for _, tr := range triples {
		if tr.Subject == "n3" {
			t.Fatal("n3 should not be in R1's subgraph")
		}
	}
}

func TestSpecUnregistered(t *testing.T) {
	g := testGraph(t)
	if spec := g.Spec("unknown"); spec != nil {
		t.Fatalf("expected nil for unregistered predicate, got %+v", spec)
	}
}
