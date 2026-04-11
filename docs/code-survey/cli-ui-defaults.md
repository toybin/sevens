# Code Survey: cmd/sevens, internal/ui, defaults

Survey date: 2026-04-10

---

## 1. `defaults/` — Bundled Assets

### What is embedded

`defaults/defaults.go` uses `//go:embed` to bundle two asset directories into the binary:

```
defaults/functions/*   — function definitions (.edn + .md per function)
defaults/templates/*   — template definitions (.edn + .md per template)
```

The embedded filesystem is exposed as the exported variable:

```go
var FS embed.FS
```

### Bundled functions (18 total)

`audit`, `bridge`, `challenge`, `contradict`, `decompose`, `discuss`, `distill`, `elaborate`, `merge`, `notice`, `promote`, `relate`, `scaffold`, `sharpen`, `summarize`, `synthesize`, `thesis`, `trim`

Each function is stored as two files: `<name>.edn` (the function definition struct) and `<name>.md` (the prompt template). `decompose` has additional prompt variants (`decompose.generate.md`, `decompose.suggest.md`). `audit` has `audit.stress-test.md`.

### Bundled templates (6 total)

`append-note`, `daily-note`, `inbox-capture`, `inbox-root`, `section-entry`

Same pattern: `.edn` definition + `.md` content template.

### There is also an `orthography/` directory under defaults

Contains `.edn` files for `keys`, `parsers`, `state-machines`, and `tokens` plus a README. These are **not** embedded (the `//go:embed` directive covers only `functions/*` and `templates/*`).

### Exported functions

```go
func ReadFunctionFile(name string) ([]byte, error)
```
Reads `functions/<name>` from the embedded FS. The caller passes a bare filename including extension (e.g., `"summarize.edn"`).

```go
func ListFunctionNames() ([]string, error)
```
Scans the embedded `functions/` directory, returns the base names of all `.edn` files (extension stripped), deduplicated and sorted. Ignores `.md` files.

```go
func ReadTemplateFile(name string) ([]byte, error)
```
Reads `templates/<name>` from the embedded FS.

```go
func ListTemplateNames() ([]string, error)
```
Same logic as `ListFunctionNames` but for `templates/`.

### How callers use it

`defaults` is a pure data store. The `internal/apply` package is the primary consumer: it calls `defaults.ReadFunctionFile` and `defaults.ReadTemplateFile` when loading a function or template that is not found in the user's config directory. `defaults.ListFunctionNames` and `defaults.ListTemplateNames` are used to enumerate what ships with the binary when building combined lists (user-defined first, then bundled).

---

## 2. `internal/ui/` — Terminal Rendering

**File:** `internal/ui/render.go`

### Exported style variables

These are `lipgloss.Style` values. All colors use ANSI indices 0–15 so they adapt to the terminal's own palette.

| Variable    | Style attributes                        |
|-------------|------------------------------------------|
| `Header`    | Bold, padding (0, 1)                     |
| `Label`     | Bold                                     |
| `Dim`       | Faint                                    |
| `Success`   | Foreground ANSI 2 (green)                |
| `Warning`   | Foreground ANSI 3 (yellow)               |
| `Error`     | Foreground ANSI 1 (red)                  |
| `Persona`   | Italic                                   |
| `NodeTitle` | Bold                                     |
| `Role`      | Faint + Italic                           |
| `Separator` | Faint                                    |

### Theme management

```go
func SetTheme(t string)   // accepts "light" or "dark"; default is "light"
func Theme() string
func DetectBackground()   // no-op, kept for call-site compatibility
```

Theme controls glamour's markdown renderer. If `~/.config/glamour/style.json` exists it takes priority over the built-in light/dark styles.

### Exported types

```go
type PrepareStep struct {
    Name    string
    Gate    string
    Fn      string   // delegation target (another function name)
    MapOver string
    Output  string
    Prompt  string   // resolved prompt text
}

type PrepareData struct {
    FnName       string
    NodeTitle    string
    Steps        []PrepareStep
    Parent       *string
    Siblings     []string
    Children     []string
    NeedsParent  bool
    NeedsSibling bool
    NeedsChild   bool
    CrossWalk    string
    ContextFiles []string
}
```

`PrepareData` is the input to `RenderPrepareChecklist`. It carries everything needed to render a human/agent-readable task checklist for a function application.

### Exported functions

```go
func RenderMarkdown(md string) (string, error)
```
Renders markdown to ANSI-formatted terminal output using glamour. Strips trailing whitespace that glamour pads in.

```go
func RenderMarkdownOrPlain(md string) string
```
Same as above but falls back to returning the raw string on error.

```go
func FormatNodeHeader(
    title string,
    parent *string,
    role string,
    children, siblings []string,
    childRoles, siblingRoles map[string]string,
    crossRefs []string,
) string
```
Renders the header block for `sevens walk` output: title, role, parent, children, siblings, cross-refs, and a separator line.

