package kernel

import (
	"fmt"
	"strings"
	"testing"
)

// ========================================================================
// Test fixtures
// ========================================================================

// testKB is a minimal in-memory KB used to exercise Contextual refinements.
type testKB struct {
	nodes map[string]string
}

func (k *testKB) ResolveNode(title string) (string, bool) {
	if k.nodes == nil {
		return "", false
	}
	content, ok := k.nodes[title]
	return content, ok
}

func newTestKB(entries map[string]string) *testKB {
	return &testKB{nodes: entries}
}

// sampleKB matches the sampleKB from docs/sketch/TypesKernel.hs.
func sampleKB() *testKB {
	return newTestKB(map[string]string{
		"Discussion - CI/CD Substrate": "# Discussion\n\n**[agent 2026-04-13]** First question here.\n**[agent 2026-04-13]** The last line is this one.",
		"Braindump":                    "# overview\n\nTop-level node.",
	})
}

// ------- task type: level-2 refinement (intrinsic, intra-value) -------

func taskType() DerivedType {
	return DerivedType{
		TName:      "task",
		ParentName: "create",
		Refinements: []Refinement{
			IntrinsicRefinement{
				NameStr: "status and deadline present in extra",
				Fn: func(v Value) error {
					fv := v.Get("extra")
					m, ok := fv.(VMap)
					if !ok {
						return errF("extra must be a map")
					}
					var missing []string
					for _, k := range []string{"status", "deadline"} {
						if _, present := m[k]; !present {
							missing = append(missing, k)
						}
					}
					if len(missing) > 0 {
						return errF("missing extra keys: %v", missing)
					}
					return nil
				},
			},
		},
	}
}

// ------- valid-edit: level-3 refinement (contextual, KB lookup) -------

func validEditType() DerivedType {
	return DerivedType{
		TName:      "valid-edit",
		ParentName: "edit",
		Refinements: []Refinement{
			ContextualRefinement{
				NameStr: "file must resolve in KB",
				Fn: func(kb KB, v Value) error {
					fv := v.Get("file")
					s, ok := fv.(VString)
					if !ok {
						return errF("file must be a string")
					}
					if _, present := kb.ResolveNode(string(s)); !present {
						return errF("file %q does not resolve in KB", string(s))
					}
					return nil
				},
			},
		},
	}
}

// ------- discussion-turn: level-3 refinement (suffix-of-last-line) -------

func discussionTurnType() DerivedType {
	return DerivedType{
		TName:      "discussion-turn",
		ParentName: "valid-edit",
		Refinements: []Refinement{
			ContextualRefinement{
				NameStr: "old_text is a suffix of the last line of file",
				Fn: func(kb KB, v Value) error {
					fv1 := v.Get("file")
					fv2 := v.Get("old_text")
					f, ok := fv1.(VString)
					if !ok {
						return errF("file must be a string")
					}
					ot, ok := fv2.(VString)
					if !ok {
						return errF("old_text must be a string")
					}
					content, present := kb.ResolveNode(string(f))
					if !present {
						return errF("file %q does not resolve", string(f))
					}
					lines := strings.Split(content, "\n")
					last := ""
					if len(lines) > 0 {
						last = lines[len(lines)-1]
					}
					if !strings.HasSuffix(last, string(ot)) {
						return errF("old_text is not a suffix of last line %q", last)
					}
					return nil
				},
			},
		},
	}
}

// ------- discussion-start: level-1 refinement (title prefix) -------

func discussionStartType() DerivedType {
	return DerivedType{
		TName:      "discussion-start",
		ParentName: "create",
		Refinements: []Refinement{
			IntrinsicRefinement{
				NameStr: "title has 'Discussion - ' prefix",
				Fn: func(v Value) error {
					fv := v.Get("title")
					s, ok := fv.(VString)
					if !ok {
						return errF("title must be a string")
					}
					if !strings.HasPrefix(string(s), "Discussion - ") {
						return errF("title must start with 'Discussion - ', got %q", string(s))
					}
					return nil
				},
			},
		},
	}
}

// exampleRegistry matches the one in docs/sketch/TypesKernel.hs.
func exampleRegistry() *Registry {
	r := NewPrimitivesRegistry()
	r.Insert(taskType())
	r.Insert(validEditType())
	r.Insert(discussionTurnType())
	r.Insert(discussionStartType())
	return r
}

// errF returns a formatted error. Tiny wrapper to keep refinement
// bodies compact in the test fixtures.
func errF(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}

// ========================================================================
// Tests
// ========================================================================

