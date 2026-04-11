# Exploratory Test Report

Date: 2026-04-08
Tester: Codex
Mode: hands-on product testing
Primary root tested: `/Users/dorseyj/Documents/sevens-sandbox`
Backend requested and used for live inference: `claude`

## Test setup

- Binary under test: `/Users/dorseyj/tools/sevens/sevens`
- Shared config dir: `~/.config/sevens`
- Sandbox root: `/Users/dorseyj/Documents/sevens-sandbox`
- Global config observed:
  - `:backend "codex"`
  - `:llm.model "claude-sonnet-4-6"`
- Important operational note:
  - sevens commands had to be run outside the sandbox because the shared DB at `~/.config/sevens/sevens.db` was not writable from the restricted environment.

## Product perspective used during testing

I tested as if I were an intelligent academic using sevens to think through a real project:

- comfortable with ideas and ambiguity
- not comfortable with implementation details or troubleshooting workflow state
- wants the tool to feel trustworthy, legible, and recoverable
- expects "cancel", "accept", "revert", "discussion", and "template" to mean what they sound like

## Flows exercised

### CLI

- `roots`
- `walk`
- `overview`
- `apply notice --backend claude --dry-run`
- `apply notice --backend claude -y`
- `new`
- `apply elaborate --backend claude -y`
- `accept`
- `apply decompose --backend claude -y`
- `accept --with ... -y`
- `pending`
- `log`
- `tree`
- `prepare`
- `submit`
- `reject`
- `new --template pros-cons`
- `revert`

### REPL

- `.info`
- `walk`
- `note` mode
- `.backend claude`
- `elaborate`
- `accept` via `y/n/r`
- revision flow in REPL
- `discuss`
- `.cancel`
- `.quit`

## Notes and artifacts created during testing

The sandbox worktree ended clean, but the test run created committed history and content in the sandbox repo.

Notable sandbox artifacts:

- `pilot neighborhood selection.md`
- `discussion - pilot neighborhood selection.md`
- `membership-first launch strategy analysis.md`
- `membership-first launch strategy pros.md`
- `membership-first launch strategy cons.md`
- `membership-first launch strategy synthesis.md`

Recent sandbox commits created during testing:

- `06ed9a4` `sevens: new node "Pilot Neighborhood Selection"`
- `6a9376f` `sevens: note on "Pilot Neighborhood Selection"`
- `6da30dd` `sevens: apply elaborate to "Pilot Neighborhood Selection"`
- `530c899` `sevens: apply discuss to "Pilot Neighborhood Selection"`
- `812a48e` `sevens: new from template pros-cons`
- `4be880b` `sevens: revert elaborate on "Pilot Neighborhood Selection"`

## Confirmed hands-on findings

### 1. Claude backend selection is not workflow-stable

Severity: High

When I ran:

`sevens apply notice "The Commons" --backend claude`

the command still tried to perform cost confirmation via Anthropic token counting. Because the run was non-interactive, it aborted unless I passed `-y`.

Observed behavior:

- `--backend claude` did not imply a fully Claude-native path.
- cost confirmation still depended on Anthropic API availability and quota.
- a non-technical user would read this as "I chose Claude; why is Anthropic blocking me?"

User-facing failure mode:

- backend choice feels fake or only partially respected.

### 2. Shared DB locking is easy to trigger

Severity: Medium

Running sevens commands in parallel against the shared DB produced lock errors immediately.

Observed behavior:

- one dry-run `apply` worked
- the second parallel dry-run failed with `Locking error: Failed locking file`

User-facing failure mode:

- if a user has multiple sevens processes, or a REPL plus CLI, or background automation, they will get opaque DB lock failures.

### 3. Sparse-note `elaborate` can return `[]`, and sevens treats it like a normal proposal

Severity: High

On a newly created blank-ish note, `elaborate` returned:

```json
[]
```

Observed behavior:

- sevens saved it as a pending suggestion
- `accept` treated it as a normal apply flow
- the CLI then attempted a git commit and reported "nothing to commit"
- the note remained unchanged

User-facing failure mode:

- the user thinks something meaningful happened because the system talks like it did
- there is no explicit "no changes proposed" state

This is one of the most important product-quality issues for non-technical users. Empty output should not look like a successful transformation.

