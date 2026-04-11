# Sevens — Responsibility Catalog

Inventoried from code surveys dated 2026-04-10. Each entry describes a
responsibility the system currently performs, what state it manages, and what
operations it exposes. Entries are organized by the area of the codebase they
were found in, but the goal is to describe *what the system does*, not to
endorse the current packaging.

---

## From `internal/store`

### Triple persistence

Insert, retrieve, delete, and query `(subject, predicate, object)` string
triples in a single SQLite table. All values are untyped strings. The table
has indexes on `(subject, predicate)`, `(predicate, object)`, and
`(subject, predicate, object)`.

**State**: one `triples` table.

**Operations**:
- Insert one or batch-insert many triples (idempotent on the full 3-tuple)
- Set a singular predicate value (delete-then-insert for predicates that must
  have exactly one value)
- Delete by subject, by subject prefix, or by predicate
- Clear all triples belonging to a root (finds members via `node/root` and
  `block/root` predicates, then deletes by subject)
- Get single object for `(subject, predicate)`
- Get all objects for `(subject, predicate)`
- Get all subjects for `(predicate, object)` (reverse lookup)
- Get all triples for a subject
- Get all triples for a predicate
- Batch fetch: all titles in a root mapped to requested predicates (avoids N+1)
- Substring search on content or titles within a root
- List all node titles in a root
- Raw SQL query escape hatch (returns string matrix with column headers)

**Note on composition**: the package exposes two 2-hop SQL JOIN helpers
(`Compose`, `ComposeInverse`), but only `ComposeInverse` has one remaining
caller (hardcoded sibling lookup). The general multi-hop path system in
`apply/patheval.go` bypasses these entirely, walking paths step-by-step with
individual `GetObjects`/`GetSubjects` calls. The store-level composition is
effectively vestigial.

### Subject identity

Construct canonical subject strings that scope node and block identity by root.
Format: `node:<6-byte-sha1-of-root>:<title>` and
`block:<6-byte-sha1-of-root>:<nodeTitle>:<path>`. Reverse lookup from subject
back to human-readable title via `node/title` predicate.

**State**: none (pure functions, except the reverse lookup which queries the
triples table).

**Operations**:
- Produce a node subject from `(root, title)`
- Produce a block subject from `(root, nodeTitle, path)`
- Resolve a subject back to its title

### Root registry

Track which filesystem directories are registered as sevens roots. Stored in
`~/.config/sevens/roots.edn` as a serialized string list. Completely
independent of the database -- none of these operations touch `*sql.DB`.

**State**: `roots.edn` file on disk.

**Operations**:
- Load registered roots
- Save (overwrite) registered roots
- Add a root idempotently

### Database lifecycle

Locate the config directory, open the shared SQLite database, initialize the
schema.

**State**: `~/.config/sevens/sevens.db` file on disk.

**Operations**:
- Get/create config directory path
- Open database (turso driver, WAL mode, 5s busy timeout, max 1 connection)
- Create triples table and indexes if not present

---

## From `internal/backend`

### LLM inference

Send a prompt to a language model and get text back. Three transport
implementations behind a single interface: direct Anthropic API call, Claude
Code CLI subprocess, and OpenAI Codex CLI subprocess. The interface is
`Complete(ctx, InferenceRequest) (string, error)`.

Prompt construction for CLI backends concatenates system prompt and user prompt
with a separator, since CLI tools lack a dedicated system prompt channel. The
API backend sends them as separate messages.

A factory function selects the implementation from global config, with fallback
chain: explicit `--backend` flag > config file default > `"anthropic"`.

**State**: none persistent. Backends are stateless per-call (API key or CLI
binary path are configuration, not mutable state).

**Operations**:
- Construct a backend from config
- Send an inference request and receive text response
- Build/prepare prompts for CLI backends

### Agent capability policy

Control what an agent is allowed to do during inference. This is a separate
concern from transport -- it determines tool access, filesystem permissions,
and MCP server availability, then passes those constraints *into* the inference
call via prompt preambles and CLI flags.

Two dimensions:
- **Exploration tier** (`"closed"` vs `"scoped"`): controls whether tools and
  file access are permitted at all. Closed disables everything. Scoped enables
  a filtered set.
