package repl

import (
	"testing"

	"sevens/internal/store"
)

func TestResolveDiscussionFilePath_UsesResolvedSubject(t *testing.T) {
	db := testREPLDB(t)
	root := "/tmp/root"
	subject := store.NodeSubject(root, "Discussion - Parent")
	if err := store.InsertTriples(db, []store.Triple{
		{Subject: subject, Predicate: "node/root", Object: root},
		{Subject: subject, Predicate: "node/title", Object: "Discussion - Parent"},
		{Subject: subject, Predicate: "node/file-path", Object: "/tmp/root/discussion - parent.md"},
	}); err != nil {
		t.Fatalf("insert triples: %v", err)
	}

	got, err := resolveDiscussionFilePath(db, root, "Discussion - Parent")
	if err != nil {
		t.Fatalf("resolveDiscussionFilePath returned error: %v", err)
	}
	if got != "/tmp/root/discussion - parent.md" {
		t.Fatalf("path = %q, want %q", got, "/tmp/root/discussion - parent.md")
	}
}
