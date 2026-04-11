package function

import "context"

// TransformBackend is the abstraction over execution mechanisms.
// Defined here (where it's used), not in the backend implementations.
type TransformBackend interface {
	Execute(ctx context.Context, prompt RenderedPrompt) (TransformResult, error)
	Name() string
}

// RenderedPrompt is a fully rendered prompt ready for a backend.
type RenderedPrompt struct {
	System string
	User   string
	Model  string
}

// TransformResult is what a backend produces.
type TransformResult struct {
	Raw    string
	Ops    []FileOp
	IsText bool // true if display-only
}

// RevisionEntry is one (attempt, feedback) pair.
type RevisionEntry struct {
	Attempt  TransformResult
	Feedback string
}
