package function

import (
	"context"
	"fmt"
	"time"

	"sevens/internal/kb"
)

// Executor orchestrates pipeline execution using the state machine,
// context resolution, and a transform backend.
type Executor struct {
	KB      *kb.KB
	Backend TransformBackend
	Store   *PipelineStore
}

// NewExecutor creates an Executor with the given dependencies.
func NewExecutor(k *kb.KB, backend TransformBackend, store *PipelineStore) *Executor {
	return &Executor{
		KB:      k,
		Backend: backend,
		Store:   store,
	}
}

// ApplyResult is what Apply returns to the caller.
type ApplyResult struct {
	Pipeline *Pipeline
	Result   *TransformResult
	// Suspended is true if the pipeline stopped at a gate.
	Suspended bool
}

// Apply starts a new pipeline for the given function and target node.
// Runs steps until a gate suspends or the pipeline completes.
func (e *Executor) Apply(ctx context.Context, root string, fn *Function, target string) (*ApplyResult, error) {
	p := NewPipeline(root, fn.Name, target)

	return e.runFromCurrent(ctx, root, fn, p)
}

// Accept accepts the current pending result and advances the pipeline.
// If there are more steps, runs them until the next gate or completion.
func (e *Executor) Accept(ctx context.Context, root string, fn *Function, pipelineID string) (*ApplyResult, error) {
	p, err := e.Store.Load(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("loading pipeline: %w", err)
	}

	if err := p.Accept(); err != nil {
		return nil, err
	}

	steps := fn.EffectiveSteps()
	completed, err := p.Advance(len(steps))
	if err != nil {
		return nil, err
	}

	if completed {
		if err := e.Store.Save(ctx, p); err != nil {
			return nil, err
		}
		// Return the last result from PriorStepResults
		var lastResult *TransformResult
		if len(p.PriorStepResults) > 0 {
			last := p.PriorStepResults[len(p.PriorStepResults)-1]
			lastResult = &last
		}
		return &ApplyResult{Pipeline: p, Result: lastResult, Suspended: false}, nil
	}

	// More steps to run
	return e.runFromCurrent(ctx, root, fn, p)
}