- **Capabilities**: named MCP servers (e.g., `"github"`, `"jira"`) that the
  call requires. Loaded from `capabilities.edn`. Checked before the call
  (warn on missing), then threaded into prompt preambles and CLI tool
  allowlists.

Backend-specific config generation (writing `mcp.json` for Claude Code,
writing stanzas into `~/.codex/config.toml` for Codex) is also part of this
responsibility -- it's the setup step that makes capabilities available at
call time.

**State**: `~/.config/sevens/capabilities.edn` (read-only at call time),
generated config files written during setup.

**Operations**:
- Load capability definitions
- Check requested capabilities against available ones
- Generate backend-specific config files (Claude mcp.json, Codex config.toml)
- Build context policy preamble for prompts
- Build tool allowlists for CLI flags

---

## From `internal/graph`

Note: this single package carries at least five distinct responsibilities.

### Markdown parsing

Read `.md` files from disk, extract YAML frontmatter and body content, parse
wiki-links, and decompose the body into structural blocks (headings,
paragraphs, list items, tasks). Pure file-to-data transformation -- no
database involvement.

Also includes: locating a root directory by walking up to find `.sevens.edn`,
scanning for all `.md` files under a root, loading root config from EDN.

**State**: none. Reads files, produces data.

**Operations**:
- Find root directory from a path
- Load root config
- Scan for markdown files
- Parse all files into `ParsedNode` values (skips files without titles,
  deduplicates)

### Triple projection (sync)

Convert parsed nodes, blocks, and config into triples and write them to the
store. This is the "sync" operation -- the bridge from filesystem to database.

Includes block subject assignment: before writing, loads existing block
subjects from the store and fuzzy-matches them to new blocks so that block
identity survives edits. This is a non-trivial matching process involving
shingle anchors and text hashes.

**State**: writes to the triples table (via store). Reads existing block
subjects for identity continuity.

**Operations**:
- Convert a parsed node to triples
- Convert a parsed block to triples
- Convert root config to triples
- Full sync: clear root's triples, recompute and insert all (the main write
  path)

### Graph querying

Read the triple store to produce structured views of the knowledge graph.
Returns typed output structs, never raw triples.

**State**: reads from triples table.

**Operations**:
- Build full overview (all nodes with parent/child/cross-ref metadata +
  validation report)
- Build walk (single node with full local context: content, parent, children,
  siblings, cross-refs, roles)
- Resolve a named group to its member node titles
- Auto-resolve group includes for nodes with `include-group: true`
- Validate graph (orphans, missing parents, >9 children, length violations)

### Node and block editing (preparation)

Prepare file mutations as pure data without writing anything to disk. Caller
receives a diff-like structure and is responsible for applying it and
re-syncing.

**State**: reads from triples table and current file on disk.

**Operations**:
- List blocks in a node (with scope)
- Resolve a specific block by node+path or by subject
- Prepare an append to a node's body
- Prepare an insert under a named heading (with optional heading creation)
- Prepare a block extraction into a new node
- Render a block back to markdown

### Block diffing and identity tracking

Compare the stored block structure of a node against its current file on disk.
Classify each block as unchanged, edited, scope-changed, reordered, inserted,
or deleted. Uses fuzzy matching via shingle anchors, text hashes, and a scored
candidate resolution system.

This cuts across sync (where it assigns stable subjects) and inspection (where
it shows what changed). The matching core (`resolveBlockMatches`) is shared.

**State**: reads stored blocks from triples table, reads current file from
disk.

**Operations**:
- Diff two block lists (pure function on parsed data)
- Build a rich block diff for a node (stored vs. current file)
- Compute block identity hashes (text, scope, tag, anchor shingles)

### Inbox overview

Summarize a container node's children with kind classification and content
metrics. Classifies children as `"note"`, `"capture"`, `"discussion"`,
`"date"`, `"empty"`, or `"error"`. This is an opinionated view -- the
classification logic embeds domain knowledge about what kinds of notes exist.

**State**: reads from triples table and files on disk.

**Operations**:
- Build inbox overview for a node (returns classified child summaries)

### Prototype subsystems (not wired to main paths)

Two self-contained experiments:
- **Construction diffing**: models structural change tracking for stable-ID
  node sets across snapshots
