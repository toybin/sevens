# Architecture

How the packages compose. The codebase follows a 4-layer concept architecture derived from the concept design specs in `docs/design/`.

## Package dependency graph

```
cmd/sevens
  ├─ internal/workflow        (orchestration: composes concept actions)
  │    ├─ internal/function
  │    ├─ internal/kb
  │    └─ internal/projection/md
  ├─ internal/repl
  │    ├─ internal/function
  │    ├─ internal/kb
  │    ├─ internal/projection/md
  │    ├─ internal/backend
  │    ├─ internal/config
  │    └─ internal/ui
  ├─ internal/function
  │    ├─ internal/function/picker      (picker expression AST + evaluator)
  │    ├─ internal/kb
  │    ├─ internal/graphops
  │    ├─ internal/triple
  │    ├─ internal/types
  │    ├─ internal/types/kernel         (runtime type kernel, validator, schema)
  │    ├─ internal/sevtypes
  │    └─ internal/ednformat
  ├─ internal/kb
  │    ├─ internal/graphops
  │    ├─ internal/triple
  │    └─ internal/sevtypes
  ├─ internal/projection/md
  │    ├─ internal/kb
  │    ├─ internal/triple
  │    └─ internal/types
  ├─ internal/projection/edn
  │    ├─ internal/kb
  │    ├─ internal/triple
  │    └─ internal/ednformat
  ├─ internal/graphops
  │    └─ internal/triple
  ├─ internal/triple          (no internal deps)
  ├─ internal/backend         (no internal deps)
  ├─ internal/config          (no internal deps)
  ├─ internal/ui              (no internal deps)
  ├─ internal/sevtypes        (no internal deps)
  ├─ internal/ednformat       (no internal deps)
  └─ internal/types
       └─ internal/sevtypes
```

## Layer model

### Layer 1: `triple` -- bare triple store

Pure persistence. Everything is `(subject, predicate, object)` strings in SQLite. No domain knowledge, no predicate semantics.

Key types: `Triple`, `Store`.

Operations: `Assert`, `Retract`, `RetractBySubject`, `ByPredicateObject`, `Lookup` -- basic CRUD over triples. The store also provides `Search` for full-text queries.

### Layer 2: `graphops` -- predicate metadata and path composition

Knows about predicate properties (functional vs. relational, inverses, symmetry) and path composition, but nothing about sevens or PKM. Reusable outside this project.

Key types: `Graph`, `PredicateSpec`, `Multiplicity`.

Operations: `RegisterPredicate` declares predicate metadata. `Set` enforces functional multiplicity (retracts old value before asserting). `Compose` walks multi-hop paths via predicate chains. `Reachable` computes transitive closure. `Lookup` does single-predicate reads with functional/relational awareness.

### Layer 3: `kb` -- PKM domain model

The layer that makes sevens *sevens*. Imposes the structure of organized thinking onto the graph: nodes in trees, bounded breadth, navigable relationships.

Key types: `KB`, `Session`, `WalkResult` (via `sevtypes`), `OverviewNode` (via `sevtypes`).

Responsibilities:
- **Node CRUD** -- `CreateNode`, `DeleteNode`, `SetContent`, `MoveNode` with cycle detection.
- **Cross-references** -- `LinkNodes`, `UnlinkNodes` for inter-node relationships.
- **Walk** -- `Walk` gathers a focused node's neighborhood (parent, children, siblings, cross-refs, subtree) shaped by a `GatherSpec`. This is the primary input to function execution.
- **Overview** -- `Overview` returns the full tree structure for a root.
- **Sessions** -- `StartSession`, `EndSession` for working context tracking.
- **Roots** -- `RegisterRoot`, `ClearRoot` for root lifecycle.
- **Predicates** -- defines all sevens-specific predicate specs (`node/title`, `node/parent`, `node/content`, `block/heading`, `session/focus`, etc.) and registers them with the graphops layer.

### Layer 4: `function` + `projection` + `types` + `types/kernel` + `function/picker` -- transforms and surface formats

Five packages that operate on top of the KB:

**`function`** -- typed transformations on the knowledge base, composed as curried pipelines with gates and control flow.

Key types: `Function`, `Step`, `Executor`, `PipelineStore`, `Pipeline`, `TransformBackend`.

