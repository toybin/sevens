# Sevens Walkthrough

A hands-on guide to every feature in sevens, organized as chapters. Each chapter builds on the previous one. By the end, you'll know how to build a knowledge graph, use AI functions to analyze and grow it, manage multi-step pipelines, create templates, work in the REPL, query the triple store directly, and use agent mode for external AI integration.

This guide assumes you're comfortable with a terminal and have sevens built (`go build -o sevens ./cmd/sevens/`).

---

## Chapter 1: Your First Knowledge Graph

### Creating a root

A "root" is a directory that contains your knowledge graph. Initialize one:

```
sevens init ~/Documents/commons --alias commons
```

This creates three things:
1. The directory (if it doesn't exist)
2. A `.sevens.edn` config file inside it
3. A git repository (if not already one)

The `.sevens.edn` file is minimal:

```edn
{:path "~/Documents/commons"
 :alias "commons"}
```

### Writing markdown files

Create a markdown file in your root directory. Every file needs frontmatter with at least a `title`:

```markdown
---
title: The Commons
---

A neighborhood that has one place that's a library but also a tool library
and a seed bank and a repair cafe all mashed together. The idea is shared
infrastructure for things that don't make sense to own individually.
```

The filename doesn't matter for identity -- sevens uses the `title` field. By convention, filenames are lowercase kebab-case (e.g., `the-commons.md`), but this is just for your convenience on disk.

### Creating child nodes

Child nodes point to their parent with the `parent` field:

```markdown
---
title: Lending Infrastructure Design
parent: "[[The Commons]]"
---

The technical challenge of creating unified lending systems that work across
completely different domains -- a book and a table saw have different checkout
periods, care requirements, liability profiles...
```

The `[[wiki-link]]` syntax in the parent field creates the tree relationship. You can also use wiki-links in body text to create cross-references between any nodes:

```markdown
This connects to the ideas in [[Commons Governance Models]] about
who decides lending policies.
```

### Syncing

After creating or editing files, sync them into the database:

```
sevens sync
```

Output:

```
[sync] scanned 3 files, 87 triples
```

If you're inside the root directory, sevens finds it automatically. If you're elsewhere, pass `--root`:

```
sevens sync --root ~/Documents/commons
```

If you have multiple roots registered, running `sevens sync` with no root specified syncs all of them.

### Exploring the tree

**Overview** -- see the full tree:

```
sevens overview
```

```
The Commons
├── Lending Infrastructure Design
├── Commons Governance Models
└── Multi-Use Space Design
```

**Walk** -- see a node's content and neighborhood:

```
sevens walk "The Commons"
```

```
The Commons
children: Lending Infrastructure Design, Commons Governance Models, Multi-Use Space Design
────────────────────────────────────────────────────────────
A neighborhood that has one place that's a library but also a tool library
and a seed bank and a repair cafe all mashed together...
```

**Tree** -- see just the subtree rooted at a node:

```
sevens tree "Lending Infrastructure Design"
```

**Search** -- find nodes by title or content:

```
sevens search "governance"
```

```
Title matches:
  Commons Governance Models

Content matches:
  Lending Infrastructure Design
```

---

## Chapter 2: AI Functions

### What functions are

A function is a named AI operation. It declares three things:

1. **What context to gather** from the graph (path specs)
2. **What instructions to give the AI** (prompt template, persona)
3. **What to do with the result** (display text, or create/edit files)

### Applying a function

The simplest use case -- run an analysis function:

```
sevens apply notice "The Commons"
```

```
[notice] notice -> "The Commons"
[cost] 2847 tokens, ~$0.0043 (auto-approved, below $0.01)

**Gaps** -- There's no discussion of how the institution handles failure modes.
What happens when a tool is returned broken?

**Tensions** -- The governance section wants distributed authority but the
revenue model assumes central management.

**Assumptions** -- The document assumes a physical neighborhood with walkable density.
```

The `notice` function reads the target node, its parent, siblings, and children, then asks the AI to surface patterns, gaps, tensions, and assumptions. The output is text -- it doesn't modify the graph.

Other analysis functions:

```
sevens apply challenge "The Commons"        # devil's advocate
sevens apply contradict "The Commons"       # find inconsistencies with other nodes
sevens apply thesis "The Commons"           # what is this subtree trying to say?
sevens apply synthesize "The Commons"       # patterns across the neighborhood
```

Structure-modifying functions create or edit files:

```
sevens apply elaborate "Lending Infrastructure Design"    # expand sparse content
sevens apply sharpen "Commons Governance Models"          # rewrite for precision
sevens apply trim "The Commons"                           # remove redundancy
sevens apply summarize "The Commons"                      # condense verbose content
```

### How context is gathered

Each function declares path specs that walk the graph to gather context. For example, `notice` gathers:

- **parent**: follow `node/parent` forward, fetch `node/content`
- **siblings**: follow `node/parent` forward then `node/parent~` (inverse, i.e., children of parent), exclude self, fetch `node/content`
- **children**: follow `node/parent~` (inverse), fetch `node/content`

The `~` suffix means inverse traversal. `node/parent~` from a node finds everything that has that node as its parent -- i.e., its children.

Functions also have context policies:
- **minimal**: only the target node (used by `challenge` to force evaluation on its own terms)
- **neighborhood**: target plus parent, siblings, and children (most functions)
- **full**: entire subtree

### The function definition format

Functions are defined as EDN files with markdown prompt sidecars. Here's `challenge.edn`:

```edn
{:name "challenge"
 :description "Devil's advocate -- stress-test claims, assumptions, and blind spots"
 :agent {:persona "critic"
         :system-prompt "Find weaknesses and name them directly..."
         :context-policy "minimal"}
 :requires [{:role "history"}]
 :input "node"
 :output "text"}
```

The prompt template lives in `challenge.md` alongside it. The persona field shapes the AI's behavior. The context-policy limits what the AI sees.

Multi-step functions have per-step prompt files. `decompose` has:
- `decompose.edn` -- the function definition with two steps
- `decompose.suggest.md` -- prompt for the suggestion step
- `decompose.generate.md` -- prompt for the generation step

### Creating your own functions

```
sevens define my-analysis --description "Analyze the argument structure"
```

This creates two files in `~/.config/sevens/functions/`:
- `my-analysis.edn` -- the function definition
- `my-analysis.md` -- the prompt template (edit this)

The generated prompt template includes placeholders for `{{title}}`, `{{parent}}`, `{{content}}`, and `{{context}}`. Edit the `.md` file to customize what the AI does.

You can also create functions with an inline prompt:

```
sevens define quick-check --description "Quick structural check" --prompt "Is this note well-structured? Point out any problems."
```

List all available functions:

```
sevens functions
```

### Dry-run mode

Preview what a function will send to the AI without actually calling it:

```
sevens apply notice "The Commons" --dry-run
```

This prints the fully rendered prompt, useful for debugging custom functions.

---

## Chapter 3: Pipelines and Approval

### Multi-step functions

Some functions have multiple steps. `decompose` is the canonical example:

1. **suggest**: AI proposes child topics for a dense node
2. **generate**: AI creates content for the approved children

Between steps, the pipeline pauses at a "gate" for your review.

### The gate system

```
sevens apply decompose "The Commons"
```

```
[decompose] suggest -> "The Commons"
[cost] 3102 tokens, ~$0.0047 (auto-approved, below $0.01)

Proposed children:
  1. Commons Governance Models
  2. Commons Revenue and Sustainability
  3. Lending Infrastructure Design
  4. Multi-Use Space Design

Run `sevens accept "The Commons"` to apply.
    or: sevens accept "The Commons" --with "feedback"
    or: sevens reject "The Commons"
```

The pipeline is now in `Pending` state. You have three options:

**Accept** -- approve the suggestions and advance to the next step:

```
sevens accept "The Commons"
```

**Reject** -- discard the suggestions entirely:

```
sevens reject "The Commons"
```

**Revise** -- re-run the step with your feedback:

```
sevens accept "The Commons" --with "Add a node about existing community infrastructure"
```

### Revision with feedback

When you revise, the AI sees your feedback alongside the previous attempt and produces a new suggestion:

```
sevens accept "The Commons" --with "Add a node about existing community infrastructure"
```

```
[decompose] suggest (revision) -> "The Commons"

Revised children:
  1. Commons Governance Models
  2. Commons Revenue and Sustainability
  3. Existing Community Infrastructure    <- added
  4. Lending Infrastructure Design
  5. Multi-Use Space Design

Run `sevens accept "The Commons"` to apply.
```

You can revise multiple times. The revision chain is preserved according to the gate's history policy:
- **none**: stateless retry -- AI sees only your latest feedback
- **latest**: AI sees the most recent attempt plus your feedback
- **full**: AI sees the complete chain of attempts and feedback

### The pipeline state machine

Pipeline states and transitions:

```
Running     step is executing
Pending     gated step produced a result; awaiting review
Accepted    human approved; ready to advance
Looping     in a loop (discussion mode), accumulating results
Rejected    human rejected (terminal)
Cancelled   pipeline discarded (terminal)
Completed   all steps done (terminal)
```

Key transitions:
- `apply` -> Running -> Pending (if gated)
- `accept` -> Pending -> Accepted -> next step
- `reject` -> Pending -> Rejected
- `accept --with` -> Pending -> Running -> Pending (revision)

Pipeline state is persisted as triples in the database. You can close your terminal, come back the next day, and resume:

```
sevens pending
```

```
The Commons  decompose  step 0  pipeline:abc123:20260410T143200:def456
```

```
sevens accept "The Commons"
```

### The audit function

`audit` demonstrates multi-step composition. It runs two steps:

1. **observe**: delegates to the `notice` function, pauses at a gate
2. **stress-test**: uses a critic persona to challenge the observations

Each step can be reviewed independently.

### Discussion mode

The `discuss` function creates an interactive conversation:

```
sevens discuss "Lending Infrastructure Design"
```

The AI (using a facilitator persona) asks probing questions. You respond in your terminal. Each exchange is a loop iteration that appends to a conversation transcript stored as a discussion child node.

Discussion is also available through the REPL (see Chapter 5).

---

## Chapter 4: Deterministic Functions (Templates)

### What deterministic functions are

Not everything needs AI. Deterministic functions create predictable structure without calling an LLM -- no API key needed. They're defined the same way as AI functions but with `{:backend {:kind "deterministic"}}` in their step definition.

### Built-in templates

List them:

```
sevens templates
```

```
daily-note       Create or scaffold today's daily note under inbox
inbox-capture    Create a quick capture note under inbox
inbox-root       Bootstrap the inbox container node
append-note      Append a timestamped note to a target node
section-entry    Insert a bullet under a specific heading
```

### Creating nodes from templates

**Daily note:**

```
sevens new --template daily-note
```

Creates a node titled with today's date (e.g., "2026-04-11") under an "inbox" parent. If the inbox node doesn't exist, sevens bootstraps it automatically using the `inbox-root` template.

**With variables:**

```
sevens new --template daily-note --set summary="Planning session for Q3"
```

### Quick capture

The `capture` command is a shortcut for `inbox-capture`:

```
sevens capture "insurance liability model"
```

This creates a new node titled "insurance liability model" under inbox. You can override the parent:

```
sevens capture "insurance liability model" --parent "Lending Infrastructure Design"
```

### The instantiate command

For more control, use `instantiate` directly:

```
sevens instantiate append-note --text "Revisit the deposit model" --at .
```

This appends a timestamped note to the focused node.

```
sevens instantiate section-entry --heading "Open Questions" --text "What about liability?" --at "The Commons"
```

This inserts a bullet under the "Open Questions" heading in "The Commons", creating the heading if it doesn't exist.

### Preview mode

See what a template would produce without writing files:

```
sevens new --template daily-note --dry-run
```

```
Template Preview
function: daily-note
mode:     create-node
title:    2026-04-11
parent:   inbox
────────────────────────────────────────────────────────────
# 2026-04-11
```

### The deterministic backend

Deterministic functions support three modes:

- **create-node**: creates a new node with a title pattern, parent, and content template
- **append-node**: appends content to an existing node
- **insert-block**: inserts content under a specific heading in a node

Each mode uses template variables (`{{date}}`, `{{time}}`, `{{title}}`, `{{summary}}`, etc.) that are resolved at execution time. User-provided variables via `--set key=value` merge with built-in variables.

---

## Chapter 5: The REPL

### Starting a session

```
sevens repl "The Commons"
```

This starts an interactive session focused on "The Commons". If you omit the node title, the REPL uses your active focus session (from `sevens focus`).

### Navigation

| Command | Effect |
|---|---|
| `<title>` | Focus a node by title |
| `focus <title>` / `f <title>` | Explicit focus (when title matches a command name) |
| `..` / `up` | Move to parent |
| `root` | Clear focus (go to root) |
| `child <n>` / `c <n>` | Focus the Nth child |
| `sibling <n>` / `s <n>` | Focus the Nth sibling |
| `<n>` | Focus the Nth item from the last printed list |

When you navigate, the REPL shows the node's neighborhood:

```
The Commons> child 1
Lending Infrastructure Design
parent: The Commons
children: (none)
siblings: Commons Governance Models, ...
────────────────────────────────────────────────────────────
The technical challenge of creating unified lending systems...
```

### Viewing commands

| Command | Effect |
|---|---|
| `walk` | Walk the focused node (same as the CLI `walk`) |
| `children` | List children with numbers |
| `siblings` | List siblings with numbers |
| `overview` | Print full tree |
| `blocks` | List block structure of the focused node |
| `blocks <title>` | List blocks for another node |
| `diff-blocks` | Show block-level changes since last sync |
| `inbox` | List inbox children |
| `inbox <title>` | List children of another container |
| `search <query>` | Search titles and content |
| `pending` | List pending suggestions |
| `log` | Show operation log for the focused node |
| `sync` | Sync filesystem changes into the graph |

### Applying functions interactively

Type a function name to apply it to the focused node:

```
The Commons> notice
[notice] notice -> "The Commons"
...

The Commons> challenge
[challenge] challenge -> "The Commons"
...
```

Override the model:

```
The Commons> notice --model fast
```

Preview the prompt:

```
The Commons> notice --dry-run
```

Accept and reject pending results:

```
The Commons> accept
The Commons> reject
```

### Discussion mode

Start a discussion:

```
The Commons> discuss
```

The AI asks a probing question. Type your response directly. The AI responds. Continue the conversation as long as you want.

End the discussion (saves the transcript):

```
.end
```

Cancel (discards the transcript):

```
.cancel
```

Non-interactive mode (single exchange):

```
The Commons> discuss -n
```

### Note mode

Quick annotation appended to the focused node:

```
The Commons> note
```

Type your note, then press Enter. It's appended with a timestamp.

### Block operations

Select a block by number from a `blocks` listing, then apply functions to it or extract it:

```
The Commons> blocks
  p.0    paragraph   A neighborhood that has one place...
  h.1    h2          Lending Model
  p.2    paragraph   The technical challenge of creating...

The Commons> extract-block h.1
```

### Session commands

| Command | Effect |
|---|---|
| `.info` | Show current state (focus, root, model, includes) |
| `.model <tier>` | Switch model (fast, capable, powerful, or raw model ID) |
| `.backend <name>` | Switch inference backend |
| `.theme light\|dark` | Switch rendering theme |
| `.dry` | Toggle dry-run mode |
| `.include <title>...` | Add nodes to context |
| `.include clear` | Clear all context includes |
| `.exclude <title>` | Remove a node from context |
| `.functions` | List available functions |
| `.help` | Show full command list |
| `.quit` | Exit the REPL |

---

## Chapter 6: Blocks

### What blocks are

Blocks are structural elements within a node's content -- headings, paragraphs, list items, tasks. Each block is tracked as its own entity in the graph with `block/*` predicates:

- `block/node` -- which document node this block belongs to
- `block/content` -- the block's text
- `block/kind` -- heading, paragraph, list-item, task, etc.
- `block/path` -- structural position (e.g., `h.1`, `p.2`, `li.3`)
- `block/scope` -- the heading scope containing this block

### Listing blocks

```
sevens blocks "The Commons"
```

```
The Commons
────────────────────────────────────────────────────────────
  p.0     paragraph   A neighborhood that has one place that's a library but also...
  h.1     h2          Lending Model
  p.2     paragraph   The technical challenge of creating unified lending systems...
  h.3     h2          Governance
  p.4     paragraph   How decisions get made about what to stock, lending periods...
```

### Diffing blocks

See what changed since the last sync:

```
sevens diff-blocks "The Commons"
```

```
The Commons
the-commons.md
────────────────────────────────────────────────────────────
Edited:
  p.2  The technical challenge has been revised to include...
    scope: Lending Model

Inserted:
  p.5  New paragraph about insurance requirements
    scope: Governance

Deleted:
  p.3  Old paragraph about checkout periods
```

Include unchanged blocks for a complete picture:

```
sevens diff-blocks "The Commons" --unchanged
```

### Extracting blocks into new nodes

Extract a heading section into a new child node:

```
sevens extract-block "The Commons" h.1
```

This takes the "Lending Model" heading and all content under it, creates a new node titled "Lending Model" as a child of "The Commons", and removes the extracted content from the source.

Extract with a custom title and parent:

```
sevens extract-block "The Commons" h.1 "Lending Infrastructure Design" --parent "The Commons"
```

### Block identity across edits

Blocks maintain stable identity across edits using structural path tracking. When you edit a paragraph, sevens matches it to the same block in the previous sync using fuzzy text matching and structural position. This means block-level operations (like diffing) work correctly even when you've added, removed, or reordered content.

---

## Chapter 7: Agent Mode

### What agent mode is

Agent mode is for when you're already working inside an AI assistant (like Claude Code or Codex) and want the assistant to use sevens as a tool. Instead of sevens calling the LLM, the external AI calls sevens.

### The prepare checklist

```
sevens prepare notice "Lending Infrastructure Design"
```

Output:

```
[task] notice -> "Lending Infrastructure Design"

[read] target
  $ sevens walk "Lending Infrastructure Design"

[read] parent
  $ sevens walk "The Commons" --depth 0

[read] siblings (4 nodes)
  $ sevens walk "Commons Governance Models" --depth 0
  $ sevens walk "Commons Revenue and Sustainability" --depth 0
  $ sevens walk "Existing Community Infrastructure" --depth 0
  $ sevens walk "Multi-Use Space Design" --depth 0

[instruction]
  Examine this node and its neighborhood. Surface what the author
  can't easily see from inside their own thinking...

[output] text
[submit]
  $ sevens submit "Lending Infrastructure Design" --function notice \
      --step notice --output text --response-file /tmp/notice-notice.txt
```

This is a structured checklist telling the external AI:
1. What files to read (via `sevens walk` commands)
2. What instruction to follow
3. How to submit the result

### Submitting results

The external AI writes its response to a file and submits:

```
sevens submit "Lending Infrastructure Design" \
  --function notice \
  --step notice \
  --output text \
  --response-file /tmp/notice-notice.txt
```

For functions that produce file operations (like `decompose` or `elaborate`), use `--output ops`. For suggestion steps, use `--output suggestions`. The response is validated and the pipeline state machine advances.

If the output type is `ops` or `suggestions`, the pipeline enters `Pending` state and you can review with `sevens accept` or `sevens reject`.

### When to use agent mode vs standalone

**Standalone** (`sevens apply`): You have an API key configured. Sevens calls the LLM directly. Simpler workflow.

**Agent mode** (`sevens prepare` / `sevens submit`): You're already in an AI assistant session. The assistant reads the checklist, does the work, and submits. Useful when:
- Your AI access is through a workplace tool (no separate API key)
- You want the assistant to have additional context beyond what sevens gathers
- You want to use a specific model that sevens doesn't directly support

---

## Chapter 8: Configuration

### Global config

Location: `~/.config/sevens/config.edn`

```edn
{:llm {:provider "anthropic"
       :model "claude-sonnet-4-20250514"
       :api-key-env "ANTHROPIC_API_KEY"}
 :models {"fast" {:model "claude-haiku-4-20250514"}
          "capable" {:model "claude-sonnet-4-20250514"}
          "powerful" {:model "claude-opus-4-20250514"}}
 :backend "anthropic"
 :cost-threshold 0.01
 :theme "dark"
 :system-prompt "You are analyzing a personal knowledge graph..."
 :context-files ["~/notes/style-guide.md"]}
```

If this file doesn't exist, sevens uses sensible defaults (Anthropic, Claude Sonnet, API key from `ANTHROPIC_API_KEY` environment variable).

### Root config

Location: `.sevens.edn` in each root directory

```edn
{:path "~/Documents/commons"
 :alias "commons"
 :max-chars 5000}
```

Created by `sevens init`. The `:path` and `:alias` are set at init time. `:max-chars` is optional and sets the character limit validation threshold.

### Model profiles

Define named profiles in the global config under `:models`:

```edn
{:models {"fast" {:model "claude-haiku-4-20250514"}
          "capable" {:model "claude-sonnet-4-20250514"}
          "powerful" {:model "claude-opus-4-20250514"}}}
```

Use them:

```
sevens apply notice "The Commons" --model fast
sevens apply decompose "The Commons" --model powerful
```

Profiles inherit unset fields from the default `:llm` config. So a profile that only specifies `:model` inherits the provider and API key.

### Backend configuration

Sevens supports three backend types:

**anthropic** -- direct API calls to Anthropic (default):
```edn
{:llm {:provider "anthropic"
       :model "claude-sonnet-4-20250514"
       :api-key-env "ANTHROPIC_API_KEY"}}
```

**codex** -- delegates to the Codex CLI:
```edn
{:backends {"codex" {:type "codex"
                     :command "codex"
                     :generated-conf-dir "~/.config/sevens/generated/codex"}}}
```

**claude** -- delegates to Claude Code CLI:
```edn
{:backends {"claude" {:type "claude"
                      :command "claude"
                      :generated-conf-dir "~/.config/sevens/generated/claude"}}}
```

Generate MCP configs for CLI backends:

```
sevens config generate codex
sevens config generate claude
sevens config generate all
```

Switch backends per command:

```
sevens apply notice "The Commons" --backend codex
```

Or in the REPL:

```
.backend codex
```

### Cost thresholds

The `:cost-threshold` setting (in USD) controls automatic approval:

```edn
{:cost-threshold 0.01}
```

Operations below this cost are auto-approved without prompting. Set to `0` to always prompt.

### Context files

Global context files are injected into every AI call:

```edn
{:context-files ["~/notes/style-guide.md" "~/notes/project-context.md"]}
```

Functions can also declare their own context files.

### Viewing configuration

```
sevens config show
```

```
Backend: anthropic
Model:   claude-sonnet-4-20250514
MCP Servers (2):
  filesystem -- File system access
  fetch -- Web fetch
```

---

## Chapter 9: The Triple Store

### Everything is triples

Every piece of information in sevens is stored as a triple: `(subject, predicate, object)`. Node content, tree structure, wiki-links, block structure, session state, operation logs -- all the same shape.

Subject strings encode identity:

```
node:<6-byte-hash>:<title>     for nodes
block:<6-byte-hash>:<title>:<path>   for blocks
session:current                for the active session
log:<unique-id>                for log entries
pipeline:<hash>:<timestamp>:<random>  for pipeline state
```

### Querying with sevens query

The `query` command runs SQL directly against the triples table:

```
sevens query "SELECT subject, object FROM triples WHERE predicate = 'node/title'"
```

Template variables are available:
- `{{root}}` -- the current root directory path
- `{{target}}` / `{{focused}}` -- the focused node title (from `sevens focus`)

Find all wiki-links:

```
sevens query "SELECT t1.object AS source, t2.object AS target
  FROM triples t1
  JOIN triples t2 ON t1.subject = t2.subject
  WHERE t1.predicate = 'node/title'
  AND t2.predicate = 'node/link'"
```

Find nodes with high character counts:

```
sevens query "SELECT t1.object AS title, CAST(t2.object AS INTEGER) AS chars
  FROM triples t1
  JOIN triples t2 ON t1.subject = t2.subject
  WHERE t1.predicate = 'node/title'
  AND t2.predicate = 'node/char-count'
  ORDER BY chars DESC LIMIT 10"
```

Find operation history:

```
sevens query "SELECT t1.object AS event, t2.object AS function, t3.object AS node
  FROM triples t1
  JOIN triples t2 ON t1.subject = t2.subject
  JOIN triples t3 ON t1.subject = t3.subject
  WHERE t1.predicate = 'log/event'
  AND t2.predicate = 'log/function'
  AND t3.predicate = 'log/node'
  ORDER BY t1.subject DESC LIMIT 20"
```

### The predicate vocabulary

**Node predicates:**

| Predicate | Multiplicity | Description |
|---|---|---|
| `node/title` | functional | Human-readable name |
| `node/parent` | functional | Single parent (inverse: children) |
| `node/content` | functional | Textual body |
| `node/file` | functional | Path to source file |
| `node/root` | functional | Root directory this node belongs to |
| `node/char-count` | functional | Character count |
| `node/link` | relational | Cross-reference to another node |
| `node/role` | relational | Named role within sibling set |

**Block predicates:**

| Predicate | Multiplicity | Description |
|---|---|---|
| `block/node` | functional | The document node this block belongs to |
| `block/root` | functional | Root directory |
| `block/content` | functional | Block text |
| `block/scope` | functional | Heading scope containing this block |
| `block/kind` | functional | heading, paragraph, list-item, task |
| `block/path` | functional | Structural position (e.g., h.1, p.2) |

**Session predicates:**

| Predicate | Description |
|---|---|
| `session/focus` | Primary node of attention |
| `session/include` | Additional context nodes |
| `session/exclude` | Nodes excluded from context |
| `session/started` | Session start time |

**Log predicates:**

| Predicate | Description |
|---|---|
| `log/event` | Event type (applied, rejected, reverted, etc.) |
| `log/function` | Which function was used |
| `log/node` | Target node |
| `log/step` | Which step produced this entry |
| `log/timestamp` | When it happened |
| `log/commit` | Git commit hash |
| `log/files-created` | Files created by this operation |
| `log/files-edited` | Files edited by this operation |

### Path composition (morphism walks)

Functions gather context by composing predicate paths. The composition `["node/parent", "node/parent~"]` means:

1. From the target, follow `node/parent` forward (get the parent)
2. From the parent, follow `node/parent~` (inverse -- get all children of the parent)
3. Exclude self

This gives you siblings. The composition is the same mechanism as arrow composition in category theory: if you can follow arrow A from X to Y and arrow B from Y to Z, composing A then B gets you from X to Z.

Common compositions:
- **Parent**: `["node/parent"]`
- **Children**: `["node/parent~"]`
- **Siblings**: `["node/parent", "node/parent~"]` (exclude self)
- **Grandparent**: `["node/parent", "node/parent"]`
- **Cousins**: `["node/parent", "node/parent", "node/parent~", "node/parent~"]`

---

## Chapter 10: Architecture

### The concept design approach

Sevens is designed using Daniel Jackson's Concept Design framework. Each major piece of functionality is a "concept" with its own state, actions, and operational principle. Concepts compose through explicit synchronization rules.

The concepts are:
- **Graph** -- bare triple store (CRUD on triples)
- **GraphOps** -- predicate metadata, path composition, functional vs. relational predicates
- **KnowledgeBase** -- the PKM domain model: nodes, blocks, sessions, logs, structural validation
- **Function** -- typed transformations, pipeline state machine, gates, control flow
- **Projection** (contract) -- faithful round-trip between graph state and a human-readable medium

### Layer model

```
triple          Layer 1: bare triple store
  |
graphops        Layer 2: predicate metadata, path composition
  |
kb              Layer 3: PKM domain model
  |             |
function        projection/md
```

Each layer depends only on layers below. The CLI (`cmd/sevens`) and REPL (`internal/repl`) sit above everything and coordinate syncs between concepts.

**Layer 1 (triple):** Stores and retrieves triples. No domain knowledge. Idempotent assertions, retractions, queries by subject/predicate/object.

**Layer 2 (graphops):** Knows that predicates have properties (functional vs. relational, invertible, symmetric, transitive). Provides `set` (replace for functional predicates), `compose` (walk predicate paths), and `reachable` (transitive closure).

**Layer 3 (kb):** The PKM domain. Defines the predicate vocabulary (`node/title`, `node/parent`, `block/content`, etc.), subject identity scheme (`node:<hash>:<title>`), structural queries (walk, overview, children, siblings), validation (7+/-2 constraint, orphan detection), sessions, and logging.

**Function:** Typed transformations on the knowledge base. Defines functions, steps, gates, pipelines, and the state machine. Delegates actual computation to backends (LLM, deterministic, agent).

**Projection:** The contract for reading and writing between the graph and a human-editable medium. The markdown implementation (`projection/md`) handles file I/O, frontmatter parsing, wiki-link extraction, block parsing, git operations, and block identity tracking.

### The projection contract

The projection guarantees round-trip fidelity:
- **render then parse**: produces the same graph state (modulo whitespace)
- **parse then render**: produces the same presentational form (modulo normalization)

The `sync` operation reads files from disk, parses them into triples, reconciles against current graph state, and applies the changeset. The `write` operation renders graph state back to files.

Currently only the markdown format is implemented. The contract is designed to support other formats (org-mode, web app) without changing the graph or function layers.

### Category K connections

The design is informed by an epistemic framework called Category K (see `docs/design/CATEGORY-K.md`). The key ideas that shaped sevens:

- **Triples as binary relations**: the axiom "knowledge is synchronic binary relation" maps directly to the triple store
- **Dumb morphisms, smart objects**: predicates are simple edges; all semantic weight is in the nodes (the intermediary principle)
- **Arrow composition**: path specs compose predicates the same way arrows compose in a category
- **Channel 1 vs Channel 2**: analysis functions (notice, challenge) are Channel 1 (recognition within existing structure); structure functions (decompose, elaborate) are Channel 2 (new information entering the graph)
- **Gates as T-crossing**: the pipeline suspension model makes the diachronic transition explicit -- the AI proposes, the human approves, the partition changes
- **Types as subgraph shapes**: a "node" is recognized by its predicate pattern, not a type field

---

## Quick Reference

### Common workflows

**Start a new project:**
```
sevens init ~/Documents/project --alias project
# create markdown files with title frontmatter
sevens sync
sevens overview
```

**Analyze and grow:**
```
sevens focus "Root Node"
sevens apply notice .
sevens apply decompose .
sevens accept . --with "also add a section about X"
sevens accept .
```

**Daily capture:**
```
sevens new --template daily-note
sevens capture "quick thought about Y"
```

**Interactive exploration:**
```
sevens repl "Root Node"
# navigate, apply functions, discuss, all in one session
```

**Review pending:**
```
sevens pending
sevens accept "Node Title"
# or
sevens reject "Node Title"
```

**Undo a mistake:**
```
sevens revert "Node Title"
```