// Reject rejects the current pending result. Terminal.
func (e *Executor) Reject(ctx context.Context, pipelineID string) (*Pipeline, error) {
	p, err := e.Store.Load(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("loading pipeline: %w", err)
	}

	if err := p.Reject(); err != nil {
		return nil, err
	}

	if err := e.Store.Save(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

// Revise re-executes the current step with feedback, keeping the pipeline Pending.
func (e *Executor) Revise(ctx context.Context, root string, fn *Function, pipelineID, feedback string) (*ApplyResult, error) {
	p, err := e.Store.Load(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("loading pipeline: %w", err)
	}

	steps := fn.EffectiveSteps()
	if p.CurrentStep >= len(steps) {
		return nil, fmt.Errorf("step index %d out of range", p.CurrentStep)
	}
	step := steps[p.CurrentStep]

	// Re-run the backend with feedback appended
	rc, err := ResolveContext(ctx, e.KB, root, p.Target, step, "")
	if err != nil {
		return nil, fmt.Errorf("resolving context for revise: %w", err)
	}

	promptText := RenderPrompt(step.Backend.PromptTemplate, rc)
	// Append revision history and feedback
	history := p.RevisionHistory(step.Gate.HistoryPolicy)
	if len(history) > 0 {
		promptText += "\n\n<revision-history>\n"
		for _, entry := range history {
			promptText += fmt.Sprintf("Previous attempt: %s\nFeedback: %s\n\n", entry.Attempt.Raw, entry.Feedback)
		}
		promptText += "</revision-history>\n"
	}
	promptText += fmt.Sprintf("\n<feedback>%s</feedback>\n", feedback)

	prompt := RenderedPrompt{
		System: step.Backend.SystemPrompt,
		User:   promptText,
		Model:  step.Backend.Persona,
	}

	result, err := e.Backend.Execute(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("backend execution during revise: %w", err)
	}

	if err := p.Revise(step, feedback, result); err != nil {
		return nil, err
	}

	if err := e.Store.Save(ctx, p); err != nil {
		return nil, err
	}

	return &ApplyResult{Pipeline: p, Result: &result, Suspended: true}, nil
}

// Cancel cancels a pending or looping pipeline. Terminal.
func (e *Executor) Cancel(ctx context.Context, fn *Function, pipelineID string) (*Pipeline, error) {
	p, err := e.Store.Load(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("loading pipeline: %w", err)
	}

	steps := fn.EffectiveSteps()
	if p.CurrentStep >= len(steps) {
		return nil, fmt.Errorf("step index %d out of range", p.CurrentStep)
	}

	if err := p.Cancel(steps[p.CurrentStep]); err != nil {
		return nil, err
	}

	if err := e.Store.Save(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

// ContinueLoop continues a looping step.
func (e *Executor) ContinueLoop(ctx context.Context, root string, fn *Function, pipelineID string) (*ApplyResult, error) {
	p, err := e.Store.Load(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("loading pipeline: %w", err)
	}

	steps := fn.EffectiveSteps()
	if p.CurrentStep >= len(steps) {
		return nil, fmt.Errorf("step index %d out of range", p.CurrentStep)
	}
	step := steps[p.CurrentStep]

	if err := p.ContinueLoop(step); err != nil {
		return nil, err
	}

	// Run the step again
	return e.executeStep(ctx, root, fn, p, step)
}

// EndLoop ends a looping step, advancing to Accepted.
func (e *Executor) EndLoop(ctx context.Context, root string, fn *Function, pipelineID string) (*ApplyResult, error) {
	p, err := e.Store.Load(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("loading pipeline: %w", err)
	}

	if err := p.EndLoop(); err != nil {
		return nil, err
	}

	// Now in Accepted phase; advance to next step or complete
	steps := fn.EffectiveSteps()
	completed, err := p.Advance(len(steps))
	if err != nil {
		return nil, err
	}

	if completed {
		if err := e.Store.Save(ctx, p); err != nil {
			return nil, err
		}
		var lastResult *TransformResult
		if len(p.PriorStepResults) > 0 {
			last := p.PriorStepResults[len(p.PriorStepResults)-1]
			lastResult = &last
		}
		return &ApplyResult{Pipeline: p, Result: lastResult, Suspended: false}, nil
	}

	return e.runFromCurrent(ctx, root, fn, p)
}

// runFromCurrent runs steps starting from p.CurrentStep until gate or completion.
func (e *Executor) runFromCurrent(ctx context.Context, root string, fn *Function, p *Pipeline) (*ApplyResult, error) {
	steps := fn.EffectiveSteps()

	for p.CurrentStep < len(steps) && !p.Phase.IsTerminal() {
		if p.Phase != PhaseRunning {
			// Suspended at a gate or looping
			if err := e.Store.Save(ctx, p); err != nil {
				return nil, err
			}
			return &ApplyResult{
				Pipeline:  p,
				Result:    p.CurrentResult,
				Suspended: true,
			}, nil
		}

		step := steps[p.CurrentStep]
		result, err := e.executeStep(ctx, root, fn, p, step)
		if err != nil {
			return nil, err
		}

		// executeStep already called CompleteStep on the pipeline.
		// Check if we suspended.
		if p.Phase == PhasePending || p.Phase == PhaseLooping {
			if err := e.Store.Save(ctx, p); err != nil {
				return nil, err
			}
			return result, nil
		}

		// Phase is Accepted (auto-accept or ungated). Advance.
		if p.Phase == PhaseAccepted {
			completed, err := p.Advance(len(steps))
			if err != nil {
				return nil, err
			}
			if completed {
				if err := e.Store.Save(ctx, p); err != nil {
					return nil, err
				}
				var lastResult *TransformResult
				if len(p.PriorStepResults) > 0 {
					last := p.PriorStepResults[len(p.PriorStepResults)-1]
					lastResult = &last
				}
				return &ApplyResult{Pipeline: p, Result: lastResult, Suspended: false}, nil
			}
			// Continue to next step
		}
	}

	// Should not reach here normally
	if err := e.Store.Save(ctx, p); err != nil {
		return nil, err
	}
	return &ApplyResult{Pipeline: p, Result: p.CurrentResult, Suspended: false}, nil
}

// executeStep runs a single step: resolve context, render prompt, call backend, complete.
func (e *Executor) executeStep(ctx context.Context, root string, fn *Function, p *Pipeline, step Step) (*ApplyResult, error) {
	// Gather previous step output
	prevOutput := ""
	if len(p.PriorStepResults) > 0 {
		prevOutput = p.PriorStepResults[len(p.PriorStepResults)-1].Raw
	}

	// Resolve context
	rc, err := ResolveContext(ctx, e.KB, root, p.Target, step, prevOutput)
	if err != nil {
		return nil, fmt.Errorf("resolving context for step %q: %w", step.Name, err)
	}

	// Render prompt
	promptText := RenderPrompt(step.Backend.PromptTemplate, rc)

	prompt := RenderedPrompt{
		System: step.Backend.SystemPrompt,
		User:   promptText,
	}

	// Call backend
	result, err := e.Backend.Execute(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("backend execution for step %q: %w", step.Name, err)
	}

	// Parse output based on step's declared shape
	switch step.Output.Shape {
	case ShapeText:
		result.IsText = true
	case ShapeFileOps:
		ops, parseErr := ParseOps(result.Raw)
		if parseErr == nil {
			result.Ops = ops
			result.IsText = false
		}
	case ShapeStructured:
		result.IsText = false
	}

	// Complete the step (state transition)
	if err := p.CompleteStep(step, result); err != nil {
		return nil, fmt.Errorf("completing step %q: %w", step.Name, err)
	}

	// Log to KB
	_ = e.KB.AppendLog(ctx, kb.LogEntry{
		Event:     "step-completed",
		Root:      root,
		Function:  fn.Name,
		Node:      p.Target,
		Step:      step.Name,
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		Result:    result.Raw,
	})

	return &ApplyResult{
		Pipeline:  p,
		Result:    &result,
		Suspended: p.Phase == PhasePending || p.Phase == PhaseLooping,
	}, nil
}