### 4. CLI accept will try to commit even when nothing changed

Severity: Medium

In the no-op elaborate case, CLI `accept` still attempted a git commit and emitted a failure about there being nothing to commit.

Observed behavior:

- noisy and confusing
- makes "accept" feel flaky even when the real issue is "no file changes"

User-facing failure mode:

- "Why did accepting this suggestion fail if nothing was wrong?"

### 5. CLI revision still drops the revised pending state

Severity: High

I ran a live multi-step decompose flow on `The Commons`, then revised the suggestion using:

`sevens accept "The Commons" --with "..."`

Observed behavior:

- the revised suggestion was printed successfully
- `log` showed the new suggested output
- `pending` did not show a corresponding decompose pending item afterward

This confirms in practice that the CLI revision path still does not preserve revised pending state the way the REPL does.

### 6. `accept <node>` is unsafe when a node has multiple stale pending items

Severity: Critical

This was the most dangerous hands-on behavior I hit.

After revising `decompose` on `The Commons`, I ran:

`sevens accept "The Commons"`

Observed behavior:

- it did not continue the revised decompose flow
- instead, it acted on an older pending `discuss` item on the same node
- the node-level command target was too coarse to tell the user what it was about to accept

User-facing failure mode:

- the user thinks they are accepting one suggestion
- sevens accepts a different one because pending state is keyed and resolved too loosely

For a non-technical user, this destroys trust immediately.

### 7. `pending` is cluttered and ambiguous when stale suspensions accumulate

Severity: High

The sandbox already had many stale pending entries, especially repeated `discuss` items on `The Commons`.

Observed behavior:

- `pending` printed many nearly identical rows
- there was no obvious unique handle or timestamp to act on
- it was not clear which entry `accept` or `reject` would target

User-facing failure mode:

- impossible to confidently manage review state from the CLI

### 8. REPL revision flow is materially better than CLI revision flow

Severity: Positive finding with caveat

In the REPL, I:

- wrote note content
- ran `elaborate`
- chose `r`
- supplied revision feedback
- received a revised proposal
- then accepted it with `y`

Observed behavior:

- the loop behaved coherently
- the revised elaboration applied cleanly
- this was one of the few places where the product felt genuinely usable as a thinking partner

Caveat:

- the proposal preview was still too thin
- for edit ops, the review surface showed only `~ Pilot Neighborhood Selection`, not the proposed textual change

### 9. REPL note mode is functionally useful but visually hostile for pasted text

Severity: Medium usability issue

I pasted three short paragraphs into note mode.

Observed behavior:

- content saved successfully
- but the terminal output became a huge wall of echoed redraw noise

User-facing failure mode:

- a non-technical writer pasting a paragraph will feel like they broke the terminal

### 10. Discussion cancel is not real cancel

Severity: Critical

I tested REPL `discuss` on `Pilot Neighborhood Selection`, allowed the initial agent turn to generate, then immediately ran `.cancel`.

Observed behavior:

- sevens printed `discussion discarded`
- the discussion child file still existed
- the generated agent turn was still in the file
- the git commit that created it still existed
- the tree still showed the discussion child

This is a direct product lie.

User-facing failure mode:

- the user explicitly says "cancel"
- sevens says it canceled
- the artifact remains in both filesystem and history

### 11. Discussion mode auto-creates and commits before the user has opted in

Severity: High

Starting `discuss` immediately:

- ran the model
- created a discussion child
- auto-accepted it
- committed it
- only then entered interactive conversation mode

This is not inherently wrong, but in combination with broken cancel semantics it becomes risky.

User-facing failure mode:

- the user may think they are merely "opening a discussion"
- in reality sevens has already created durable repo history

### 12. Discuss logging is confusing and semantically muddy

Severity: Medium

After the discussion test, `log "Pilot Neighborhood Selection"` showed the discussion event as:

- a `suggested` event
- carrying created-file and commit semantics

That is conceptually muddy. It reads like something halfway between suggested and applied.

User-facing failure mode:

- the log is not a reliable mental model of what actually happened

### 13. `prepare` is one of the better surfaces in the product

Severity: Positive finding

The `prepare decompose "The Commons"` output was clear and useful:

- explicit step list
- rendered instruction text
- exact follow-up submit commands

