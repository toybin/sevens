#lang forge/temporal

-- ============================================================
-- Minimal example: the review gate sub-machine
--
-- Models just the Pending/Accept/Reject/Revise cycle at a
-- single gate in the pipeline. Small enough to see the whole
-- thing work.
-- ============================================================

-- The states of the gate. `abstract` + children = enum.
abstract sig GateState {}
one sig Idle, Pending, Accepted, Rejected extends GateState {}

-- A Result value. We don't care what's inside it, just that
-- different results are distinguishable.
sig Result {}

-- The gate itself. `one sig` = exactly one gate in the model.
-- `var` fields are mutable across time steps.
one sig Gate {
    var phase: one GateState,
    var currentResult: lone Result,  -- lone = 0 or 1
    var revisionCount: one Int       -- how many revisions so far
}

-- ============================================================
-- Initial state: gate is idle, no result, no revisions
-- ============================================================
pred init {
    Gate.phase = Idle
    no Gate.currentResult
    Gate.revisionCount = 0
}

-- ============================================================
-- Transitions
-- ============================================================

-- A step completes and produces a result for review.
pred produce {
    Gate.phase = Idle
    Gate.phase' = Pending
    some r: Result | Gate.currentResult' = r
    Gate.revisionCount' = Gate.revisionCount
}

-- Human accepts the pending result.
pred accept {
    Gate.phase = Pending
    Gate.phase' = Accepted
    Gate.currentResult' = Gate.currentResult
    Gate.revisionCount' = Gate.revisionCount
}

-- Human rejects the pending result.
pred reject {
    Gate.phase = Pending
    Gate.phase' = Rejected
    Gate.currentResult' = Gate.currentResult
    Gate.revisionCount' = Gate.revisionCount
}

-- Human requests revision: new result replaces the old one.
pred revise {
    Gate.phase = Pending
    some Gate.currentResult
    Gate.phase' = Pending
    -- a NEW result appears (different from current)
    some r: Result | {
        r != Gate.currentResult
        Gate.currentResult' = r
    }
    Gate.revisionCount' = add[Gate.revisionCount, 1]
}

-- Nothing happens (terminal states stay put).
pred stutter {
    Gate.phase' = Gate.phase
    Gate.currentResult' = Gate.currentResult
    Gate.revisionCount' = Gate.revisionCount
}

-- One of these happens at each time step.
pred step {
    produce or accept or reject or revise or stutter
}

-- Terminal states must stutter.
pred terminalsMustStutter {
    (Gate.phase = Accepted or Gate.phase = Rejected)
        implies stutter
}

-- ============================================================
-- Traces: valid behaviors of the gate
-- ============================================================
pred traces {
    init
    always step
    always terminalsMustStutter
}

-- ============================================================
-- Let's ask Forge some questions!
-- ============================================================

-- Question 1: "Show me a trace where the gate gets accepted."
-- This is `run` -- find a satisfying instance.
-- run { traces and eventually Gate.phase = Accepted }
--     for exactly 3 Result

-- Question 2: "Show me a trace with at least 2 revisions
-- before acceptance."
-- run { traces and eventually {
--     Gate.phase = Accepted and Gate.revisionCount >= 2
-- }} for exactly 4 Result

-- Question 3: "Is it possible to be Pending with no result?"
-- If this is SAT, we have a bug. If UNSAT, we're safe.
-- (This is a check disguised as a run.)
-- run { traces and eventually {
--     Gate.phase = Pending and no Gate.currentResult
-- }} for exactly 3 Result

-- Question 4: "Once accepted, does the gate stay accepted forever?"
-- We express this as a pred, then assert it.
pred acceptedIsForever {
    always {
        Gate.phase = Accepted implies always Gate.phase = Accepted
    }
}

-- Question 5: "Once rejected, does the gate stay rejected forever?"
pred rejectedIsForever {
    always {
        Gate.phase = Rejected implies always Gate.phase = Rejected
    }
}

-- Question 6: "Can the revision count ever decrease?"
pred revisionCountNeverDecreases {
    always {
        Gate.revisionCount' >= Gate.revisionCount
    }
}

-- Run: show me a trace where acceptance happens after 2 revisions
run {
    traces
    eventually { Gate.phase = Accepted and Gate.revisionCount >= 2 }
} for exactly 4 Result