func TestSubsumption(t *testing.T) {
	r := exampleRegistry()

	cases := []struct {
		name     string
		sub      TypeName
		super    TypeName
		expected bool
	}{
		{"task <: create", "task", "create", true},
		{"create </: task", "create", "task", false},
		{"discussion-turn <: edit", "discussion-turn", "edit", true},
		{"discussion-turn <: valid-edit", "discussion-turn", "valid-edit", true},
		{"task </: edit", "task", "edit", false},
		{"task <: task (reflexive)", "task", "task", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := r.IsSubtype(c.sub, c.super)
			if got != c.expected {
				t.Errorf("IsSubtype(%s, %s) = %v, want %v", c.sub, c.super, got, c.expected)
			}
		})
	}
}

func TestRootPrimitive(t *testing.T) {
	r := exampleRegistry()
	cases := []struct {
		name     TypeName
		expected Primitive
	}{
		{"task", PCreate},
		{"discussion-turn", PEdit},
		{"discussion-start", PCreate},
		{"valid-edit", PEdit},
	}
	for _, c := range cases {
		t.Run(string(c.name), func(t *testing.T) {
			got, ok := r.RootPrimitive(c.name)
			if !ok {
				t.Fatalf("RootPrimitive(%s): not found", c.name)
			}
			if got != c.expected {
				t.Errorf("RootPrimitive(%s) = %v, want %v", c.name, got, c.expected)
			}
		})
	}
}

func TestComposedShape(t *testing.T) {
	r := exampleRegistry()
	// task inherits create's fields (title, parent, content, extra).
	fs := r.ComposedShape("task")
	names := []FieldName{}
	for _, f := range fs {
		names = append(names, f.Name)
	}
	expected := []FieldName{"title", "parent", "content", "extra"}
	if len(names) != len(expected) {
		t.Fatalf("composed shape of task: %v, want %v", names, expected)
	}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("field %d: got %s, want %s", i, n, expected[i])
		}
	}
}

func TestValidateEdit_MissingFile(t *testing.T) {
	r := exampleRegistry()
	v := NewValue(
		FieldPair{"old_text", VString("foo")},
		FieldPair{"new_text", VString("bar")},
	)
	err := r.Validate(sampleKB(), "edit", v)
	if err == nil {
		t.Fatal("expected error for missing required `file`, got nil")
	}
	if !strings.Contains(err.Error(), "file required but absent") {
		t.Errorf("expected 'file required but absent', got %q", err.Error())
	}
}

func TestValidateEdit_AnyFilePasses(t *testing.T) {
	// Primitive edit doesn't check whether the file resolves — that's
	// valid-edit's job. A plain edit with any non-empty file passes.
	r := exampleRegistry()
	v := NewValue(
		FieldPair{"file", VString("nonexistent")},
		FieldPair{"old_text", VString("foo")},
		FieldPair{"new_text", VString("bar")},
	)
	if err := r.Validate(sampleKB(), "edit", v); err != nil {
		t.Errorf("unexpected error on primitive edit: %v", err)
	}
}

func TestValidateValidEdit_FileDoesNotResolve(t *testing.T) {
	r := exampleRegistry()
	v := NewValue(
		FieldPair{"file", VString("nonexistent")},
		FieldPair{"old_text", VString("foo")},
		FieldPair{"new_text", VString("bar")},
	)
	err := r.Validate(sampleKB(), "valid-edit", v)
	if err == nil {
		t.Fatal("expected valid-edit to reject unresolved file")
	}
	if !strings.Contains(err.Error(), "does not resolve") {
		t.Errorf("expected 'does not resolve' error, got %q", err.Error())
	}
}