This is close to something I would want as a serious user.

Main caveats:

- it does not visibly carry backend choice through the rest of the workflow
- the generated submit examples do not help with later approval ambiguity

### 14. `submit` works for agent-mode intake, but backend continuity breaks after that

Severity: Medium

I successfully:

- prepared `decompose` for `Pilot Neighborhood Selection`
- wrote a suggestion file by hand
- submitted it via `sevens submit ... --step suggest --output suggestions`

That part worked.

But the next stage exposed a design gap:

- `accept` has no backend flag
- if the pipeline needs to continue with live inference, backend choice falls back to global config, not the backend implied by the workflow

User-facing failure mode:

- agent-mode workflows do not have stable execution context from one step to the next

### 15. Template-based note creation is useful and promising

Severity: Positive finding

I created a pros/cons subtree under the scratch note.

Observed behavior:

- the structure was created correctly
- parent/child wiring looked right
- sibling roles were written into frontmatter

This felt like a good fit for a non-technical thinker, especially someone doing argument mapping or decision framing.

Caveats:

- output is a bit noisy and repetitive (`[apply] Created` plus `[new] Created`)
- there is no lightweight preview before creation

### 16. Revert works, but only in a narrower and less intuitive sense than a normal user would expect

Severity: High

I ran `revert "Pilot Neighborhood Selection"` after the elaborate and discuss tests.

Observed behavior:

- revert chose the last applied elaboration commit, not the later discussion commit
- the elaborated note content was restored to a simpler note version
- the discussion child created by the supposedly canceled discuss flow remained untouched

User-facing failure mode:

- "Revert the last thing sevens did to this note" is not what the tool actually means
- it means something closer to "revert the last log entry with a matching applied commit on this target"

That distinction is far too subtle for the intended user persona.

## Feature enhancements I actively wanted while using the tool

These are not just abstract ideas. These are things I wanted in the moment while testing.

### 1. Accept/reject specific pending items, not just by node title

Needed shape:

- `sevens pending` should show stable IDs, timestamps, function names, and summaries
- `sevens accept <pending-id>` should exist

This would directly prevent the "accepted the wrong pending item" failure.

### 2. Real preview/diff before approval

For ops, I wanted:

- exact file names
- for edits, visible old/new text diff
- for creates, first 10-20 lines of proposed file

The current preview is too abstract for safe review.

### 3. A real no-op state

If the model returns `[]` or otherwise produces no changes, the product should say:

- `No changes proposed`
- optionally why
- and skip accept/apply/commit machinery

### 4. Backend pinning across the whole workflow

Once I choose `claude`, I want:

- `apply`
- `accept`
- `revise`
- `prepare`
- `submit` continuation
- `repl`

all to use Claude unless I explicitly change it.

Right now backend choice feels per-command and brittle.

### 5. True cancel semantics

For note mode and discussion mode, I want cancel to mean:

- nothing persisted
- no file remains
- no commit remains

If that cannot be guaranteed, the product should say so explicitly.

### 6. Better note-mode paste handling

For long pasted text, I wanted:

- clean multiline capture
- minimal terminal redraw noise
- maybe explicit paste mode
- maybe `.end` only, not empty-line submit

### 7. Better recovery and hygiene tools

I wanted commands like:

- `sevens pending --stale`
- `sevens pending --node "The Commons"`
- `sevens pending --function discuss`
- `sevens clear-pending <id>`
- `sevens clean discussion "Node Title"`

The sandbox already had stale pending clutter, and the tool offers almost no cleanup ergonomics.

### 8. A clearer author-facing dashboard

As a non-technical thinker, I wanted one place to answer:

- what am I focused on
- what pending items exist for it
- what changed recently
- what discussion threads are active
- what notes are over length or structurally overloaded

Right now that view is scattered across `status`, `pending`, `log`, `walk`, and validation warnings.

### 9. Better criteria/decision templates

For academic and strategic thinking, I wanted templates like:

- comparison matrix
- research question
- hypothesis and counterargument
- evidence tracker
- pilot-site evaluation memo

The existing template system is promising, but I immediately wanted more domain-specific scaffolds.

### 10. Easier way to fold discussion insights back into structure

The biggest thinking workflow I wanted was:

- discuss a node
- select 2-3 generated insights
- promote them into edits or child notes