```go
func FormatStep(fnName, stepName, nodeTitle string) string
```
Renders a pipeline progress line: `[fnName] stepName → "nodeTitle"`.

```go
func FormatPersona(persona string) string
```
Renders `[persona]` in italic style.

```go
func FormatCost(tokens int, cost float64, autoApproved bool, threshold float64) string
```
Renders a cost/token line. Shows auto-approval status if below threshold.

```go
func FormatOp(action, name string) string
```
Renders a single file operation. `"create"` gets green `+`, `"edit"` gets yellow `~`.

```go
func FormatLogEntry(timestamp, event, function, step, commit, note string) string
```
Renders one log entry line with color-coded event type (completed/accepted/applied = green, rejected/reverted = red, suggested = yellow).

```go
func RenderPrepareChecklist(d PrepareData) string
```
Formats a function application as a human/agent-readable task checklist. Emits sections for: task header, pipeline summary, read targets, context reads, and one block per step with `[instruction]`, `[output]`, `[submit]`, and optional `[gate]` sections including the exact `sevens` CLI invocations to run.

```go
func FormatPending(target, function, step, summary, subject string) string
```
Renders a one-line pending suspension entry. The `subject` is the stable suspension ID; only the timestamp portion is shown to keep the line short.

### How callers use it

`internal/ui` is consumed exclusively by `cmd/sevens/main.go` (and `cmd/sevens/repl.go`). It has no dependencies on other internal packages. The pattern is: compute data in the command handler, pass it to a `ui.Format*` or `ui.Render*` function, write to stdout or stderr. Style variables (`ui.Dim`, `ui.NodeTitle`, etc.) are also used inline in the CLI code for ad-hoc formatting not covered by a dedicated format function.

---

## 3. `cmd/sevens/` — CLI Entry Point

**Files:** `main.go`, `repl.go`

The binary is built from `package main`. All command constructors return `*cobra.Command`. `main()` assembles them into a single root command and installs a custom usage function that groups commands into categories.

### Root

```
sevens — Context server for AI agents over a tree-structured knowledge graph
```

The usage function groups subcommands into four display categories: Graph, Functions, Session, Structure. The `config` and `repl` commands appear in an "Other" group.

### Internal helpers (package-private)

```go
func openDB() (*sql.DB, error)
func resolveRoot(explicit string) (string, error)
func resolveNodeTitle(title string) (string, error)
func syncRoot(rootDir string) error
func syncAllRoots() error
func runPipeline(root, nodeTitle string, fn *apply.Function, startStep int, prev string, dryRun bool, confirm bool, includes []string, model string, allowedSteps map[string]bool, backendName string, blockPath string, blockID string) error
func completeNodeTitles(...) ([]string, cobra.ShellCompDirective)
func completeFunctionNames(...) ([]string, cobra.ShellCompDirective)
```

`resolveRoot` walks up from cwd looking for `.sevens.edn`; if not found, falls back to the active focus session's root. `resolveNodeTitle` resolves `"."` to the focused node title. `runPipeline` is the core function that wires together graph walks, context assembly, backend selection, and `engine.RunPipeline`, then prints the result (suspension or completion).

### Commands

#### Graph commands

| Command | Signature | What it does |
|---------|-----------|--------------|
| `init [path]` | flags: `--alias`, `--max-chars` | Creates `.sevens.edn`, optionally runs `git init`, calls `syncRoot`. |
| `sync` | flag: `--root` | Rebuilds the SQLite triples DB from markdown files. If no root found, syncs all registered roots. |
| `overview` | flags: `--root`, `--edn` | Prints the full node tree. `--edn` outputs raw EDN. |
| `walk <node-title>` | flags: `--root`, `--depth`, `--edn` | Walks a node's neighborhood (parent, children, siblings, cross-refs) and renders header + content. `"."` resolves to focused node. |
| `tree <node-title>` | flags: `--root`, `--edn` | Shows the subtree rooted at a given node as an ASCII tree. |
| `blocks <node-title>` | flags: `--root`, `--edn` | Lists the block structure of a node (path, kind, text summary). |
| `diff-blocks <node-title>` | flags: `--root`, `--edn`, `--unchanged` | Shows block-level changes since last sync: edited, scope-changed, reordered, inserted, deleted. |
| `inbox [node-title]` | flags: `--root`, `--edn` | Shows inbox-style child summaries for a container node (defaults to `"inbox"`). |
| `extract-block <source> <block-path> [new-title]` | flags: `--root`, `--parent` | Creates a new node from a block or heading section within a source node. |
| `roots` | none | Lists all registered sevens root directories, marking the active one. |
| `search <query>` | flag: `--root` | Searches node titles and content, prints matching node titles. |
| `query <sql>` | flag: `--root` | Runs a raw SQL query against the triples store. Substitutes `{{root}}`, `{{target}}`, `{{focused}}` template variables. |

#### Function commands

