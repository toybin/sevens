package function

import (
	"context"

	"sevens/internal/backend"
)

// LLMBackend adapts a backend.Backend to the TransformBackend interface.
type LLMBackend struct {
	inner backend.Backend
}

// NewLLMBackend wraps an existing backend.Backend.
func NewLLMBackend(b backend.Backend) *LLMBackend {
	return &LLMBackend{inner: b}
}

// Execute converts the RenderedPrompt to an InferenceRequest, calls the
// inner backend, and wraps the raw text response in a TransformResult.
func (lb *LLMBackend) Execute(ctx context.Context, prompt RenderedPrompt) (TransformResult, error) {
	req := backend.InferenceRequest{
		SystemPrompt: prompt.System,
		Prompt:       prompt.User,
		Model:        prompt.Model,
	}

	raw, err := lb.inner.Complete(ctx, req)
	if err != nil {
		return TransformResult{}, err
	}

	return TransformResult{
		Raw:    raw,
		IsText: true,
	}, nil
}

// Name returns the inner backend's name.
func (lb *LLMBackend) Name() string {
	return lb.inner.Name()
}
