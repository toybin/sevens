package function

import (
	"context"
	"testing"
)

func TestAgentBackend_Execute(t *testing.T) {
	ab := &AgentBackend{}

	prompt := RenderedPrompt{
		System: "You are a facilitator.",
		User:   "Analyze the node titled My Note.",
		Model:  "facilitator",
	}

	result, err := ab.Execute(context.Background(), prompt)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should return empty text result.
	if result.Raw != "" {
		t.Fatalf("expected empty Raw, got %q", result.Raw)
	}
	if !result.IsText {
		t.Fatal("expected IsText true")
	}

	// Should capture the prompt for external retrieval.
	if ab.PreparedPrompt.System != prompt.System {
		t.Fatalf("expected captured System %q, got %q", prompt.System, ab.PreparedPrompt.System)
	}
	if ab.PreparedPrompt.User != prompt.User {
		t.Fatalf("expected captured User %q, got %q", prompt.User, ab.PreparedPrompt.User)
	}
	if ab.PreparedPrompt.Model != prompt.Model {
		t.Fatalf("expected captured Model %q, got %q", prompt.Model, ab.PreparedPrompt.Model)
	}
}

func TestAgentBackend_Name(t *testing.T) {
	ab := &AgentBackend{}
	if ab.Name() != "agent" {
		t.Fatalf("expected name 'agent', got %q", ab.Name())
	}
}

func TestAgentBackend_MultipleExecutions(t *testing.T) {
	ab := &AgentBackend{}

	// First call.
	prompt1 := RenderedPrompt{User: "first prompt"}
	_, _ = ab.Execute(context.Background(), prompt1)

	// Second call should overwrite the captured prompt.
	prompt2 := RenderedPrompt{User: "second prompt"}
	_, _ = ab.Execute(context.Background(), prompt2)

	if ab.PreparedPrompt.User != "second prompt" {
		t.Fatalf("expected second prompt captured, got %q", ab.PreparedPrompt.User)
	}
}