func TestValidateDiscussionTurn_CorrectSuffix(t *testing.T) {
	r := exampleRegistry()
	v := NewValue(
		FieldPair{"file", VString("Discussion - CI/CD Substrate")},
		FieldPair{"old_text", VString("The last line is this one.")},
		FieldPair{"new_text", VString("The last line is this one.\n\n**[agent]** reply")},
	)
	if err := r.Validate(sampleKB(), "discussion-turn", v); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateDiscussionTurn_WrongSuffix(t *testing.T) {
	r := exampleRegistry()
	v := NewValue(
		FieldPair{"file", VString("Discussion - CI/CD Substrate")},
		FieldPair{"old_text", VString("wrong text")},
		FieldPair{"new_text", VString("doesn't matter")},
	)
	err := r.Validate(sampleKB(), "discussion-turn", v)
	if err == nil {
		t.Fatal("expected suffix mismatch to fail")
	}
	if !strings.Contains(err.Error(), "not a suffix") {
		t.Errorf("expected 'not a suffix' error, got %q", err.Error())
	}
}

func TestValidateTask_StatusAndDeadlinePresent(t *testing.T) {
	r := exampleRegistry()
	v := NewValue(
		FieldPair{"title", VString("My Task")},
		FieldPair{"content", VString("do the thing")},
		FieldPair{"extra", VMap(map[string]string{
			"status":   "todo",
			"deadline": "2026-05-01",
		})},
	)
	if err := r.Validate(sampleKB(), "task", v); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateTask_MissingDeadline(t *testing.T) {
	r := exampleRegistry()
	v := NewValue(
		FieldPair{"title", VString("My Task")},
		FieldPair{"content", VString("do the thing")},
		FieldPair{"extra", VMap(map[string]string{"status": "todo"})},
	)
	err := r.Validate(sampleKB(), "task", v)
	if err == nil {
		t.Fatal("expected task validation to fail without deadline")
	}
	if !strings.Contains(err.Error(), "deadline") {
		t.Errorf("expected error to mention deadline, got %q", err.Error())
	}
}

func TestValidateDiscussionStart_WrongTitlePrefix(t *testing.T) {
	r := exampleRegistry()
	v := NewValue(
		FieldPair{"title", VString("Not A Discussion")},
		FieldPair{"content", VString("body")},
	)
	err := r.Validate(sampleKB(), "discussion-start", v)
	if err == nil {
		t.Fatal("expected wrong-prefix title to fail")
	}
	if !strings.Contains(err.Error(), "Discussion -") {
		t.Errorf("expected title-prefix error, got %q", err.Error())
	}
}

func TestValidateDiscussionStart_CorrectTitle(t *testing.T) {
	r := exampleRegistry()
	v := NewValue(
		FieldPair{"title", VString("Discussion - Braindump")},
		FieldPair{"content", VString("body")},
	)
	if err := r.Validate(sampleKB(), "discussion-start", v); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestSchemaInstructionMentionsRefinements confirms that schema output
// (the prompt-facing text) actually names the refinement clauses —
// which is what guarantees the LLM is told exactly what the validator
// will enforce.
func TestSchemaInstructionMentionsRefinements(t *testing.T) {
	r := exampleRegistry()
	s := r.SchemaInstruction("discussion-turn")
	if !strings.Contains(s, "old_text is a suffix of the last line of file") {
		t.Errorf("schema for discussion-turn missing suffix constraint:\n%s", s)
	}
	if !strings.Contains(s, "file must resolve in KB") {
		t.Errorf("schema for discussion-turn missing file-resolution constraint:\n%s", s)
	}
}

// TestSchemaInstructionIncludesJSONPreamble verifies that the
// composed schema starts with the primitive's prescriptive "you
// MUST respond with JSON" preamble — not just a type summary.
// Without this, the prompt doesn't actually instruct the LLM on
// wire format and the LLM returns prose.
func TestSchemaInstructionIncludesJSONPreamble(t *testing.T) {
	r := exampleRegistry()

	editSchema := r.SchemaInstruction("edit")
	if !strings.Contains(editSchema, "MUST respond with a JSON object") {
		t.Errorf("edit schema missing JSON preamble:\n%s", editSchema)
	}
	if !strings.Contains(editSchema, `"action":"edit"`) {
		t.Errorf("edit schema missing action example:\n%s", editSchema)
	}
	// The example must show the full envelope (ops array), not the
	// bare op — bug #2 from the code review.
	if !strings.Contains(editSchema, `"ops":[`) {
		t.Errorf("edit schema example should show full envelope with ops wrapper:\n%s", editSchema)
	}

	createSchema := r.SchemaInstruction("create")
	if !strings.Contains(createSchema, `"action":"create"`) {
		t.Errorf("create schema missing action example:\n%s", createSchema)
	}
	if !strings.Contains(createSchema, `"ops":[`) {
		t.Errorf("create schema example should show full envelope with ops wrapper:\n%s", createSchema)
	}

	// Derived type inherits the primitive's preamble and adds
	// constraints.
	turnSchema := r.SchemaInstruction("discussion-turn")
	if !strings.Contains(turnSchema, "MUST respond with a JSON object") {
		t.Errorf("discussion-turn schema missing JSON preamble:\n%s", turnSchema)
	}
}

// TestValidateUnknownType ensures we don't silently accept a reference
// to a type that isn't in the registry.
func TestValidateUnknownType(t *testing.T) {
	r := exampleRegistry()
	v := NewValue(FieldPair{"text", VString("hello")})
	err := r.Validate(sampleKB(), "nonexistent-type", v)
	if err == nil {
		t.Fatal("expected unknown-type error")
	}
	if !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("expected 'unknown type' error, got %q", err.Error())
	}
}

// TestAncestorsOrder verifies that Ancestors walks child-first up to
// the primitive root (important for RootPrimitive and IsSubtype).
func TestAncestorsOrder(t *testing.T) {
	r := exampleRegistry()
	chain := r.Ancestors("discussion-turn")
	expected := []TypeName{"discussion-turn", "valid-edit", "edit"}
	if len(chain) != len(expected) {
		t.Fatalf("Ancestors(discussion-turn) = %v, want %v", chain, expected)
	}
	for i, n := range chain {
		if n != expected[i] {
			t.Errorf("chain[%d] = %s, want %s", i, n, expected[i])
		}
	}
}
