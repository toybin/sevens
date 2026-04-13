# Code Survey: `internal/engine`

Package `engine` is the pipeline execution layer. It drives LLM inference
steps, manages suspension/resume of human-gated workflows, and owns the
suspension record lifecycle in the database.

---

## Exported Types

### `StepResult`

```go
type StepResult struct {
    StepName   string
    Output     string         // raw LLM output
    OutputType string         // "suggestions", "ops", "text"
    Ops        []apply.FileOp // parsed ops if output type is "ops"
}
```

The return value of a completed pipeline step. `Output` is always the raw
string from the LLM. `Ops` is populated only when `OutputType == "ops"`.

---

### `Suspension`

```go
type Suspension struct {
    Subject     string // stable ID, e.g. "suspension:<hash>:<timestamp>:<rand>"
    Root        string
    Function    string
    Target      string
    TargetLabel string
    BlockID     string
    BlockPath   string
    StepName    string
    StepIndex   int
    GateType    string // "approve"
    Output      string // the output to review
    OutputType  string
    Ops         []apply.FileOp
    Summary     string
    Backend     string // backend name used when this suspension was created
}
```

A paused pipeline waiting for human review. Persisted as triples in the
database. `Subject` is the DB identifier. `Target` is the node title.
`TargetLabel` may include a block path suffix (e.g. `"Node#1.0"`).
`BlockID`/`BlockPath` are populated only when the target is a block rather
than an entire node.

---

### `EvalResult`

```go
type EvalResult = mo.Either[Suspension, StepResult]
```

A type alias for `mo.Either`. Left means the pipeline suspended (needs human
input); Right means a step completed successfully. Callers use `.IsLeft()`,
`.MustRight()`, and `mo.Left`/`mo.Right` constructors from the `samber/mo`
package.

---

### `PipelineConfig`

```go
type PipelineConfig struct {
    DB            *sql.DB
    Root          string
    NodeTitle     string
    TargetBlock   *graph.BlockTarget
    Function      *apply.Function
    GlobalConfig  apply.GlobalConfig
    Walk          *graph.WalkOutput
    ContextStr    string
    DryRun        bool
    Confirm       bool
    StreamText    bool
    AllowedSteps  map[string]bool
    Backend       backend.Backend
    ModelOverride string
    Instruction   string
}
```

Everything needed to execute a pipeline. `Walk` is pre-built graph context for
the target node. `ContextStr` is an already-loaded string of context file
content. `AllowedSteps` filters which steps run (nil means run all).
`Backend` is the inference backend; nil falls back to the Anthropic API
directly via `apply.CallLLM`. `DryRun` renders the prompt and returns without
calling the LLM or writing to the DB.

---

### `ReviseConfig`

```go
type ReviseConfig struct {
    DB            *sql.DB
    Root          string
    NodeTitle     string
    Function      *apply.Function
    Suspension    *Suspension
    Feedback      string
    Confirm       bool
    StreamText    *os.File
    Backend       backend.Backend
    GlobalConfig  *apply.GlobalConfig
    ContextStr    string
    ModelOverride string
    Instruction   string
}
```

Parameters for `ReviseStep`. Differs from `PipelineConfig` in that it carries
the existing `Suspension` being revised and a `Feedback` string (the human's
revision note). `GlobalConfig` is a pointer and can be nil (the function will
load it from disk if so). `StreamText` is an `*os.File` rather than a bool.

---

## Exported Functions

### Pipeline execution

```go
func RunPipeline(ctx context.Context, cfg PipelineConfig, startStep int, prev string) EvalResult
```

Runs `cfg.Function`'s steps sequentially starting at `startStep`, passing
each step's output as `prev` to the next. Suspends (returns Left) at any step
with `gate == "approve"` or at the final step if the output type is `"ops"`.
Non-ops final steps return Right (done). `DryRun` renders the prompt and
returns Right after the first step without writing anything to the DB.

```go
func EvalStep(ctx context.Context, cfg PipelineConfig, step apply.Step, stepIndex int, prev string) EvalResult
```

