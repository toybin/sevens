package function

import (
	"testing"
)

// --- Helpers ---

func result(text string) TransformResult {
	return TransformResult{Raw: text, IsText: true}
}

func opsResult(text string) TransformResult {
	return TransformResult{Raw: text, Ops: []FileOp{{Action: "create", Title: "new"}}}
}

func ungatedStep() Step {
	return Step{Name: "ungated", Output: Signature{Shape: ShapeText}}
}

func gatedStep() Step {
	return Step{
		Name:   "gated",
		Output: Signature{Shape: ShapeFileOps},
		Gate:   &GateSpec{Revisable: true, Cancelable: true, HistoryPolicy: HistoryFull},
	}
}

func autoAcceptStep() Step {
	return Step{
		Name:   "auto",
		Output: Signature{Shape: ShapeFileOps},
		Gate:   &GateSpec{AutoAccept: true},
	}
}

func loopStep() Step {
	return Step{
		Name:   "loop",
		Output: Signature{Shape: ShapeText},
		Flow:   &ControlFlow{Kind: FlowLoop, Termination: TerminateUser, Accumulator: AccumulatorAppend},
		Gate:   &GateSpec{Cancelable: true},
	}
}

func nonRevisableGatedStep() Step {
	return Step{
		Name:   "gated-no-revise",
		Output: Signature{Shape: ShapeFileOps},
		Gate:   &GateSpec{Revisable: false, Cancelable: true},
	}
}

func nonCancelableGatedStep() Step {
	return Step{
		Name: "gated-no-cancel",
		Gate: &GateSpec{Revisable: true, Cancelable: false},
	}
}

// --- NewPipeline ---

func TestNewPipeline(t *testing.T) {
	p := NewPipeline("/root", "notice", "My Note")
	if p.Phase != PhaseRunning {
		t.Fatalf("expected Running, got %s", p.Phase)
	}
	if p.CurrentStep != 0 {
		t.Fatalf("expected step 0, got %d", p.CurrentStep)
	}
	if p.ID == "" {
		t.Fatal("expected non-empty ID")
	}
}

// --- CompleteStep transitions ---

func TestCompleteStepGated(t *testing.T) {
	p := NewPipeline("/root", "decompose", "Note")
	p.CompleteStep(gatedStep(), result("suggestions"))

	if p.Phase != PhasePending {
		t.Fatalf("expected Pending after gated complete, got %s", p.Phase)
	}
	if p.CurrentResult == nil || p.CurrentResult.Raw != "suggestions" {
		t.Fatal("expected result to be set")
	}
}

func TestCompleteStepAutoAccept(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(autoAcceptStep(), result("done"))

	if p.Phase != PhaseAccepted {
		t.Fatalf("expected Accepted after auto-accept, got %s", p.Phase)
	}
}

func TestCompleteStepUngated(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(ungatedStep(), result("output"))

	if p.Phase != PhaseAccepted {
		t.Fatalf("expected Accepted after ungated complete, got %s", p.Phase)
	}
}

func TestCompleteStepLoop(t *testing.T) {
	p := NewPipeline("/root", "discuss", "Note")
	p.CompleteStep(loopStep(), result("turn 1"))

	if p.Phase != PhaseLooping {
		t.Fatalf("expected Looping, got %s", p.Phase)
	}
}

// --- Accept ---

func TestAccept(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(gatedStep(), result("ok"))

	if err := p.Accept(); err != nil {
		t.Fatal(err)
	}
	if p.Phase != PhaseAccepted {
		t.Fatalf("expected Accepted, got %s", p.Phase)
	}
}

func TestAcceptWrongPhase(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	// Phase is Running, not Pending
	if err := p.Accept(); err == nil {
		t.Fatal("expected error accepting from Running")
	}
}

// --- Reject ---

func TestReject(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(gatedStep(), result("bad"))

	if err := p.Reject(); err != nil {
		t.Fatal(err)
	}
	if p.Phase != PhaseRejected {
		t.Fatalf("expected Rejected, got %s", p.Phase)
	}
	if !p.Phase.IsTerminal() {
		t.Fatal("Rejected should be terminal")
	}
}

// --- Revise ---

