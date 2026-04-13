# Code Review Report

Date: 2026-04-08
Reviewer: Codex
Scope: repository source, docs, generated API map, and current working tree

## Review basis

- Read the top-level design and architecture docs.
- Regenerated the API map with `./scripts/api-map.sh`.
- Read the main CLI, REPL, engine, graph, apply, backend, and store packages.
- Ran `go test ./...` successfully.

## Assumptions and limits

- This review is of the current working tree, not a committed diff. The repo is effectively uncommitted, so findings are against the full implementation as it exists now.
- Function and template definitions under `~/.config/sevens` were not reviewed. Findings about function execution focus on the engine and rendering code, not the quality of user-defined prompts.
- External backend behavior was reviewed from code only. I did not exercise live Anthropic/Codex/Claude calls.
- I did not run destructive git workflows manually. Git findings come from code inspection of staging, commit, and revert paths.
- The review assumes the documented multi-root workflow is intended and supported, because the CLI and docs explicitly expose it.

## Executive summary

The biggest problem is structural: the triple store does not namespace node identity by root, but the product claims to support multiple roots. That makes duplicate titles across roots a data-corruption bug, not just a UI bug.

The second class of problems is workflow integrity: logging, revision, submit, discussion cancel, and git commit behavior all have failure modes where the system records the wrong state, loses audit fidelity, or modifies more of the repo than the user asked it to.

The test suite passing is not a strong signal of correctness here. The highest-risk defects are mostly state-model and workflow bugs that are either untested or only partially tested on happy paths.

## Confirmed findings

### 1. Critical: multi-root identity is unsafe

Severity: Critical

The store uses the node title itself as the triple subject. That means `"Foo"` in root A and `"Foo"` in root B are the same logical entity in storage. Any subject-keyed predicate like `node/content`, `node/file-path`, `node/parent`, `sibling/role`, `context/file`, computed content metrics, logs keyed by target title, and suspensions keyed by target title can collide.

Relevant code:

- [internal/graph/triples.go](/Users/dorseyj/tools/sevens/internal/graph/triples.go)
- [internal/store/triples.go](/Users/dorseyj/tools/sevens/internal/store/triples.go)
- [internal/graph/query.go](/Users/dorseyj/tools/sevens/internal/graph/query.go)
- [internal/apply/resolve.go](/Users/dorseyj/tools/sevens/internal/apply/resolve.go)
- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)

Why this is severe:

- `PopulateTriples` clears a root by enumerating its subjects and deleting by subject. If the same subject exists in another root, those shared subject triples are deleted too.
- Many reads use `GetObject(subject, predicate)` without rechecking `node/root`, so once a collision exists, the read path has no defense.
- Root-scoped queries only help for enumerating node sets. They do not repair subject collisions after the fact.

Likely failure modes:

- Syncing one root causes another root’s node content or file path to disappear or silently change.
- `walk`, `apply`, `search`, `prepare`, `accept`, and REPL focus resolve the “right” title in the “wrong” root but then read stale or foreign content by shared subject.
- Cross-root duplicate titles produce wrong parents, wrong children, wrong sibling roles, or wrong edit targets.
- Pending suggestions and revision history can appear attached to the wrong root when titles overlap.

Where it will express itself:

- Any environment with more than one root.
- Any user importing a second notebook with overlapping titles like `Inbox`, `Index`, `Journal`, `Ideas`, `Discussion - X`, or `Daily Note`.
- Any background workflow that calls `sevens sync` with no explicit root and syncs all registered roots.

### 2. High: git operations capture unrelated repo changes

Severity: High

The system’s git helper stages the entire repo with `git add -A` and commits it. The higher-level commands use that helper for `sync`, `accept`, `new`, `discuss`, and `revert`.

Relevant code:

- [internal/apply/git.go](/Users/dorseyj/tools/sevens/internal/apply/git.go)
- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)
- [internal/repl/discuss.go](/Users/dorseyj/tools/sevens/internal/repl/discuss.go)

Why this is severe:

- Sevens commits whatever is dirty, not what it changed.
- A user with unrelated work in the repo can have that work swept into a sevens-generated commit.
- The revert model assumes the logged files describe the commit’s effect, but the commit may contain unrelated files.