Right now that bridge feels partial and fragile.

## Overall product judgment from hands-on use

### What felt strong

- `notice` on a mature node produced genuinely useful thinking feedback.
- REPL revision flow was the closest thing to a strong "AI thinking partner" interaction.
- `prepare` is a good surface.
- templates are promising and practical.

### What felt unsafe

- node-level accept/reject when multiple pending items exist
- discussion cancel
- backend continuity
- silent no-op generation
- git behavior around commits and revert semantics

### What felt most important to fix before wider real use

1. make pending state specific, inspectable, and safely targetable
2. make cancel real or stop calling it cancel
3. make backend selection coherent across full workflows
4. make no-op outputs explicit
5. improve approval previews so users can see what they are saying yes to

## Final sandbox state

- sandbox git worktree: clean
- sandbox history: modified by the test run
- scratch artifacts: still present and committed

If cleanup is desired, it should be done deliberately rather than assumed, because at least one "cancel" path already proved non-reversible in the user-facing sense.

## Remediation handoff

This section is intentionally terse and implementation-oriented so another agent can pick it up quickly.

### P0. Make pending items addressable and safe

Intent:

- stop `accept <node>` / `reject <node>` from acting on an arbitrary pending item
- make pending review specific, inspectable, and deterministic

Likely touchpoints:

- [internal/engine/engine.go](/Users/dorseyj/tools/sevens/internal/engine/engine.go)
- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)
- [internal/repl/repl.go](/Users/dorseyj/tools/sevens/internal/repl/repl.go)
- [internal/repl/dispatch.go](/Users/dorseyj/tools/sevens/internal/repl/dispatch.go)
- [internal/ui/render.go](/Users/dorseyj/tools/sevens/internal/ui/render.go)

Expected shape:

- give each suspension a durable ID or expose the existing subject cleanly
- `pending` should show timestamp, function, step, summary, and ID
- `accept` / `reject` should accept either a suspension ID or an unambiguous single pending item
- if multiple pending items exist for a node, refuse the ambiguous form and ask for the specific item

Tests to add:

- multiple pending suspensions on one node: `accept <node>` should fail with ambiguity
- `accept <suspension-id>` should resolve the intended suspension only
- `pending` output should include stable identifiers
- REPL `accept` loop on a node with multiple pendings should force disambiguation

### P0. Make discussion cancel truthful

Intent:

- `.cancel` must either fully roll back the discussion artifact or stop claiming it did

Likely touchpoints:

- [internal/repl/discuss.go](/Users/dorseyj/tools/sevens/internal/repl/discuss.go)
- [internal/repl/repl.go](/Users/dorseyj/tools/sevens/internal/repl/repl.go)
- possibly [internal/apply/git.go](/Users/dorseyj/tools/sevens/internal/apply/git.go)

Expected shape:

- track whether the discussion file was newly created vs edited
- on cancel:
  - remove newly created discussion files
  - restore edited files correctly
  - avoid relying on absolute pathspec checkout
- if the creation was already committed, either:
  - avoid committing before the user opts into saving, or
  - reword the flow so "cancel" means "exit without further turns" rather than "discard"

Tests to add:

- non-git root: discuss then cancel should not leave the discussion file behind if it was created in-session
- git root: discuss then cancel should restore an edited discussion file to pre-session content
- git root: cancel should work when file paths are absolute on disk
- regression test that `.cancel` does not leave a created discussion child in the tree

### P0. Fix CLI revision lifecycle

Intent:

- CLI `accept --with ...` should preserve a new pending suspension, just like the REPL path

Likely touchpoints:

- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)
- [internal/engine/engine.go](/Users/dorseyj/tools/sevens/internal/engine/engine.go)

Expected shape:

- after successful revision, write the new suspension before resolving the old one
- keep CLI and REPL on the same suspension lifecycle semantics

Tests to add:

- revise a suggestions step via CLI, then `FindSuspension` should return the revised suspension
- revise an ops step via CLI, then `accept` should apply the revised ops, not lose them
- old suspension status should become `revised`

### P0. Make backend choice stable across a workflow

Intent:

- once the user selects `claude`, follow-on steps should not silently fall back to unrelated backend assumptions

Likely touchpoints:

- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)
- [internal/repl/repl.go](/Users/dorseyj/tools/sevens/internal/repl/repl.go)
- [internal/engine/engine.go](/Users/dorseyj/tools/sevens/internal/engine/engine.go)
- [internal/apply/confirm.go](/Users/dorseyj/tools/sevens/internal/apply/confirm.go)
- [internal/backend/factory.go](/Users/dorseyj/tools/sevens/internal/backend/factory.go)

Expected shape:

- `accept` should take `--backend`
- `prepare`/`submit` continuation should preserve backend choice
- cost confirmation should not require Anthropic-specific APIs when using CLI backends, or it should degrade cleanly

Tests to add:

- CLI apply with `--backend claude` followed by `accept --backend claude` should use Claude for continuation
- cost confirmation path for CLI backends should not call Anthropic token counting if backend is not Anthropics API
- REPL backend switch should persist across apply → revise → accept

### P1. Handle no-op model outputs explicitly

Intent:

- `[]` or empty parsed ops should be surfaced as "no changes proposed", not as a normal review/apply flow

Likely touchpoints:

- [internal/engine/engine.go](/Users/dorseyj/tools/sevens/internal/engine/engine.go)
- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)
- [internal/repl/repl.go](/Users/dorseyj/tools/sevens/internal/repl/repl.go)

Expected shape:

- if output type is `ops` and parsed ops length is zero:
  - do not write an actionable suspension
  - log a no-op or completed-without-changes event
  - explain clearly to the user that nothing was proposed

Tests to add:

- backend returns ```json [] ``` for ops
- CLI apply should not instruct the user to accept no-op output
- REPL should not enter y/n/r for a no-op ops result
- git commit should not be attempted after accepting a no-op

### P1. Improve approval previews

Intent:

- users need to see what they are approving, especially for edit ops

Likely touchpoints:

- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)
- [internal/repl/repl.go](/Users/dorseyj/tools/sevens/internal/repl/repl.go)
- [internal/ui/render.go](/Users/dorseyj/tools/sevens/internal/ui/render.go)

Expected shape:

- for create ops: preview title + first lines of content
- for edit ops: show old/new snippet or unified diff
- for suggestions: render cleanly even when returned as JSON

Tests to add:

- snapshot tests for create preview
- snapshot tests for edit preview
- REPL `y/n/r` should display meaningful diff context before prompt

### P1. Fix agent-mode step indexing

Intent:

- `submit --step ...` must persist the real step index so `accept` resumes the correct next stage

Likely touchpoints:

- [cmd/sevens/main.go](/Users/dorseyj/tools/sevens/cmd/sevens/main.go)
- [internal/engine/engine.go](/Users/dorseyj/tools/sevens/internal/engine/engine.go)
- maybe [internal/apply/types.go](/Users/dorseyj/tools/sevens/internal/apply/types.go)

Expected shape:

- resolve step name against the function definition and store the actual index
- reject unknown step names

Tests to add:

- submit for `suggest` on a two-step function should store step index 0
- submit for `generate` on the same function should store step index 1
- accept after submitted step 0 should continue to step 1
- accept after submitted final step should apply final output, not continue incorrectly

### P1. Tighten git semantics

Intent:

- sevens should commit only what it changed, not the whole repo

Likely touchpoints:

- [internal/apply/git.go](/Users/dorseyj/tools/sevens/internal/apply/git.go)
- all call sites in CLI/REPL that commit after apply/new/discuss/revert

Expected shape:

- pass explicit file lists into commit helper
- keep sync commits separate from user worktree changes
- make revert operate on accurately tracked changed files

Tests to add:

- dirty unrelated file in repo + sevens apply on target note
- sevens commit should include only touched sevens files
- revert should not disturb unrelated dirty files

### P2. Improve note-mode paste ergonomics

Intent:

- pasted thought-dumps should feel stable and readable

Likely touchpoints:

- [internal/repl/repl.go](/Users/dorseyj/tools/sevens/internal/repl/repl.go)

Expected shape:

- consider explicit multiline capture mode without per-character redraw noise
- prefer `.end` to terminate note mode instead of empty-line submit

Tests to add:

- REPL integration test with multiline input
- ensure pasted blank lines are preserved
- ensure `.cancel` and `.end` semantics remain clear
