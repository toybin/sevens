package graph

import "testing"

func TestRematchStableIDs_InsertSiblingKeepsSameParent(t *testing.T) {
	oldRoot := &SearchASTNode{
		StableID: "doc",
		Kind:     "doc",
		Label:    "doc",
		Children: []*SearchASTNode{
			{
				StableID: "today",
				Kind:     "heading",
				Label:    "Today",
				Children: []*SearchASTNode{
					{StableID: "task-a", Kind: "task", Label: "add block triples"},
					{StableID: "task-b", Kind: "task", Label: "decide stable identity"},
				},
			},
			{StableID: "blocked", Kind: "heading", Label: "Blocked"},
		},
	}
	newRoot := &SearchASTNode{
		Kind:  "doc",
		Label: "doc",
		Children: []*SearchASTNode{
			{
				Kind:  "heading",
				Label: "Today",
				Children: []*SearchASTNode{
					{Kind: "paragraph", Label: "intro note"},
					{Kind: "task", Label: "add block triples"},
					{Kind: "task", Label: "decide stable identity"},
				},
			},
			{Kind: "heading", Label: "Blocked"},
		},
	}

	matches := RematchStableIDs(oldRoot, newRoot).Matches
	if matches["today"] != "0" {
		t.Fatalf("today matched to %q, want %q", matches["today"], "0")
	}
	if matches["task-a"] != "0.1" {
		t.Fatalf("task-a matched to %q, want %q", matches["task-a"], "0.1")
	}
	if matches["task-b"] != "0.2" {
		t.Fatalf("task-b matched to %q, want %q", matches["task-b"], "0.2")
	}
}

func TestRematchStableIDs_MoveChildToSiblingParent(t *testing.T) {
	oldRoot := &SearchASTNode{
		StableID: "doc",
		Kind:     "doc",
		Label:    "doc",
		Children: []*SearchASTNode{
			{
				StableID: "today",
				Kind:     "heading",
				Label:    "Today",
				Children: []*SearchASTNode{
					{StableID: "task", Kind: "task", Label: "decide stable identity"},
				},
			},
			{StableID: "blocked", Kind: "heading", Label: "Blocked"},
		},
	}
	newRoot := &SearchASTNode{
		Kind:  "doc",
		Label: "doc",
		Children: []*SearchASTNode{
			{Kind: "heading", Label: "Today"},
			{
				Kind:  "heading",
				Label: "Blocked",
				Children: []*SearchASTNode{
					{Kind: "task", Label: "decide stable identity"},
				},
			},
		},
	}

	matches := RematchStableIDs(oldRoot, newRoot).Matches
	if matches["task"] != "1.0" {
		t.Fatalf("task matched to %q, want %q", matches["task"], "1.0")
	}
}

func TestRematchStableIDs_RecursesToStructurallySimilarParent(t *testing.T) {
	oldRoot := &SearchASTNode{
		StableID: "doc",
		Kind:     "doc",
		Label:    "doc",
		Children: []*SearchASTNode{
			{
				StableID: "identity",
				Kind:     "heading",
				Label:    "Identity",
				Children: []*SearchASTNode{
					{StableID: "decision", Kind: "paragraph", Label: "content anchors are required"},
				},
			},
			{StableID: "later", Kind: "heading", Label: "Later"},
		},
	}
	newRoot := &SearchASTNode{
		Kind:  "doc",
		Label: "doc",
		Children: []*SearchASTNode{
			{Kind: "heading", Label: "Today"},
			{
				Kind:  "heading",
				Label: "Decisions",
				Children: []*SearchASTNode{
					{
						Kind:  "heading",
						Label: "Identity",
						Children: []*SearchASTNode{
							{Kind: "paragraph", Label: "content anchors are required"},
						},
					},
				},
			},
			{Kind: "heading", Label: "Later"},
		},
	}

	matches := RematchStableIDs(oldRoot, newRoot).Matches
	if matches["identity"] != "1.0" {
		t.Fatalf("identity matched to %q, want %q", matches["identity"], "1.0")
	}
	if matches["decision"] != "1.0.0" {
		t.Fatalf("decision matched to %q, want %q", matches["decision"], "1.0.0")
	}
}