Likely failure modes:

- `sevens sync` creates commits containing the user’s unrelated edits.
- Accepting AI ops produces commits that mix AI edits with unrelated human edits.
- `new` or `discuss` unexpectedly commits unrelated files.
- Revert only restores logged sevens files, leaving other files from the same commit still reverted or still committed inconsistently.

Where it will express itself:

- Any dirty worktree.
- Any user running sevens inside a normal project repo instead of a dedicated notes-only repo.

### 3. High: log subject collisions corrupt audit history

Severity: High

Log subjects are generated from `(node title, RFC3339 timestamp)` with second-level precision. Multiple events for the same node within the same second collapse onto one subject.

Relevant code:

- [internal/apply/log.go](/Users/dorseyj/tools/sevens/internal/apply/log.go)
- [internal/repl/repl.go](/Users/dorseyj/tools/sevens/internal/repl/repl.go)
- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)

Why this is severe:

- `accepted` and `applied` are often written back-to-back.
- The primary key is `(subject, predicate, object)`, so merged subjects can accumulate predicates from multiple logical events.
- Reads reconstruct one `LogEntry` per subject, not per event.

Likely failure modes:

- History shows a single event that appears both accepted and applied.
- Revert logic chooses the wrong “last applied” event.
- Revision history becomes incomplete or misleading.
- Downstream prompt context using `history` or cross-walk output becomes unstable.

Where it will express itself:

- Fast local executions.
- Any path that writes two or more log entries synchronously.
- REPL flows more than CLI, because they often perform accept/apply/resync in one tight sequence.

### 4. High: CLI revision path leaves no new pending suspension

Severity: High

`sevens accept --with ...` re-runs the step, displays output, and resolves the old suspension as revised, but does not create a replacement pending suspension. The next approval step has nothing to act on.

Relevant code:

- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)
- [internal/engine/engine.go](/Users/dorseyj/tools/sevens/internal/engine/engine.go)
- [internal/repl/repl.go](/Users/dorseyj/tools/sevens/internal/repl/repl.go)

Why this matters:

- The REPL explicitly works around this by writing a new suspension after revision.
- The CLI and REPL therefore do not share the same workflow guarantees.

Likely failure modes:

- User revises a suggestion successfully, then `sevens accept` reports no pending suggestions.
- Revised ops are shown but can no longer be approved through the normal pipeline.
- User assumes revised output was persisted for review, but only the log changed.

Where it will express itself:

- Any CLI revision flow, especially multi-step functions and ops-producing final steps.

### 5. High: `submit` records the wrong step index

Severity: High

Agent-mode `submit` stores every suspension with `suspension/step-index = 0`, regardless of the actual step submitted.

Relevant code:

- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)

Why this matters:

- `accept` resumes from `sus.StepIndex + 1`.
- For any multi-step function, a submission for step 2 or 3 will resume as if step 0 had just completed.

Likely failure modes:

- Approval resumes the wrong next step.
- Intermediate output is fed into the wrong prompt.
- Multi-step agent workflows appear randomly broken while single-step functions seem fine.

Where it will express itself:

- Any `prepare`/`submit` workflow for multi-step functions.
- Functions using staged suggestions then ops generation.

### 6. High: revised runs do not use the same prompt semantics as initial runs

Severity: High

Normal execution can use `Requires`, resolved parent/sibling/child content, history injection, cross-walk output, context paths, agent-specific system prompts, and explicit model overrides. `ReviseStep` bypasses all of that and rebuilds a plain prompt with `RenderStepPrompt`.

Relevant code:

- [internal/engine/engine.go](/Users/dorseyj/tools/sevens/internal/engine/engine.go)
- [internal/apply/resolve.go](/Users/dorseyj/tools/sevens/internal/apply/resolve.go)

Why this matters:

- A revision is not actually a revision of the same call; it is a different call with different context semantics.
- Functions that rely on resolved graph context are especially vulnerable.

Likely failure modes:

- A function works initially but produces nonsense or degraded output on revision.
- Revision ignores sibling content, child content, prior history, or custom step agent configuration.
- CLI backend revision uses a different model or system prompt than the original run.

Where it will express itself:

- Context-heavy functions.
- Functions using `:requires`, `:context`, `:cross-walk`, or per-step agent overrides.

