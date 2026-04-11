package engine

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "turso.tech/database/tursogo"

	"sevens/internal/apply"
	"sevens/internal/graph"
	"sevens/internal/store"
)

func testDB(t *testing.T) *sql.DB {
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

func insertTestNode(t *testing.T, db *sql.DB, root, title, content string) {
	t.Helper()
	subj := store.NodeSubject(root, title)
	if err := store.InsertTriples(db, []store.Triple{
		{Subject: subj, Predicate: "node/title", Object: title},
		{Subject: subj, Predicate: "node/root", Object: root},
		{Subject: subj, Predicate: "node/content", Object: content},
	}); err != nil {
		t.Fatalf("insertTestNode: %v", err)
	}
}

// insertSuspension inserts a full set of suspension triples for the given subject/target.
func insertSuspension(t *testing.T, db *sql.DB, subject, target, status string, roots ...string) {
	t.Helper()
	triples := []store.Triple{
		{Subject: subject, Predicate: "suspension/target", Object: target},
		{Subject: subject, Predicate: "suspension/function", Object: "decompose"},
		{Subject: subject, Predicate: "suspension/step", Object: "suggest"},
		{Subject: subject, Predicate: "suspension/step-index", Object: "0"},
		{Subject: subject, Predicate: "suspension/gate", Object: "approve"},
		{Subject: subject, Predicate: "suspension/output-type", Object: "suggestions"},
		{Subject: subject, Predicate: "suspension/raw-output", Object: `[{"title":"Test"}]`},
		{Subject: subject, Predicate: "suspension/status", Object: status},
		{Subject: subject, Predicate: "suspension/timestamp", Object: "2026-04-08T12:00:00Z"},
	}
	if len(roots) > 0 && roots[0] != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "suspension/root", Object: roots[0]})
	}
	err := store.InsertTriples(db, triples)
	if err != nil {
		t.Fatalf("insertSuspension: %v", err)
	}
}

// --- FindSuspension tests ---

func TestFindSuspension_NoneExists(t *testing.T) {
	db := testDB(t)

	sus, subj, err := FindSuspension(db, "NoSuchNode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sus != nil {
		t.Errorf("expected nil suspension, got %+v", sus)
	}
	if subj != "" {
		t.Errorf("expected empty subject, got %q", subj)
	}
}

func TestFindSuspension_SinglePending(t *testing.T) {
	db := testDB(t)

	subject := "suspension:TestNode:20260408T120000"
	insertSuspension(t, db, subject, "TestNode", "pending")

	sus, subj, err := FindSuspension(db, "TestNode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sus == nil {
		t.Fatal("expected a suspension, got nil")
	}
	if subj != subject {
		t.Errorf("expected subject %q, got %q", subject, subj)
	}
	if sus.Target != "TestNode" {
		t.Errorf("expected target %q, got %q", "TestNode", sus.Target)
	}
	if sus.Function != "decompose" {
		t.Errorf("expected function %q, got %q", "decompose", sus.Function)
	}
	if sus.StepName != "suggest" {
		t.Errorf("expected step %q, got %q", "suggest", sus.StepName)
	}
	if sus.StepIndex != 0 {
		t.Errorf("expected step-index 0, got %d", sus.StepIndex)
	}
	if sus.GateType != "approve" {
		t.Errorf("expected gate %q, got %q", "approve", sus.GateType)
	}
	if sus.OutputType != "suggestions" {
		t.Errorf("expected output-type %q, got %q", "suggestions", sus.OutputType)
	}
}

func TestWriteSuspension_PreservesBlockTargetMetadata(t *testing.T) {
	db := testDB(t)

	block := &graph.BlockTarget{
		Subject:   "block:root:Node:1.0",
		NodeTitle: "Node",
		Path:      "1.0",
	}
	WriteSuspension(db, "root", "Node", block.Label(), block, "notice", "default", "approve", "text", "hello", 0, "summary", nil, "codex")

	sus, subj, err := FindSuspension(db, "root", "Node")
	if err != nil {
		t.Fatalf("FindSuspension returned error: %v", err)
	}
	if sus == nil {
		t.Fatal("expected suspension, got nil")
	}
	if subj == "" {
		t.Fatal("expected subject")
	}
	if sus.TargetLabel != "Node#1.0" {
		t.Fatalf("TargetLabel = %q, want %q", sus.TargetLabel, "Node#1.0")
	}
	if sus.BlockID != block.Subject {
		t.Fatalf("BlockID = %q, want %q", sus.BlockID, block.Subject)
	}
	if sus.BlockPath != "1.0" {
		t.Fatalf("BlockPath = %q, want %q", sus.BlockPath, "1.0")
	}
}

