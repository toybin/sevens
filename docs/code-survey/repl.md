# Code Survey: `internal/repl`

Package `repl` implements an interactive REPL for sevens. It wraps the same
internal pipeline, graph, and store packages as the CLI, but with persistent
focus state, shorter syntax, and inline navigation.

---

## API Surface

There are two exported symbols. Everything else is unexported methods and
helpers on `REPL`.

### Exported type: `Mode` (`repl.go`)

```go
type Mode int

const (
    ModeNormal     Mode = iota
    ModeNote            // collecting note text
    ModeDiscussion      // multi-turn conversation
)
```

Tracks which input-handling branch the main loop should use. Held as `REPL.mode`.

### Exported type: `REPL` (`repl.go`)

```go
type REPL struct {
    db                 *sql.DB
    root               string
    focus              string            // focused node title; "" = no focus
    focusBlock         *graph.BlockListEntry
    includes           []string          // extra context nodes for apply calls
    dryRun             bool
    modelFlag          string            // model override; "" = use globalCfg default
    backendName        string            // backend override; "" = use globalCfg default
    lastList           []string          // last numbered list printed (for numeric nav)
    lastBlocks         []graph.BlockListEntry
    globalCfg          apply.GlobalConfig
    mode               Mode
    noteLines          []string          // buffer for note mode
    discussNode        string            // "Discussion - <Title>" when in discussion mode
    discussFileCreated bool
    discussFilePath    string
    discussCommit      string            // short commit hash if a commit was made during enterDiscussion
    rl                 *readline.Instance
}
```

All fields are unexported; callers construct and use a `REPL` only through
`New` and `Run`.

### Exported function: `New` (`repl.go`)

```go
func New(db *sql.DB, root string, focusNode string, globalCfg apply.GlobalConfig) (*REPL, error)
```

Constructs the REPL and initializes readline (prompt, history file,
tab-completer). `focusNode` may be `""`. Applies theme from `globalCfg` if
set. Resolves `focusNode` to its canonical case via the store before storing
it.

### Exported method: `Run` (`repl.go`)

```go
func (r *REPL) Run() error
```

Starts the interactive loop. Reads lines via readline, routes mode-specific
input (`ModeNote`, `ModeDiscussion`) before falling through to `dispatch`.
Returns `nil` on EOF or Ctrl-C with empty input.

---

## File-by-file breakdown

### `repl.go` — Core state, main loop, pipeline runner

**Significant unexported functions and methods:**

| Name | Signature | Role |
|---|---|---|
| `prompt` | `(r *REPL) prompt() string` | Builds the readline prompt string from current focus and optional block path. |
| `updatePrompt` | `(r *REPL) updatePrompt()` | Pushes a new prompt string to readline. |
| `setFocus` | `(r *REPL) setFocus(title string)` | Sets focus, clears block focus and last-list state, updates prompt. |
| `setFocusBlock` | `(r *REPL) setFocusBlock(block graph.BlockListEntry)` | Sets the sub-block focus within the current node. |
| `clearFocusBlock` | `(r *REPL) clearFocusBlock()` | Clears block focus, updates prompt. |
| `requireFocus` | `(r *REPL) requireFocus() (string, error)` | Guard: returns current focus or an error if none is set. |
| `runPipeline` | `(r *REPL) runPipeline(nodeTitle string, fn *apply.Function, startStep int, prev string, dryRun bool, opts ...pipelineOpts) error` | The core LLM dispatch path. Builds walk, assembles context (config files, includes, auto-group), initializes backend, resolves block target, configures and calls `engine.RunPipeline`. Handles suspension output (ops or text) and auto-accept. |
| `doAccept` | `(r *REPL) doAccept(nodeTitle, withFeedback string, susSubjectOverride ...string) error` | Accepts a pending suspension: optionally sends revision feedback via `engine.ReviseStep`, then either continues the pipeline to the next step or executes ops. Commits changed files via git. |
| `doReject` | `(r *REPL) doReject(nodeTitle string, susSubjectOverride ...string) error` | Logs a rejection and calls `engine.ResolveSuspension`. |
| `doRevert` | `(r *REPL) doRevert(nodeTitle string) error` | Finds the last applied git commit from the log and reverts it. |
| `enterNote` | `(r *REPL) enterNote() error` | Enters `ModeNote`, sets prompt. |
| `handleNoteInput` | `(r *REPL) handleNoteInput(line string) error` | Buffers lines; `.end`/`.done` commits, `.cancel` discards. |
| `endNote` | `(r *REPL) endNote() error` | Appends buffered note text to the node file under `## Notes`, commits, resyncs. |
| `handleNew` | `(r *REPL) handleNew(title string) error` | Creates a new child node file, commits, resyncs, and re-focuses to the new node. |
| `includeGroup` | `(r *REPL) includeGroup(name string) error` | Resolves a named group from `.sevens.edn` and bulk-adds all member titles to `r.includes`. |
| `resync` | `(r *REPL) resync() error` | Re-scans files and repopulates the triple store. Called after any write. |
| `resyncQuiet` | `(r *REPL) resyncQuiet() error` | Same as `resync` but redirects stderr to `/dev/null` (used for auto-sync before each command). |
| `effectiveCfg` | `(r *REPL) effectiveCfg() apply.GlobalConfig` | Returns `globalCfg` with any session-level model override applied. |
| `validateRootFlag` | `(r *REPL) validateRootFlag(explicit string) error` | Rejects `--root` values that differ from the session root (the REPL is bound to one root). |

