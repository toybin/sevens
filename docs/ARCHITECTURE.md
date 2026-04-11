# Architecture

How the packages compose. Read alongside [API-MAP.md](API-MAP.md) (auto-generated via `scripts/api-map.sh`).

## Package dependency graph

```
cmd/sevens
  └─ internal/repl
       ├─ internal/engine
       │    ├─ internal/apply
       │    ├─ internal/backend
       │    └─ internal/graph
       ├─ internal/apply
       ├─ internal/backend
       ├─ internal/graph
       ├─ internal/store
       └─ internal/ui
```

## Layers

### `store` — Triple store

Pure persistence. Everything is `(subject, predicate, object)` strings in SQLite. No domain knowledge.

- `InsertTriple` / `GetObject` / `GetSubjects` — basic CRUD
- `Compose` / `ComposeInverse` — two-hop graph traversal (morphism composition)
- `SearchContent` / `SearchTitles` — full-text search
- `ConfigDir` / `OpenDB` / `LoadRoots` / `SaveRoots` — filesystem + DB bootstrapping

### `graph` — Filesystem ↔ triples sync

Reads `.md` files with YAML frontmatter, parses wikilinks, populates triples. Also provides graph queries.

- **Sync path**: `FindRoot → LoadConfig → ScanFiles → ParseAllFiles → PopulateTriples`
- **Query**: `BuildWalk` (focused node + local neighborhood), `BuildOverview` (full tree)
- **Validation**: `Validate` checks orphans, missing parents, overflow
- `ResolveGroup` — expands a named group definition into a list of node titles

`BuildWalk` is the "camera" — given a title and depth, returns the node, its parent, children, siblings, and unwalked frontier. This is the primary input to function execution.

### `apply` — Function system

The domain model. Types, loading, rendering, and file-level execution.

**Core types:**
- `Function` — EDN spec + `.md` prompt template(s). Has `Steps` (pipeline) or `Prompt` (single-step).
- `Step` — one pipeline stage with input/output types, optional `Gate` (pause for review), optional `Requires`.
- `Require` — declares what graph context a step needs: `"target"`, `"parent"`, `"children"`, `"siblings"`, `"history"`.
- `ResolvedContext` — the fully-fetched graph neighborhood, ready for template substitution.
- `FileOp` — LLM output: `{"action": "create"|"edit", ...}`.
- `LogEntry` — append-only history of what functions did to a node.

**Loading**: `LoadFunction` reads `<name>.edn` + `<name>.md` / `<name>.<step>.md` from `~/.config/sevens/functions/`.

**Rendering**: Two paths depending on whether the function declares `Requires`:
- `RenderStepPrompt` — simple variable substitution (`{{title}}`, `{{content}}`, `{{parent}}`, `{{children}}`, `{{prev}}`, `{{context}}`, `{{timestamp}}`)
- `ResolveContext → RenderWithContext` — fetches graph neighbors per `Requires`, then substitutes all variables including `{{children-content}}`, `{{siblings}}`, `{{history}}`, `{{cross-walk-output}}`, `{{instruction}}`

**Execution**: `ParseOps` (JSON → `[]FileOp`) → `ExecuteOps` (creates/edits `.md` files on disk).

**Git**: `CommitAll`, `RevertCommit`, `HasChanges`, `IsGitRepo`.

**Log**: `AppendLogDB` / `ReadLogDB` — writes log events as triples (`log:<timestamp>:<node>` subject).

### `engine` — Pipeline orchestration

The state machine that runs functions step by step.

**Core type**: `EvalResult = Either[Suspension, StepResult]`
- `Right(StepResult)` — step completed, pipeline can advance
- `Left(Suspension)` — step hit a gate, paused for human review

**Flow:**
```
RunPipeline(cfg, startStep, prev)
  for each step:
    EvalStep / EvalComposedStep
      → resolve context, render prompt, call backend
      → parse output (ops or text)
      → if step has gate: WriteSuspension → return Left(Suspension)
      → else: feed output as `prev` to next step
```