func TestRunPipeline_DryRunDoesNotWriteLogOrSuspension(t *testing.T) {
	db := testDB(t)
	root := t.TempDir()
	insertTestNode(t, db, root, "Node", "# Node\n\nBody")

	walk, err := graph.BuildWalk(db, root, "Node", 1)
	if err != nil {
		t.Fatalf("BuildWalk: %v", err)
	}

	fn := &apply.Function{
		Name:   "notice",
		Prompt: "Title: {{title}}",
		Input:  "node",
		Output: "text",
	}

	result := RunPipeline(context.Background(), PipelineConfig{
		DB:           db,
		Root:         root,
		NodeTitle:    "Node",
		Function:     fn,
		GlobalConfig: apply.GlobalConfig{},
		Walk:         walk,
		DryRun:       true,
	}, 0, "")

	if result.IsLeft() {
		t.Fatalf("expected dry-run to return StepResult, got suspension")
	}
	entries, err := apply.ReadLogDB(db, root, "Node")
	if err != nil {
		t.Fatalf("ReadLogDB: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no log entries, got %#v", entries)
	}
	sus, _, err := FindSuspension(db, root, "Node")
	if err != nil {
		t.Fatalf("FindSuspension: %v", err)
	}
	if sus != nil {
		t.Fatalf("expected no suspension, got %#v", sus)
	}
}

func TestFindSuspension_ResolvedIgnored(t *testing.T) {
	db := testDB(t)

	// Insert one accepted (resolved) suspension — should be ignored.
	insertSuspension(t, db, "suspension:TestNode:20260408T110000", "TestNode", "accepted")

	sus, subj, err := FindSuspension(db, "TestNode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sus != nil {
		t.Errorf("expected nil for resolved suspension, got %+v (subj=%q)", sus, subj)
	}
}

func TestFindSuspension_MultipleReturnsMostRecentPending(t *testing.T) {
	db := testDB(t)

	// Older resolved suspension
	insertSuspension(t, db, "suspension:TestNode:20260408T100000", "TestNode", "rejected")

	// Older pending suspension (should NOT be returned — less recent)
	err := store.InsertTriples(db, []store.Triple{
		{Subject: "suspension:TestNode:20260408T110000", Predicate: "suspension/target", Object: "TestNode"},
		{Subject: "suspension:TestNode:20260408T110000", Predicate: "suspension/function", Object: "decompose"},
		{Subject: "suspension:TestNode:20260408T110000", Predicate: "suspension/step", Object: "suggest"},
		{Subject: "suspension:TestNode:20260408T110000", Predicate: "suspension/step-index", Object: "0"},
		{Subject: "suspension:TestNode:20260408T110000", Predicate: "suspension/gate", Object: "approve"},
		{Subject: "suspension:TestNode:20260408T110000", Predicate: "suspension/output-type", Object: "suggestions"},
		{Subject: "suspension:TestNode:20260408T110000", Predicate: "suspension/raw-output", Object: `[{"title":"Old"}]`},
		{Subject: "suspension:TestNode:20260408T110000", Predicate: "suspension/status", Object: "pending"},
		{Subject: "suspension:TestNode:20260408T110000", Predicate: "suspension/timestamp", Object: "2026-04-08T11:00:00Z"},
	})
	if err != nil {
		t.Fatalf("insert older pending: %v", err)
	}

	// Most recent pending suspension — this one should be returned.
	err = store.InsertTriples(db, []store.Triple{
		{Subject: "suspension:TestNode:20260408T120000", Predicate: "suspension/target", Object: "TestNode"},
		{Subject: "suspension:TestNode:20260408T120000", Predicate: "suspension/function", Object: "decompose"},
		{Subject: "suspension:TestNode:20260408T120000", Predicate: "suspension/step", Object: "suggest"},
		{Subject: "suspension:TestNode:20260408T120000", Predicate: "suspension/step-index", Object: "0"},
		{Subject: "suspension:TestNode:20260408T120000", Predicate: "suspension/gate", Object: "approve"},
		{Subject: "suspension:TestNode:20260408T120000", Predicate: "suspension/output-type", Object: "suggestions"},
		{Subject: "suspension:TestNode:20260408T120000", Predicate: "suspension/raw-output", Object: `[{"title":"Test"}]`},
		{Subject: "suspension:TestNode:20260408T120000", Predicate: "suspension/status", Object: "pending"},
		{Subject: "suspension:TestNode:20260408T120000", Predicate: "suspension/timestamp", Object: "2026-04-08T12:00:00Z"},
	})
	if err != nil {
		t.Fatalf("insert newer pending: %v", err)
	}

	sus, subj, err := FindSuspension(db, "TestNode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sus == nil {
		t.Fatal("expected a suspension, got nil")
	}
	wantSubj := "suspension:TestNode:20260408T120000"
	if subj != wantSubj {
		t.Errorf("expected most-recent subject %q, got %q", wantSubj, subj)
	}
}