| Command | Signature | What it does |
|---------|-----------|--------------|
| `apply <function> <node-title>` | flags: `--root`, `--dry-run`, `--yes`, `--include`, `--model`, `--backend`, `--block` | Runs a function pipeline against a node. `--dry-run` prints the rendered prompt without calling the LLM. `--block` targets a specific block within the node. |
| `discuss <node-title>` | flags: `--root`, `--dry-run`, `--yes`, `--include`, `--model`, `--backend` | Convenience alias for `apply discuss`. |
| `accept <node-title or suspension-id>` | flags: `--root`, `--with`, `--yes`, `--steps`, `--backend` | Accepts a pending suspension. Without `--with`: applies ops or advances the pipeline. With `--with <feedback>`: calls `engine.ReviseStep` to re-run with feedback and writes a new suspension. Accepts a bare suspension ID (`suspension:...`) to resolve ambiguity when multiple suspensions exist for a node. |
| `reject <node-title or suspension-id>` | flag: `--root` | Rejects a pending suspension, logs the rejection. |
| `pending` | flag: `--root` | Lists all active suspensions for the current root. |
| `functions` | none | Lists all available functions (user-defined + bundled), with description. |
| `templates` | none | Lists all available templates (user-defined + bundled), with description. |
| `define <name>` | flags: `--description` (required), `--prompt` | Creates a new function `.edn` file in the user config dir. If `--prompt` is omitted, also creates a `.md` prompt template with placeholder content. |
| `prepare <function> <node-title>` | flag: `--root` | Compiles a function into an agent task checklist via `ui.RenderPrepareChecklist`. Shows what reads are needed, what each step does, and exact CLI commands to submit. |
| `submit <node-title>` | flags: `--root`, `--function` (required), `--step`, `--output` (required), `--response`, `--response-file` | Records an agent's response for a function step. For `ops` output: parses ops and writes a suspension. For `suggestions`: writes a suspension. For `text`: logs completion. |

#### Session commands

| Command | Signature | What it does |
|---------|-----------|--------------|
| `focus <node-title>` | flags: `--root`, `--include`, `--exclude` | Pins a node as the active focus; enables `"."` shorthand in other commands. Verifies the node exists via a walk. |
| `unfocus` | none | Clears the active focus session. |
| `status` | none | Shows the current focus session: node, root, created-at, includes, excludes, global context files, node-level context files. |
| `log <node-title>` | flag: `--root` | Shows the operation log for a node (all events: completed, suggested, accepted, applied, rejected, reverted). |

#### Structure commands

| Command | Signature | What it does |
|---------|-----------|--------------|
| `new [title]` | flags: `--root`, `--template`, `--parent`, `--set`, `--title`, `--summary`, `--heading`, `--text`, `--dry-run` | Creates a new node. Without `--template`: bare node with heading. With `--template`: instantiates a template. `--dry-run` previews without writing. |
| `capture [title]` | flags: `--root`, `--parent`, `--set`, `--title`, `--summary`, `--dry-run` | Quick-capture using the `inbox-capture` template specifically. |
| `instantiate <template> [args...]` | flags: `--root`, `--parent`, `--at`, `--set`, `--title`, `--summary`, `--heading`, `--text`, `--dry-run` | General template instantiation. `--at` targets an existing node for `append-node` / `insert-block` mode templates. |
| `revert <node-title>` | flag: `--root` | Reverts the last applied operation by checking out files from the parent git commit, then re-committing. Requires a git repo. Prompts for confirmation. |

#### Config command (subcommand group)

```
config generate <backend>   — codex | claude | all
    Reads capabilities.edn and writes MCP config files to ~/.config/sevens/generated/<backend>/.

config show
    Prints current backend, model, MCP servers, and named backends.
```

#### REPL command (`repl.go`)

```
repl [node-title]   flag: --root
```

Starts an interactive REPL session via `repl.New(db, root, focusNode, globalCfg)`. If a node title is provided as an argument, or if there is an active focus session matching the resolved root, that node becomes the initial focus. Installs a `SIGINT`/`SIGTERM` handler to close the database cleanly.

### How the pieces wire together

1. Every command calls `resolveRoot` to determine the root directory, then `openDB` to open the SQLite store.
2. Graph reads go through `graph.BuildWalk`, `graph.BuildOverview`, etc.
3. Function execution goes through `runPipeline`, which builds context, selects a backend via `backend.FromConfig`, constructs `engine.PipelineConfig`, and calls `engine.RunPipeline`. The result is either a `Suspension` (gate hit, pending human review) or a `Completion`.
4. Suspensions are stored in the DB; `accept` and `reject` resolve them. `accept --with` calls `engine.ReviseStep` to re-run a step with feedback.
5. Template instantiation goes through `apply.ExecuteTemplate` / `apply.PreviewTemplate`.
6. After any write operation, `syncRoot` is called to rebuild the triples DB and validate the graph.
7. All terminal output passes through `internal/ui` for consistent styling.
