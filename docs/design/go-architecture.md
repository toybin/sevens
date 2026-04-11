# Go Package Architecture

Status: current as of 2026-04-11

---

## Package Dependency Graph

```
sevtypes                    (shared sync vocabulary: FileOp)
  ↑
triple                      (Layer 1: bare triple store)
  ↑
graphops                    (Layer 2: predicate metadata, path composition)
  ↑
kb                          (Layer 3: PKM domain model)
  ↑               ↑
function        projection/md
  ↑               ↑
  +-- apply*      +-- projection (interface)
  +-- backend*

cmd/sevens                  (CLI bootstrap: syncs between concepts)
  ↑
  +-- all of the above

internal/repl               (REPL bootstrap: same syncs, interactive)
  ↑
  +-- kb, projection/md, plus legacy packages
```

`*` = bridge imports for migration. Will be removed when old packages
are retired.

No circular dependencies. Every arrow points from higher to lower.
The CLI and REPL are the orchestration layers that wire everything
together.

---

## Key Design Decisions

### Concrete types over premature interfaces

The `function.Executor` takes `*kb.KB` directly, not a `KnowledgeBase`
interface. Rationale:

- Go's implicit interface satisfaction means we can add an interface
  later without changing KB or Executor. Adding it now adds ceremony
  without benefit.
- Tests use in-memory SQLite (via turso `:memory:`) which is fast
  enough for unit tests. No mock needed.
- The concept boundary is enforced by package separation and the
  import graph, not by interfaces.

If we later need to mock KB for testing (e.g., testing failure modes),
we define the interface at that point in the function package. Go
makes this a backward-compatible change.

Interfaces ARE used where polymorphism is required:
- `TransformBackend`: LLM, deterministic, and agent backends must be
  swappable. This is real polymorphism.
- `Projection`: markdown, org-mode, webapp are different implementations
  of the same contract. This is real polymorphism.

### Shared types in sevtypes

Types that flow between concepts through syncs live in `sevtypes`.
Currently just `FileOp`. This avoids circular imports -- neither
`function` nor `projection` owns `FileOp`; both import it from the
neutral ground.

The `function` and `projection` packages re-export it as a type alias
(`type FileOp = sevtypes.FileOp`) for convenience. Callers don't need
to know about `sevtypes`.

### Bridge imports

Two files in `function/` import old packages:
- `convert.go` imports `apply` to convert function definitions
- `llmbackend.go` imports `backend` to wrap the existing backend

These are migration adapters, not architectural dependencies. They
will be removed when:
- Function definitions are loaded directly from EDN (no conversion)
- The backend package is absorbed into the function package or
  reimplemented behind the TransformBackend interface

### KB exposes Graph and Store

`kb.KB` has accessor methods: `Graph() *graphops.Graph` and
`Graph().Store() *triple.Store`. This is intentional -- it lets
higher layers reach down when needed (e.g., projection.md building
triples directly) without duplicating the API surface.

This is a pragmatic violation of strict layering. The alternative
(KB wrapping every Store and GraphOps method) would add hundreds of
pass-through methods. The accessor pattern is simpler and the import
graph still prevents cycles.

---

## Package Inventory

| Package | Layer | Purpose | Tests | Key Types |
|---|---|---|---|---|
| `sevtypes` | shared | Sync vocabulary | - | `FileOp` |
| `triple` | 1 | Triple store CRUD | 15 | `Triple`, `Store` |
| `graphops` | 2 | Predicate metadata, path composition | 20 | `Graph`, `PredicateSpec`, `PathStep` |
| `kb` | 3 | PKM domain model | 28 | `KB`, `WalkContext`, `OverviewNode`, `LogEntry`, `Session`, `Violation` |
| `function` | - | Pipeline state machine, executor | 37 | `Function`, `Step`, `Pipeline`, `Executor`, `PipelineStore`, `TransformBackend`, `GateSpec`, `ControlFlow` |
| `projection` | - | Contract interface | - | `Projection` (interface), `SyncResult`, `ApplyResult` |
| `projection/md` | - | Markdown implementation | 23 | `MarkdownProjection`, `ParsedNode`, `Frontmatter` |

---

## Concept-to-Package Mapping

| Concept | Primary Package | Support Packages |
|---|---|---|
| Graph | `triple` | - |
| GraphOps | `graphops` | `triple` |
| KnowledgeBase | `kb` | `graphops`, `triple` |
| Function | `function` | `kb`, `graphops`, `triple`, `sevtypes` |
| Projection | `projection` (interface), `projection/md` (impl) | `kb`, `triple`, `sevtypes` |
| Configuration | `apply` (legacy, not yet migrated) | `store` (legacy) |
| Syncs | `cmd/sevens` (CLI), `internal/repl` (REPL) | all |
