# Code Survey: `internal/apply`

The `apply` package is the central engine for sevens. It owns: the domain model
for functions and templates, LLM invocation, graph context resolution, file
operations, git integration, session state, logging, and token/cost accounting.
It is not a self-contained runnable — callers in `cmd/` orchestrate the pipeline
using the primitives here.

---

## `types.go` — Core domain model

All the EDN-mapped structs that define what a "function" and a "template" are.

### Exported types

**`PathSpec`**
Declares a morphism path to traverse from a target node, used for context
gathering. A `"~"` suffix on a predicate name means inverse traversal.

```go
type PathSpec struct {
    Path        []string
    ExcludeSelf bool
    With        []string  // predicates to fetch from terminal nodes
    As          string    // template variable name for results
}
```

**`Require`**
Declares a structural role a function step needs resolved from the graph.

```go
type Require struct {
    Role     string  // "target", "parent", "siblings", "children", "child[N]", "history"
    Type     string  // "node", "node[]"
    Optional bool
    Ref      string  // reference to another step's output (pipelines)
    As       string  // template variable name override
}
```

**`AgentConfig`**
Per-function or per-step LLM persona and context policy overrides.

```go
type AgentConfig struct {
    Persona        string
    SystemPrompt   string
    Model          string   // "fast", "capable", "powerful", or raw model ID
    ContextPolicy  string   // "minimal", "neighborhood", "full", "cached", "custom"
    Exploration    string   // "closed" (default), "scoped"
    Capabilities   []string // MCP server names
    AllowFileReads bool
    ReadOnly       bool
}
```

**`Step`**
One stage in a function's pipeline.

```go
type Step struct {
    Name     string
    Prompt   string   // inline prompt or loaded from <name>.<step>.md
    Input    string   // "node", "suggestions", "ops", "text"
    Output   string   // "suggestions", "ops", "text"
    Gate     string   // "" = auto-advance, "approve" = wait for user
    Requires []Require
    Fn       string   // delegate to another function by name
    MapOver  string   // predicate path to map over (e.g., "node/parent~")
    Agent    *AgentConfig
}
```

**`Function`**
A named, reusable LLM-powered transformation defined as an EDN file. A function
with `Steps` uses the pipeline; a function with only `Prompt` is a single-step
shorthand.

```go
type Function struct {
    Name         string
    Description  string
    Prompt       string       // single-step shorthand
    Input        string
    Output       string
    Steps        []Step
    Requires     []Require
    Context      []PathSpec
    Agent        *AgentConfig
    Backend      string
    ContextFiles []string
    CrossWalk    string   // inject another function's last output
    AdHoc        bool     // accepts arbitrary {{instruction}} at invocation
}
```

Methods on `Function`:

```go
func (f *Function) EffectiveSteps() []Step         // normalizes single-prompt fn to one-step pipeline
func (f *Function) ValidateComposition() error     // checks output/input chaining between steps
```

**`ResolvedNode`**
A fetched node's content for template rendering.

```go
type ResolvedNode struct {
    Title   string
    Content string
    Role    string
}
```

**`ResolvedBlock`** (unexported fields are all exported here)
A resolved sub-block target within a node.

```go
type ResolvedBlock struct {
    ID        string
    Path      string
    Kind      string
    Text      string
    Markdown  string
    Signifier string
    Scope     []string
}
```

**`ResolvedContext`**
All resolved inputs for a function step, ready for template rendering.

```go
type ResolvedContext struct {
    Target          ResolvedNode
    NodeTitle       string
    NodeContent     string
    TargetKind      string
    TargetLabel     string
    Block           *ResolvedBlock
    Parent          *ResolvedNode
    Siblings        []ResolvedNode
    Children        []ResolvedNode
    ChildTitles     []string
    SiblingTitles   []string
    History         []LogEntry
    Prev            string         // previous step output
    CrossWalkOutput string
    Instruction     string         // ad-hoc instruction via {{instruction}}
}
```

**`FileOp`**
A single file operation returned by the LLM as structured output.

```go
type FileOp struct {
    Action           string            // "create" or "edit"
    Title            string            // for "create": new node title
    Parent           string            // for "create": parent node title
    File             string            // for "edit": node title of file to edit
    OldText          string            // for "edit": exact text to find
    NewText          string            // for "edit": replacement text
    Content          string            // for "create": markdown body
    ExtraFrontmatter map[string]string // internal: extra frontmatter fields
}
```

**`LogEntry`**
One event in the append-only log for a node.

