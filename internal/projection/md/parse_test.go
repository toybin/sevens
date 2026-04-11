package md

import (
	"testing"
)

// --- ParseFrontmatter ---

func TestParseFrontmatterBasic(t *testing.T) {
	input := "---\ntitle: My Note\nparent: \"[[Root]]\"\n---\n\nHello world"
	fm, body := ParseFrontmatter(input)
	if fm.Title != "My Note" {
		t.Fatalf("expected title 'My Note', got %q", fm.Title)
	}
	if fm.Parent != "Root" {
		t.Fatalf("expected parent 'Root', got %q", fm.Parent)
	}
	if body != "Hello world" {
		t.Fatalf("expected body 'Hello world', got %q", body)
	}
}

func TestParseFrontmatterNoParent(t *testing.T) {
	input := "---\ntitle: Root Note\n---\n\nContent here"
	fm, body := ParseFrontmatter(input)
	if fm.Title != "Root Note" {
		t.Fatalf("expected 'Root Note', got %q", fm.Title)
	}
	if fm.Parent != "" {
		t.Fatalf("expected empty parent, got %q", fm.Parent)
	}
	if body != "Content here" {
		t.Fatalf("expected 'Content here', got %q", body)
	}
}

func TestParseFrontmatterAllFields(t *testing.T) {
	input := "---\ntitle: Pro\nparent: \"[[Analysis]]\"\nsibling-role: support\ninclude-group: true\n---\n\nArguments for"
	fm, _ := ParseFrontmatter(input)
	if fm.SiblingRole != "support" {
		t.Fatalf("expected role 'support', got %q", fm.SiblingRole)
	}
	if !fm.IncludeGroup {
		t.Fatal("expected include-group true")
	}
}

func TestParseFrontmatterNoFrontmatter(t *testing.T) {
	input := "Just plain text with no frontmatter"
	fm, body := ParseFrontmatter(input)
	if fm.Title != "" {
		t.Fatalf("expected empty title, got %q", fm.Title)
	}
	if body != input {
		t.Fatalf("expected body to be original input")
	}
}

func TestParseFrontmatterUnclosed(t *testing.T) {
	input := "---\ntitle: Broken\nno closing delimiter"
	fm, body := ParseFrontmatter(input)
	if fm.Title != "" {
		t.Fatal("expected empty title for unclosed frontmatter")
	}
	if body != input {
		t.Fatal("expected body to be original input for unclosed frontmatter")
	}
}

func TestParseFrontmatterEmptyBody(t *testing.T) {
	input := "---\ntitle: Empty\n---\n"
	fm, body := ParseFrontmatter(input)
	if fm.Title != "Empty" {
		t.Fatalf("expected 'Empty', got %q", fm.Title)
	}
	if body != "" {
		t.Fatalf("expected empty body, got %q", body)
	}
}

// --- RenderFrontmatter ---

func TestRenderFrontmatter(t *testing.T) {
	fm := Frontmatter{Title: "My Note", Parent: "Root"}
	got := RenderFrontmatter(fm)
	expected := "---\ntitle: My Note\nparent: \"[[Root]]\"\n---\n"
	if got != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, got)
	}
}

func TestRenderFrontmatterNoParent(t *testing.T) {
	fm := Frontmatter{Title: "Root"}
	got := RenderFrontmatter(fm)
	expected := "---\ntitle: Root\n---\n"
	if got != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, got)
	}
}

func TestRenderFrontmatterWithRole(t *testing.T) {
	fm := Frontmatter{Title: "Pro", Parent: "Analysis", SiblingRole: "support"}
	got := RenderFrontmatter(fm)
	if !contains(got, "sibling-role: support") {
		t.Fatalf("expected sibling-role in output:\n%s", got)
	}
}

// --- RenderNode ---

func TestRenderNode(t *testing.T) {
	fm := Frontmatter{Title: "Test", Parent: "Root"}
	got := RenderNode(fm, "Hello world")
	if !contains(got, "title: Test") {
		t.Fatal("missing title")
	}
	if !contains(got, "Hello world") {
		t.Fatal("missing body")
	}
}

func TestRenderNodeEmptyBody(t *testing.T) {
	fm := Frontmatter{Title: "Empty"}
	got := RenderNode(fm, "")
	if contains(got, "\n\n\n") {
		t.Fatal("empty body should not produce extra newlines")
	}
}

// --- Round-trip ---

func TestFrontmatterRoundTrip(t *testing.T) {
	original := Frontmatter{Title: "Note", Parent: "Root", SiblingRole: "support"}
	rendered := RenderFrontmatter(original)
	parsed, _ := ParseFrontmatter(rendered)

	if parsed.Title != original.Title {
		t.Fatalf("title: expected %q, got %q", original.Title, parsed.Title)
	}
	if parsed.Parent != original.Parent {
		t.Fatalf("parent: expected %q, got %q", original.Parent, parsed.Parent)
	}
	if parsed.SiblingRole != original.SiblingRole {
		t.Fatalf("role: expected %q, got %q", original.SiblingRole, parsed.SiblingRole)
	}
}

// --- ExtractWikiLinks ---

func TestExtractWikiLinks(t *testing.T) {
	content := "See [[Governance]] and also [[Revenue]]. Also [[Governance]] again."
	links := ExtractWikiLinks(content)
	if len(links) != 2 {
		t.Fatalf("expected 2 unique links, got %d: %v", len(links), links)
	}
	if links[0] != "Governance" || links[1] != "Revenue" {
		t.Fatalf("expected [Governance Revenue], got %v", links)
	}
}

func TestExtractWikiLinksNone(t *testing.T) {
	links := ExtractWikiLinks("No links here")
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %v", links)
	}
}

func TestExtractWikiLinksMultiline(t *testing.T) {
	content := "Line 1 [[A]]\nLine 2 [[B]]\nLine 3 [[C]] and [[A]]"
	links := ExtractWikiLinks(content)
	if len(links) != 3 {
		t.Fatalf("expected 3 unique links, got %d: %v", len(links), links)
	}
}

// --- SanitizeFilename ---

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Note", "my-note.md"},
		{"Commons Governance Models", "commons-governance-models.md"},
		{"Discussion: The Commons", "discussion-the-commons.md"},
		{"  Spaces  ", "spaces.md"},
		{"UPPER case", "upper-case.md"},
		{"symbols!@#$%", "symbols.md"},
		{"already-kebab", "already-kebab.md"},
		{"", "untitled.md"},
	}
	for _, tc := range tests {
		got := SanitizeFilename(tc.input)
		if got != tc.expected {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstr(s, substr)
}

func containsSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