**Unexported type:**

```go
type pipelineOpts struct {
    instruction    string  // ad-hoc instruction for {{instruction}}
    skipAutoAccept bool    // don't auto-enter y/n/r after suspension
    suppressHint   bool    // don't print the manual accept hint when skipAutoAccept is set
    blockPath      string
    blockID        string
    includes       []string
}
```

Optional overrides passed to `runPipeline`.

**Package-level helpers:**

- `functionNames() []string` — lists all defined function names from `apply.ListFunctions`.
- `isFunctionName(name string) bool` — checks whether a string is a known function name.
- `resolveSuspensionBlock(...)` — resolves a `graph.BlockTarget` from a suspension's block ID or path.
- `historyFile() (string, error)` — returns the readline history file path under the config dir.
- `nowISO() string` — returns current UTC time as RFC3339.
- `opName(op apply.FileOp) string` — returns the display name for a file op (title > file > "unknown").
- `printEDN(v any) error` — encodes and prints a value as EDN to stdout.

---

### `dispatch.go` — Command grammar and all command handlers

This file contains the full command dispatch switch and every named-command
handler. It is the largest file in the package.

**Main dispatcher:**

```go
func (r *REPL) dispatch(line string) error
```

Implements a seven-level precedence order:

1. Dot commands (`.info`, `.model`, `.quit`, etc.)
2. Navigation keywords (`..`, `up`, `root`, `focus`/`f`, `child`/`c`, `sibling`/`s`)
3. Bare positive integer → numeric selection from last list
4. Named commands (`walk`, `templates`, `capture`, `instantiate`, `inbox`, `blocks`, `diff-blocks`, `extract-block`, `children`, `siblings`, `search`, `pending`, `log`, `accept`, `reject`, `revert`, `overview`, `sync`, `note`, `discuss`, `new`)
5. Function names (any name from `apply.ListFunctions`) → `handleApply`
6. Node title → implicit focus
7. Unknown → error

Before dispatching, auto-resyncs the graph (silently) for most commands.

**Dot command handler:**

```go
func (r *REPL) handleDot(tokens []string) error
```

Handles: `.quit`/`.exit`/`.q`, `.clear`, `.help`/`.h`, `.info`, `.functions`/`.fns`,
`.model`, `.backend`, `.theme`, `.dry`, `.include`, `.exclude`.

**Navigation handlers:**