```go
type LogEntry struct {
    Event        string
    Root         string
    Function     string
    Target       string
    Step         string
    StepIndex    int
    Timestamp    string
    Ops          []FileOp
    RawOutput    string
    Commit       string
    Note         string
    FilesCreated []string
    FilesEdited  []string
    Summary      string
}
```

**`NodeTemplate`**
A structural pattern for creating or modifying nodes.

```go
type NodeTemplate struct {
    Name              string
    Description       string
    Mode              string             // "create-node" (default), "append-node", "insert-block"
    TitlePattern      string
    DraftTitlePattern string
    ParentPattern     string
    ParentTemplate    string             // create parent from this template if missing
    Target            *TemplateTarget
    Placement         *TemplatePlacement
    Type              string
    Content           string
    SiblingRole       string
    Params            []TemplateParam
    Draft             *TemplateDraft
    CommitMessage     string
    Children          []NodeTemplate     // subtree template
}
```

Supporting template types:

```go
type TemplateTarget struct {
    Root   string
    Parent string
    Node   string
}

type TemplatePlacement struct {
    Kind            string
    Heading         string
    HeadingLevel    int
    CreateIfMissing bool
}

type TemplateParam struct {
    Name     string
    Required bool
    Default  string
}

type TemplateDraft struct {
    WhenMissingParams bool
    Open              bool
}
```

**`LLMConfig`** — connection details for an LLM provider.

```go
type LLMConfig struct {
    Provider  string
    Model     string
    APIKeyEnv string
    APIKey    string
}
```

**`BackendConfig`** — configuration for a specific execution backend.

```go
type BackendConfig struct {
    Type             string  // "anthropic", "codex", "claude"
    Command          string  // CLI binary path
    GeneratedConfDir string
}
```

**`GlobalConfig`** — top-level config from `~/.config/sevens/config.edn`.

```go
type GlobalConfig struct {
    LLM           LLMConfig
    Models        map[string]LLMConfig
    Backend       string
    Backends      map[string]BackendConfig
    SystemPrompt  string
    ContextFiles  []string
    CostThreshold float64
    Theme         string  // "light" or "dark"
}

func (g *GlobalConfig) ResolveModel(name string) LLMConfig
```

`ResolveModel` looks up a named profile (e.g., "fast") and inherits any
unset fields from the default `LLM` config.

### Exported standalone function

```go
func EffectiveAgent(fn *Function, step *Step) *AgentConfig
```

Returns step-level `AgentConfig` if set, otherwise falls back to function-level.

---

## `functions.go` — Function loading and prompt rendering

Loads `Function` definitions from EDN files (user config dir with bundled
fallbacks), and provides all prompt-rendering utilities.

### Exported functions

```go
func LoadFunction(name string) (*Function, error)
```
Loads a named function from `~/.config/sevens/functions/<name>.edn`. Inline
prompts and `.md` sidecar files are both supported. Falls back to bundled
defaults. Validates composition after load.

```go
func ListFunctions() ([]Function, error)
```
Returns all available functions (user + bundled), sorted by name. Logs warnings
for malformed functions without failing.

```go
func LoadContextFiles(root string, paths []string) string
```
Reads and concatenates context files, resolving `~/`-prefixed and relative
paths. Wraps each file in `<context-file path="...">` XML tags.

```go
func RenderStepPrompt(prompt, title, content, parent string, children []string, prev, context string) string
```
Simple template rendering: substitutes the standard `{{variable}}` tokens into a
prompt string. Convenience wrapper around `RenderStepPromptWithVars`.

```go
func RenderStepPromptWithVars(prompt string, vars PromptVars) string
```
Full rendering with a `PromptVars` struct. Replaces all known `{{...}}` tokens
including block-specific ones. Also injects `{{timestamp}}`.

```go
func RenderPrompt(fn *Function, title, content, parent string, children []string) string
```
Convenience wrapper for single-step functions (no `prev`, no `context`).

```go
func ParseOps(llmOutput string) ([]FileOp, error)
```
Parses the LLM's JSON output (a `[]FileOp`) from a response that may be
wrapped in a code fence. Handles truncated responses and validates each op.

```go
func SanitizeFilename(title string) string
```
Converts a node title to a safe lowercase `.md` filename, dropping
filesystem-unsafe characters.

### Exported type

```go
type PromptVars struct {
    Title, Content, NodeTitle, NodeContent string
    Parent    string
    Children  []string
    Prev      string
    Context   string
    TargetKind, TargetLabel string
    BlockID, BlockPath, BlockKind, BlockText, BlockMarkdown, BlockSignifier, BlockScope string
}
```

### Unexported functions

