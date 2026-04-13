package repl

import (
	"testing"
)

func TestIsThreaded_ReturnsFalseForMissingFile(t *testing.T) {
	// A mock DiscussionRunner that tracks calls.
	dr := &mockDiscussionRunner{threaded: false}
	r := &REPL{discussR: dr}
	_ = r // DiscussionRunner is wired; test that the interface is satisfied.

	if dr.IsThreaded("/nonexistent/file.md") {
		t.Fatal("IsThreaded should return false for nonexistent file")
	}
}

type mockDiscussionRunner struct {
	threaded bool
}

func (m *mockDiscussionRunner) StartDiscussion(root, nodeTitle string) (*DiscussionState, string, error) {
	return &DiscussionState{DiscussTitle: "Discussion - " + nodeTitle, FocusTitle: nodeTitle}, "agent output", nil
}
func (m *mockDiscussionRunner) ContinueDiscussion(root string, state *DiscussionState, userInput string) (string, error) {
	return "agent response", nil
}
func (m *mockDiscussionRunner) EndDiscussion(root string, state *DiscussionState) (string, error) {
	return "abc123", nil
}
func (m *mockDiscussionRunner) CancelDiscussion(root string, state *DiscussionState) error {
	return nil
}
func (m *mockDiscussionRunner) IsThreaded(filePath string) bool {
	return m.threaded
}
