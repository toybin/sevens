package types

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sevens/internal/config"
	"sevens/internal/graphops"
	"sevens/internal/kb"
	"sevens/internal/triple"

	_ "turso.tech/database/tursogo"
)

func ctx() context.Context { return context.Background() }

// newKB creates an in-memory KB for testing.
func newKB(t *testing.T) *kb.KB {
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
	return kb.New(graph)
}

// setupTypesDir creates a temp directory with type EDN files and
// sets config.OverrideConfigDir so LoadTypeDef finds them.
func setupTypesDir(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	config.OverrideConfigDir = dir
	t.Cleanup(func() { config.OverrideConfigDir = "" })

	typesDir := filepath.Join(dir, "types")
	if err := os.MkdirAll(typesDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(typesDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadTypeDef(t *testing.T) {
	setupTypesDir(t, map[string]string{
		"task.edn": `{:name "task"
 :predicates {:required ["status" "deadline"]
              :optional ["priority" "assignee"]}
 :structure {:parent-type "project"
             :children {:min 0 :max 5}}
 :projection {:frontmatter ["status" "deadline" "priority"]
              :orthography {"assignee" {:signifier "@" :value-model "person"}}}}`,
	})

	td, err := LoadTypeDef("task")
	if err != nil {
		t.Fatal(err)
	}
	if td.Name != "task" {
		t.Fatalf("expected name 'task', got %q", td.Name)
	}
	if len(td.Predicates.Required) != 2 {
		t.Fatalf("expected 2 required, got %d", len(td.Predicates.Required))
	}
	if td.Predicates.Required[0] != "status" || td.Predicates.Required[1] != "deadline" {
		t.Fatalf("unexpected required: %v", td.Predicates.Required)
	}
	if len(td.Predicates.Optional) != 2 {
		t.Fatalf("expected 2 optional, got %d", len(td.Predicates.Optional))
	}
	if td.Structure.ParentType != "project" {
		t.Fatalf("expected parent-type 'project', got %q", td.Structure.ParentType)
	}
	if td.Structure.ChildrenMax != 5 {
		t.Fatalf("expected children max 5, got %d", td.Structure.ChildrenMax)
	}
	if td.Projection.Orthography["assignee"].Signifier != "@" {
		t.Fatalf("expected signifier '@', got %q", td.Projection.Orthography["assignee"].Signifier)
	}
}

func TestLoadTypeDefNotFound(t *testing.T) {
	setupTypesDir(t, map[string]string{})
	_, err := LoadTypeDef("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestListTypeDefs(t *testing.T) {
	setupTypesDir(t, map[string]string{
		"note.edn": `{:name "note" :predicates {:required [] :optional ["tags"]} :structure {} :projection {}}`,
		"task.edn": `{:name "task" :predicates {:required ["status"] :optional []} :structure {} :projection {}}`,
	})

	defs, err := ListTypeDefs()
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 types, got %d", len(defs))
	}
	if defs[0].Name != "note" || defs[1].Name != "task" {
		t.Fatalf("expected [note, task], got [%s, %s]", defs[0].Name, defs[1].Name)
	}
}

func TestLoadAllTypeDefs(t *testing.T) {
	setupTypesDir(t, map[string]string{
		"note.edn": `{:name "note" :predicates {:required [] :optional []} :structure {} :projection {}}`,
		"task.edn": `{:name "task" :predicates {:required ["status"] :optional []} :structure {} :projection {}}`,
	})

	m, err := LoadAllTypeDefs()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 types, got %d", len(m))
	}
	if m["task"] == nil || m["note"] == nil {
		t.Fatal("expected both 'task' and 'note' in map")
	}
}

// --- Conformance tests ---

// seedNode creates a node with meta/* predicates in the KB.
func seedNode(t *testing.T, k *kb.KB, root, title string, meta map[string]string, parent *string) {
	t.Helper()
	_, err := k.CreateNode(ctx(), root, title, "content", parent)
	if err != nil {
		t.Fatal(err)
	}
	subject := kb.NodeSubject(root, title)
	for key, val := range meta {
		err := k.Graph().Store().Assert(ctx(), triple.Triple{
			Subject: subject, Predicate: "meta/" + key, Object: val,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestConformance_FullMatch(t *testing.T) {
	setupTypesDir(t, map[string]string{})
	k := newKB(t)
	root := "/test"

	seedNode(t, k, root, "My Task", map[string]string{
		"status":   "todo",
		"deadline": "2026-05-01",
	}, nil)

	td := &TypeDef{
		Name: "task",
		Predicates: PredicateSpec{
			Required: []string{"status", "deadline"},
			Optional: []string{"priority"},
		},
	}

	subject := kb.NodeSubject(root, "My Task")
	result, err := CheckConformance(ctx(), k, subject, td)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Conforms {
		t.Fatal("expected node to conform")
	}
	if len(result.Missing) != 0 {
		t.Fatalf("expected no missing, got %v", result.Missing)
	}
	sort.Strings(result.Present)
	if result.Present[0] != "deadline" || result.Present[1] != "status" {
		t.Fatalf("unexpected present: %v", result.Present)
	}
}

func TestConformance_PartialMatch(t *testing.T) {
	setupTypesDir(t, map[string]string{})
	k := newKB(t)
	root := "/test"

	seedNode(t, k, root, "Partial", map[string]string{
		"status": "done",
	}, nil)

	td := &TypeDef{
		Name: "task",
		Predicates: PredicateSpec{
			Required: []string{"status", "deadline"},
		},
	}

	subject := kb.NodeSubject(root, "Partial")
	result, err := CheckConformance(ctx(), k, subject, td)
	if err != nil {
		t.Fatal(err)
	}
	if result.Conforms {
		t.Fatal("expected node NOT to conform (missing deadline)")
	}
	if len(result.Missing) != 1 || result.Missing[0] != "deadline" {
		t.Fatalf("expected missing [deadline], got %v", result.Missing)
	}
	if len(result.Present) != 1 || result.Present[0] != "status" {
		t.Fatalf("expected present [status], got %v", result.Present)
	}
}

func TestConformance_ExtraPredicates(t *testing.T) {
	setupTypesDir(t, map[string]string{})
	k := newKB(t)
	root := "/test"

	seedNode(t, k, root, "Extra", map[string]string{
		"status":  "done",
		"custom":  "value",
		"another": "one",
	}, nil)

	td := &TypeDef{
		Name: "note",
		Predicates: PredicateSpec{
			Required: []string{},
			Optional: []string{"status"},
		},
	}

	subject := kb.NodeSubject(root, "Extra")
	result, err := CheckConformance(ctx(), k, subject, td)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Conforms {
		t.Fatal("expected node to conform (no required predicates)")
	}
	sort.Strings(result.Extra)
	if len(result.Extra) != 2 {
		t.Fatalf("expected 2 extra, got %v", result.Extra)
	}
	if result.Extra[0] != "another" || result.Extra[1] != "custom" {
		t.Fatalf("expected [another, custom], got %v", result.Extra)
	}
}

func TestConformance_ChildrenConstraints(t *testing.T) {
	setupTypesDir(t, map[string]string{})
	k := newKB(t)
	root := "/test"

	seedNode(t, k, root, "Parent", map[string]string{}, nil)
	parentTitle := "Parent"
	seedNode(t, k, root, "Child 1", map[string]string{}, &parentTitle)
	seedNode(t, k, root, "Child 2", map[string]string{}, &parentTitle)
	seedNode(t, k, root, "Child 3", map[string]string{}, &parentTitle)

	// Max 2 children
	td := &TypeDef{
		Name:       "small-parent",
		Predicates: PredicateSpec{},
		Structure: StructureSpec{
			ChildrenMax: 2,
		},
	}

	subject := kb.NodeSubject(root, "Parent")
	result, err := CheckConformance(ctx(), k, subject, td)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructureOK {
		t.Fatal("expected structure check to fail (3 children, max 2)")
	}
	if result.Conforms {
		t.Fatal("expected non-conformance due to structure")
	}

	// Max 5 children -- should pass
	td.Structure.ChildrenMax = 5
	result, err = CheckConformance(ctx(), k, subject, td)
	if err != nil {
		t.Fatal(err)
	}
	if !result.StructureOK {
		t.Fatal("expected structure check to pass (3 children, max 5)")
	}
}

func TestFindConformingNodes(t *testing.T) {
	setupTypesDir(t, map[string]string{})
	k := newKB(t)
	root := "/test"

	seedNode(t, k, root, "Task A", map[string]string{"status": "todo", "deadline": "2026-05-01"}, nil)
	seedNode(t, k, root, "Task B", map[string]string{"status": "done", "deadline": "2026-04-01"}, nil)
	seedNode(t, k, root, "Note C", map[string]string{"tags": "misc"}, nil)

	td := &TypeDef{
		Name: "task",
		Predicates: PredicateSpec{
			Required: []string{"status", "deadline"},
		},
	}

	titles, err := FindConformingNodes(ctx(), k, root, td)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(titles)
	if len(titles) != 2 {
		t.Fatalf("expected 2 conforming nodes, got %d: %v", len(titles), titles)
	}
	if titles[0] != "Task A" || titles[1] != "Task B" {
		t.Fatalf("expected [Task A, Task B], got %v", titles)
	}
}

func TestInferTypes(t *testing.T) {
	setupTypesDir(t, map[string]string{})
	k := newKB(t)
	root := "/test"

	seedNode(t, k, root, "My Node", map[string]string{
		"status":   "todo",
		"deadline": "2026-05-01",
		"tags":     "work",
	}, nil)

	types := map[string]*TypeDef{
		"task": {
			Name:       "task",
			Predicates: PredicateSpec{Required: []string{"status", "deadline"}},
		},
		"note": {
			Name:       "note",
			Predicates: PredicateSpec{Required: []string{}, Optional: []string{"tags"}},
		},
		"project": {
			Name:       "project",
			Predicates: PredicateSpec{Required: []string{"roadmap"}},
		},
	}

	subject := kb.NodeSubject(root, "My Node")
	results, err := InferTypes(ctx(), k, subject, types)
	if err != nil {
		t.Fatal(err)
	}

	conforming := 0
	for _, r := range results {
		if r.Conforms {
			conforming++
		}
	}
	// task (has status+deadline) and note (no required) should conform.
	// project (requires roadmap) should not.
	if conforming != 2 {
		t.Fatalf("expected 2 conforming types, got %d", conforming)
	}
}

// --- Schema instruction tests ---

func TestComposeSchemaInstruction_Primitive(t *testing.T) {
	allTypes := map[string]*TypeDef{
		"text": {
			Name:              "text",
			Primitive:         true,
			SchemaInstruction: "Respond with JSON containing a text field.",
		},
	}

	result := ComposeSchemaInstruction(allTypes["text"], allTypes)
	if result != "Respond with JSON containing a text field." {
		t.Fatalf("expected primitive instruction, got %q", result)
	}
}

func TestComposeSchemaInstruction_Subtype(t *testing.T) {
	allTypes := map[string]*TypeDef{
		"create": {
			Name:              "create",
			Primitive:         true,
			SchemaInstruction: "Respond with JSON containing an ops field.",
		},
		"task": {
			Name:    "task",
			Extends: "create",
			Predicates: PredicateSpec{
				Required: []string{"status", "deadline"},
				Optional: []string{"priority"},
			},
			Structure: StructureSpec{
				ParentType: "project",
			},
		},
	}

	result := ComposeSchemaInstruction(allTypes["task"], allTypes)

	// Should start with the base instruction.
	if !strings.HasPrefix(result, "Respond with JSON containing an ops field.") {
		t.Fatalf("expected to start with base instruction, got:\n%s", result)
	}
	// Should contain the subtype constructor.
	if !strings.Contains(result, `conform to type "task"`) {
		t.Fatalf("expected type name in constructor, got:\n%s", result)
	}
	if !strings.Contains(result, "status, deadline") {
		t.Fatalf("expected required fields, got:\n%s", result)
	}
	if !strings.Contains(result, "priority") {
		t.Fatalf("expected optional field, got:\n%s", result)
	}
	if !strings.Contains(result, `conform to type "project"`) {
		t.Fatalf("expected parent type constraint, got:\n%s", result)
	}
}

func TestComposeSchemaInstruction_SuggestionSubtype(t *testing.T) {
	allTypes := map[string]*TypeDef{
		"suggestion": {
			Name:              "suggestion",
			Primitive:         true,
			SchemaInstruction: "Respond with JSON containing a suggestions field.",
		},
		"dimension": {
			Name:    "dimension",
			Extends: "suggestion",
			Predicates: PredicateSpec{
				Required: []string{"scope"},
			},
		},
	}

	result := ComposeSchemaInstruction(allTypes["dimension"], allTypes)

	if !strings.HasPrefix(result, "Respond with JSON containing a suggestions field.") {
		t.Fatalf("expected to start with base instruction, got:\n%s", result)
	}
	if !strings.Contains(result, `conform to type "dimension"`) {
		t.Fatalf("expected suggestion hint, got:\n%s", result)
	}
	if !strings.Contains(result, "scope") {
		t.Fatalf("expected required field in hint, got:\n%s", result)
	}
}

func TestComposeSchemaInstruction_NoExtends(t *testing.T) {
	allTypes := map[string]*TypeDef{
		"orphan": {
			Name:              "orphan",
			SchemaInstruction: "Custom instruction.",
		},
	}

	result := ComposeSchemaInstruction(allTypes["orphan"], allTypes)
	if result != "Custom instruction." {
		t.Fatalf("expected own instruction, got %q", result)
	}
}

func TestBuildSignifierMap(t *testing.T) {
	allTypes := map[string]*TypeDef{
		"task": {
			Name: "task",
			Projection: ProjectionSpec{
				Orthography: map[string]OrthographyBinding{
					"assignee": {Signifier: "@", ValueModel: "person"},
					"priority": {Signifier: "!", ValueModel: "priority"},
					"status":   {Signifier: "", ValueModel: ""},
				},
			},
		},
		"note": {
			Name: "note",
			Projection: ProjectionSpec{
				Orthography: map[string]OrthographyBinding{
					"tag": {Signifier: "#", ValueModel: "tag"},
				},
			},
		},
	}

	m := BuildSignifierMap(allTypes)
	if m["@"] != "assignee" {
		t.Fatalf("expected @->assignee, got %q", m["@"])
	}
	if m["!"] != "priority" {
		t.Fatalf("expected !->priority, got %q", m["!"])
	}
	if m["#"] != "tag" {
		t.Fatalf("expected #->tag, got %q", m["#"])
	}
	// Empty signifier should not be in map.
	if _, exists := m[""]; exists {
		t.Fatal("empty signifier should not be in map")
	}
}

func TestBuildSignifierMap_FirstBindingWins(t *testing.T) {
	// When two types bind different predicates to the same signifier,
	// the first one encountered wins. Since map iteration is random,
	// just verify the value is one of the two valid predicates.
	allTypes := map[string]*TypeDef{
		"a": {
			Name: "a",
			Projection: ProjectionSpec{
				Orthography: map[string]OrthographyBinding{
					"owner": {Signifier: "@"},
				},
			},
		},
		"b": {
			Name: "b",
			Projection: ProjectionSpec{
				Orthography: map[string]OrthographyBinding{
					"assignee": {Signifier: "@"},
				},
			},
		},
	}

	m := BuildSignifierMap(allTypes)
	if m["@"] != "owner" && m["@"] != "assignee" {
		t.Fatalf("expected @->owner or @->assignee, got %q", m["@"])
	}
}

func TestLoadTypeDef_PrimitiveFields(t *testing.T) {
	setupTypesDir(t, map[string]string{
		"create.edn": `{:name "create"
 :primitive true
 :schema-instruction "Respond with ops."}`,
		"task.edn": `{:name "task"
 :extends "create"
 :predicates {:required ["status"] :optional []}
 :structure {}
 :projection {}}`,
	})

	create, err := LoadTypeDef("create")
	if err != nil {
		t.Fatal(err)
	}
	if !create.Primitive {
		t.Fatal("expected create to be primitive")
	}
	if create.SchemaInstruction != "Respond with ops." {
		t.Fatalf("unexpected schema instruction: %q", create.SchemaInstruction)
	}

	task, err := LoadTypeDef("task")
	if err != nil {
		t.Fatal(err)
	}
	if task.Extends != "create" {
		t.Fatalf("expected task to extend create, got %q", task.Extends)
	}
	if task.Primitive {
		t.Fatal("task should not be primitive")
	}
}