// --- ResolveSuspension tests ---

func TestResolveSuspension_Accepted(t *testing.T) {
	db := testDB(t)

	subject := "suspension:TestNode:20260408T120000"
	insertSuspension(t, db, subject, "TestNode", "pending")

	if err := ResolveSuspension(db, subject, "accepted"); err != nil {
		t.Fatalf("ResolveSuspension accepted: %v", err)
	}

	status, err := store.GetObject(db, subject, "suspension/status")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if status != "accepted" {
		t.Errorf("expected status %q, got %q", "accepted", status)
	}

	// The suspension should no longer appear as pending.
	sus, _, err := FindSuspension(db, "TestNode")
	if err != nil {
		t.Fatalf("FindSuspension: %v", err)
	}
	if sus != nil {
		t.Errorf("expected no pending suspension after resolution, got %+v", sus)
	}
}

func TestResolveSuspension_Rejected(t *testing.T) {
	db := testDB(t)

	subject := "suspension:TestNode:20260408T120000"
	insertSuspension(t, db, subject, "TestNode", "pending")

	if err := ResolveSuspension(db, subject, "rejected"); err != nil {
		t.Fatalf("ResolveSuspension rejected: %v", err)
	}

	status, err := store.GetObject(db, subject, "suspension/status")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if status != "rejected" {
		t.Errorf("expected status %q, got %q", "rejected", status)
	}
}

// --- ListSuspensions tests ---

func TestListSuspensions_Empty(t *testing.T) {
	db := testDB(t)

	results, err := ListSuspensions(db, "")
	if err != nil {
		t.Fatalf("ListSuspensions empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 suspensions, got %d", len(results))
	}
}

func TestListSuspensions_Multiple(t *testing.T) {
	db := testDB(t)

	// Insert two pending suspensions for different nodes.
	insertSuspension(t, db, "suspension:Alpha:20260408T120000", "Alpha", "pending")
	insertSuspension(t, db, "suspension:Beta:20260408T130000", "Beta", "pending")
	// Insert one resolved — should not appear.
	insertSuspension(t, db, "suspension:Gamma:20260408T140000", "Gamma", "accepted")

	results, err := ListSuspensions(db, "")
	if err != nil {
		t.Fatalf("ListSuspensions multiple: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 pending suspensions, got %d", len(results))
	}

	// Verify both targets appear.
	targets := map[string]bool{}
	for _, s := range results {
		targets[s.Target] = true
	}
	if !targets["Alpha"] {
		t.Error("expected Alpha in results")
	}
	if !targets["Beta"] {
		t.Error("expected Beta in results")
	}
}

