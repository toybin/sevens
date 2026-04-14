package function

import (
	"context"

	"sevens/internal/types/kernel"
)

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
	Raw         string
	Ops         []FileOp
	Suggestions []Suggestion
	IsText      bool // true if display-only

	// ResolvedType is the kernel TypeName the executor committed
	// to for this step: either a picker's resolution, or the
	// primitive derived from the step's declared shape. Populated
	// by the executor in executeStep. Used by downstream pickers
	// (via StepContext.PriorOutputType) so multi-step pipelines
	// can dispatch on earlier steps' resolved types.
	ResolvedType kernel.TypeName
}

// RevisionEntry is one (attempt, feedback) pair.
type RevisionEntry struct {
	Attempt  TransformResult
	Feedback string
}