- **AST rematching**: models how stable IDs might be recovered after a tree
  is rebuilt without them

Neither is currently called from sync, query, or edit paths.

---

## From `internal/engine`

### Pipeline execution

Drive a function's LLM steps forward sequentially. For each step: resolve
model/agent config, render prompt with context, call the backend, parse output.
Handle composed steps (delegate to another function) and map-over steps (run a
sub-pipeline for each related node). Returns `Either[Suspension, StepResult]`
-- the type system forces callers to handle both cases.

`EvalStep` itself never suspends -- it always returns Right. Suspension
decisions live in `RunPipeline`, which checks for gates and final-ops steps.
Dry-run mode renders the prompt and returns without calling the LLM or writing
to the DB.

**State**: reads from triples table (via graph walk and context resolution).
Calls backend for inference. Writes log entries via `apply.AppendLogDB`.

**Operations**:
- Run a full pipeline from a given start step
- Evaluate a single step
- Evaluate a composed/map-over step

### Suspension lifecycle

Write, find, list, and resolve suspension records in the database. A
suspension is a paused pipeline waiting for human review -- persisted as
triples keyed by a generated subject ID.

**State**: `suspension:*` triples in the database.

**Operations**:
- Write a new pending suspension
- Find the most recent pending suspension for a node
- Find a suspension by exact subject ID
- Find all pending suspensions for a node
- List all pending suspensions (optionally by root)
- Resolve a suspension (mark as accepted/rejected)

**Boundary issue**: `RunPipeline` both runs steps AND writes suspensions,
coupling these two responsibilities. Agent-mode `submit` creates suspensions
without running a pipeline, showing they can be independent. The revision
path (`ReviseStep`) re-runs an LLM call on an existing suspension but does
NOT write a replacement suspension -- callers are responsible for that. The
REPL writes the replacement; the CLI does not. This is a documented source
of bugs.

### Revision

Re-run a previously suspended step with human feedback injected into the
prompt. Builds a revision history from the log (prior attempts as XML),
appends the feedback, and calls the LLM again.

This straddles pipeline execution and suspension lifecycle: it performs
inference (pipeline concern) against an existing suspension (lifecycle
concern) but delegates suspension replacement to the caller (boundary issue
noted above).

**State**: reads log entries and suspension from DB, calls backend.

**Operations**:
- Revise a step with feedback
- Build revision history XML from log entries

---

## From `internal/apply`

Note: this single package carries at least fourteen distinct responsibilities.
The survey itself describes it as "the central engine for sevens," despite the
existence of a separate `internal/engine` package. This naming confusion
reflects the organic accumulation of concerns.

### Domain type definitions

All the core vocabulary types for the system: `Function`, `Step`, `PathSpec`,
`Require`, `AgentConfig`, `FileOp`, `LogEntry`, `NodeTemplate`,
`GlobalConfig`, `LLMConfig`, `BackendConfig`, `ResolvedContext`,
`ResolvedNode`, `ResolvedBlock`, `PromptVars`. Nearly every other package
depends on these types.

**State**: none (type definitions only).

### Function definition loading

Read function definitions from EDN files with markdown sidecar prompts.
User config dir (`~/.config/sevens/functions/`) with bundled fallback from
`defaults/`. Validates composition (output/input chaining) after load.

**State**: reads from filesystem.

**Operations**:
- Load a named function
- List all available functions (user + bundled)

### Context resolution

Given a function's `Requires` and `Context` declarations, a graph walk, and
optionally a block target, fetch the right nodes, siblings, children, history,
and cross-walk output from the triple store. Assembles a `ResolvedContext`
ready for prompt rendering.

**State**: reads from triples table via graph queries.

**Operations**:
- Determine effective requires for a function/step
- Resolve full context from graph
- Check whether a function uses the requires system

### Prompt rendering

Substitute `{{variable}}` tokens into prompt templates. Two paths:
- Simple: direct string replacement for functions without `Requires`
- Full: render from a `ResolvedContext` with all resolved nodes, history,
  cross-walk output, context files, and block-specific variables

**State**: none (pure string transformation).

**Operations**:
- Render a step prompt (simple path)
- Render with full resolved context
- Format resolved nodes as XML content blocks
- Format history as XML block