func TestListSuspensions_ScopedByRoot(t *testing.T) {
	db := testDB(t)

	// Two nodes in different roots.
	insertSuspension(t, db, "suspension:InRoot:20260408T120000", "InRoot", "pending", "/test/root")
	insertSuspension(t, db, "suspension:OutOfRoot:20260408T130000", "OutOfRoot", "pending", "/other/root")

	results, err := ListSuspensions(db, "/test/root")
	if err != nil {
		t.Fatalf("ListSuspensions scoped: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 suspension for /test/root, got %d", len(results))
	}
	if len(results) > 0 && results[0].Target != "InRoot" {
		t.Errorf("expected target %q, got %q", "InRoot", results[0].Target)
	}
}

// --- BuildRevisionHistory tests ---

func TestBuildRevisionHistory_Empty(t *testing.T) {
	db := testDB(t)

	result := BuildRevisionHistory(db, "NoSuchNode", 0)
	if result != "" {
		t.Errorf("expected empty string for node with no log, got %q", result)
	}
}

func TestBuildRevisionHistory_WithSuggestions(t *testing.T) {
	db := testDB(t)

	// Insert a log entry via AppendLogDB.
	entry := apply.LogEntry{
		Event:     "suggested",
		Function:  "decompose",
		Target:    "TestNode",
		Step:      "suggest",
		StepIndex: 0,
		Timestamp: "2026-04-08T12:00:00Z",
		RawOutput: `[{"title":"Idea A"},{"title":"Idea B"}]`,
	}
	if err := apply.AppendLogDB(db, entry); err != nil {
		t.Fatalf("AppendLogDB: %v", err)
	}

	result := BuildRevisionHistory(db, "TestNode", 0)
	if result == "" {
		t.Fatal("expected non-empty revision history, got empty string")
	}
	if !strings.Contains(result, "<previous-attempts>") {
		t.Errorf("expected <previous-attempts> tag, got: %s", result)
	}
	if !strings.Contains(result, "</previous-attempts>") {
		t.Errorf("expected </previous-attempts> tag, got: %s", result)
	}
	if !strings.Contains(result, "<suggestion>") {
		t.Errorf("expected <suggestion> tag, got: %s", result)
	}
	if !strings.Contains(result, "Idea A") {
		t.Errorf("expected suggestion content in output, got: %s", result)
	}
}

func TestBuildRevisionHistory_WrongStepIndexIgnored(t *testing.T) {
	db := testDB(t)

	// Log entry for step 0.
	entry := apply.LogEntry{
		Event:     "suggested",
		Function:  "decompose",
		Target:    "TestNode",
		Step:      "suggest",
		StepIndex: 0,
		Timestamp: "2026-04-08T12:00:00Z",
		RawOutput: "some output",
	}
	if err := apply.AppendLogDB(db, entry); err != nil {
		t.Fatalf("AppendLogDB: %v", err)
	}

	// Query for step 1 — should return empty because no log at step 1.
	result := BuildRevisionHistory(db, "TestNode", 1)
	if result != "" {
		t.Errorf("expected empty string for wrong step index, got %q", result)
	}
}

func TestBuildRevisionHistory_SuggestionAndRevision(t *testing.T) {
	db := testDB(t)

	// First suggestion.
	suggestion := apply.LogEntry{
		Event:     "suggested",
		Function:  "decompose",
		Target:    "TestNode",
		Step:      "suggest",
		StepIndex: 0,
		Timestamp: "2026-04-08T12:00:00Z",
		RawOutput: "first suggestion",
	}
	if err := apply.AppendLogDB(db, suggestion); err != nil {
		t.Fatalf("AppendLogDB suggestion: %v", err)
	}

	// Revision note that follows.
	revision := apply.LogEntry{
		Event:     "revision",
		Target:    "TestNode",
		Note:      "please be more specific",
		Timestamp: "2026-04-08T12:01:00Z",
	}
	if err := apply.AppendLogDB(db, revision); err != nil {
		t.Fatalf("AppendLogDB revision: %v", err)
	}

	result := BuildRevisionHistory(db, "TestNode", 0)
	if result == "" {
		t.Fatal("expected non-empty history")
	}
	if !strings.Contains(result, "<revision>") {
		t.Errorf("expected <revision> tag, got: %s", result)
	}
	if !strings.Contains(result, "please be more specific") {
		t.Errorf("expected revision note content, got: %s", result)
	}
	if !strings.Contains(result, "first suggestion") {
		t.Errorf("expected suggestion content, got: %s", result)
	}
}
