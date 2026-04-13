# Concept Design Audit #2

Post-remediation audit. Compares current code against concept specs after
BUG-1 through BUG-11 fixes, workflow layer addition, and REPL adapter wiring.

## Layer compliance

| Layer | Package | Status | Notes |
|-------|---------|--------|-------|
| 1: Triple | `internal/triple` | Clean | All 6 actions, all 6 queries implemented. No domain knowledge. |
| 2: GraphOps | `internal/graphops` | Clean | All 3 actions, all 4 queries. Predicate specs in-memory as designed. |
| 3: KB | `internal/kb` | Clean | All 9 actions, all 7 queries + validate. Predicates registered. |
| Function | `internal/function` | Mostly clean | Pipeline state machine complete. Agent mode declared but unimplemented. |
| Projection | `internal/projection/md` | Clean | Sync, Write, ApplyOps, git. Round-trip fidelity. |
| Config | `internal/config` | Clean | Global config, roots, model resolution. |
| Workflow | `internal/workflow` | New, correct | 5 sync rules implemented. Not yet adopted by CLI. |

**Import discipline is correct.** No layer violations. triple imports nothing.
graphops imports only triple. kb imports only graphops+triple. function imports
kb (not projection). projection/md imports kb (not function). workflow imports
all (correct -- it's the orchestrator).

## Findings

### HIGH: CLI still reimplements sync orchestration

The workflow package exists and correctly encodes the sync rules. But
`cmd/sevens/pipeline_cmds.go` still has inline orchestration:

- `applyCmd2` (lines 115-147): duplicates `workflow.ApplyFunction` --
  calls proj.ApplyOps, projmd.CommitFiles, syncRoot inline.
- `acceptCmd2` (lines 265-305): duplicates `workflow.AcceptPipeline` --
  same pattern, plus backend resolution with pipeline.BackendName.
- `rejectCmd2` (lines 326-401): duplicates pipeline-finding logic that
  `workflow.FindPendingPipeline` already handles.

Both CLI and REPL maintain separate code paths to the same operations.
The REPL uses the adapter interfaces (now fully wired) but those adapters
call executor directly rather than through the workflow layer. So there
are effectively three implementations of the AcceptOps sync: CLI inline,
REPL adapter, and workflow package.

**Fix:** Replace CLI inline orchestration with workflow calls. Have the
REPL's PipelineRunner adapter call workflow functions instead of the
executor directly. Then there's one implementation of each sync rule.

### HIGH: Agent mode declared but not implemented

`function/types.go:87` defines `BackendAgent` as a backend kind.
`concept-function.md` (lines 499-508) describes agent mode as a
TransformBackend where Execute returns a checklist. No `AgentBackend`
struct exists.

The CLI has `prepare` and `submit` commands that work -- but they bypass
the pipeline state machine entirely. `prepare` renders a prompt; `submit`
writes triples directly. They don't create a Pipeline, don't go through
the executor, and don't use the BackendAgent kind.

This means agent mode works but is architecturally disconnected from the
Function concept's pipeline machinery. It's a separate code path that
happens to produce similar outcomes.

### MEDIUM: Pipeline transition logging scattered

syncs.md specifies `PipelineTransitionLog` as a cross-cutting sync:
every state transition should produce a log entry. Currently:

- `executor.go` logs in Accept, Reject, Revise, Cancel, EndLoop
- `executor.go:executeStep` logs "step-completed"
- CLI `acceptCmd2` does NOT log when it calls Revise (line 245)
- CLI `rejectCmd2` relies on executor.Reject's internal logging
- The workflow layer logs "applied" after materializeOps

This means: if you run a revise via the CLI's accept --with, the revision
gets logged by the executor but the subsequent accept does not -- the
CLI handles display but doesn't call KB.AppendLog for the completion.

### MEDIUM: Discussion mode not lifted to workflow

syncs.md defines three syncs for discussion (EnterDiscussion,
DiscussionTurn, EndDiscussion). These are implemented inline in
`internal/repl/discuss.go` (~200 lines). They are:

- Not testable outside the REPL
- Not available to the CLI (no `sevens discuss` through the pipeline)
- Not reachable through the workflow layer

The discuss function EXISTS in the function definitions (`discuss.edn`)
and loads correctly. But the actual discussion loop (multi-turn with
.end/.cancel) is REPL-specific orchestration that should be in workflow.

### LOW: Unregistered predicate "session/root"

`kb/session.go:98` asserts triples with predicate `session/root`:
```go
{Subject: CurrentSessionSubject, Predicate: "session/root", Object: root}
```

But `kb/predicates.go` does not register `session/root` in `allSpecs()`.
The predicate is used in `LoadCurrentSession` (line 122) for filtering
by root. Since Layer 2's `Set` allows unregistered predicates (they're
treated as relational by default), this works but violates the concept
design's principle that KB defines all predicates it uses.

### LOW: FileOp JSON tags were missing (fixed)

`sevtypes/fileop.go` originally had no JSON struct tags. LLM-produced
snake_case JSON (`old_text`, `new_text`) was silently dropped during
unmarshal. Fixed this session by adding `json:"snake_case"` tags. This
was the true root cause of BUG-4 (structure-modifying functions not
applying).

This was invisible in tests because the executor tests used
`TransformResult.Raw` directly (text), not parsed ops. The workflow
tests caught it because they exercise the full path: mock LLM response
-> ParseOps -> materializeOps -> file on disk.

### LOW: Block path format drift

concept-knowledge-base.md and WALKTHROUGH.md describe block paths as
kind-prefixed (`p.0`, `h.1`). The actual implementation in
`projection/md/blocks.go:formatBlockPath` produces numeric-only paths
(`0`, `1`, `4.0`). The code is consistent internally; the docs are stale.

## What's correct

The following were potential concerns that turned out to be clean:

- **Layer imports**: No violations found. The dependency graph follows
  the concept hierarchy exactly.
- **Predicate vocabulary**: All 32+ predicates from the KB concept spec
  are registered (except session/root).
- **Pipeline state machine**: All 7 phases, all transitions, revision
  chain with history policies -- all implemented and tested.
- **Projection round-trip**: Sync reads files and writes triples. Write
  renders triples to files. ApplyOps edits files in place. Git ops work.
- **Validation**: Cycle detection (fixed), overflow, orphan, overlength,
  missing-parent -- all implemented.
- **Context resolution**: Path specs with `With` predicates now resolve
  content. Template variables substitute correctly. History role works.

## Recommendations (priority order)

1. **Migrate CLI to workflow layer.** Replace inline sync orchestration
   in applyCmd2, acceptCmd2, rejectCmd2 with workflow.ApplyFunction,
   workflow.AcceptPipeline, workflow.RejectPipeline. This eliminates the
   duplication and ensures CLI/REPL/test all use the same code path.

2. **Lift discussion mode to workflow.** Extract the three discussion
   syncs from repl/discuss.go into workflow functions. The REPL becomes
   a thin UI layer over the workflow.

3. **Decide on agent mode.** Either implement AgentBackend as a proper
   TransformBackend (prepare returns checklist, submit provides result
   into pipeline), or remove BackendAgent from types.go and document
   prepare/submit as a CLI-only coordination pattern outside the
   pipeline state machine.

4. **Register session/root predicate.** Add it to predicates.go allSpecs.

5. **Centralize pipeline logging.** Move all transition logging into the
   executor (it already handles most cases). Remove any logging from
   CLI/REPL that duplicates executor logging.

6. **Update WALKTHROUGH.md block path format.** Change `p.0`, `h.1` to
   `0`, `1` to match the actual output.
