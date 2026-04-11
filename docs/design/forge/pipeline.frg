#lang forge/temporal

-- ============================================================
-- Pipeline State Machine
-- Models the Function concept's curried pipeline from
-- concept-function.md
-- ============================================================

-- In Forge, `sig` declares a type. Think of it as defining
-- what kinds of things exist in our model.
--
-- `abstract sig` means: every instance must be one of the
-- child sigs. Like an enum.
--
-- `one sig` means: exactly one instance of this type exists.
-- Used for enum values and singletons.

-- Pipeline states from the concept doc.
-- `abstract` + child sigs = an enum in Forge.
abstract sig PipelineState {}
one sig Running, Pending, Accepted, Looping,
        Rejected, Cancelled, Completed extends PipelineState {}

-- A Step in the pipeline. Steps are static (defined at design
-- time), so no `var` fields.
sig Step {
    nextStep: lone Step,    -- lone = zero or one (last step has none)
    gated: one Bool,        -- does this step pause for review?
    looping: one Bool,      -- does this step loop?
    revisable: one Bool,    -- can the user revise at this gate?
    cancelable: one Bool    -- can the user cancel at this gate?
}

-- A Result produced by executing a step.
-- These are created during execution, so we model them simply.
sig Result {}

-- A RevisionEntry: one (attempt, feedback) pair.
sig RevisionEntry {
    attempt: one Result,
    feedback: one Result   -- modeling feedback as a Result for simplicity
}

-- The Pipeline itself. This is the live, mutable state.
-- `var` fields change across time steps (Temporal Forge).
--
-- `one sig` because we're modeling a single pipeline's
-- lifecycle. To model multiple pipelines, we'd use `sig`.
one sig Pipeline {
    firstStep: one Step,

    -- mutable state (changes across time):
    var currentStep: one Step,
    var state: one PipelineState,
    var currentResult: lone Result,        -- lone: may or may not have one
    var revisionChain: set RevisionEntry   -- set: zero or more
}

-- ============================================================
-- Initial state
-- ============================================================

-- `pred` declares a named constraint. This one says what the
-- pipeline looks like at the start.
pred init {
    Pipeline.currentStep = Pipeline.firstStep
    Pipeline.state = Running
    no Pipeline.currentResult
    no Pipeline.revisionChain
}

-- ============================================================
-- Transitions
-- ============================================================