func TestRevise(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(gatedStep(), result("attempt 1"))

	err := p.Revise(gatedStep(), "try again", result("attempt 2"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Phase != PhasePending {
		t.Fatalf("expected Pending after revise, got %s", p.Phase)
	}
	if p.CurrentResult.Raw != "attempt 2" {
		t.Fatalf("expected 'attempt 2', got %q", p.CurrentResult.Raw)
	}
	if len(p.RevisionChain) != 1 {
		t.Fatalf("expected 1 revision entry, got %d", len(p.RevisionChain))
	}
	if p.RevisionChain[0].Attempt.Raw != "attempt 1" {
		t.Fatalf("expected revision chain to contain 'attempt 1'")
	}
	if p.RevisionChain[0].Feedback != "try again" {
		t.Fatalf("expected feedback 'try again'")
	}
}

func TestReviseMultipleTimes(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(gatedStep(), result("v1"))
	p.Revise(gatedStep(), "nope", result("v2"))
	p.Revise(gatedStep(), "still wrong", result("v3"))

	if len(p.RevisionChain) != 2 {
		t.Fatalf("expected 2 revision entries, got %d", len(p.RevisionChain))
	}
	if p.RevisionChain[0].Attempt.Raw != "v1" {
		t.Fatalf("chain[0] should be v1, got %q", p.RevisionChain[0].Attempt.Raw)
	}
	if p.RevisionChain[1].Attempt.Raw != "v2" {
		t.Fatalf("chain[1] should be v2, got %q", p.RevisionChain[1].Attempt.Raw)
	}
	if p.CurrentResult.Raw != "v3" {
		t.Fatalf("current should be v3, got %q", p.CurrentResult.Raw)
	}
}

func TestReviseNonRevisable(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(nonRevisableGatedStep(), result("ok"))

	err := p.Revise(nonRevisableGatedStep(), "feedback", result("new"))
	if err != ErrNotRevisable {
		t.Fatalf("expected ErrNotRevisable, got %v", err)
	}
}

func TestReviseWrongPhase(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	// Still Running
	err := p.Revise(gatedStep(), "feedback", result("new"))
	if err != ErrNotPending {
		t.Fatalf("expected ErrNotPending, got %v", err)
	}
}

// --- Advance ---

func TestAdvanceToNextStep(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(gatedStep(), result("step 0 output"))
	p.Accept()

	completed, err := p.Advance(3) // 3 total steps
	if err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Fatal("should not be completed with 2 more steps")
	}
	if p.Phase != PhaseRunning {
		t.Fatalf("expected Running, got %s", p.Phase)
	}
	if p.CurrentStep != 1 {
		t.Fatalf("expected step 1, got %d", p.CurrentStep)
	}
	if p.CurrentResult != nil {
		t.Fatal("currentResult should be cleared after advance")
	}
	if len(p.RevisionChain) != 0 {
		t.Fatal("revision chain should be cleared after advance")
	}
	if len(p.PriorStepResults) != 1 {
		t.Fatalf("expected 1 prior result, got %d", len(p.PriorStepResults))
	}
}

func TestAdvanceToCompleted(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(gatedStep(), result("final"))
	p.Accept()

	completed, err := p.Advance(1) // only 1 step total
	if err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("should be completed when advancing past last step")
	}
	if p.Phase != PhaseCompleted {
		t.Fatalf("expected Completed, got %s", p.Phase)
	}
	if !p.Phase.IsTerminal() {
		t.Fatal("Completed should be terminal")
	}
}

func TestAdvanceWrongPhase(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	_, err := p.Advance(3)
	if err != ErrNotAccepted {
		t.Fatalf("expected ErrNotAccepted, got %v", err)
	}
}

// --- Loop ---

func TestLoopContinueAndEnd(t *testing.T) {
	step := loopStep()
	p := NewPipeline("/root", "discuss", "Note")

	// First iteration
	p.CompleteStep(step, result("turn 1"))
	if p.Phase != PhaseLooping {
		t.Fatalf("expected Looping, got %s", p.Phase)
	}

	// Continue loop (user responds, triggers next iteration)
	p.ContinueLoop(step)
	if p.Phase != PhaseRunning {
		t.Fatalf("expected Running after continue, got %s", p.Phase)
	}

	// Second iteration
	p.CompleteStep(step, result("turn 2"))
	if p.Phase != PhaseLooping {
		t.Fatalf("expected Looping again, got %s", p.Phase)
	}

	// Check accumulator has both turns (append policy)
	if p.Accumulator == nil {
		t.Fatal("expected accumulator to be set")
	}

	// End loop
	p.EndLoop()
	if p.Phase != PhaseAccepted {
		t.Fatalf("expected Accepted after end loop, got %s", p.Phase)
	}
}

func TestLoopContinueWrongPhase(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	err := p.ContinueLoop(loopStep())
	if err != ErrNotLooping {
		t.Fatalf("expected ErrNotLooping, got %v", err)
	}
}

func TestEndLoopWrongPhase(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	err := p.EndLoop()
	if err != ErrNotLooping {
		t.Fatalf("expected ErrNotLooping, got %v", err)
	}
}

// --- Cancel ---

func TestCancelFromPending(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(gatedStep(), result("nah"))

	err := p.Cancel(gatedStep())
	if err != nil {
		t.Fatal(err)
	}
	if p.Phase != PhaseCancelled {
		t.Fatalf("expected Cancelled, got %s", p.Phase)
	}
}

func TestCancelFromLooping(t *testing.T) {
	p := NewPipeline("/root", "discuss", "Note")
	p.CompleteStep(loopStep(), result("turn 1"))

	err := p.Cancel(loopStep())
	if err != nil {
		t.Fatal(err)
	}
	if p.Phase != PhaseCancelled {
		t.Fatalf("expected Cancelled, got %s", p.Phase)
	}
}