- `readFunctionAsset(userPath, bundledName string) ([]byte, error)` — tries user
  path first, then bundled defaults.
- `stripBrackets(s string) string` — removes `[[` / `]]` Obsidian link syntax
  from LLM-returned node references.

---

## `resolve.go` — Graph context resolution and prompt rendering

Resolves what a function step needs from the graph and formats it for
injection into prompts.

### Exported functions

```go
func EffectiveRequires(fn *Function, step *Step) []Require
```
Merges function-level and step-level `Requires`, with step-level taking
precedence per role.

```go
func ResolveContext(db *sql.DB, root string, fn *Function, step *Step, walk *graph.WalkOutput, targetBlock *graph.BlockTarget) (*ResolvedContext, error)
```
The primary resolution entry point. Fetches parent, siblings, children, and
history from the graph based on `Requires` declarations. Also handles
cross-walk injection (output from a different function's last run) and
evaluates `Context` path specs via `EvalPaths`.

```go
func FormatResolvedNodes(tag string, nodes []ResolvedNode) string
```
Renders a slice of `ResolvedNode` as XML-tagged content blocks for injection
into prompts.

```go
func FormatHistory(entries []LogEntry) string
```
Renders log entries as a `<history>` XML block for prompt injection.

```go
func RenderWithContext(prompt string, ctx *ResolvedContext, contextFiles string) string
```
The authoritative prompt renderer for functions that declare `:requires` or
`:context`. Replaces all `{{...}}` tokens using a `ResolvedContext`.

```go
func HasRequires(fn *Function) bool
```
Returns `true` if the function has any `Requires`, `Context` paths, or a
`CrossWalk`. Callers use this to choose between `RenderStepPrompt` (simple)
and `RenderWithContext` (full resolution).

---

## `patheval.go` — Graph path evaluation

Implements the morphism path language used in `PathSpec.Path`.

### Exported types and functions

**`PathResult`**

```go
type PathResult struct {
    As    string         // template variable name
    Nodes []ResolvedNode
}
```

```go
func EvalPath(db *sql.DB, startSubject string, spec PathSpec) (*PathResult, error)
```
Evaluates one `PathSpec` starting from a subject. Each predicate in `Path` is
traversed forward or inverse (suffix `"~"`). Deduplicates and sorts terminal
nodes, then fetches any predicates listed in `spec.With`.

```go
func EvalPaths(db *sql.DB, startSubject string, specs []PathSpec) (map[string]*PathResult, error)
```
Evaluates all path specs for a function, returning results keyed by the `:as`
name.

---

## `ops.go` — File operation execution

Executes `[]FileOp` against the filesystem.

### Exported functions

```go
func ExecuteOps(ops []FileOp, root string, db *sql.DB) (filesCreated []string, filesEdited []string, err error)
```
Runs each `FileOp` in order. For `"create"`: writes a new markdown file with
YAML frontmatter. For `"edit"`: performs an exact string replacement (with
fuzzy fallback and interactive confirmation on failure). Stops at the first
error, returning partial results.

### Unexported functions

- `createFile(op FileOp, root string) (string, error)` — writes a new node
  file with generated frontmatter.
- `editFile(op FileOp, root string, db *sql.DB) (string, error)` — resolves
  the node's file path from the database and applies `OldText` → `NewText`
  replacement.
- `stripContentFrontmatter(content string) string` — removes LLM-generated
  frontmatter so system-generated frontmatter is authoritative.
- `findBestMatch(content, target string) (string, float64)` — sliding-window
  fuzzy match for when exact `OldText` is not found. Score threshold is 0.6.
- `similarity(a, b string) float64` — line-based similarity ratio used by
  `findBestMatch`.
- `truncate(s string, n int) string` — truncates a string for display.

---

## `templates.go` — Template loading and execution

Loads `NodeTemplate` definitions and executes them against the graph.

### Exported functions

```go
func LoadTemplate(name string) (*NodeTemplate, error)
```
Loads a named template from `~/.config/sevens/templates/<name>.edn` with
bundled fallback. Also loads a `<name>.md` sidecar for `Content` if the EDN
has none. Defaults `Mode` to `"create-node"` if unset.

```go
func ListTemplates() ([]string, error)
```
Returns all available template names (user + bundled), sorted.

```go
func ResolveTemplateVars(tmpl *NodeTemplate, vars map[string]string) map[string]string
```
Applies builtin defaults (`date`, `today`, `time`, `timestamp`,
`template-name`) and param defaults via substitution.

```go
func MissingTemplateVars(tmpl *NodeTemplate, vars map[string]string) []string
```
Returns required variables still missing after defaults are applied.

```go
func RenderTemplate(tmpl *NodeTemplate, vars map[string]string) *NodeTemplate
```
Substitutes `{{var}}` in all string fields of the template tree, returning a
new `*NodeTemplate`. Recursively renders child templates.

```go
func CleanRenderedTemplate(tmpl *NodeTemplate) *NodeTemplate
```
Strips any unresolved `{{...}}` tokens from a rendered template (used when
not in draft mode).

```go
func DraftTitle(tmpl *NodeTemplate) string
```
Returns a usable draft title from `DraftTitlePattern` or `TitlePattern`,
replacing remaining `{{...}}` with the literal string `"draft"`.

```go
func ExtractVariables(tmpl *NodeTemplate) []string
```
Returns the set of required user-provided variable names, excluding builtins.
Inspects `Params` if declared, otherwise scans pattern strings for `{{...}}`
tokens. Includes variables from child templates.

```go
func InstantiateTemplate(tmpl *NodeTemplate, parent string, root string) []FileOp
```
Converts a rendered `NodeTemplate` into a `[]FileOp` (creates the node and
recursively creates child nodes).

```go
func BindTemplateArgs(tmpl *NodeTemplate, args []string, vars map[string]string) map[string]string
```
Maps positional CLI arguments onto template params in declaration order,
skipping already-provided keys. Falls back to binding `args[0]` to `"title"`
or `"topic"` for simple one-arg templates.

```go
func PreviewTemplate(db *sql.DB, root string, tmpl *NodeTemplate, opts TemplateExecutionOptions) (*TemplatePreview, error)
```
Computes what `ExecuteTemplate` would do without writing any files. Returns a
`TemplatePreview` for display.

```go
func ExecuteTemplate(db *sql.DB, root string, tmpl *NodeTemplate, opts TemplateExecutionOptions) (*TemplateExecutionResult, error)
```
The main template execution entry point. Handles all three modes:
- `"create-node"` (default): calls `InstantiateTemplate` → `ExecuteOps`,
  bootstraps a missing parent via `ParentTemplate` if configured.
- `"append-node"`: appends content to an existing node via `graph.PrepareAppendToNode`.
- `"insert-block"`: inserts content under a heading via `graph.PrepareInsertUnderHeading`.

After file operations, writes `sibling/role` triples to the database.

```go
func SiblingRoleTriples(tmpl *NodeTemplate) []store.Triple
```
Generates `sibling/role` triples for any child templates that declare a
`SiblingRole`.

### Exported option/result types

```go
type TemplateExecutionOptions struct {
    Parent     string
    TargetNode string
    Vars       map[string]string
}

type TemplateExecutionResult struct {
    TemplateName    string
    Mode            string
    TargetNode      string
    EffectiveParent string
    PrimaryTitle    string
    Created         []string
    Edited          []string
    Missing         []string
    Draft           bool
    CommitMessage   string
}

type TemplatePreview struct {
    TemplateName    string
    Mode            string
    Title           string
    Parent          string
    TargetNode      string
    Heading         string
    HeadingLevel    int
    CreateIfMissing bool
    BootstrapParent string
    Missing         []string
    Draft           bool
    Content         string
}
```

---

## `git.go` — Git operations

Thin wrappers over `git` CLI invocations. All take `root string` as the
repository path.

### Exported functions

```go
func IsGitRepo(root string) bool
func HasChanges(root string) (bool, error)
func CommitAll(root, message string) (string, error)      // returns short hash
func ChangedFiles(root string) ([]string, error)          // filters to .md and .sevens.edn
func CommitFiles(root, message string, files []string) (string, error)
func RevertCommit(root, hash string) (string, error)      // stashes dirty state, reverts, pops stash
```

### Unexported functions

- `runGit(root string, args ...string) (string, error)` — executes `git -C
  <root> <args...>` and returns combined output.

---

## `llm.go` — LLM invocation and config loading

### Exported functions

```go
func LoadGlobalConfig() (GlobalConfig, error)
```
Reads `~/.config/sevens/config.edn`. Returns a config with defaults if the
file does not exist. Fills in missing fields from `defaultLLMConfig` and
sets `CostThreshold` to 0.01 if unset.

```go
func CallLLM(ctx context.Context, config LLMConfig, systemPrompt, prompt string, streamTo io.Writer) (string, error)
```
Makes a streaming Anthropic API call. If `streamTo` is non-nil, streams text
directly to it; otherwise prints progress dots to stderr. Currently only
`"anthropic"` is implemented; other providers return an error.

### Unexported functions

- `resolveAPIKey(config LLMConfig) (string, error)` — prefers `config.APIKey`,
  falls back to the environment variable named by `config.APIKeyEnv`.

---

## `log.go` — Append-only event log

Stores and retrieves `LogEntry` records in the triple store.

### Exported functions

```go
func AppendLogDB(db *sql.DB, entry LogEntry) error
```
Writes a log entry as a set of triples under a unique subject of the form
`log:<timestamp>:<sanitized-title>:<random-suffix>`.

```go
func ReadLogDB(db *sql.DB, parts ...string) ([]LogEntry, error)
```
Reads log entries for a node, ordered by subject (which encodes timestamp).
Accepts either `ReadLogDB(db, nodeTitle)` or `ReadLogDB(db, root, nodeTitle)`.
The two-argument form filters by root to avoid cross-vault collisions.

### Unexported functions

- `logSubject(nodeTitle, timestamp string) string` — generates a
  collision-resistant triple subject for a log entry.
- `logSubjectToEntry(db *sql.DB, subject string) (*LogEntry, error)` —
  reconstructs a `LogEntry` from its triples.

---

## `session.go` — Focused node session

A "session" is a small EDN file that records the currently focused node and
its context inclusions/exclusions across CLI invocations.

### Exported types and functions

```go
type Session struct {
    Root      string
    NodeTitle string
    CreatedAt string
    Includes  []string
    Excludes  []string
}

func SessionPath() (string, error)
func SaveSession(s *Session) error
func LoadSession() (*Session, error)  // returns nil, nil if no session file
func ClearSession() error
```

---

## `tokens.go` — Token counting and cost estimation

### Exported types and functions

```go
type ModelPricing struct {
    InputPerMillion  float64
    OutputPerMillion float64
}

func LookupPricing(model string) (ModelPricing, bool)
```
Returns per-million-token USD costs for known Claude models. Matches by prefix
(`claude-opus-4`, `claude-sonnet-4`, `claude-haiku-4`).

```go
func CountTokens(config LLMConfig, systemPrompt, userPrompt string) (int, error)
```
Calls the Anthropic token counting API for an exact count. Falls back to a
character-based heuristic (~4 chars/token) if the API call fails.

---

## `confirm.go` — Cost confirmation prompt

### Exported functions

```go
func ConfirmCost(config LLMConfig, backendName, systemPrompt, userPrompt string, threshold float64) (bool, error)
```
Counts tokens, estimates cost, and prompts for Y/n confirmation before an LLM
call. If `threshold > 0` and the estimated cost is below it, auto-approves
without prompting. For non-Anthropic backends or unavailable API keys, skips
token counting: auto-approves if `threshold > 0`, otherwise shows a simplified
prompt.

---

## How the pieces fit together

The intended caller flow for a function invocation:

1. **Config** — `LoadGlobalConfig()` to get `GlobalConfig`; resolve model tier
   with `GlobalConfig.ResolveModel(agentConfig.Model)`.
2. **Function** — `LoadFunction(name)` to get `*Function`.
3. **Context** — For each step in `fn.EffectiveSteps()`:
   - If `HasRequires(fn)`: call `ResolveContext(db, root, fn, step, walk, block)`
     to get `*ResolvedContext`, then `RenderWithContext(step.Prompt, ctx, contextFiles)`.
   - Otherwise: `RenderStepPromptWithVars(step.Prompt, vars)` (simpler path).
4. **Cost** — `ConfirmCost(config, backend, systemPrompt, prompt, threshold)`.
5. **LLM** — `CallLLM(ctx, config, systemPrompt, prompt, streamTo)`.
6. **Parse** — If step output is `"ops"`: `ParseOps(llmOutput)`.
7. **Execute** — `ExecuteOps(ops, root, db)` → `(filesCreated, filesEdited, err)`.
8. **Git** — `CommitFiles(root, message, files)` or `CommitAll(root, message)`.
9. **Log** — `AppendLogDB(db, LogEntry{...})`.

The template flow is separate and simpler:

1. `LoadTemplate(name)` → `*NodeTemplate`
2. `BindTemplateArgs(tmpl, args, vars)` → bound vars map
3. `PreviewTemplate(db, root, tmpl, opts)` → display preview (no writes)
4. `ExecuteTemplate(db, root, tmpl, opts)` → `*TemplateExecutionResult`
5. `CommitFiles(root, result.CommitMessage, result.Created+result.Edited)`

Session state (`LoadSession`, `SaveSession`, `ClearSession`) is used by the CLI
to persist the focused node between commands and does not participate in the
function/template pipeline directly.
