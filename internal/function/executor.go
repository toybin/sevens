package function

import (
	"context"
	"fmt"
	"time"

	"sevens/internal/kb"
	"sevens/internal/types"
	"sevens/internal/types/kernel"
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
	if e.Backend != nil {
		p.BackendName = e.Backend.Name()
	}

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

	e.logEvent(ctx, "accepted", root, p)

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

	e.logEvent(ctx, "rejected", p.Root, p)

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

	e.logEvent(ctx, "revised", root, p)

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

	e.logEvent(ctx, "cancelled", p.Root, p)

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
	allTypes, _ := types.LoadAllTypeDefs()
	return e.executeStep(ctx, root, fn, p, step, allTypes)
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

	e.logEvent(ctx, "loop-ended", root, p)

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

// logEvent records a pipeline transition to the KB log.
func (e *Executor) logEvent(ctx context.Context, event, root string, p *Pipeline) {
	_ = e.KB.AppendLog(ctx, kb.LogEntry{
		Event:     event,
		Root:      root,
		Function:  p.FunctionName,
		Node:      p.Target,
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// resolveSchemaInstruction composes the schema instruction for a step by
// walking the type chain. If the step declares an output-type, that type's
// definition is loaded and composed with its primitive ancestor. Otherwise,
// the step's OutputShape maps to a primitive type name and that primitive's
// schema instruction is used directly.
//
// allTypes is a pre-loaded map of type definitions to avoid repeated disk/DB
// lookups on every step.
func (e *Executor) resolveSchemaInstruction(step Step, allTypes map[string]*types.TypeDef) string {
	if allTypes == nil {
		return ""
	}

	// If the step declares an output type, use its composed instruction.
	if step.Output.TypeName != "" {
		if td, ok := allTypes[step.Output.TypeName]; ok {
			return types.ComposeSchemaInstruction(td, allTypes)
		}
	}

	// Fall back to the primitive type for this shape.
	primName := PrimitiveTypeName(step.Output.Shape)
	if td, ok := allTypes[primName]; ok {
		return types.ComposeSchemaInstruction(td, allTypes)
	}

	return ""
}

// runFromCurrent runs steps starting from p.CurrentStep until gate or completion.
func (e *Executor) runFromCurrent(ctx context.Context, root string, fn *Function, p *Pipeline) (*ApplyResult, error) {
	steps := fn.EffectiveSteps()

	// Load type definitions once for the entire run.
	allTypes, _ := types.LoadAllTypeDefs()

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
		result, err := e.executeStep(ctx, root, fn, p, step, allTypes)
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
// allTypes is a pre-loaded map of type definitions (may be nil).
func (e *Executor) executeStep(ctx context.Context, root string, fn *Function, p *Pipeline, step Step, allTypes map[string]*types.TypeDef) (*ApplyResult, error) {
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

	// Phase 1: if this step declares an OutputPicker, evaluate it
	// BEFORE the LLM to resolve the effective output type. The
	// picker's decision drives both the schema instruction injected
	// into the system prompt and the type the response is validated
	// against — so the LLM is told exactly one shape to produce.
	//
	// A picker is pure data loaded from the function's EDN config,
	// not a Go-side hook. Every function that needs conditional
	// output declares its picker the same way; the executor has no
	// knowledge of which function is running.
	var routedType kernel.TypeName
	if step.OutputPicker != nil {
		resolved, rerr := resolveStepPicker(ctx, e.KB, root, p.Target, prevOutput, step, p)
		if rerr != nil {
			return nil, fmt.Errorf(
				"resolving output picker for step %q in function %q: %w",
				step.Name, fn.Name, rerr)
		}
		routedType = resolved
	}

	// Select backend based on step configuration
	var be TransformBackend
	var prompt RenderedPrompt

	switch step.Backend.Kind {
	case BackendDeterministic:
		be = &DeterministicBackend{}
		// For deterministic: System = JSON config, User = rendered content
		promptText := RenderPrompt(step.Backend.PromptTemplate, rc)
		prompt = RenderedPrompt{
			System: step.Backend.Handler,
			User:   promptText,
		}
	default:
		be = e.Backend
		promptText := RenderPrompt(step.Backend.PromptTemplate, rc)
		// Inject output schema instruction into system prompt.
		// If the picker resolved a specific primitive, look up THAT
		// primitive's legacy schema so the LLM is told exactly which
		// JSON shape to produce. Otherwise fall back to the step's
		// declared type (the static path).
		systemPrompt := step.Backend.SystemPrompt
		var schema string
		if routedType != "" {
			if allTypes != nil {
				if td, ok := allTypes[string(routedType)]; ok {
					schema = types.ComposeSchemaInstruction(td, allTypes)
				}
			}
		}
		if schema == "" {
			schema = e.resolveSchemaInstruction(step, allTypes)
		}
		if schema != "" {
			if systemPrompt != "" {
				systemPrompt += "\n\n"
			}
			systemPrompt += schema
		}
		// Prepend persona to system prompt if defined.
		if step.Backend.Persona != "" {
			systemPrompt = "Persona: " + step.Backend.Persona + "\n\n" + systemPrompt
		}
		prompt = RenderedPrompt{
			System: systemPrompt,
			User:   promptText,
		}
	}

	// Call backend
	result, err := be.Execute(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("backend execution for step %q: %w", step.Name, err)
	}

	// Parse output using universal JSON parser.
	env, _ := ParseOutput(result.Raw, step.Output.Shape)
	if env != nil {
		switch {
		case len(env.Ops) > 0:
			// Validate ops against the primitive schema BEFORE they flow
			// downstream. This rejects malformed edits (empty file,
			// missing old_text, etc.) loudly at the parse boundary
			// instead of silently slug-falling-back to untitled.md.
			//
			// If a router resolved a specific output type, enforce
			// that every op matches that type — a discuss call that
			// was routed to "edit" must not emit create ops.
			var verr error
			if routedType != "" {
				verr = ValidateOpsAgainst(env.Ops, routedType)
			} else {
				verr = ValidateOps(env.Ops)
			}
			if verr != nil {
				return nil, fmt.Errorf(
					"step %q: LLM produced invalid %s output: %w",
					step.Name, shapeLabel(step.Output.Shape), verr)
			}
			result.Ops = env.Ops
			result.IsText = false
		case len(env.Suggestions) > 0:
			result.Suggestions = env.Suggestions
			result.IsText = false
		case env.Text != "":
			result.Raw = env.Text
			result.IsText = true
		default:
			result.IsText = step.Output.Shape == ShapeText
		}
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