### 7. Medium: discussion cancel does not reliably discard changes

Severity: Medium

Discussion cancel claims to discard, but rollback depends on git and is fragile even there.

Relevant code:

- [internal/repl/discuss.go](/Users/dorseyj/tools/sevens/internal/repl/discuss.go)
- [internal/graph/sync.go](/Users/dorseyj/tools/sevens/internal/graph/sync.go)

Why this matters:

- In non-git roots, cancel does nothing to undo file creation or edits.
- In git roots, the stored file path is absolute, but the code passes it straight to `git checkout --`, which is not a reliable repo-relative pathspec.
- New untracked discussion files are not removed by checkout.

Likely failure modes:

- `.cancel` leaves the discussion file behind.
- `.cancel` keeps appended user turns or generated agent turns.
- The UI says “discussion discarded” when the file is still changed.

Where it will express itself:

- Non-git roots.
- First-time discussion creation.
- Any repo where git pathspec resolution rejects the absolute path.

### 8. Medium: store API claims replacement semantics it does not provide

Severity: Medium

The code comments say insert is “REPLACE semantics,” but the schema key is `(subject, predicate, object)`. Re-inserting the same subject and predicate with a different object creates an additional row instead of replacing the old one.

Relevant code:

- [internal/store/triples.go](/Users/dorseyj/tools/sevens/internal/store/triples.go)

Why this matters:

- Callers may assume unique predicates are safe to overwrite via `InsertTriple`.
- A few paths already compensate manually by deleting first, which is a sign the abstraction is misleading.

Likely failure modes:

- Hidden duplicate state for predicates that are logically singular.
- `GetObject` returns whichever row SQLite happens to surface first.
- Future features that update root config or mutable singleton predicates will accumulate stale values.

Where it will express itself:

- New code more than current code, unless it follows the explicit delete-then-insert pattern.
- Any feature that mutates metadata without a full root resync.

## Secondary risks and likely blind spots

These are not all confirmed bugs, but they are strong candidates for failure:

### State reconstruction from the triples table is fragile

- The system leans on a single triples table for content, logs, suspensions, config, and computed metrics.
- Many readers assume uniqueness where the schema does not enforce it.
- Expect hard-to-debug nondeterminism if multiple writes occur for logically singleton predicates.

### REPL and CLI behavior are drifting

- The REPL contains workflow fixes not mirrored in the CLI.
- The more these paths diverge, the more likely one mode will silently lose state while the other appears correct.

### Revert semantics are weaker than the UI suggests

- Revert is file-based, not commit-accurate.
- It reconstructs a previous state from logged file lists, not by reverting exactly what sevens committed.
- This is especially risky because commits may include unrelated files.

### Agent-mode integration is under-tested

- `prepare` and `submit` do not appear to have robust end-to-end safeguards for multi-step pipelines.
- Step indexing, context parity, and gate handling are all places where agent mode can desynchronize from native execution.

## Most probable user-visible symptoms

- “Why did syncing one notebook break another notebook with the same note titles?”
- “Why did sevens commit files I never touched with it?”
- “Why did my revised suggestion disappear?”
- “Why does `accept` resume the wrong step after `submit`?”
- “Why does discussion cancel say discarded when the file is still there?”
- “Why is the operation log missing entries or showing merged weirdness?”
- “Why does revise produce worse output than the first run?”

## Recommended remediation order

1. Fix node identity so subjects are root-scoped or otherwise globally unique.
2. Fix git staging so sevens commits only the files it changed.
3. Make log entry subjects collision-proof.
4. Unify CLI and REPL revision handling around one correct suspension lifecycle.
5. Fix `submit` to derive and persist the real step index.
6. Make `ReviseStep` use the same prompt resolution path as initial execution.
7. Rework discussion cancel to track and reverse actual file operations, not best-effort git checkout.
8. Tighten store semantics so singular predicates are actually singular by API and schema design.

## Verification status

- `./scripts/api-map.sh` ran successfully and regenerated `API-MAP.md`.
- `go test ./...` passed.

That passing test result should not be read as a cleanliness signal for the failures above. The highest-risk defects are mostly workflow and state-model issues that require targeted tests that do not currently appear to exist.