Evaluates a single LLM step. Resolves model, system prompt, context policy,
and agent config; renders the prompt; calls the backend or Anthropic API
directly; parses ops if applicable. Always returns Right (it never suspends
itself — suspension is the caller's responsibility in `RunPipeline`).

```go
func EvalComposedStep(ctx context.Context, cfg PipelineConfig, step apply.Step, stepIndex int, prev string) EvalResult
```

Handles steps that delegate to another function (`step.Fn != ""`) or map a
function over related nodes (`step.MapOver != ""`). For map-over, queries the
triple store for related node titles, runs a sub-pipeline for each, and
aggregates outputs/ops. Propagates the first suspension encountered.

---

### Suspension management

```go
func WriteSuspension(
    db *sql.DB,
    root, nodeTitle, targetLabel string,
    block *graph.BlockTarget,
    function, step, gate, outputType, rawOutput string,
    stepIndex int,
    summary string,
    ops []apply.FileOp,
    backendName string,
)
```

Creates a new `pending` suspension record as triples in the DB. Subject is
generated by `suspensionSubject` (root hash + timestamp + random bytes).

```go
func FindSuspension(db *sql.DB, parts ...string) (*Suspension, string, error)
```

Variadic: accepts `(nodeTitle)` or `(root, nodeTitle)`. Returns the most
recent pending suspension for the node, or `nil, "", nil` if none exists.
Resolved (accepted/rejected) suspensions are ignored.

```go
func FindSuspensionBySubject(db *sql.DB, parts ...string) (*Suspension, error)
```

Variadic: accepts `(subject)` or `(root, subject)`. Looks up a suspension by
its exact DB subject ID. Returns nil if the suspension is not pending, or if
`root` is provided and does not match.

```go
func FindSuspensions(db *sql.DB, parts ...string) ([]Suspension, error)
```

Variadic: accepts `(nodeTitle)` or `(root, nodeTitle)`. Returns all pending
suspensions for a node, ordered by timestamp descending.

```go
func ListSuspensions(db *sql.DB, root string) ([]Suspension, error)
```

Returns all pending suspensions across the DB, optionally scoped to a specific
root directory. Empty `root` returns everything.

```go
func ResolveSuspension(db *sql.DB, subject, status string) error
```

Marks a suspension as resolved by writing `status` (typically `"accepted"` or
`"rejected"`) to the `suspension/status` triple.

---

### Revision

```go
func ReviseStep(cfg ReviseConfig) (*apply.LogEntry, string, error)
```

Re-runs a previously suspended step with human feedback. Logs the revision
note, rebuilds the walk, re-renders the prompt with the prior output as `prev`,
appends the revision history XML, and calls the LLM. Returns the new log entry
and raw LLM output. Does not write a new suspension — the caller is responsible
for replacing or closing the existing one.

```go
func BuildRevisionHistory(db *sql.DB, parts ...any) string
```

Variadic: accepts `(nodeTitle, stepIndex)` or `(root, nodeTitle, stepIndex)`.
Reads the log for the node and assembles a `<previous-attempts>` XML block
containing the suggestion/revision thread for the given step, suitable for
injection into a prompt.

---

## Unexported Internals Worth Knowing

### `suspensionSubject(root string) string`

Generates the DB subject ID for a new suspension. Format:
`suspension:<6-byte SHA1 of root>:<timestamp>:<4-byte random hex>`. The root
hash scopes the ID without embedding the full path.

### `findSuspensionBySubject(db *sql.DB, subject string) (*Suspension, string, error)`

The shared hydration function used by `FindSuspension`, `FindSuspensionBySubject`,
`FindSuspensions`, and `ListSuspensions`. Reads all triples for a subject and
populates a `Suspension` struct, including JSON-unmarshalling the `ops` field.

### `promptVars(cfg PipelineConfig, prev, context string) apply.PromptVars`

Builds the `apply.PromptVars` struct used for template rendering, drawing from
the walk output and (if present) the block target. Determines `TargetKind` as
`"node"` or `"block"`.

### `targetLabel(nodeTitle string, block *graph.BlockTarget) string`

Returns the display label for the pipeline target. For a block target, this
delegates to `block.Label()` (which produces the `"Node#path"` form). For a
node, returns the node title as-is.

### `resolveSuspensionBlock(db *sql.DB, root, nodeTitle string, sus *Suspension) *graph.BlockTarget`

Reconstructs a `*graph.BlockTarget` from a suspension's `BlockID` or
`BlockPath`. Used by `ReviseStep` to restore block context when re-running a
step.

### `summarizeOps`, `summarizeSuggestions`, `joinParts`

Small helpers that generate the human-readable `summary` string stored on log
entries and suspension records.

---

## How the Pieces Fit Together

### Normal forward execution

A caller (typically a CLI command) constructs a `PipelineConfig` and calls
`RunPipeline`. The pipeline iterates over the function's steps:

- For each step, it calls `EvalStep` (or `EvalComposedStep` for delegating/
  mapping steps).
- `EvalStep` renders the prompt, calls the backend, and returns a `StepResult`.
- If the step has a gate or is the last ops step, `RunPipeline` writes a log
  entry via `apply.AppendLogDB`, calls `WriteSuspension` to persist the
  suspension, and returns `Left(Suspension)`.
- The caller receives a `Suspension` and can present the output to the user.

### Resumption after human review

When the user approves or rejects:

- `ResolveSuspension(db, subject, "accepted"/"rejected")` closes the record.
- If the user wants to continue after an accepted ops-output step, the caller
  calls `RunPipeline` again with `startStep = suspension.StepIndex + 1`.

### Revision loop

When the user rejects and wants a different result:

1. Caller calls `ReviseStep(ReviseConfig{..., Feedback: "..."})`.
2. `ReviseStep` logs the feedback as a `"revision"` log entry, re-renders the
   prompt including `BuildRevisionHistory` output, and calls the LLM.
3. The caller receives the new log entry and output, then decides whether to
   write a new suspension (via `WriteSuspension`) or resolve the old one.

### Suspension queries

- `FindSuspension` — get the single most-recent pending suspension for a node
  (used when resuming).
- `FindSuspensions` — all pending suspensions for a node, ordered newest-first.
- `FindSuspensionBySubject` — look up by exact subject ID (used when the UI
  already holds the ID).
- `ListSuspensions` — enumerate all pending suspensions, optionally by root
  (used for a queue/inbox view).