func TestCancelNonCancelable(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(nonCancelableGatedStep(), result("stuck"))

	err := p.Cancel(nonCancelableGatedStep())
	if err != ErrNotCancelable {
		t.Fatalf("expected ErrNotCancelable, got %v", err)
	}
}

func TestCancelWrongPhase(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	// Running phase
	err := p.Cancel(gatedStep())
	if err == nil {
		t.Fatal("expected error cancelling from Running")
	}
}

// --- Terminal states ---

func TestTerminalStatesAreTerminal(t *testing.T) {
	for _, phase := range []PipelinePhase{PhaseRejected, PhaseCancelled, PhaseCompleted} {
		if !phase.IsTerminal() {
			t.Fatalf("expected %s to be terminal", phase)
		}
	}
	for _, phase := range []PipelinePhase{PhaseRunning, PhasePending, PhaseAccepted, PhaseLooping} {
		if phase.IsTerminal() {
			t.Fatalf("expected %s to NOT be terminal", phase)
		}
	}
}

// --- Revision history ---

func TestRevisionHistoryPolicies(t *testing.T) {
	p := NewPipeline("/root", "fn", "Note")
	p.CompleteStep(gatedStep(), result("v1"))
	p.Revise(gatedStep(), "f1", result("v2"))
	p.Revise(gatedStep(), "f2", result("v3"))

	// Full: all entries
	full := p.RevisionHistory(HistoryFull)
	if len(full) != 2 {
		t.Fatalf("HistoryFull: expected 2, got %d", len(full))
	}

	// Latest: only the last entry
	latest := p.RevisionHistory(HistoryLatest)
	if len(latest) != 1 {
		t.Fatalf("HistoryLatest: expected 1, got %d", len(latest))
	}
	if latest[0].Attempt.Raw != "v2" {
		t.Fatalf("HistoryLatest: expected v2, got %q", latest[0].Attempt.Raw)
	}

	// None: empty
	none := p.RevisionHistory(HistoryNone)
	if len(none) != 0 {
		t.Fatalf("HistoryNone: expected 0, got %d", len(none))
	}
}

// --- Multi-step pipeline end-to-end ---

func TestFullPipelineLifecycle(t *testing.T) {
	// Simulate: decompose (suggest -> gate -> generate)
	p := NewPipeline("/root", "decompose", "My Note")

	// Step 0: suggest (gated)
	p.CompleteStep(gatedStep(), result("3 children proposed"))
	if p.Phase != PhasePending {
		t.Fatalf("step 0: expected Pending, got %s", p.Phase)
	}

	// Revise once
	p.Revise(gatedStep(), "add a 4th", result("4 children proposed"))
	if p.Phase != PhasePending {
		t.Fatalf("after revise: expected Pending, got %s", p.Phase)
	}

	// Accept
	p.Accept()
	if p.Phase != PhaseAccepted {
		t.Fatalf("after accept: expected Accepted, got %s", p.Phase)
	}

	// Advance to step 1
	completed, _ := p.Advance(2)
	if completed {
		t.Fatal("should not be completed yet")
	}
	if p.CurrentStep != 1 {
		t.Fatalf("expected step 1, got %d", p.CurrentStep)
	}

	// Step 1: generate (gated)
	p.CompleteStep(gatedStep(), opsResult("4 files created"))
	if p.Phase != PhasePending {
		t.Fatalf("step 1: expected Pending, got %s", p.Phase)
	}

	// Accept and advance (last step)
	p.Accept()
	completed, _ = p.Advance(2)
	if !completed {
		t.Fatal("expected completed after last step")
	}
	if p.Phase != PhaseCompleted {
		t.Fatalf("expected Completed, got %s", p.Phase)
	}

	// Prior results should have both steps
	if len(p.PriorStepResults) != 2 {
		t.Fatalf("expected 2 prior results, got %d", len(p.PriorStepResults))
	}
}

func TestDiscussionLifecycle(t *testing.T) {
	// Simulate: discussion (looping single step)
	step := loopStep()
	p := NewPipeline("/root", "discuss", "My Note")

	// Turn 1: AI responds
	p.CompleteStep(step, result("[agent] What about liability?"))
	if p.Phase != PhaseLooping {
		t.Fatalf("expected Looping, got %s", p.Phase)
	}

	// User responds -> continue
	p.ContinueLoop(step)
	if p.Phase != PhaseRunning {
		t.Fatalf("expected Running, got %s", p.Phase)
	}

	// Turn 2: AI responds
	p.CompleteStep(step, result("[agent] Good point about tiers"))

	// User responds -> continue
	p.ContinueLoop(step)

	// Turn 3: AI responds
	p.CompleteStep(step, result("[agent] Let's explore insurance"))

	// User says .end
	p.EndLoop()
	if p.Phase != PhaseAccepted {
		t.Fatalf("expected Accepted after .end, got %s", p.Phase)
	}

	// Advance (only step, so completes)
	completed, _ := p.Advance(1)
	if !completed {
		t.Fatal("expected completed")
	}

	// Accumulator should have all turns
	if len(p.PriorStepResults) != 1 {
		t.Fatalf("expected 1 prior result (the accumulated transcript)")
	}
}
