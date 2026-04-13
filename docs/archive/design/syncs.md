# Sevens Synchronization Rules

Status: draft v1
Describes how the core concepts (Graph, GraphOps, KnowledgeBase, Function,
Projection) compose through explicit synchronization rules.

---

## Why Syncs Matter Here

The concept specs define what each concept does independently. The syncs
define what happens when they work together. In the current code, sync
logic is scattered across CLI commands, REPL handlers, and bridge code.
Making it explicit reveals the coordination rules and ensures CLI and
REPL implement the same behavior.

---

## Sync: ApplyFunction

The core workflow: user applies a function to a node.

```
sync ApplyFunction
when
  CLI.apply (functionName, nodeTitle)
where
  in Function: fn loaded from functionName
  in KnowledgeBase: target node exists for nodeTitle
then
  Function.apply (fn, target, backend)
  -- if pipeline suspends:
  Function.PipelineStore.save (pipeline)
  KnowledgeBase.appendLog (step-completed)
  -- if pipeline completes with text:
  KnowledgeBase.appendLog (completed)
  -- if pipeline completes with file-ops:
  -> triggers AcceptOps sync
```

---

## Sync: AcceptOps

When a pipeline produces file operations and they're accepted.

```
sync AcceptOps
when
  Function.accept (pipelineID)
where
  in Function: pipeline result has ops
then
  Projection.applyOps (root, ops)
  Projection.commit (root, message)
  Projection.sync (root)
  KnowledgeBase.appendLog (applied, commit, filesCreated, filesEdited)
```

This is the sync that's currently inline in `acceptCmd2` (lines 226-259
of pipeline_cmds.go). It coordinates three concepts: Function produces
ops, Projection materializes them as files and commits, KnowledgeBase
records the history.

---

## Sync: RejectPipeline

```
sync RejectPipeline
when
  CLI.reject (pipelineID)
then
  Function.reject (pipelineID)
  KnowledgeBase.appendLog (rejected)
```

---

## Sync: RevisePipeline

```
sync RevisePipeline
when
  CLI.accept (pipelineID, feedback)
where
  feedback is non-empty
then
  Function.revise (pipelineID, feedback, backend)
  KnowledgeBase.appendLog (revised)
```

---

## Sync: SyncFiles

When the user runs `sevens sync`.

```
sync SyncFiles
when
  CLI.sync (root)
then
  Projection.commit (root, "sevens: sync")
    -- pre-commit any uncommitted changes
  Projection.sync (root)
    -- parse files, reconcile, write triples
  KnowledgeBase.validate (root)
    -- check structural health
```

---

## Sync: RevertOperation

```
sync RevertOperation
when
  CLI.revert (nodeTitle)
where
  in KnowledgeBase: last "applied" log entry with commit hash
then
  Projection.revert (root, commitHash)
  Projection.commit (root, "sevens: revert ...")
  KnowledgeBase.appendLog (reverted)
  Projection.sync (root)
```

---

## Sync: CreateFromTemplate

```
sync CreateFromTemplate
when
  CLI.new (templateName, parentTitle, vars)
then
  Function.applyDeterministic (template, vars)
    -- produces FileOps
  Projection.applyOps (root, ops)
  Projection.commit (root, "sevens: new from template ...")
  Projection.sync (root)
```

Note: templates are deterministic functions. The sync is the same shape
as AcceptOps but without the gate/review cycle.

---

## Sync: PipelineTransitionLog

Every pipeline state transition should produce a log entry. This is a
cross-cutting sync.

```
sync PipelineTransitionLog
when
  Function.pipeline transitions to any new phase
where
  transition is (accept | reject | revise | complete | cancel)
then
  KnowledgeBase.appendLog (event=transition, function, node, step, timestamp)
```

Currently partially implemented: `executeStep` in executor.go logs
"step-completed", but accept/reject/revise logging is done in the CLI
commands rather than in the executor. This sync should be enforced at
the executor level.

---

## Sync: FocusSession

```
sync FocusSession
when
  CLI.focus (nodeTitle, includes, excludes)
where
  in KnowledgeBase: node exists
then
  KnowledgeBase.startSession (nodeSubject)
  KnowledgeBase.addInclude (sessionSubject, includeSubjects)
```

Currently: focus persists to an EDN file (apply.SaveSession), not to
the graph. The sync should write session state as triples once the
EDN file is retired.

---

## What the CLI Is

The CLI is not a concept. It is the **bootstrap pseudo-concept** in
WYSIWID terms -- the entry point that initiates actions and coordinates
syncs. It corresponds to the `Web/request` concept in the WYSIWID paper.

The CLI's responsibilities:
1. Parse user input (cobra commands, flags, arguments)
2. Resolve context (root directory, focused node, "." shorthand)
3. Initiate syncs by calling concept actions in the right order
4. Present results (formatting, terminal rendering)

The CLI owns NO state and NO domain logic. Everything it does is
calling concept actions and coordinating syncs. If the CLI contains
domain logic (e.g., deciding what constitutes a "valid" node, or how
to construct a subject string), that logic belongs in a concept.

### CLI-specific concerns (not concepts)

- **Root resolution**: walking up from cwd to find `.sevens.edn`.
  This is a filesystem heuristic, not domain logic. Could be a utility
  function in the projection package.
- **Node title resolution**: expanding "." to the focused node title.
  This reads session state (KnowledgeBase concern) but the resolution
  itself is CLI convenience.
- **Tab completion**: reading node titles and function names for shell
  completion. Pure presentation.