### Path evaluation

Walk a morphism path specification through the triple store. Each predicate
in the path is traversed forward or inverse (suffix `"~"`), one hop per SQL
query. Deduplicates and sorts terminal nodes, optionally fetches additional
predicates from them.

**State**: reads from triples table.

**Operations**:
- Evaluate one path spec from a starting subject
- Evaluate all path specs for a function

### LLM output parsing

Extract structured `FileOp` JSON from LLM text responses. Handles code fence
wrappers, truncated responses, and per-op validation.

**State**: none (pure parsing).

**Operations**:
- Parse ops from LLM output string

### File operation execution

Apply `[]FileOp` to the filesystem. Create new markdown files with generated
frontmatter. Edit existing files via exact string replacement with fuzzy
match fallback and interactive confirmation when exact match fails.

**State**: reads file paths from triples table, reads/writes files on disk.

**Operations**:
- Execute a list of file operations (create/edit)

### Template loading and execution

Completely separate from the function pipeline. Load `NodeTemplate`
definitions from EDN with markdown sidecar content. Resolve variables
(builtins like `date`/`time` + user params). Three execution modes:
create-node, append-node, insert-block. Can bootstrap missing parent nodes
via a parent template. Writes sibling role triples after file creation.

**State**: reads/writes filesystem, reads/writes triples table.

**Operations**:
- Load a named template
- List all templates
- Resolve/bind template variables
- Check for missing required variables
- Render a template (substitute variables)
- Preview what execution would do (no writes)
- Execute a template (create/append/insert files, write role triples)
- Generate FileOps from a rendered template
- Extract sibling role triples from a template

### Git operations

Thin wrappers over `git` CLI. All scoped to a root directory.

**State**: git repository on disk.

**Operations**:
- Check if directory is a git repo
- Check for uncommitted changes
- Commit all changes (git add -A)
- Commit specific files
- List changed files (filtered to .md and .sevens.edn)
- Revert a commit (stash, revert, pop)

### Event logging

Append-only log of operations performed on nodes. Stored as triples keyed
by a generated subject encoding timestamp, node title, and random suffix.

**State**: `log:*` triples in the database.

**Operations**:
- Append a log entry
- Read log entries for a node (optionally scoped by root)

### Session / focus persistence

Persist the currently focused node and its context inclusions/exclusions
across CLI invocations. Stored as an EDN file on disk. Independent of the
database.

**State**: `~/.config/sevens/session.edn` file.

**Operations**:
- Get session file path
- Save session
- Load session
- Clear session

### Direct LLM invocation

Make a streaming Anthropic API call. This duplicates functionality in
`internal/backend` -- `apply.CallLLM` is a direct Anthropic call while
`backend.AnthropicBackend.Complete` is also a direct Anthropic call. The
engine uses `backend.Backend` when provided but falls back to
`apply.CallLLM` when not. Two paths to the same thing.

**State**: none (stateless API call).

**Operations**:
- Call LLM with system prompt and user prompt, optionally streaming
- Load global config from disk

### Token counting and cost estimation

Count tokens via the Anthropic API (with character-based fallback), look up
per-model pricing, estimate cost. Used for pre-call confirmation prompts.

**State**: none.

**Operations**:
- Count tokens for a prompt
- Look up model pricing
- Confirm cost with user (auto-approve below threshold)

### Global configuration loading

Read `~/.config/sevens/config.edn` into `GlobalConfig`. Apply defaults.
Resolve named model profiles (e.g., "fast" -> specific model ID with
inherited settings).

**State**: reads `config.edn` from disk.

**Operations**:
- Load global config
- Resolve a named model profile

---

## From `internal/repl`

Note: the REPL reimplements much of the orchestration that `cmd/sevens` also
performs (pipeline execution, accept/reject, revert, template execution, git
commits, resync). The domain operations are the same; the REPL adds persistent
in-memory state and two interactive modes. The accept/reject paths diverge
from the CLI in how they handle suspension replacement after revision -- a
documented source of bugs.

### Interactive focus and navigation

