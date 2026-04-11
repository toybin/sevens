package repl

import (
	"testing"

	"sevens/internal/kb"
)

func TestResolveDiscussionFilePath_UsesResolvedSubject(t *testing.T) {
	root := "/tmp/root"
	subject := kb.NodeSubject(root, "Discussion - Parent")

	q := &testGraphQuerier{
		titles: map[string]string{
			"Discussion - Parent": subject,
		},
		objs: map[string]map[string]string{
			subject: {
				"node/file-path": "/tmp/root/discussion - parent.md",
			},
		},
	}

	r := &REPL{root: root, graphQ: q}

	got, err := r.resolveDiscussionFilePath(root, "Discussion - Parent")
	if err != nil {
		t.Fatalf("resolveDiscussionFilePath returned error: %v", err)
	}
	if got != "/tmp/root/discussion - parent.md" {
		t.Fatalf("path = %q, want %q", got, "/tmp/root/discussion - parent.md")
	}
}