- **Cost confirmation**: prompting before expensive LLM calls. This is
  an interaction policy, not a concept. It gates the backend call but
  doesn't affect concept state.

---

## What the REPL Is

The REPL is a second bootstrap pseudo-concept, like the CLI but with
persistent in-memory state across commands. It initiates the same syncs
as the CLI.

The REPL's additional concerns beyond CLI:
- **Ephemeral navigation state**: last list, last blocks, numbered
  selection. Dies with the process. Not concept state.
- **Mode switching**: normal mode, discussion mode, note mode. These
  are interaction modes that determine which syncs fire on user input.
- **Auto-resync**: the REPL re-syncs before most commands. This is a
  tightened version of the SyncFiles sync, triggered automatically
  rather than explicitly.

### Discussion mode as a sync pattern

Discussion mode in the REPL is:

```
sync EnterDiscussion
when
  REPL.discuss (nodeTitle)
then
  Function.apply (discussFn, nodeTitle, backend)
    -- discussFn is a looping function
  Projection.applyOps (root, createDiscussionNode)
  Projection.commit (root, "sevens: discussion draft")
  -- pipeline enters Looping phase

sync DiscussionTurn
when
  REPL.userInput (text) during discussion mode
then
  -- append user turn to discussion file
  Projection.applyOps (root, appendTurn)
  Function.continueLoop (pipelineID, backend)
  Projection.applyOps (root, appendAgentTurn)

sync EndDiscussion
when
  REPL.end () during discussion mode
then
  Function.endLoop (pipelineID)
  Projection.commit (root, "sevens: discussion on ...")
  Projection.sync (root)
  KnowledgeBase.appendLog (discussion-completed)

sync CancelDiscussion
when
  REPL.cancel () during discussion mode
then
  Function.cancel (pipelineID)
  Projection.revert (root, draftCommit)
  Projection.sync (root)
```

This makes explicit what the current REPL code does implicitly across
~200 lines in internal/repl/discuss.go.

---

## What the Markdown Format Is

The markdown format is an implementation of the Projection contract,
not a concept. But it has design decisions that should be documented:

### Frontmatter conventions

```
---
title: Node Title           # required; becomes node/title predicate
parent: "[[Parent Title]]"  # optional; becomes node/parent predicate
sibling-role: support       # optional; becomes node/role predicate
include-group: true         # optional; triggers group auto-includes
---
```

The frontmatter is **redundant with graph state** -- it's an ergonomic
shortcut so the tool can discover relationships without full-body
parsing. On sync, frontmatter is the source of truth for these fields.
On write (graph -> file), frontmatter is regenerated from graph state.

### Wiki-link syntax

`[[Target Title]]` in body text creates a `node/link` predicate.
Extracted during parsing, not stored in frontmatter.

### Filename conventions

Titles are sanitized to lowercase kebab-case .md filenames:
`"Commons Governance Models"` -> `commons-governance-models.md`.
This is a one-way function -- you can't recover the original title
from the filename. The title lives in frontmatter.

### Block structure (future)

When the DSL extensions are present, the markdown parser decomposes
the document body into blocks (headings, paragraphs, list items, tasks)
with stable identity tracked across edits. This is entirely within the
projection -- the graph sees blocks as nodes with `block/*` predicates.

---

## What the Backend System Is

The backend system (LLM, deterministic, agent) is not a concept. It's
the **execution mechanism** behind the Function concept's
TransformBackend interface.

### Backend selection

```
resolution order:
  1. explicit --backend flag
  2. function-level backend config
  3. global config default
  4. "anthropic" fallback
```

### Prompt construction pipeline

For LLM backends, a prompt goes through:
1. **Context resolution** (Function concept): gather graph context
   based on step's Requires and PathSpecs
2. **Template rendering** (Function concept): substitute `{{variables}}`
   into the prompt template
3. **System prompt assembly** (backend-specific): combine persona,
   system prompt, context policy preamble
4. **Capability injection** (backend-specific): MCP server references,
   tool allowlists, file access permissions
5. **Transport** (backend-specific): API call, CLI subprocess, or
   agent checklist

Steps 1-2 are concept logic (Function). Steps 3-5 are backend
implementation detail.

### Agent mode

Agent mode is a backend where `Execute` doesn't call an LLM. Instead:
- `prepare` renders the prompt as a human-readable checklist
- The external AI reads the checklist, does the work, produces output
- `submit` provides a TransformResult that enters the pipeline

This is the same pipeline, same gates, same state machine. The only
difference is who computes the transformation.

---

## Open Questions

1. **Should syncs be enforced in the Executor rather than the CLI?**
   Currently the CLI orchestrates the AcceptOps sync (calling
   Projection.applyOps, commit, sync, and KB.appendLog). If the
   Executor did this, the REPL would get the same behavior for free.
   But it would make the Executor depend on Projection, which is
   currently outside its dependency tree.

2. **Where does cost confirmation live?** It gates the backend call
   but isn't a concept action. Current code does it in the CLI before
   calling the executor. It could be a pre-hook on TransformBackend,
   or a separate concern entirely.

3. **EDN session file retirement**: the session syncs currently write
   to an EDN file for REPL compat. Once sessions are triple-based,
   the FocusSession sync writes to KB directly and the EDN file goes
   away.

4. **Auto-resync in REPL**: the REPL re-syncs before most commands.
   Should this be an explicit sync rule ("before any query, ensure
   graph is fresh") or an implementation detail of the REPL?