-- Helper: nothing changes (frame condition).
-- In Forge, you MUST explicitly say what doesn't change,
-- otherwise the solver is free to change anything.
pred stutter {
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.state' = Pipeline.state
    Pipeline.currentResult' = Pipeline.currentResult
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Running -> Pending (gated step produces a result)
pred completeToPending {
    -- guard (precondition)
    Pipeline.state = Running
    Pipeline.currentStep.gated = True

    -- action (postcondition)
    -- `some r: Result` means "there exists a new result"
    some r: Result | {
        Pipeline.currentResult' = r
    }
    Pipeline.state' = Pending

    -- frame: what doesn't change
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Running -> advance automatically (ungated step)
pred completeAndAdvance {
    -- guard
    Pipeline.state = Running
    Pipeline.currentStep.gated = False
    Pipeline.currentStep.looping = False
    some Pipeline.currentStep.nextStep  -- not the last step

    -- action
    Pipeline.currentStep' = Pipeline.currentStep.nextStep
    Pipeline.state' = Running
    no Pipeline.currentResult'
    no Pipeline.revisionChain'
}

-- Running -> Completed (ungated last step)
pred completeToCompleted {
    -- guard
    Pipeline.state = Running
    Pipeline.currentStep.gated = False
    Pipeline.currentStep.looping = False
    no Pipeline.currentStep.nextStep  -- last step

    -- action
    Pipeline.state' = Completed
    some r: Result | Pipeline.currentResult' = r
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Running -> Looping (looping step, first iteration done)
pred completeToLooping {
    -- guard
    Pipeline.state = Running
    Pipeline.currentStep.looping = True

    -- action
    Pipeline.state' = Looping
    some r: Result | Pipeline.currentResult' = r
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Pending -> Accepted
pred accept {
    -- guard
    Pipeline.state = Pending

    -- action
    Pipeline.state' = Accepted

    -- frame
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.currentResult' = Pipeline.currentResult
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Pending -> Rejected
pred reject {
    -- guard
    Pipeline.state = Pending

    -- action
    Pipeline.state' = Rejected

    -- frame
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.currentResult' = Pipeline.currentResult
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Pending -> Pending (with revised result)
pred revise {
    -- guard
    Pipeline.state = Pending
    Pipeline.currentStep.revisable = True
    some Pipeline.currentResult

    -- action: new result, old result+feedback added to chain
    some newResult: Result, entry: RevisionEntry | {
        entry.attempt = Pipeline.currentResult
        Pipeline.currentResult' = newResult
        Pipeline.revisionChain' = Pipeline.revisionChain + entry
    }
    Pipeline.state' = Pending
    Pipeline.currentStep' = Pipeline.currentStep
}

-- Pending -> Cancelled
pred cancel {
    -- guard
    Pipeline.state = Pending
    Pipeline.currentStep.cancelable = True

    -- action
    Pipeline.state' = Cancelled

    -- frame
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.currentResult' = Pipeline.currentResult
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Accepted -> Running (advance to next step)
pred advanceFromAccepted {
    -- guard
    Pipeline.state = Accepted
    some Pipeline.currentStep.nextStep

    -- action
    Pipeline.currentStep' = Pipeline.currentStep.nextStep
    Pipeline.state' = Running
    no Pipeline.currentResult'
    no Pipeline.revisionChain'
}

-- Accepted -> Completed (was the last step)
pred advanceToCompleted {
    -- guard
    Pipeline.state = Accepted
    no Pipeline.currentStep.nextStep

    -- action
    Pipeline.state' = Completed
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.currentResult' = Pipeline.currentResult
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Looping -> Running (continue: next iteration)
pred continueLoop {
    -- guard
    Pipeline.state = Looping

    -- action: loop restarts at same step
    Pipeline.state' = Running
    Pipeline.currentStep' = Pipeline.currentStep
    -- result carries forward (accumulator)
    Pipeline.currentResult' = Pipeline.currentResult
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Looping -> advance (end loop: break out)
pred endLoop {
    -- guard
    Pipeline.state = Looping
    some Pipeline.currentStep.nextStep

    -- action: move to next step
    Pipeline.currentStep' = Pipeline.currentStep.nextStep
    Pipeline.state' = Running
    no Pipeline.currentResult'
    no Pipeline.revisionChain'
}

-- Looping -> Completed (end loop, was last step)
pred endLoopCompleted {
    -- guard
    Pipeline.state = Looping
    no Pipeline.currentStep.nextStep

    -- action
    Pipeline.state' = Completed
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.currentResult' = Pipeline.currentResult
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- Looping -> Cancelled
pred cancelLoop {
    -- guard
    Pipeline.state = Looping
    Pipeline.currentStep.cancelable = True

    -- action
    Pipeline.state' = Cancelled
    Pipeline.currentStep' = Pipeline.currentStep
    Pipeline.currentResult' = Pipeline.currentResult
    Pipeline.revisionChain' = Pipeline.revisionChain
}

-- ============================================================
-- System behavior: one transition per time step
-- ============================================================

-- The system must take exactly one transition at each step,
-- OR stutter (do nothing) if in a terminal state.
pred transition {
    completeToPending
    or completeAndAdvance
    or completeToCompleted
    or completeToLooping
    or accept
    or reject
    or revise
    or cancel
    or advanceFromAccepted
    or advanceToCompleted
    or continueLoop
    or endLoop
    or endLoopCompleted
    or cancelLoop
    or stutter
}

-- Terminal states stutter forever (no escape).
pred terminalStutter {
    (Pipeline.state = Completed or
     Pipeline.state = Rejected or
     Pipeline.state = Cancelled)
    implies stutter
}

-- ============================================================
-- Structural constraints on Steps
-- ============================================================

-- Steps form a linear sequence (no branching yet).
pred wellformedSteps {
    -- firstStep is reachable
    all s: Step | s = Pipeline.firstStep or
                  reachable[s, Pipeline.firstStep, nextStep]
    -- no cycles
    all s: Step | not reachable[s, s, nextStep]
}

-- ============================================================
-- Traces: valid execution histories
-- ============================================================

-- `always` means "at every time step in the trace."
-- This says: start in init, always take a valid transition,
-- always enforce terminal stutter.
pred traces {
    init
    wellformedSteps
    always transition
    always terminalStutter
}

-- ============================================================
-- Properties to check
-- ============================================================

-- Property 1: terminal states are truly terminal.
-- Once completed/rejected/cancelled, state never changes.
pred terminalIsForever {
    always {
        Pipeline.state = Completed implies
            always Pipeline.state = Completed
        Pipeline.state = Rejected implies
            always Pipeline.state = Rejected
        Pipeline.state = Cancelled implies
            always Pipeline.state = Cancelled
    }
}

-- Property 2: you can't be Pending without a result.
pred pendingHasResult {
    always {
        Pipeline.state = Pending implies some Pipeline.currentResult
    }
}

-- Property 3: revision chain only grows in Pending state.
pred revisionChainOnlyGrowsInPending {
    always {
        Pipeline.revisionChain != Pipeline.revisionChain'
        implies Pipeline.state = Pending
    }
}

-- Property 4: every pipeline eventually reaches a terminal state.
-- (This is a liveness property -- needs fairness assumptions to hold.)
pred eventuallyTerminates {
    eventually {
        Pipeline.state = Completed or
        Pipeline.state = Rejected or
        Pipeline.state = Cancelled
    }
}

-- Property 5: revision only possible when step is revisable.
pred revisionRespectsConfig {
    always {
        (some Pipeline.revisionChain' - Pipeline.revisionChain)
        implies Pipeline.currentStep.revisable = True
    }
}

-- ============================================================
-- Run and Check commands
-- ============================================================

-- Find a valid trace (sanity check: does the model have any
-- valid behaviors?)
run { traces } for exactly 3 Step, exactly 5 Result,
                    exactly 5 RevisionEntry, 8 Bool
                for {nextStep is linear}

-- Verify properties hold over all valid traces.
-- If a check fails, Forge shows a counterexample.

-- check { traces implies terminalIsForever }
--     for exactly 3 Step, 5 Result, 5 RevisionEntry

-- check { traces implies pendingHasResult }
--     for exactly 3 Step, 5 Result, 5 RevisionEntry

-- check { traces implies revisionChainOnlyGrowsInPending }
--     for exactly 3 Step, 5 Result, 5 RevisionEntry

-- check { traces implies revisionRespectsConfig }
--     for exactly 3 Step, 5 Result, 5 RevisionEntry
