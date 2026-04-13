# sevens

Sevens is a local-first thinking tool. You write markdown files about whatever you're working through -- a project, a research topic, an argument, a plan -- and sevens turns them into a structured knowledge graph you can explore, query, and grow with the help of AI. The name comes from cognitive science: people can hold about seven things (plus or minus two) in working memory. When a topic gets too big, break it into five to nine parts. The result is a tree of ideas where each level is small enough to think about clearly.

## Core Mental Model

**Triples.** Everything in sevens is stored as three-part facts: `(subject, predicate, object)`. A node's content, its parent, its wiki-links, its character count, the history of operations applied to it -- all stored as triples in the same table. This means every piece of information is queryable the same way.

**Functions.** Named AI operations that transform your thinking. Each function declares what context to gather from the graph (path specs), what instructions to give the AI (prompt template), and what to do with the result (display text or create/edit files). Functions range from analysis (`notice`, `challenge`) to structure creation (`decompose`, `elaborate`).

**Pipelines.** Multi-step functions that pause for human review. The AI proposes; you accept, reject, or revise with feedback. Pipeline state is persisted -- you can close your terminal and resume later.

**Projection.** The markdown files on disk are a projection of the graph. You edit files in your text editor; `sevens sync` reads them back into the graph. Functions that create or edit nodes write markdown files. Git tracks every change.

## Installation

Requires Go 1.22 or later:

```
go build -o sevens ./cmd/sevens/
```

Set your Anthropic API key (or configure a different backend):

```
export ANTHROPIC_API_KEY=sk-ant-...
```

## Getting Started

Initialize a new knowledge graph:

```
sevens init ~/Documents/my-project --alias project
```

This creates a `.sevens.edn` config file, initializes git, and runs the first sync. Create some markdown files with frontmatter:

```markdown
---
title: The Commons
---

A neighborhood that has one place that's a library but also a tool library
and a seed bank and a repair cafe all mashed together...
```

Then sync and explore:

```
sevens sync
sevens overview
sevens walk "The Commons"
```

## Commands

### Graph

| Command | Description |
|---|---|
| `sevens init [path]` | Initialize a new root (`--alias`, `--max-chars`) |
| `sevens sync` | Scan markdown files and rebuild the database (`--root`) |
| `sevens overview` | Print full tree (`--root`, `--edn`) |
| `sevens walk <title>` | Show a node's content and neighborhood (`--root`, `--shape`, `--edn`) |
| `sevens tree <title>` | Show the subtree rooted at a node |
| `sevens blocks <title>` | List block structure of a node |
| `sevens diff-blocks <title>` | Show block-level changes since last sync (`--unchanged`) |
| `sevens inbox [title]` | Show child summaries for a container node |
| `sevens extract-block <source> <path> [title]` | Create a new node from a block (`--parent`) |
| `sevens roots` | List all registered roots |
| `sevens search <query>` | Search node titles and content |
| `sevens query <sql>` | Run SQL against the triples store (`--all` for all roots; default scopes to current root) |

### Functions

| Command | Description |
|---|---|
| `sevens apply <function> <title>` | Apply a function to a node (`--dry-run`, `--model`, `--backend`) |
| `sevens accept <title>` | Accept pending suggestions (`--with "feedback"` for revision) |
| `sevens reject <title>` | Reject pending suggestions |
| `sevens pending` | List nodes with pending suggestions |
| `sevens functions` | List available functions |
| `sevens templates` | List available templates (deterministic functions) |
| `sevens define <name>` | Define a new function (`--description`, `--prompt`) |
| `sevens prepare <function> <title>` | Compile a function into an agent task checklist |
| `sevens submit <title>` | Submit an agent's response (`--function`, `--output`, `--response-file`) |

### Session

| Command | Description |
|---|---|
| `sevens focus <title>` | Pin a node as active focus (`--include`, `--exclude`) |
| `sevens unfocus` | Clear the active focus session |
| `sevens status` | Show current focus and pending state |
| `sevens log <title>` | Show operation log for a node |

### Structure

| Command | Description |
|---|---|
| `sevens new [title]` | Create a new node (`--template`, `--parent`, `--set key=value`) |
| `sevens capture [title]` | Quick-capture to inbox (`--parent`, `--set`, `--title`, `--summary`) |
| `sevens instantiate <template> [args...]` | Run a template (`--parent`, `--at`, `--set`, `--dry-run`) |
| `sevens revert <title>` | Undo last applied operation on a node |
| `sevens discuss <title>` | Run the discuss function (`--dry-run`, `--model`, `--backend`) |

### Config

| Command | Description |
|---|---|
| `sevens config show` | Show current configuration |
| `sevens config generate <backend>` | Generate MCP configs (codex, claude, all) |

### Interactive

| Command | Description |
|---|---|
| `sevens repl [title]` | Start an interactive REPL session |

## Built-in Functions

### Analysis (text output -- no graph changes)

| Function | Description |
|---|---|
| `notice` | Surface patterns, gaps, tensions, and implicit assumptions |
| `challenge` | Devil's advocate -- stress-test claims and assumptions |
| `contradict` | Find actual inconsistencies with other nodes you already wrote |
| `thesis` | Infer the implicit argument a subtree is trying to make |
| `synthesize` | Detect non-obvious connections across a node's neighborhood |

### Structure (file operations -- creates or edits nodes)

| Function | Description |
|---|---|
| `decompose` | Break a dense node into 5-7 children (two-step pipeline with gate) |
| `elaborate` | Expand sparse content with deeper detail |
| `bridge` | Write a connecting narrative between sibling nodes |
| `scaffold` | Build section structure and placeholder prompts |
| `summarize` | Condense verbose content into a concise summary |
| `sharpen` | Rewrite a node's core claim to be maximally precise |
| `trim` | Remove redundancy, scope drift, and padding |
| `merge` | Synthesize children's content back into the parent |
| `distill` | Extract actionable insights from a discussion |
| `relate` | Propose wiki-links from content to other nodes |
| `promote` | Turn synthesize/notice suggestions into new child nodes |