A `Function` is a named sequence of `Step`s. Each step declares what context to gather (`Require`, `PathSpec`), what output to produce (`Signature` with `OutputShape`, and optionally a `picker.OutputPicker` for dependent dispatch), how to execute (`BackendSpec`: LLM, deterministic, or agent), and whether to pause for review (`GateSpec`). The `Executor` orchestrates execution: evaluate the picker if present, resolve context, compose the kernel schema instruction, render prompt, call backend, validate the response through the kernel, apply gate. `PipelineStore` persists pipeline state as triples.

**`function/picker`** -- picker expression AST and evaluator for functions with dependent output types. Small closed-vocabulary expression language (twelve constructors: `LitType`, `LitStr`, `If`, `And`, `Or`, `Not`, `Eq`, `Concat`, `TargetTitle`, `ExistsNode`, `HasType`, `PriorOutputType`). A function's EDN declares `:output-picker {:alternatives [...] :expr (...)}`; the executor evaluates the expression before the LLM call and uses the resolved type to drive both schema-instruction injection and parse-time validation. `docs/picker-language.md` is the reference. Currently used by `discuss` to route between `create` and `edit` primitives based on whether a `Discussion - <target>` child exists in the KB.

**`types/kernel`** -- runtime type kernel. The authoritative source for primitive type definitions, schema composition, and output validation. Loads the four primitives (`text`, `create`, `edit`, `suggestion`) from structured EDN files under `internal/types/kernel/primitives/` (embedded via `//go:embed`). Each primitive declares an `:envelope` (scalar or array), `:item-fields` / `:scalar-field`, `:item-constants` (routing tags like `action=create`), and a hand-written `:example` that gets `json.Marshal`'d at load time so the example shown to the LLM is canonical by construction. The same structured data drives `SchemaInstruction` (prompt rendering) and `LocalFields` (validator). `Validate(kb, typeName, value)` walks the composed shape and any refinements (`Intrinsic` or `Contextual`). `IsSubtype`, `Ancestors`, `RootPrimitive`, `ChildTypeOf`, `ComposedShape`, `CollectRefinements` — all cycle-guarded — provide the subsumption and chain-walk machinery. See `docs/sketch/TypesKernel.hs` for the design spec and `docs/design/SESSION-HANDOFF-2026-04-14.md` for current state.

**`projection`** -- the contract between graph state and human-editable forms. The `Projection` interface lives in `internal/projection`; implementations live in sub-packages.

- `projection/md` -- markdown files with YAML frontmatter. `Sync` parses `.md` files into triples. `ApplyOps` executes `FileOp`s (create/edit files). `Commit`/`Revert` wrap git.
- `projection/edn` -- EDN config files (function definitions, type definitions, value models). Syncs `.edn` files into triples so the runtime reads everything from the graph.

**`types`** -- legacy node-level type system. Was the original home for type definitions, conformance queries, projection mappings, and schema instructions. Now mostly superseded by `types/kernel`. Still loaded at runtime and still consulted as a fallback by the executor and preview paths for `:output-type` declarations on functions — but no default function uses `:output-type`, so this path is effectively dead on the current function set. Retained until the context-type unification lands (see `docs/design/SESSION-HANDOFF-2026-04-14.md`).

Key types: `TypeDef`, `PredicateSpec`, `StructureSpec`, `ProjectionSpec`, `GatherSpec`.

### Orchestration: `workflow`, `cmd/sevens`, `repl`

**`workflow`** -- encodes concept synchronization rules as functions. Each workflow composes actions across KB, Function, and Projection in the correct order. Owns no state; coordinates and returns results for the caller to display.

Key type: `Deps` -- bundles `KB`, `MarkdownProjection`, `PipelineStore`, and `TransformBackend`. Constructed once per CLI/REPL invocation.

**`cmd/sevens`** -- CLI entry point. Cobra commands for sync, walk, apply, pipeline management, and REPL launch. `bridge.go` provides the `kbStack` helper that initializes the full `triple.Store` -> `graphops.Graph` -> `kb.KB` stack.

**`repl`** -- interactive shell with focus state, mode switching, and inline navigation.

Modes:
- `ModeNormal` -- command dispatch (dot commands, navigation, function invocation)
- `ModeDiscussion` -- `[you]>` prompt, each line appended as user turn, auto-runs discuss function
- `ModeNote` -- `[note]>` prompt, collects text, appends to node on `.end`

