package function

import (
	"crypto/rand"
	"crypto/sha1"
	"errors"
	"fmt"
	"time"
)

// PipelinePhase is the current state of a pipeline.
type PipelinePhase int

const (
	PhaseRunning   PipelinePhase = iota
	PhasePending
	PhaseAccepted
	PhaseLooping
	PhaseRejected
	PhaseCancelled
	PhaseCompleted
)

func (p PipelinePhase) String() string {
	switch p {
	case PhaseRunning:
		return "Running"
	case PhasePending:
		return "Pending"
	case PhaseAccepted:
		return "Accepted"
	case PhaseLooping:
		return "Looping"
	case PhaseRejected:
		return "Rejected"
	case PhaseCancelled:
		return "Cancelled"
	case PhaseCompleted:
		return "Completed"
	default:
		return fmt.Sprintf("Phase(%d)", int(p))
	}
}

// IsTerminal returns true if the phase is a final state.
func (p PipelinePhase) IsTerminal() bool {
	return p == PhaseRejected || p == PhaseCancelled || p == PhaseCompleted
}

// Pipeline is the live, mutable state of a curried function application.
type Pipeline struct {
	ID               string
	FunctionName     string
	Root             string
	Target           string // node title
	CurrentStep      int
	Phase            PipelinePhase
	CurrentResult    *TransformResult
	Accumulator      *TransformResult   // for looping steps
	RevisionChain    []RevisionEntry
	PriorStepResults []TransformResult
}

// Errors for invalid state transitions.
var (
	ErrNotPending  = errors.New("pipeline is not in Pending phase")
	ErrNotAccepted = errors.New("pipeline is not in Accepted phase")
	ErrNotLooping  = errors.New("pipeline is not in Looping phase")
	ErrNotRevisable = errors.New("step is not revisable")
	ErrNotCancelable = errors.New("step is not cancelable")
	ErrTerminal    = errors.New("pipeline is in a terminal phase")
)

// NewPipeline creates a pipeline in the Running phase at step 0.
func NewPipeline(root, functionName, target string) *Pipeline {
	return &Pipeline{
		ID:           generatePipelineID(root),
		FunctionName: functionName,
		Root:         root,
		Target:       target,
		CurrentStep:  0,
		Phase:        PhaseRunning,
	}
}

// --- Transition methods ---
//
// These are pure state transitions. They validate the current phase,
// apply the transition, and return an error if the transition is
// invalid. They do NOT call backends or touch the graph -- that's
// the Executor's job.

// CompleteStep is called after a backend produces a result for the
// current step. Transitions based on the step's gate and flow config.
func (p *Pipeline) CompleteStep(step Step, result TransformResult) error {
	if p.Phase != PhaseRunning {
		return fmt.Errorf("cannot complete: %w", ErrTerminal)
	}

	p.CurrentResult = &result

	// Looping step -> Looping
	if step.Flow != nil && step.Flow.Kind == FlowLoop {
		p.Phase = PhaseLooping
		return nil
	}

	// Gated step -> Pending (unless auto-accept)
	if step.Gate != nil {
		if step.Gate.AutoAccept {
			p.Phase = PhaseAccepted
		} else {
			p.Phase = PhasePending
		}
		return nil
	}

	// Ungated, not looping -> auto-advance
	p.Phase = PhaseAccepted
	return nil
}

// Accept transitions Pending -> Accepted.
func (p *Pipeline) Accept() error {
	if p.Phase != PhasePending {
		return ErrNotPending
	}
	p.Phase = PhaseAccepted
	return nil
}

// Reject transitions Pending -> Rejected.
func (p *Pipeline) Reject() error {
	if p.Phase != PhasePending {
		return ErrNotPending
	}
	p.Phase = PhaseRejected
	return nil
}

// Revise records the current result in the revision chain and
// prepares for re-execution. The caller must provide the new result
// after calling the backend. Pipeline stays in Pending.
func (p *Pipeline) Revise(step Step, feedback string, newResult TransformResult) error {
	if p.Phase != PhasePending {
		return ErrNotPending
	}
	if step.Gate == nil || !step.Gate.Revisable {
		return ErrNotRevisable
	}
	if p.CurrentResult != nil {
		p.RevisionChain = append(p.RevisionChain, RevisionEntry{
			Attempt:  *p.CurrentResult,
			Feedback: feedback,
		})
	}
	p.CurrentResult = &newResult
	// Phase stays Pending
	return nil
}

// Advance transitions Accepted -> Running at the next step,
// or -> Completed if this was the last step.
// Returns true if the pipeline completed.
func (p *Pipeline) Advance(totalSteps int) (completed bool, err error) {
	if p.Phase != PhaseAccepted {
		return false, ErrNotAccepted
	}

	// Store accepted result
	if p.CurrentResult != nil {
		p.PriorStepResults = append(p.PriorStepResults, *p.CurrentResult)
	}

	p.CurrentStep++
	p.CurrentResult = nil
	p.RevisionChain = nil

	if p.CurrentStep >= totalSteps {
		p.Phase = PhaseCompleted
		return true, nil
	}

	p.Phase = PhaseRunning
	return false, nil
}

// ContinueLoop transitions Looping -> Running for the next iteration.
// If accumulator policy is Append, the current result is appended.
func (p *Pipeline) ContinueLoop(step Step) error {
	if p.Phase != PhaseLooping {
		return ErrNotLooping
	}

	// Handle accumulator
	if step.Flow != nil && step.Flow.Accumulator == AccumulatorAppend {
		if p.Accumulator == nil {
			p.Accumulator = p.CurrentResult
		} else if p.CurrentResult != nil {
			// Append raw output
			combined := p.Accumulator.Raw + "\n" + p.CurrentResult.Raw
			p.Accumulator = &TransformResult{
				Raw:    combined,
				IsText: p.Accumulator.IsText,
			}
		}
	} else {
		// Replace: current result IS the accumulator
		p.Accumulator = p.CurrentResult
	}

	p.Phase = PhaseRunning
	return nil
}

// EndLoop breaks out of a loop. Transitions Looping -> Accepted
// (the accumulated result becomes the step result for advancement).
func (p *Pipeline) EndLoop() error {
	if p.Phase != PhaseLooping {
		return ErrNotLooping
	}
	// The accumulator (or current result) becomes the accepted result
	if p.Accumulator != nil {
		p.CurrentResult = p.Accumulator
	}
	p.Accumulator = nil
	p.Phase = PhaseAccepted
	return nil
}

// Cancel transitions Pending or Looping -> Cancelled.
func (p *Pipeline) Cancel(step Step) error {
	if p.Phase != PhasePending && p.Phase != PhaseLooping {
		return fmt.Errorf("cannot cancel from %s", p.Phase)
	}
	if step.Gate != nil && !step.Gate.Cancelable {
		return ErrNotCancelable
	}
	p.Phase = PhaseCancelled
	return nil
}

// RevisionHistory returns the revision chain formatted according
// to the gate's history policy, for injection into a prompt.
func (p *Pipeline) RevisionHistory(policy HistoryPolicy) []RevisionEntry {
	switch policy {
	case HistoryNone:
		return nil
	case HistoryLatest:
		if len(p.RevisionChain) == 0 {
			return nil
		}
		return p.RevisionChain[len(p.RevisionChain)-1:]
	case HistoryFull:
		return p.RevisionChain
	default:
		return nil
	}
}

func generatePipelineID(root string) string {
	h := sha1.Sum([]byte(root))
	ts := time.Now().UTC().Format("20060102T150405")
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("pipeline:%x:%s:%x", h[:6], ts, b)
}