### Multi-step

| Function | Description |
|---|---|
| `audit` | Two-pass review: notice patterns, then stress-test claims |
| `discuss` | Interactive conversation with a facilitator persona |

### Deterministic (templates -- no AI, no API key needed)

| Function | Description |
|---|---|
| `daily-note` | Create today's daily note under inbox |
| `inbox-capture` | Create a quick capture note under inbox |
| `inbox-root` | Bootstrap the inbox container node |
| `append-note` | Append a timestamped note to a target node |
| `section-entry` | Insert a bullet under a specific heading |

Run `sevens functions` to see available functions with Haskell-style type signatures showing each function's input/output contract (e.g., `node → text`, `node → [file]`).

## Function System

Functions are defined as EDN files with optional markdown prompt sidecars. They live in two places:

- `defaults/functions/` -- built-in functions shipped with sevens
- `~/.config/sevens/functions/` -- user-defined functions

A function definition (`notice.edn`):

```edn
{:name "notice"
 :description "Surface patterns, gaps, tensions, and implicit assumptions"
 :context
 [{:path ["node/parent"]
   :with ["node/content"]
   :as "parent"}
  {:path ["node/parent" "node/parent~"]
   :exclude-self true
   :with ["node/content"]
   :as "siblings"}
  {:path ["node/parent~"]
   :with ["node/content"]
   :as "children"}]
 :input "node"
 :output "text"}
```

The `:context` field declares path specs -- morphism paths through the graph that gather context for the AI. `["node/parent" "node/parent~"]` means "go to parent, then find all children of that parent" -- i.e., siblings. The `~` suffix means inverse traversal.

The prompt template lives in a `.md` sidecar file (`notice.md`). Multi-step functions use `<name>.<step>.md` (e.g., `decompose.suggest.md`, `decompose.generate.md`).

Create a new function:

```
sevens define my-function --description "What it does"
```

This creates both the `.edn` and `.md` files in `~/.config/sevens/functions/`.

## Pipelines and Approval

Multi-step functions pause at gates for human review:

```
sevens apply decompose "The Commons"
# AI suggests children, pipeline suspends

sevens accept "The Commons" --with "Add a node about community infrastructure"
# AI revises suggestions with your feedback, suspends again

sevens accept "The Commons"
# AI generates content for the approved children, commits to git
```

Pipeline state is persisted as triples in the database. You can close your terminal and resume later. Check what's waiting:

```
sevens pending
```

## The REPL

Start an interactive session:

```
sevens repl "The Commons"
```

Navigate with `walk`, `..` (parent), `child 2`, `sibling 1`, or type a node title directly. Apply functions by name: type `notice` to run notice on the focused node. Use `discuss` for interactive multi-turn conversation, `note` for quick annotations, and `.help` for the full command list.

Key REPL commands:
- **Navigation:** `<title>`, `..`, `child <n>`, `sibling <n>`, `root`
- **Viewing:** `walk`, `overview`, `blocks`, `siblings`, `children`, `search`, `inbox`, `log`
- **Functions:** `<function-name>`, `accept`, `reject`, `discuss`, `note`, `revert`
- **Session:** `.info`, `.model <tier>`, `.backend <name>`, `.include <title>`, `.functions`, `.quit`

## Configuration

### Global config: `~/.config/sevens/config.edn`

```edn
{:llm {:provider "anthropic"
       :model "claude-sonnet-4-20250514"
       :api-key-env "ANTHROPIC_API_KEY"}
 :models {"fast" {:model "claude-haiku-4-20250514"}
          "capable" {:model "claude-sonnet-4-20250514"}
          "powerful" {:model "claude-opus-4-20250514"}}
 :backend "claude"
 :cost-threshold 0.01
 :theme "dark"
 :context-files ["~/notes/style-guide.md"]}
```

Use model profiles with `--model fast` or `.model fast` in the REPL.

### Root config: `.sevens.edn`

Created by `sevens init` in each knowledge graph directory:

```edn
{:path "~/Documents/my-project"
 :alias "project"}
```

### Backend configuration

Sevens supports multiple inference backends:

```edn
{:backend "claude"
 :backends {"codex" {:type "codex"
                     :command "codex"
                     :generated-conf-dir "~/.config/sevens/generated/codex"}
            "claude" {:type "claude"
                      :command "claude"
                      :generated-conf-dir "~/.config/sevens/generated/claude"}}}
```

Generate MCP configs for CLI backends:

```
sevens config generate codex
sevens config generate claude
```

## Architecture

Sevens is built as a layered concept architecture:

```
triple          Layer 1: bare triple store (CRUD on triples)
  |
graphops        Layer 2: predicate metadata, path composition
  |
kb              Layer 3: PKM domain model (nodes, blocks, sessions, logs)
  |
function        Typed transformations, pipeline state machine
projection/md   Markdown read/write, git, block tracking
projection/edn  EDN config → triple store sync
types           Node-level type system
  |
workflow        Orchestrates function execution and discussion
cmd/sevens      CLI commands
internal/repl   Interactive REPL
```

Each layer depends only on layers below it. The CLI and REPL are orchestration layers that coordinate syncs between concepts.

For the full design documentation, see:

- `docs/ARCHITECTURE.md` -- package structure and layer overview
- `docs/WALKTHROUGH.md` -- full tutorial from init to pipelines
- `docs/design/concept-*.md` -- concept specifications (Graph, GraphOps, KnowledgeBase, Function, Projection, Types, Config)
