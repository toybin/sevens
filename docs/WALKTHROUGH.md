# Sevens Walkthrough

A hands-on guide to every feature in sevens. Each section builds on the
previous. By the end you will know how to build a knowledge graph, use AI
functions, manage pipelines, create templates, work in the REPL, query the
triple store, and integrate with external agents.

The running example throughout is a knowledge graph called "The Commons" for
organizing ideas about community governance.

---

## 1. What Sevens Is

Sevens is a CLI tool that manages a tree-structured knowledge graph stored as
plain Markdown files. Every file is a node. Nodes have parents, children, and
siblings — forming a tree backed by a triple store (SQLite). AI functions
operate on nodes: analyzing content, proposing edits, creating children,
running multi-step pipelines with human approval gates. Deterministic templates
handle structured creation without an LLM. A REPL provides interactive
navigation, and the whole system is designed for composition with external AI
agents.

---

## 2. Installation and Setup

### Build

```
cd sevens
go build -o sevens ./cmd/sevens/
```

### Initialize global config

```
sevens config init
```

This creates `~/.config/sevens/config.edn` with default settings, seeds
built-in function definitions into `~/.config/sevens/functions/`, and seeds
type definitions into `~/.config/sevens/types/`.

### config.edn overview

The generated config looks like:

```edn
{:llm {:provider "anthropic"
       :model "claude-sonnet-4-20250514"
       :api-key-env "ANTHROPIC_API_KEY"}

 :models {"fast"     {:model "claude-haiku-4-20250514"}
          "capable"  {:model "claude-sonnet-4-20250514"}
          "powerful" {:model "claude-opus-4-20250514"}}

 :backend "claude"  ; default: Claude CLI subprocess. Use "anthropic" for API.

 :cost-threshold 0.01

 :theme "dark"}
```

Key fields:

- `:llm` — default model and API key source
- `:models` — named profiles usable via `--model fast`
- `:backend` — default inference backend (`anthropic`, `claude`, `codex`)
- `:backends` — CLI backend configurations (for Claude CLI or Codex subprocess)
- `:cost-threshold` — operations below this USD amount auto-approve
- `:theme` — terminal rendering theme (`light` or `dark`)
- `:system-prompt` — global prompt prepended to every LLM call
- `:context-files` — files injected into every AI call (supports `~` expansion)

---

## 3. Your First Knowledge Graph

### Initialize a root

```
sevens init ~/Documents/commons --alias commons
```

This creates the directory, a `.sevens.edn` marker file, and initializes git.
The marker file:

```edn
{:path "~/Documents/commons"
 :alias "commons"}
```

### Create some files

```
cd ~/Documents/commons

cat > "The Commons.md" << 'EOF'
# The Commons

A knowledge graph about community governance — decision-making, resource
allocation, and collective action.
EOF

mkdir governance
cat > "governance/Decision Making.md" << 'EOF'
---
title: Decision Making
parent: The Commons
---

How groups make binding decisions. Consensus, voting, delegation,
and hybrid approaches.
EOF

cat > "governance/Resource Allocation.md" << 'EOF'
---
title: Resource Allocation
parent: The Commons
---

How shared resources are distributed and maintained. Commons management,
budgeting, and sustainability.
EOF
```

### Sync

```
sevens sync
```

Output:

```
[sync] Committed changes: a1b2c3d
[sync] scanned 3 files, 42 triples
```

Sync reads every `.md` file under the root, parses frontmatter and content,
and writes triples to the database. If the root is a git repo (it is — `init`
creates one), sync auto-commits any uncommitted changes first.

### Overview

```
sevens overview
```

```
The Commons (1.2k)
├── Decision Making (412)
└── Resource Allocation (389)
```

Shows the full tree with character counts.

### Walk a node

```
sevens walk "Decision Making"
```

```
Decision Making
  parent: The Commons
  children: (none)
  siblings: Resource Allocation
  cross-refs: (none)
────────────────────────────────────────────────────────────
How groups make binding decisions. Consensus, voting, delegation,
and hybrid approaches.

─── The Commons (parent)
A knowledge graph about community governance — decision-making, resource
allocation, and collective action.

─── Resource Allocation (sibling)
How shared resources are distributed and maintained. Commons management,
budgeting, and sustainability.
```

Walk shows the node's content and its graph neighborhood. The default
`neighborhood` shape includes content from parent and sibling nodes.

---

## 4. Walk and Context Shapes

Walk accepts a `--shape` flag that controls how much context is gathered.
Shapes are defined as type definitions in `~/.config/sevens/types/`:

| Shape | What it gathers | Use case |
|-------|----------------|----------|
| `sevens/minimal` | Target node only | Focused edits |
| `sevens/neighborhood` | Target + parent + children + siblings with content | Default; analysis |
| `sevens/children` | Target + children with content | Subtree review |
| `sevens/subtree` | Full recursive subtree from target | Deep analysis |

The `sevens/` prefix distinguishes built-in shapes from user-defined types.
You can omit the prefix as shorthand: `--shape minimal` works the same as
`--shape sevens/minimal`.

```
sevens walk "The Commons" --shape sevens/subtree
```

This prints The Commons and all descendants with their full content.

Walk replaces the need for separate overview/tree/inbox commands for context
gathering — use the shape to control scope. Each shape is stored as an EDN
type definition:

```edn
;; ~/.config/sevens/types/sevens-neighborhood.edn
{:name "sevens/neighborhood"
 :context-policy true
 :description "Target plus parent, children, and siblings with content"
 :gather {:target true :parent true :children true :siblings true}}
```

The `--edn` flag on walk, overview, and tree outputs EDN instead of formatted
text, useful for piping to other tools.

---

## 5. AI Functions

### Applying a function

```
sevens apply notice "Decision Making"
```

This calls the LLM with the node's content and neighborhood, using the
`notice` function's prompt template. The result is displayed as text.

### Dry run

```
sevens apply notice "Decision Making" --dry-run
```

Prints the fully rendered prompt without calling the LLM. Useful for
inspecting what context gets sent.

### Model override

```
sevens apply notice "Decision Making" --model fast
```

Uses the `fast` profile from config (maps to claude-haiku).

### Backend override

```
sevens apply notice "Decision Making" --backend claude
```

Routes through the Claude CLI subprocess instead of the API.

### How context is gathered

Each function definition specifies context paths that determine which related
nodes are included. For example, `notice` gathers parent, siblings, and
children:

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