## Supporting packages

**`backend`** -- LLM inference behind the `Backend` interface (`Complete(ctx, InferenceRequest) (string, error)`). Three implementations: `AnthropicBackend` (direct API), `CodexBackend` (shells out to `codex`), `ClaudeBackend` (shells out to `claude`). `FromConfig` factory reads config and returns the right implementation. Also handles MCP server capability wiring.

**`sevtypes`** -- types shared across concept boundaries. `FileOp` (flows from Function to Projection), `WalkNode`, `WalkResult`, `OverviewNode`, `GatherSpec`. These are the exchange vocabulary between concepts; no single concept owns them.

**`ednformat`** -- shared EDN struct types for parsing `.edn` config files. The canonical wire format for EDN-to-Go unmarshaling. Consumer packages (`function`, `projection/edn`) import these instead of defining their own copies.

**`config`** -- loads global configuration from `~/.config/sevens/config.edn`. No graph, function, or LLM dependencies.

**`ui`** -- stateless terminal rendering. Glamour for markdown, lipgloss for styling. `SetTheme` toggles light/dark. All colors use ANSI indices for terminal palette adaptation.

**`types`** -- see Layer 4 above.

## Core flows

### Sync flow (files to triples)

```
sevens sync <root>
  -> projection/md.Sync(root)
       -> ScanFiles(root)           -- find all .md files
       -> parse each file           -- frontmatter + markdown -> triples
       -> kb.ClearRoot(root)        -- retract stale triples
       -> write triples             -- assert new state
  -> projection/edn.Sync(configDir)
       -> scan .edn files           -- function defs, type defs, value models
       -> expand into triples       -- store in graph for runtime queries
```

### Walk flow (query to render)

```
user focuses a node (REPL or CLI)
  -> kb.Walk(root, title, gatherSpec)
       -> graphops.Compose           -- follow predicate paths
       -> collect parent, children, siblings, cross-refs, subtree
       -> return WalkResult
  -> ui.Render(walkResult)           -- glamour markdown rendering
```

### Apply flow (function to backend to files)

```
user invokes function on focused node
  -> function.Executor.Apply(fn, target, instruction)
       -> ResolveContext              -- gather graph neighborhood per step's Requires/Paths
       -> RenderPrompt               -- template substitution (target, children, siblings, etc.)
       -> TransformBackend.Execute   -- LLM call (or deterministic handler, or agent)
       -> parse output               -- JSON FileOps or display text
       -> if gate: create pending pipeline, return for review
       -> if no gate: return result directly
  -> projection/md.ApplyOps          -- create/edit .md files on disk
  -> projection/md.Sync              -- resync files to triples
```

### Pipeline lifecycle (apply to gate to accept/reject/revise)

```
function with gate produces output
  -> PipelineStore.Create            -- persist pipeline state as triples
  -> pipeline enters PhasePending
  -> user reviews output

  accept:
    -> PipelineStore.Advance         -- phase -> PhaseAccepted
    -> projection/md.ApplyOps        -- write files
    -> projection/md.Commit          -- git commit
    -> advance to next step or PhaseCompleted

  reject:
    -> PipelineStore.Advance         -- phase -> PhaseRejected
    -> projection/md.Revert          -- undo changes if RollbackOnReject

  revise:
    -> re-execute step with feedback appended
    -> new result replaces pending output
    -> back to PhasePending for re-review
```

### Discussion flow

```
user types "discuss"
  -> workflow starts discuss function (creates/continues Discussion child node)
  -> auto-accept ops (no review gate -- discussion is conversational)
  -> show agent turns, enter ModeDiscussion
  -> user types text -> append to discussion file -> resync -> run discuss -> show turns
  -> .end -> commit -> exit to ModeNormal
```

## Design references

The concept design specs that motivated this architecture:

- `docs/design/concept-graph.md` -- Layer 1: bare triple store purpose and boundaries
- `docs/design/concept-graph-ops.md` -- Layer 2: predicate metadata, path composition
- `docs/design/concept-knowledge-base.md` -- Layer 3: PKM domain model
- `docs/design/concept-function.md` -- Layer 4: typed transformations and pipelines
- `docs/design/concept-projection.md` -- Contract: presentational surface
- `docs/design/concept-types.md` -- Named predicate patterns
- `docs/design/concept-config.md` -- Global and per-root configuration