| Handler | What it does |
|---|---|
| `handleNavUp()` | Walks up to parent, or unfocuses if at root |
| `handleFocusExplicit(title)` | Resolves canonical title and sets focus |
| `handleRelativeNav(rel, indexStr)` | Focuses the Nth child or sibling by 1-based index |
| `handleNumericSelect(n)` | Selects item N from `lastBlocks` (block focus) or `lastList` (node focus) |

**Viewing handlers:**

| Handler | What it does |
|---|---|
| `handleWalk(tokens)` | Prints node header + content; if a block is selected, prints block detail |
| `handleChildren()` | Lists children numbered, stores in `lastList` |
| `handleSiblings()` | Lists siblings numbered, stores in `lastList` |
| `handleSearch(query)` | Title and content search; deduplicates, stores in `lastList` |
| `handlePending()` | Lists all pending suspensions, stores targets in `lastList` |
| `handleLog(tokens)` | Prints log entries for a node |
| `handleOverview()` | Prints full node tree with ASCII connectors |
| `showFocusSummary()` | Prints one-line parent/child/sibling context after a focus change |
| `showFocusedBlock()` | Prints block details for the currently focused block |

**Function application:**

```go
func (r *REPL) handleApply(tokens []string) error
```

Parses inline flags (`--model`, `--backend`, `--dry-run`, `--with`, `--block`,
`--include`), resolves the block target (from flag or current `focusBlock`),
and calls `runPipeline`.

**Accept/reject dispatch:**

```go
func (r *REPL) handleAccept(tokens []string) error
func (r *REPL) handleReject() error
func (r *REPL) resolveSuspensionSubject(nodeTitle string) (string, error)
```

`handleAccept` runs an interactive `[y/n/r]` loop. `y` calls `doAccept`, `n`
calls `doReject`, `r` prompts for revision text and calls `doAccept` with
feedback. Accepts an optional explicit suspension subject ID as the first
argument. `resolveSuspensionSubject` errors with a disambiguation list when
more than one suspension is pending for the node.

**Unexported type — inline flags:**

```go
type inlineFlags struct {
    model          string
    backend        string
    with           string
    root           string
    block          string
    includes       []string
    dryRun         bool
    yes            bool
    nonInteractive bool
}

func (f inlineFlags) has(name string) bool
func parseInlineFlags(tokens []string) inlineFlags
```

Parses `--flag value` and `--flag=value` forms, plus `-n` (non-interactive) and `-y`.

**Utilities:**

- `tokenize(line string) []string` — splits on whitespace, respects double-quoted strings.
- `parsePositiveInt(s string) (int, bool)` — parses a 1+ integer from a token.
- `removeString(slice []string, s string) []string` — in-place slice removal.
- `orDefault(s, def string) string` — returns `def` if `s` is empty.
- `shouldAutoSync(tokens []string) bool` — returns false for `blocks`, `diff-blocks`, `extract-block`, `sync` (avoid double sync).
- `printOverviewTree(output *graph.OverviewOutput, highlightTitle string)` — renders the node tree with `├──`/`└──` connectors.
- `countIn(sub, set []string) int` — counts items from `sub` that appear in `set`.

---

### `blocks.go` — Block-level view and mutation commands

Handlers invoked from `dispatch.go` for the block-oriented commands.

| Function | Signature | Role |
|---|---|---|
| `handleSync` | `(r *REPL) handleSync() error` | Calls `resync()` and prints "synced". |
| `handleBlocks` | `(r *REPL) handleBlocks(tokens []string) error` | Lists blocks for a node; populates `lastBlocks`. Supports `--edn` and `--root` flags. |
| `handleBlockDiff` | `(r *REPL) handleBlockDiff(tokens []string) error` | Shows block-level changes since last sync. Supports `--edn`, `--unchanged`, `--root`. |
| `handleInbox` | `(r *REPL) handleInbox(tokens []string) error` | Lists inbox items with stats. Populates `lastList` with titles. Supports `--edn`, `--root`. |
| `handleExtractBlock` | `(r *REPL) handleExtractBlock(tokens []string) error` | Extracts a block into a new node via `graph.PrepareBlockExtraction` + `apply.ExecuteOps`. Commits, resyncs, and refocuses. |