Context paths traverse the graph: `node/parent` follows the parent edge,
`node/parent~` reverses it (children of a node), and chains like
`["node/parent" "node/parent~"]` compose (parent's children = siblings).

### Output types

Functions produce one of four primitive output types:

| Type | What it does | Gate? |
|------|-------------|-------|
| `text` | Displayed to the user, not persisted | No |
| `create` | Creates new child nodes (ops) | Yes, if multi-step |
| `edit` | Modifies existing nodes (ops) | Yes, if multi-step |
| `suggestion` | Proposes children for review before creation | Yes |

### Function signatures

`sevens list functions` shows each function's type signature:

```
notice :: Parent -> Text          Surface patterns, gaps, tensions...
elaborate :: Node -> Edit         Expand sparse content with follow-up questions
decompose :: Node -> Suggestion -> Create  Break a dense node into 5-7 children...
```

The input type reflects the context policy: functions that gather parent,
siblings, and children show `Parent` (the first required role); functions
with no context requirements show `Node`.

### Built-in function list

**Analysis functions** (output: text):

| Function | Description | Context |
|----------|-------------|---------|
| `notice` | Surface patterns, gaps, tensions, and implicit assumptions | neighborhood |
| `challenge` | Devil's advocate — stress-test claims and assumptions | minimal + history |
| `contradict` | Find nodes that conflict with this node's claims | neighborhood |
| `thesis` | Infer the implicit argument from a node and its children | children |

**Edit functions** (output: edit):

| Function | Description | Context |
|----------|-------------|---------|
| `sharpen` | Rewrite the core claim to be maximally precise | parent |
| `elaborate` | Expand sparse content with follow-up questions | minimal |
| `trim` | Remove redundancy, scope drift, padding | children + parent |
| `merge` | Synthesize children's content back into parent | children |
| `bridge` | Write connecting narrative between a node and siblings | siblings + parent |
| `summarize` | Condense verbose content into a concise summary | minimal |
| `relate` | Propose wiki links from content to other nodes | siblings + parent |
| `scaffold` | Build section structure and placeholder prompts | parent + children |

**Create functions** (output: create):

| Function | Description | Context |
|----------|-------------|---------|
| `promote` | Turn prior suggestions into new child nodes | minimal + cross-walk |
| `distill` | Extract actionable insights from a discussion | parent + siblings |

**Suggestion function** (output: suggestion):

| Function | Description | Context |
|----------|-------------|---------|
| `synthesize` | Detect non-obvious connections across the neighborhood | full neighborhood |

**Multi-step pipelines:**

| Function | Steps | Description |
|----------|-------|-------------|
| `decompose` | suggest (suggestion, gate) -> generate (create) | Break a node into 5-7 children |
| `audit` | observe (text, gate) -> stress-test (text) | Two-pass review: patterns then stress-test |

**Discussion:**

| Function | Description |
|----------|-------------|
| `discuss` | Create a discussion child with questions and observations |

---

## 6. Pipelines and Approval

### Multi-step functions

Some functions have multiple steps. `decompose` first suggests children
(step: suggest), then after approval generates them (step: generate).

```
sevens apply decompose "Decision Making"
```

```
[suggest] decompose → Decision Making

Proposed children:
  1. Consensus Models
     Groups that require unanimous agreement
  2. Voting Systems
     Majority, supermajority, ranked choice
  3. Delegation Patterns
     How authority flows from group to individual

Run `sevens accept "Decision Making"` to approve and continue.
    or: sevens accept "Decision Making" --with "feedback"
    or: sevens reject "Decision Making"
```

### The state machine

A pipeline progresses through phases:

1. **Running** — the current step is executing
2. **Pending** — the step completed and is waiting for approval (at a gate)
3. **Completed** — all steps are done

### Accept

```
sevens accept "Decision Making"
```

Accepts the pending suggestions and runs the next step (generate), which
creates the actual child nodes as Markdown files.

### Accept with revision

```
sevens accept "Decision Making" --with "Add a section on sortition"
```

Re-runs the current step incorporating your feedback. You'll see the revised
suggestions and must run `accept` again to confirm.

### Reject

```
sevens reject "Decision Making"
```

Discards the pending suggestions. The pipeline is abandoned.

### Pending

```
sevens pending
```

Lists all nodes with pending suggestions across the graph:

```
Decision Making  decompose/suggest  (pipeline abc123)
```

---

## 7. Deterministic Functions (Templates)

Templates are functions that run without an LLM. They create or modify nodes
using pattern-based title generation and content templates.

### List templates

```
sevens templates
```

```
daily-note       Create or scaffold today's daily note under inbox
inbox-capture    Create a quick capture note under inbox
inbox-root       Ensure the inbox root node exists
append-note      Append a timestamped note to a target node
section-entry    Insert a bullet under a specific heading in a target node
```

### Capture (quick inbox entry)

```
sevens capture "Sortition research"
```

Creates a node titled "Sortition research" under `inbox/` (bootstrapping
the inbox root node if it doesn't exist).

```
sevens capture "Budget models" --summary "Compare participatory budgeting approaches"
```

### Instantiate a template

```
sevens instantiate daily-note
```

Creates today's daily note (title: `2026-04-12`) under inbox.

```
sevens instantiate append-note --at "Decision Making" --set text="Revisit after reading Ostrom"
```

Appends a timestamped note to the "Decision Making" node.

```
sevens instantiate section-entry --at "Decision Making" --set heading="Open Questions" --set text="How does sortition scale?"
```

Inserts a bullet under the "Open Questions" heading.

### Template modes

Templates operate in one of three modes:

- **create-node** — creates a new Markdown file with frontmatter
- **append-node** — appends content to an existing node
- **insert-block** — inserts content under a specific heading (creates the heading if `create-if-missing` is true)

### Dry run

```
sevens instantiate daily-note --dry-run
```

Shows what would be created without writing files.

### New (simple node creation)

```
sevens new "Collective Action" --parent "The Commons"
```

Creates a bare node without a template. If you have focus set (see section 16),
the REPL's `new` command uses the focused node as the default parent.

### Template definition format

```edn
{:name "inbox-capture"
 :description "Create a quick capture note under inbox"
 :steps [{:name "create"
          :output "ops"
          :backend {:kind "deterministic"
                    :config {:mode "create-node"
                             :title-pattern "{{title}}"
                             :parent "inbox"
                             :parent-template "inbox-root"}}}]
 :params [{:name "title" :required true}
          {:name "summary"}]}
```

The `:parent-template` field bootstraps the parent node if it doesn't exist,
by recursively executing the named template.

---

## 8. The Type System

### Primitives

Four primitive output types are built-in:

```edn
;; text — displayed, not persisted
{:name "text"
 :primitive true
 :schema-instruction "Respond with JSON containing a \"text\" field..."}

;; create — new nodes
{:name "create"
 :primitive true
 :schema-instruction "Respond with JSON containing an \"ops\" field..."}

;; edit — modify existing nodes
{:name "edit"
 :primitive true
 :schema-instruction "Respond with JSON containing an \"ops\" field..."}

;; suggestion — proposed children for review
{:name "suggestion"
 :primitive true
 :schema-instruction "Respond with JSON containing a \"suggestions\" field..."}
```

Each primitive carries a schema instruction that tells the LLM how to format
its response. This instruction is injected into the system prompt automatically.

### Subtypes

User-defined types extend primitives:

```edn
;; ~/.config/sevens/types/task.edn
{:name "task"
 :extends "create"
 :predicates {:required ["status" "deadline"]
              :optional ["priority" "assignee" "estimate"]}
 :structure {:parent-type "project"
             :children {:min 0 :max 5}}
 :projection {:frontmatter ["status" "deadline" "priority" "assignee"]
              :orthography {"status"   {:value-model "task-status"}
                            "assignee" {:signifier "@" :value-model "person"}
                            "priority" {:signifier "!" :value-model "priority"}
                            "estimate" {:signifier "~" :value-model "duration"}}}}
```

A node conforms to a type when its predicates match the shape. Required
predicates must be present; structural constraints (parent type, child count)
must be satisfied.

### Conformance checking

```
sevens types check "Decision Making"
```

```
text    CONFORMS
create  CONFORMS
edit    CONFORMS
task    no
  missing: status, deadline
  structure: FAIL
note    CONFORMS
  present: (none required)
```

Primitives (`text`, `create`, `edit`) have no required predicates, so every
node conforms to them. Custom types with predicates or structure constraints
will show `no` when requirements are not met.

### Find conforming nodes

```
sevens types nodes note
```

Lists all nodes that conform to the `note` type.

### List types

```
sevens types
```

Shows all defined types with their predicate requirements and which functions
produce each type.

---

## 9. Value Models

Value models define how predicate values are validated and normalized. They
live in `~/.config/sevens/value-models/`.

### Enum

```edn
;; priority.vm.edn
{:name "priority"
 :kind "enum"
 :members ["low" "medium" "high" "urgent"]
 :aliases {"!" "urgent" "!!" "high"}}
```

Payload must be a member or resolve through an alias. `!` resolves to `urgent`.

### State machine

```edn
;; task-status.vm.edn
{:name "task-status"
 :kind "state-machine"
 :states ["todo" "in-progress" "done" "blocked"]
 :transitions [["todo" "in-progress"]
               ["todo" "blocked"]
               ["in-progress" "done"]
               ["in-progress" "blocked"]
               ["blocked" "in-progress"]]
 :aliases {"x" "done" " " "todo" "::blocked" "blocked"}}
```

Validates that the current value + proposed value form a valid transition.
Aliases like `x` map to `done` in property lists.

### Date

```edn
{:name "date"
 :kind "date"
 :format "2006-01-02"}
```

### Reference

```edn
{:name "person"
 :kind "reference"
 :resolves-to "node"
 :signifier "@"}
```

Validates that the payload resolves to an existing node (or records an
unresolved reference).

---

## 10. Open Frontmatter

Arbitrary YAML frontmatter fields on Markdown files are extracted as
`meta/*` predicates on the node. This means any frontmatter key you add
becomes queryable:

```markdown
---
parent: The Commons
role: framework
status: draft
tags: governance, theory
---
# Decision Making
```

Sync extracts these as triples:

```
node:Decision-Making    meta/role      "framework"
node:Decision-Making    meta/status    "draft"
node:Decision-Making    meta/tags      "governance, theory"
```

Round-trip fidelity: when sevens edits a node, frontmatter fields that it
doesn't understand are preserved exactly as written.

---

## 11. Block-Level Orthography

Blocks are the structural elements within a node: headings, list items,
paragraphs. Sevens tracks blocks with stable identities and paths.

### Property lists

Attach semantic metadata to headings or list items using parenthesized
property lists:

```markdown
## Implementation
(status in-progress | @ julian | #research)

- Design the API
  (x | ! urgent)

- Write tests
  (status todo)
```

### Slot arities

Each slot in a property list has one of three arities:

```
arity-0: (x)                    — bare token, resolves via aliases
arity-1: (@julian)              — signifier fused with symbol
arity-2: (status done)          — key + payload
```

### Signifiers

Configurable tokens that serve as compact keys:

| Signifier | Typical meaning | Example |
|-----------|----------------|---------|
| `@` | Person/assignee | `@julian`, `(@ julian)` |
| `#` | Tag/context | `#research` |
| `!` | Priority/urgency | `(! urgent)`, `(!urgent)` |
| `~` | Estimate | `(~2h)` |
| `::` | State transition | `(::blocked)` |

### Inline atoms

Signifier-symbol atoms also work inline in prose:

```markdown
This work relates to #research and needs input from @julian.
```

### Triple emission

Property lists and inline atoms emit triples at block scope:

```
block:123    meta/status      "in-progress"
block:123    meta/assignee    "julian"
block:123    meta/tag         "research"
```

### Block commands

```
sevens blocks "Decision Making"
```

Lists the block structure with paths, kinds, and scope:

```
Decision Making
────────────────────────────────────────────────────────────
  0      paragraph   How groups make binding decisions...
```

Since the title comes from frontmatter (`title: Decision Making`), there is
no heading block in the body — it starts directly with the paragraph at
path 0.

```
sevens diff-blocks "Decision Making"
```

Shows block-level changes since the last sync: inserted, edited, deleted,
and (with `--unchanged`) unchanged blocks.

### Extract a block into a new node

```
sevens extract-block "Decision Making" 2 "Binding Decisions" --parent "Decision Making"
```

Creates a new node from block path 2, removes that content from the source,
and commits both changes.

---

## 12. The REPL

### Starting

```
sevens repl
```

Or with an initial focus:

```
sevens repl "The Commons"
```

The REPL resumes your previous session's focus if one exists.

Many REPL commands operate on the focused node. Commands like `blocks`,
`children`, `siblings`, `note`, `discuss`, function application, and `revert`
require a focused node and will prompt you to focus one if none is set.

### Navigation

| Command | Effect |
|---------|--------|
| `<title>` | Focus a node by typing its title |
| `focus <title>` / `f <title>` | Explicit focus (when title matches a command) |
| `..` or `up` | Move to parent |
| `root` | Clear focus |
| `child <n>` / `c <n>` | Focus the Nth child |
| `sibling <n>` / `s <n>` | Focus the Nth sibling |
| `<n>` | Focus the Nth item from the last printed list |

### Viewing

| Command | Effect |
|---------|--------|
| `walk` | Walk the focused node (shows content + neighborhood) |
| `overview` | Print full tree |
| `children` | List children with numbers |
| `siblings` | List siblings with numbers |
| `inbox` | List children summaries for focused node |
| `inbox <title>` | List children summaries for another node |
| `blocks` | List block structure for focused node |
| `blocks <title>` | List block structure for another node |
| `diff-blocks` | Show block-level changes since last sync |
| `search <query>` | Search titles and content |
| `pending` | List pending suggestions |
| `log` | Show operation log for focused node |
| `sync` | Re-sync filesystem changes |

### Functions in the REPL

Type a function name to apply it to the focused node:

```
The Commons> notice
[backend] Anthropic API (claude-sonnet-4-20250514)
[notice/default] → The Commons

The governance framework assumes consensus is achievable...
```

With options:

```
The Commons> notice --model fast
The Commons> notice --dry-run
```

### Accept / reject in the REPL

After a gated step suspends:

```
The Commons> decompose
...
Proposed children: ...

(y)es / (n)o / (r)evise: y
```

Or handle it manually:

```
The Commons> accept
The Commons> reject
```

### Block-level operations

```
The Commons> blocks
  1. h1     root  The Commons
  2. para   1     A knowledge graph about community governance...

The Commons> 2
The Commons#2> sharpen
```

Selecting a block number focuses that block. Functions applied in block focus
mode target that specific block.

```
The Commons#2> extract-block
```

Extracts the focused block into a new node.

### Creating nodes

```
The Commons> new "Collective Action"
```

Creates a child of the focused node.

### Templates in the REPL

```
The Commons> templates
  daily-note       Create or scaffold today's daily note under inbox
  inbox-capture    Create a quick capture note under inbox
  ...

The Commons> capture "Research notes"
The Commons> instantiate daily-note
The Commons> instantiate append-note --set text="Review Ostrom chapter 3"
The Commons> instantiate section-entry --set heading="Open Questions" --set text="Scale limits?"
```

### Note mode

```
The Commons> note
```

Enters note mode. Type your note, then press enter on an empty line to submit.
The note is appended to the focused node.

### Discussion mode

```
The Commons> discuss
```

Starts an interactive discussion. The agent asks questions; you respond. When
done:

```
[you]> .end
```

Or discard:

```
[you]> .cancel
```

### Session dot commands

| Command | Effect |
|---------|--------|
| `.info` | Show current focus, root, model, backend, theme, includes |
| `.model <name>` | Switch model (`fast`, `capable`, `powerful`, or raw ID) |
| `.backend <name>` | Switch backend (`anthropic`, `claude`, `codex`) |
| `.theme light\|dark` | Switch rendering theme |
| `.dry` | Toggle dry-run mode |
| `.include <title>...` | Add nodes to context for apply calls |
| `.include @GroupName` | Include all children of a group node |
| `.include clear` | Clear all includes |
| `.exclude <title>` | Remove a node from includes |
| `.functions` / `.fns` | List available functions |
| `.clear` | Clear screen |
| `.help` / `.h` | Show help |
| `.quit` / `.exit` / `.q` | Exit |

---

## 13. Discussion Mode

### CLI usage

Single-turn (non-interactive):

```
sevens discuss "Decision Making"
```

Runs the discuss function once and creates a "Discussion - Decision Making"
child node.

Interactive multi-turn:

```
sevens discuss "Decision Making" --interactive
```

Opens a back-and-forth conversation. Type messages, the agent responds. End
with `.end` to save or `.cancel` to discard.

### REPL usage

```
Decision Making> discuss
```

Enters discussion mode in the REPL. The prompt changes to `[you]>`.
Type messages to continue the conversation. Commands like `walk`, `blocks`,
and function names still work during discussion mode.

If the discussion node already has threads (multiple `[user]`/`[agent]` turns
in the file), the discussion runs non-interactively. Edit the discussion file
directly to add `[user]` messages to specific threads.

### Threading

Discussions are stored as child nodes with the naming convention
"Discussion - {Node Title}". Each turn is marked with `**[agent]**` or
`**[user]**` headers.

### Dry run

```
sevens discuss "Decision Making" --dry-run
```

Prints the rendered prompt without calling the LLM.

---

## 14. Agent Mode

Agent mode bridges sevens with external AI agents (Claude CLI, Codex, etc.)
that run outside sevens' pipeline.

### Prepare

```
sevens prepare decompose "Decision Making"
```

Compiles a function into a human-readable checklist showing what the agent
needs to do: what steps to run, what context to gather, what output format
to use. This is the "task briefing" you'd paste into an agent.

For multi-step functions like `decompose`, the briefing shows each step's
prompt template. Later steps may show empty sections (e.g.,
`<approved-suggestions></approved-suggestions>`) because they depend on
output from earlier steps. This is expected — the agent fills these in
sequentially.

### Submit

After an agent produces results, submit them back:

```
sevens submit "Decision Making" --function decompose --output ops --response-file response.json
```

This creates a pipeline with the external result, which then goes through the
normal accept/reject flow:

```
sevens accept "Decision Making"
```

Submit supports output types: `ops` (create/edit operations), `suggestions`,
and `text`.

You can also inject a result into an existing pipeline:

```
sevens submit "Decision Making" --function decompose --output suggestions --pipeline abc123 --response-file response.json
```

For multi-step functions, use `--step` to target a specific step:

```
sevens submit "Decision Making" --function decompose --step suggest --output suggestions --response-file suggestions.json
```

The `prepare` output includes the exact `submit` commands with `--step` flags
for each step.

---

## 15. Export and Harvest

These commands bridge sevens with GUI LLMs (ChatGPT, Claude web, etc.) via
copy-paste.

### Export

Renders node context as a Markdown document suitable for pasting into a GUI
LLM:

```
sevens export "Decision Making"
```

```
## Context: Decision Making

Parent: The Commons
Children: (none)

### Decision Making

How groups make binding decisions...

---
This context was exported from a knowledge graph managed by sevens.
Node: Decision Making | Shape: sevens/neighborhood
```

With a function's instruction as framing:

```
sevens export "Decision Making" --function notice --shape sevens/subtree
```

Includes the function's system prompt and rendered prompt template in the
export, plus uses the subtree shape for deeper context.

### Harvest

Generates a structuring prompt that tells the GUI LLM how to format its
response so it can be imported back:

```
sevens harvest "Decision Making" --function decompose
```

```
# Instructions for structuring your response

Please structure your response as a JSON object that can be imported into sevens.
The target node is: **Decision Making**

## Output format

Respond with a JSON object containing an "ops" field...

## Importing the response

After receiving the response, save the JSON to a file and run:

    sevens submit "Decision Making" --function decompose --output ops --response-file response.json
```

### The copy-paste workflow

1. `sevens export "Node" --function notice` — copy context into GUI LLM
2. Paste the export and converse with the GUI LLM
3. `sevens harvest "Node" --function notice` — copy the structuring prompt
4. Paste the harvest prompt to get structured output
5. Save the JSON response to a file
6. `sevens submit "Node" --function notice --output text --response-file response.json`

---

## 16. Session and Focus

### Focus

Pin a node as the active focus for subsequent commands:

```
sevens focus "Decision Making"
```

```
[focus] Decision Making
  parent: The Commons
  siblings: Resource Allocation

Use '.' as node title in other commands to reference this node.
```

Now `.` works as a shorthand:

```
sevens apply notice .
sevens walk .
sevens log .
```

Focus persists across CLI invocations and is automatically restored when
starting the REPL.

### Focus with includes

```
sevens focus "Decision Making" --include "Resource Allocation","Collective Action"
```

The included nodes are added to the context of every AI function call.

### Unfocus

```
sevens unfocus
```

### Status

```
sevens status
```

```
Focused: Decision Making
Root:    /Users/you/Documents/commons
Since:   2026-04-12T14:30:00Z
```

---

## 17. Configuration

### config.edn

Lives at `~/.config/sevens/config.edn`. All fields:

| Field | Type | Description |
|-------|------|-------------|
| `:llm` | map | `{:provider :model :api-key-env :api-key}` |
| `:models` | map | Named profiles: `{"fast" {:model "..."}}` |
| `:backend` | string | Default backend: `"anthropic"`, `"claude"`, `"codex"` |
| `:backends` | map | Backend configs: `{"claude" {:type :command :generated-conf-dir}}` |
| `:cost-threshold` | number | USD threshold for auto-approval |
| `:theme` | string | `"light"` or `"dark"` |
| `:system-prompt` | string | Global system prompt for all LLM calls |
| `:context-files` | vector | Files injected into every AI call |

### config init

```
sevens config init
```

Creates default config, seeds functions and types.

### config show

```
sevens config show
```

Displays current backend, model, MCP servers, and configured backends.

### config generate

```
sevens config generate claude
sevens config generate codex
sevens config generate all
```

Generates MCP configs for CLI backends from `capabilities.edn`.

### Model profiles

Use profiles by name:

```
sevens apply notice "Node" --model fast
sevens apply notice "Node" --model powerful
```

Or use a raw model ID:

```
sevens apply notice "Node" --model claude-opus-4-20250514
```

---

## 18. The Triple Store

### Everything is triples

Every fact in sevens is stored as a (subject, predicate, object) triple.
The SQLite database is the source of truth at runtime.

### Query

Run SQL directly against the triples table:

```
sevens query "SELECT subject, predicate, object FROM triples WHERE predicate = 'node/title'"
```

Template variables are substituted:

```
sevens query "SELECT predicate, object FROM triples WHERE subject = (SELECT subject FROM triples WHERE predicate = 'node/title' AND object = {{target}})"
```

`{{root}}` substitutes the current root path. `{{target}}` and `{{focused}}`
substitute the focused node title from the active session.

### Predicate vocabulary

| Namespace | Examples | Scope |
|-----------|----------|-------|
| `node/*` | `node/title`, `node/parent`, `node/content`, `node/file-path`, `node/root`, `node/role`, `node/char-count` | Node-level graph structure. Note: `node/parent` stores the full subject reference (`node:<hash>:<title>`), not just the title. |
| `block/*` | `block/content`, `block/kind`, `block/level`, `block/path`, `block/scope` | Block-level structure within nodes |
| `meta/*` | `meta/status`, `meta/tags`, `meta/deadline`, `meta/assignee` | User-defined semantic predicates from frontmatter and orthography |
| `session/*` | `session/focus`, `session/root`, `session/started` | Active focus session |
| `log/*` | `log/event`, `log/function`, `log/timestamp`, `log/node` | Operation audit trail |
| `fn/*` | `fn/name`, `fn/output`, `fn/persona`, `fn/context-policy` | Function definitions (future EDN projection) |
| `step/*` | `step/input`, `step/output`, `step/gate`, `step/prompt` | Pipeline step definitions |
| `type/*` | `type/name`, `type/extends`, `type/requires`, `type/optional` | Type definitions |

### Subject identity

Node subjects are deterministic: `node:{root}:{title}`. Block subjects
include the node subject and the block path. This means subjects are stable
across syncs as long as the title doesn't change.

---

## 19. Architecture

### Layer model

Sevens is organized into four layers:

1. **Triple store** — the source of truth. All facts are triples in SQLite.
2. **Knowledge base (KB)** — queries and writes over the triple store. Walk,
   overview, validation, log, session management.
3. **Projection** — bidirectional mapping between the triple store and external
   representations. The Markdown projection syncs `.md` files. The EDN
   projection (planned) syncs config files.
4. **Functions** — AI and deterministic operations that read from and write to
   the graph through the projection layer.

### Concept design

The system is organized around five concepts:

- **Graph** — the triple store and queries over it
- **Projection** — bidirectional Markdown/EDN sync
- **Function** — AI operations with typed inputs and outputs
- **Pipeline** — multi-step composition with suspension and approval
- **Session** — focus, includes, model/backend preferences

### Projections

**Markdown projection**: `.md` files are parsed into triples. Frontmatter
becomes `node/*` and `meta/*` predicates. Headings and list items become
`block/*` predicates. Writing back renders triples into Markdown with
frontmatter.

**EDN projection** (direction): `.edn` config files for types, functions,
and value models are designed to sync into the triple store and be queryable
at runtime. Currently, these are loaded from files directly; the projection
into triples is the planned next step.

### The EDN DSL

Function definitions, type definitions, and value models are all expressed in
EDN. The convention is:

- Keywords (`:create`, `:text`) are built-in language forms
- Symbols (`task`, `project`) are user-defined references
- Vectors are applied forms and match clauses: `[:create task]`
- Maps are named bindings
- Strings are literal values and templates with `{{var}}` interpolation

---

## 20. Quick Reference

### CLI commands

| Command | Usage | Description |
|---------|-------|-------------|
| `init` | `sevens init [path] [--alias name]` | Initialize a new root |
| `sync` | `sevens sync [--root dir]` | Scan markdown and rebuild database |
| `overview` | `sevens overview [--edn]` | Print full tree |
| `walk` | `sevens walk <title> [--shape S] [--edn]` | Walk a node's neighborhood |
| `tree` | `sevens tree <title> [--edn]` | Show subtree rooted at node |
| `blocks` | `sevens blocks <title> [--edn]` | List block structure |
| `diff-blocks` | `sevens diff-blocks <title> [--unchanged] [--edn]` | Block-level changes since sync |
| `inbox` | `sevens inbox [title]` | Show children summaries |
| `extract-block` | `sevens extract-block <source> <path> [title] [-p parent]` | Extract block to new node |
| `roots` | `sevens roots` | List registered roots |
| `search` | `sevens search <query>` | Search titles and content |
| `query` | `sevens query <sql>` | Run SQL against triple store |
| `apply` | `sevens apply <fn> <title> [--model M] [--dry-run] [--backend B]` | Apply a function |
| `discuss` | `sevens discuss <title> [-i] [--dry-run] [--model M] [--backend B]` | Discussion mode |
| `accept` | `sevens accept <title> [--with feedback] [--backend B]` | Accept pending suggestions |
| `reject` | `sevens reject <title>` | Reject pending suggestions |
| `revert` | `sevens revert <title>` | Undo last applied operation |
| `pending` | `sevens pending` | List all pending suggestions |
| `functions` | `sevens functions` | List available functions with signatures |
| `types` | `sevens types` | List defined types |
| `types check` | `sevens types check <title>` | Check type conformance |
| `types nodes` | `sevens types nodes <type>` | Find conforming nodes |
| `templates` | `sevens templates` | List deterministic templates |
| `define` | `sevens define <name> --description "..."` | Define a new function |
| `focus` | `sevens focus <title> [--include ...] [--exclude ...]` | Pin active focus |
| `unfocus` | `sevens unfocus` | Clear focus |
| `status` | `sevens status` | Show focus session |
| `log` | `sevens log <title>` | Show operation log |
| `prepare` | `sevens prepare <fn> <title>` | Compile function to agent checklist |
| `submit` | `sevens submit <title> --function F --output T --response-file F` | Submit agent results |
| `new` | `sevens new [title] [-t template] [-p parent] [--set k=v]` | Create a node |
| `capture` | `sevens capture [title] [--summary S]` | Quick inbox capture |
| `instantiate` | `sevens instantiate <template> [args] [-p parent] [-a target] [--set k=v]` | Run a template |
| `export` | `sevens export <title> [--shape S] [--function F]` | Export context for GUI LLM |
| `harvest` | `sevens harvest <title> [--function F] [--type T]` | Generate structuring prompt |
| `config init` | `sevens config init` | Create default config |
| `config show` | `sevens config show` | Show current config |
| `config generate` | `sevens config generate <backend>` | Generate MCP configs |
| `repl` | `sevens repl [title]` | Start interactive REPL |

### REPL commands

| Command | Description |
|---------|-------------|
| `<title>` | Focus node by title |
| `focus <title>` / `f <title>` | Explicit focus |
| `..` / `up` | Move to parent |
| `root` | Clear focus |
| `child <n>` / `c <n>` | Focus Nth child |
| `sibling <n>` / `s <n>` | Focus Nth sibling |
| `<n>` | Focus Nth item from last list |
| `walk` | Walk focused node |
| `overview` | Full tree |
| `children` | List children |
| `siblings` | List siblings |
| `inbox [title]` | Children summaries |
| `blocks [title]` | Block structure |
| `diff-blocks` | Block changes |
| `extract-block [path] [title]` | Extract block to new node |
| `search <query>` | Search |
| `pending` | List pending |
| `log` | Operation log |
| `sync` | Re-sync |
| `new <title>` | Create child node |
| `<function>` | Apply function to focus |
| `accept` | Accept pending |
| `reject` | Reject pending |
| `revert` | Revert last operation |
| `discuss` | Start discussion |
| `note` | Quick note mode |
| `templates` | List templates |
| `capture [title]` | Inbox capture |
| `instantiate <template> [args]` | Run template |
| `.info` | Session info |
| `.model <name>` | Switch model |
| `.backend <name>` | Switch backend |
| `.theme light\|dark` | Switch theme |
| `.dry` | Toggle dry-run |
| `.include <title>...` | Add context nodes |
| `.include clear` | Clear includes |
| `.exclude <title>` | Remove include |
| `.functions` / `.fns` | List functions |
| `.clear` | Clear screen |
| `.help` / `.h` | Help |
| `.quit` / `.q` / `.exit` | Exit |