Maintain a focused node (and optionally a focused block within it) across
commands. Track context includes/excludes. Keep numbered list state for
numeric selection from prior output. This is richer than the CLI's
`session.edn` persistence -- the REPL holds ephemeral navigation state
(last list, last blocks) that doesn't survive process exit.

**State**: in-memory on the `REPL` struct (focus, focusBlock, includes,
lastList, lastBlocks, model/backend overrides).

**Operations**:
- Set/clear focus (node and block)
- Navigate up/down/sibling by index
- Numeric selection from last printed list
- Include/exclude context nodes and groups
- Override model and backend for the session

### Discussion mode

A multi-turn conversation workflow. Creates a child node named
`"Discussion - <Title>"` holding `[agent ...]` and `[user ...]` turns.
Has its own lifecycle with state tracked on the REPL struct.

Entering discussion: runs the `discuss` function pipeline, auto-accepts the
result (creating the discussion file), commits a draft to git as a revert
anchor, shows the agent's response, then enters `ModeDiscussion`.

In discussion mode: user text is appended as `[user ...]` turns, the discuss
function is re-run, the agent response is auto-accepted and shown. `.end`
commits and exits. `.cancel` attempts to revert the draft commit (or remove
the file / restore from git).

**State**: in-memory (discussNode, discussFilePath, discussFileCreated,
discussCommit). Filesystem (discussion .md file). Git (draft commit). Triples
(discussion node in graph after sync).

**Operations**:
- Enter discussion (run pipeline, auto-accept, commit draft, enter mode)
- Handle user input (append turn, re-run pipeline, show response)
- End discussion (commit, resync, exit mode)
- Cancel discussion (attempt revert of draft commit, resync, exit mode)

**Boundary issue**: cancel semantics are unreliable -- the code review and
exploratory test report document that `.cancel` says "discarded" but the
file and commit may remain.

### Note mode

Buffer text input line by line. On `.end`, append all buffered text to the
focused node's file under a `## Notes` heading, commit, and resync. On
`.cancel`, discard the buffer.

**State**: in-memory (noteLines buffer, mode flag).

**Operations**:
- Enter note mode
- Buffer a line of input
- End note (append to file, commit, resync)
- Cancel note (discard buffer)

### Command dispatch and input handling

The grammar, tab completion, mode routing, flag parsing, and output
formatting for the interactive shell. Not a domain responsibility -- this is
the interaction layer that routes user input to the domain operations above
and in other packages.

**Operations**:
- Parse and dispatch command input (seven-level precedence)
- Handle dot commands (session config, help, info)
- Tab completion (context-sensitive by command)
- Parse inline flags (--model, --backend, --with, etc.)
- Auto-resync before most commands

---

## From `cmd/sevens`, `internal/ui`, `defaults`

### CLI orchestration

Wire together all internal packages to implement 21 cobra commands. No
domain logic of its own -- resolves root, opens DB, dispatches to graph
queries, pipeline execution, template execution, suspension management,
and git operations. Has its own `runPipeline` that parallels the REPL's
`runPipeline`, both assembling the same `engine.PipelineConfig` and calling
`engine.RunPipeline`.

Two paths that differ between CLI and REPL in practice:
- `accept --with` in CLI calls `engine.ReviseStep` but does not write a
  replacement suspension (known bug)
- `submit` hardcodes step index 0 regardless of actual step (known bug)

**State**: none of its own. Reads/writes via all other packages.

### Terminal rendering

Stateless presentation helpers. Render markdown to ANSI terminal output via
glamour. Format node headers, pipeline progress, costs, file operations, log
entries, pending suspensions, and agent task checklists. Provide lipgloss
style variables for consistent color/formatting. Theme management (light/dark)
controls glamour renderer.

Also defines `PrepareData`/`PrepareStep` types used to render the agent-mode
task checklist (`RenderPrepareChecklist`). These are presentation-layer types
consumed only by the CLI.

**State**: theme setting (in-memory).

### Bundled asset storage

Embedded filesystem containing 18 function definitions and 6 template
definitions (EDN + markdown sidecar files). Pure data package with four
read/list functions. Consumed by `internal/apply` as fallback when user
config directory doesn't have a definition.

Also contains an unembedded `orthography/` directory with parser/tokenizer
definitions that are not currently wired into the main paths.

**State**: compiled into the binary (read-only).