**Argument parsers** (package-private):

- `blockListArgs(tokens) (root, ednOutput, nodeTitle, err)` — parses flags for `blocks`.
- `blockDiffArgs(tokens) (root, ednOutput, showUnchanged, nodeTitle, err)` — parses flags for `diff-blocks`.
- `inboxArgs(tokens) (root, ednOutput, nodeTitle, err)` — parses flags for `inbox`.
- `extractBlockArgs(tokens, defaultSource, defaultPath, resolveTitle) (root, sourceTitle, blockPath, title, parent, err)` — parses the flexible arg forms for `extract-block`.

**Rendering helpers:**

- `printREPLBlockDiff(output graph.BlockDiffOutput, showUnchanged bool)` — renders the diff grouped by Edited / ScopeChanged / Reordered / Inserted / Deleted.
- `summarizeBlockText(text string, max int) string` — collapses whitespace and truncates with `...`.
- `blockPathLike(s string) bool` — returns true if a string looks like a block path (`"root"` or `"1.2.3"`-style).

---

### `complete.go` — Tab completion

Implements `readline.AutoCompleter` for the REPL prompt.

**Unexported type:**

```go
type completer struct {
    r *REPL
}

func newCompleter(r *REPL) readline.AutoCompleter
func (c *completer) Do(line []rune, pos int) (newLine [][]rune, length int)
```

`Do` dispatches on the current input prefix to provide context-sensitive
completion:

- After `.model ` → model tiers (`fast`, `capable`, `powerful`)
- After `.theme ` → `light`, `dark`
- After `.backend ` → backend candidates from config plus known defaults
- After `.include ` → `"clear"` + node titles + `@group` names
- After `.exclude ` → current includes list
- Bare `.` prefix → dot command names
- After `search`, `walk`, `inbox`, `blocks`, `diff-blocks`, `log`, `focus`, `f` → node titles
- After `instantiate ` → template names
- Top-level → all dot commands + named commands + function names + node titles

**Registered completion lists** (package-level vars):

```go
var dotCommands  = []string{ ".help", ".info", ".quit", ... }
var namedCommands = []string{ "walk", "templates", "capture", ... }
var modelTiers   = []string{ "fast", "capable", "powerful" }
var themes       = []string{ "light", "dark" }
```

**Helpers:**

- `completeFrom(candidates []string, prefix string) [][]rune` — case-insensitive prefix filter returning readline suffix slices.
- `(r *REPL) groupNames() []string` — returns `@name` strings for all groups in `.sevens.edn`.
- `(r *REPL) backendCandidates() []string` — returns known backend names plus any extras from `globalCfg.Backends`.

---

### `discuss.go` — Discussion mode

Manages the multi-turn conversation workflow. A discussion is a node named
`"Discussion - <FocusTitle>"` whose file holds `[agent ...]` and `[user ...]`
turns.

**Key functions:**

| Function | Signature | Role |
|---|---|---|
| `enterDiscussion` | `(r *REPL) enterDiscussion(nonInteractive bool) error` | Runs the `discuss` function pipeline, auto-accepts it, shows agent turns, then (if interactive and not threaded) enters `ModeDiscussion`. Commits a draft to git as a revert anchor. |
| `handleDiscussionInput` | `(r *REPL) handleDiscussionInput(line string) error` | In `ModeDiscussion`: `.end`/`.done` saves, `.cancel` reverts, dot commands and named commands pass through to dispatch, anything else is treated as a user message. |
| `endDiscussion` | `(r *REPL) endDiscussion() error` | Commits discussion file, resyncs, returns to `ModeNormal`. |
| `cancelDiscussion` | `(r *REPL) cancelDiscussion() error` | Reverts the draft git commit (or removes the file / restores from git), resyncs, returns to `ModeNormal`. |
| `showDiscussionTurns` | `(r *REPL) showDiscussionTurns(discussTitle string)` | Reads discussion file and prints the last contiguous block of `[agent ...]` lines after the last `[user ...]` line. |

