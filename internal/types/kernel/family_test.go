package kernel

import (
	"testing"
)

// ========================================================================
// Type family fixtures
//
// Models the registry from docs/sketch/InputDependence.hs:
//
//   task <: create       with child-type task-child
//   task-child <: create with no child-type
//   dimension <: suggestion  with child-type dimension-child
//   dimension-child <: create with no child-type
//   note <: create       with no child-type declared
// ========================================================================

func familyRegistry() *Registry {
	r := NewPrimitivesRegistry()
	r.Insert(DerivedType{
		TName:      "task",
		ParentName: "create",
		ChildType:  "task-child",
	})
	r.Insert(DerivedType{
		TName:      "task-child",
		ParentName: "create",
		// no ChildType
	})
	r.Insert(DerivedType{
		TName:      "dimension",
		ParentName: "suggestion",
		ChildType:  "dimension-child",
	})
	r.Insert(DerivedType{
		TName:      "dimension-child",
		ParentName: "create",
	})
	r.Insert(DerivedType{
		TName:      "note",
		ParentName: "create",
	})
	return r
}

// ========================================================================
// ChildTypeOf
// ========================================================================

func TestChildTypeOf_TaskHasChild(t *testing.T) {
	r := familyRegistry()
	ct, ok := r.ChildTypeOf("task")
	if !ok {
		t.Fatal("expected task to have child type")
	}
	if ct != "task-child" {
		t.Errorf("expected task-child, got %s", ct)
	}
}

func TestChildTypeOf_DimensionHasChild(t *testing.T) {
	r := familyRegistry()
	ct, ok := r.ChildTypeOf("dimension")
	if !ok {
		t.Fatal("expected dimension to have child type")
	}
	if ct != "dimension-child" {
		t.Errorf("expected dimension-child, got %s", ct)
	}
}

func TestChildTypeOf_NoteHasNoChild(t *testing.T) {
	r := familyRegistry()
	_, ok := r.ChildTypeOf("note")
	if ok {
		t.Error("expected note to have no child type")
	}
}

func TestChildTypeOf_TaskChildHasNoChild(t *testing.T) {
	// task-child has no declared child-type, and its parent (create)
	// is a primitive with no child-type. The walk should return
	// ("", false), not recurse infinitely.
	r := familyRegistry()
	_, ok := r.ChildTypeOf("task-child")
	if ok {
		t.Error("expected task-child to have no child type")
	}
}

func TestChildTypeOf_Inheritance(t *testing.T) {
	// A subtype of task that does NOT declare its own child-type
	// should inherit task's child-type via the walk.
	r := familyRegistry()
	r.Insert(DerivedType{
		TName:      "urgent-task",
		ParentName: "task",
		// no ChildType — should inherit from task
	})
	ct, ok := r.ChildTypeOf("urgent-task")
	if !ok {
		t.Fatal("expected urgent-task to inherit task's child type")
	}
	if ct != "task-child" {
		t.Errorf("expected task-child (inherited), got %s", ct)
	}
}

func TestChildTypeOf_Override(t *testing.T) {
	// A subtype that DOES declare its own child-type should override
	// its parent's.
	r := familyRegistry()
	r.Insert(DerivedType{
		TName:      "special-task",
		ParentName: "task",
		ChildType:  "special-task-child",
	})
	ct, ok := r.ChildTypeOf("special-task")
	if !ok {
		t.Fatal("expected special-task to have child type")
	}
	if ct != "special-task-child" {
		t.Errorf("expected special-task-child, got %s", ct)
	}
}

func TestChildTypeOf_UnknownType(t *testing.T) {
	r := familyRegistry()
	_, ok := r.ChildTypeOf("does-not-exist")
	if ok {
		t.Error("expected unknown type to return false")
	}
}

// ========================================================================
// SubtypesOf
// ========================================================================

func TestSubtypesOf_CreateIncludesExpectedSet(t *testing.T) {
	r := familyRegistry()
	got := r.SubtypesOf("create")

	// Expected: create (reflexive), task, task-child, dimension-child, note.
	// NOT: dimension (which extends suggestion).
	gotSet := make(map[TypeName]bool)
	for _, n := range got {
		gotSet[n] = true
	}
	expected := []TypeName{"create", "task", "task-child", "dimension-child", "note"}
	for _, e := range expected {
		if !gotSet[e] {
			t.Errorf("SubtypesOf(create) missing %s", e)
		}
	}
	if gotSet["dimension"] {
		t.Error("SubtypesOf(create) should not include dimension (<: suggestion)")
	}
}

func TestSubtypesOf_SuggestionIncludesDimension(t *testing.T) {
	r := familyRegistry()
	got := r.SubtypesOf("suggestion")
	gotSet := make(map[TypeName]bool)
	for _, n := range got {
		gotSet[n] = true
	}
	if !gotSet["dimension"] {
		t.Error("SubtypesOf(suggestion) missing dimension")
	}
	if gotSet["task"] {
		t.Error("SubtypesOf(suggestion) should not include task (<: create)")
	}
}

func TestSubtypesOf_TaskIncludesOnlyTaskSubtypes(t *testing.T) {
	// With just the family registry, SubtypesOf(task) should be {task}.
	// After inserting urgent-task extending task, should be {task, urgent-task}.
	r := familyRegistry()
	r.Insert(DerivedType{TName: "urgent-task", ParentName: "task"})
	got := r.SubtypesOf("task")
	gotSet := make(map[TypeName]bool)
	for _, n := range got {
		gotSet[n] = true
	}
	if !gotSet["task"] {
		t.Error("SubtypesOf(task) missing task (reflexive)")
	}
	if !gotSet["urgent-task"] {
		t.Error("SubtypesOf(task) missing urgent-task")
	}
	if gotSet["task-child"] {
		t.Error("SubtypesOf(task) should not include task-child (extends create, not task)")
	}
}
