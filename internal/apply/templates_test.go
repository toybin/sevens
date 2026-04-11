package apply

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "turso.tech/database/tursogo"

	"sevens/internal/store"
)

func testTemplateDB(t *testing.T) *sql.DB {
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

func TestLoadTemplate_BundledDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tmpl, err := LoadTemplate("inbox-capture")
	if err != nil {
		t.Fatalf("LoadTemplate bundled default: %v", err)
	}
	if tmpl.Name != "inbox-capture" {
		t.Fatalf("expected bundled template name %q, got %q", "inbox-capture", tmpl.Name)
	}
	if !strings.Contains(tmpl.Content, "## Notes") {
		t.Fatalf("expected bundled markdown sidecar content, got %q", tmpl.Content)
	}
	if tmpl.Target == nil || tmpl.Target.Parent != "inbox" {
		t.Fatalf("expected bundled target parent inbox, got %#v", tmpl.Target)
	}
}

func TestLoadTemplate_UserOverrideWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tmplDir := filepath.Join(home, ".config", "sevens", "templates")
	if err := os.MkdirAll(tmplDir, 0755); err != nil {
		t.Fatalf("mkdir templates dir: %v", err)
	}

	data := []byte(`{:name "inbox-capture"
 :description "override"
 :mode "create-node"
 :title-pattern "{{title}}"
 :content "override body"}`)
	if err := os.WriteFile(filepath.Join(tmplDir, "inbox-capture.edn"), data, 0644); err != nil {
		t.Fatalf("write override edn: %v", err)
	}

	tmpl, err := LoadTemplate("inbox-capture")
	if err != nil {
		t.Fatalf("LoadTemplate override: %v", err)
	}
	if tmpl.Description != "override" {
		t.Fatalf("expected override description, got %q", tmpl.Description)
	}
	if tmpl.Content != "override body" {
		t.Fatalf("expected inline override content, got %q", tmpl.Content)
	}
}

func TestListTemplates_IncludesBundledDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	names, err := ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("expected bundled templates")
	}

	found := false
	for _, name := range names {
		if name == "inbox-capture" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected bundled template inbox-capture in list")
	}
}

func TestResolveTemplateVars_AppliesDefaults(t *testing.T) {
	tmpl := &NodeTemplate{
		Name: "capture",
		Params: []TemplateParam{
			{Name: "title", Required: true},
			{Name: "slug", Default: "{{title}}"},
		},
	}
	vars := ResolveTemplateVars(tmpl, map[string]string{"title": "Inbox Item"})
	if vars["slug"] != "Inbox Item" {
		t.Fatalf("slug = %q, want %q", vars["slug"], "Inbox Item")
	}
	if vars["today"] == "" || vars["timestamp"] == "" {
		t.Fatalf("expected builtins, got %#v", vars)
	}
}

func TestDraftTitle_ReplacesUnresolvedVars(t *testing.T) {
	tmpl := &NodeTemplate{
		Name:              "capture",
		DraftTitlePattern: "Capture {{title}} {{date}}",
	}
	title := DraftTitle(tmpl)
	if strings.Contains(title, "{{") {
		t.Fatalf("draft title should not keep placeholders, got %q", title)
	}
	if !strings.Contains(title, "Capture") {
		t.Fatalf("draft title = %q", title)
	}
}

func TestCleanRenderedTemplate_StripsOptionalPlaceholders(t *testing.T) {
	tmpl := &NodeTemplate{
		Name:         "capture",
		TitlePattern: "{{title}}",
		Content:      "# {{title}}\n\n{{summary}}\n",
	}
	rendered := RenderTemplate(tmpl, map[string]string{"title": "Inbox Item"})
	cleaned := CleanRenderedTemplate(rendered)
	if strings.Contains(cleaned.Content, "{{summary}}") {
		t.Fatalf("expected summary placeholder removed, got %q", cleaned.Content)
	}
	if !strings.Contains(cleaned.Content, "Inbox Item") {
		t.Fatalf("expected title preserved, got %q", cleaned.Content)
	}
}

func TestBindTemplateArgs_UsesParamOrderWithoutOverwritingExplicitVars(t *testing.T) {
	tmpl := &NodeTemplate{
		Name: "capture",
		Params: []TemplateParam{
			{Name: "title", Required: true},
			{Name: "summary"},
			{Name: "topic"},
		},
	}

	bound := BindTemplateArgs(tmpl, []string{"Inbox Item", "Short summary", "Ignored"}, map[string]string{
		"topic": "preset-topic",
	})

	if bound["title"] != "Inbox Item" {
		t.Fatalf("title = %q, want %q", bound["title"], "Inbox Item")
	}
	if bound["summary"] != "Short summary" {
		t.Fatalf("summary = %q, want %q", bound["summary"], "Short summary")
	}
	if bound["topic"] != "preset-topic" {
		t.Fatalf("topic = %q, want %q", bound["topic"], "preset-topic")
	}
}

func TestRenderAndCleanTemplate_ApplyPlacementVars(t *testing.T) {
	tmpl := &NodeTemplate{
		Name: "section-entry",
		Placement: &TemplatePlacement{
			Kind:    "under-heading",
			Heading: "{{heading}}",
		},
		Content: "- {{text}}\n",
	}

	rendered := RenderTemplate(tmpl, map[string]string{"heading": "Today"})
	if rendered.Placement == nil || rendered.Placement.Heading != "Today" {
		t.Fatalf("placement heading = %#v, want %q", rendered.Placement, "Today")
	}

	cleaned := CleanRenderedTemplate(RenderTemplate(tmpl, map[string]string{}))
	if cleaned.Placement == nil || cleaned.Placement.Heading != "" {
		t.Fatalf("cleaned placement = %#v, want empty heading", cleaned.Placement)
	}
	if strings.Contains(cleaned.Content, "{{text}}") {
		t.Fatalf("cleaned content kept unresolved placeholder: %q", cleaned.Content)
	}
}

func TestExecuteTemplate_DoesNotBootstrapExistingEmptyParent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	db := testTemplateDB(t)

	inboxPath := filepath.Join(root, "inbox.md")
	if err := os.WriteFile(inboxPath, []byte("---\ntitle: inbox\n---\n"), 0644); err != nil {
		t.Fatalf("write inbox file: %v", err)
	}
	inboxSubj := store.NodeSubject(root, "inbox")
	if err := store.InsertTriples(db, []store.Triple{
		{Subject: inboxSubj, Predicate: "node/title", Object: "inbox"},
		{Subject: inboxSubj, Predicate: "node/root", Object: root},
		{Subject: inboxSubj, Predicate: "node/file-path", Object: inboxPath},
		{Subject: inboxSubj, Predicate: "node/content", Object: ""},
	}); err != nil {
		t.Fatalf("insert inbox triples: %v", err)
	}

	tmpl, err := LoadTemplate("inbox-capture")
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}

	result, err := ExecuteTemplate(db, root, tmpl, TemplateExecutionOptions{
		Vars: map[string]string{"title": "Capture Test"},
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	if len(result.Created) != 1 {
		t.Fatalf("created = %#v, want 1 created file", result.Created)
	}
	if result.Created[0] != "capture test.md" {
		t.Fatalf("created[0] = %q, want %q", result.Created[0], "capture test.md")
	}
	if _, err := os.Stat(filepath.Join(root, "capture test.md")); err != nil {
		t.Fatalf("expected created capture file: %v", err)
	}
}