**Package-level helpers:**

- `isThreaded(filePath string) bool` — returns true if the discussion file has more than one `# ` heading (indicating branched threads).
- `resolveDiscussionFilePath(db, root, discussTitle) (string, error)` — looks up the file path for a discussion node by title.

---

### `templates.go` — Template commands

Handles the `templates`, `capture`, and `instantiate` commands.

| Function | Signature | Role |
|---|---|---|
| `handleTemplates` | `(r *REPL) handleTemplates() error` | Lists available templates with descriptions. |
| `handleCapture` | `(r *REPL) handleCapture(tokens []string) error` | Loads the `inbox-capture` template and calls `runTemplate`. |
| `handleInstantiate` | `(r *REPL) handleInstantiate(tokens []string) error` | Loads a named template and calls `runTemplate`. Defaults `targetNode` to the focused node for `append-node`/`insert-block` modes. |
| `runTemplate` | `(r *REPL) runTemplate(tmpl *apply.NodeTemplate, parent, targetNode string, vars map[string]string, dryRun bool) error` | Resolves canonical titles, calls `apply.PreviewTemplate` (dry-run) or `apply.ExecuteTemplate`, commits changed files, resyncs, and refocuses to the primary created node. |
| `printTemplatePreview` | `(r *REPL) printTemplatePreview(preview *apply.TemplatePreview)` | Prints template metadata (mode, title, parent, missing vars, content preview). |

**Argument parser:**

```go
func parseTemplateInvokeArgs(tokens []string) (root, parent, targetNode string, vars map[string]string, args []string, dryRun bool, err error)
```

Parses `--parent`/`-p`, `--at`/`-a`, `--set key=val`, `--heading`, `--title`,
`--summary`, `--text`, `--dry-run`, `--root`.

---

### `styles.go` — Lipgloss style constants

All unexported package-level `lipgloss.Style` variables:

| Variable | Usage |
|---|---|
| `promptStyle` | Node title in the readline prompt (bold) |
| `systemStyle` | REPL meta-output, e.g. "[backend] anthropic" (faint) |
| `modeStyle` | Mode indicators like `[note]>`, `[you]>` (bold+italic) |
| `listNumStyle` | Number prefix in numbered lists (faint) |
| `listItemStyle` | List item text (no override) |
| `keyStyle` | Key in `.info` output (faint) |
| `valStyle` | Value in `.info` output (bold) |
| `helpCmdStyle` | Regular command names in `.help` (bold, ANSI cyan) |
| `helpDescStyle` | Description text in `.help` (faint) |
| `dotCmdStyle` | Dot command names in `.help` (bold, ANSI magenta) |

---

## How the pieces relate

```
caller
  └── New(db, root, focusNode, globalCfg) → *REPL
        └── Run()
              ├── readline loop
              │     ├── ModeNote    → handleNoteInput / endNote
              │     ├── ModeDiscussion → handleDiscussionInput (discuss.go)
              │     └── ModeNormal  → dispatch (dispatch.go)
              │           ├── handleDot
              │           ├── navigation: handleNavUp, handleFocusExplicit, handleRelativeNav, handleNumericSelect
              │           ├── handleWalk, handleChildren, handleSiblings, handleSearch, handlePending, handleLog, handleOverview
              │           ├── handleBlocks, handleBlockDiff, handleInbox, handleExtractBlock  (blocks.go)
              │           ├── handleTemplates, handleCapture, handleInstantiate  (templates.go)
              │           ├── handleApply  → runPipeline → engine.RunPipeline
              │           │                              → doAccept / doReject
              │           ├── enterNote  → ModeNote
              │           └── enterDiscussion → runPipeline + doAccept → ModeDiscussion
              └── complete.go  (called by readline, not by dispatch)
```

The intended API is minimal: construct with `New`, call `Run`. The entire
command surface is internal. The package has no exported interfaces and no
callbacks; it drives everything itself via the `engine`, `apply`, `graph`,
`store`, and `backend` packages.
