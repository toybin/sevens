package function

import "context"

// AgentBackend is a TransformBackend for agent mode. Instead of calling an LLM,
// it captures the rendered prompt for external consumption (via `sevens prepare`)
// and returns an empty result so the pipeline suspends at the gate.
type AgentBackend struct {
	// PreparedPrompt is set after Execute, for the caller to retrieve and display.
	PreparedPrompt RenderedPrompt
}

// Execute captures the prompt and returns an empty result.
// The pipeline's CompleteStep will see the gate and transition to PhasePending,
// allowing an external agent to submit a result later via `sevens submit`.
func (ab *AgentBackend) Execute(_ context.Context, prompt RenderedPrompt) (TransformResult, error) {
	ab.PreparedPrompt = prompt
	return TransformResult{Raw: "", IsText: true}, nil
}

// Name returns the backend name.
func (ab *AgentBackend) Name() string { return "agent" }