**Suspension lifecycle:**
1. `WriteSuspension` — creates `suspension:*` triples in DB (target, function, step, raw-output, ops, status=pending)
2. `FindSuspension` — finds most recent pending suspension for a node
3. `ResolveSuspension` — marks status as accepted/rejected/revised
4. `ReviseStep` — re-runs the step with feedback appended, creates new log entry

**Composed steps**: `EvalComposedStep` handles `:fn` (delegate to another function) and `:map-over` (run across multiple nodes).

### `backend` — LLM inference

Pluggable inference behind the `Backend` interface:

```go
type Backend interface {
    Name() string
    Complete(ctx, InferenceRequest) (string, error)
}
```

Three implementations:
- `AnthropicBackend` — direct API calls via `anthropic-sdk-go`
- `CodexBackend` — shells out to `codex exec`
- `ClaudeBackend` — shells out to `claude`

`FromConfig` is the factory: reads `GlobalConfig.Backend` / `GlobalConfig.Backends` and returns the right implementation.

`Capabilities` / `GenerateCodexConfig` / `GenerateClaudeConfig` handle MCP server wiring for backends that support tools.

### `repl` — Interactive shell

Wraps everything into a readline loop with focus state, mode switching, and inline navigation.

**Modes:**
- `ModeNormal` — standard command dispatch
- `ModeDiscussion` — `[you]>` prompt, each line appended as user turn, auto-runs discuss function
- `ModeNote` — `[note]>` prompt, collects text, appends to node on `.end`

**Main loop**: `Run → readline → dispatch`

**Dispatch grammar** (checked in order):
1. Dot commands (`.help`, `.model`, `.include`, `.quit`, etc.)
2. Navigation (`..`, `up`, `root`)
3. Focus by title (bare node title or `focus <title>`)
4. Relative nav (`child 2`, `sibling 1`)
5. Numeric select (bare number from last listing)
6. Named commands (`walk`, `children`, `siblings`, `search`, `pending`, `log`, `accept`, `reject`, `revert`, `overview`, `note`, `discuss`, `new`)
7. Function name (bare word matching a loaded function → `handleApply`)

### `ui` — Terminal rendering

Stateless display helpers. Glamour for markdown, lipgloss for styling. `SetTheme` toggles light/dark.

## Intended composition: the core flow

```
focus node
  → graph.BuildWalk (local neighborhood)
  → user invokes function
  → apply.ResolveContext (gathers what function needs from graph)
  → apply.RenderWithContext (substitutes template variables)
  → engine.RunPipeline → backend.Complete (LLM call)
  → LLM returns JSON → apply.ParseOps
  → engine: gate → WriteSuspension → return Left(Suspension)
  → user reviews → accept → apply.ExecuteOps → files change on disk
  → graph.PopulateTriples (resync: files → triples)
```

## Discussion mode flow

```
user types "discuss"
  → enterDiscussion
  → runPipeline (discuss function creates/continues Discussion child)
  → auto-accept ops (no review gate — discussion is conversational)
  → show agent turns
  → enter [you]> mode
  → user types text → append to discussion file → resync → runPipeline → auto-accept → show turns
  → .end → commit → exit to normal mode
```

## Known bugs (as of 2026-04-08)

1. **`runPipeline` auto-enters `handleAccept`** — but `enterDiscussion` / `handleDiscussionInput` call `runPipeline` then `doAccept` separately. Discussion mode should bypass the y/n/r loop entirely.

2. **Discussion mode swallows commands** — `handleDiscussionInput` only checks `.end`/`.cancel`/`.info`/`.model`. Everything else (`.fns`, `pending`, `discuss`, bare dot commands) gets appended as user text to the discussion file.

3. **Revision creates no new suspension** — after `ReviseStep`, the old suspension is resolved but a new one is only written for ops-type output. The `[y/n/r]>` loop then calls `FindSuspension` and finds nothing. (Partially addressed by adding `WriteSuspension` in the revision path.)

4. **Colons in node titles** — `discuss` creates children titled `"Discussion: <title>"`. Colons break Obsidian wikilinks (`:` is the alias separator). These titles need a different separator.
