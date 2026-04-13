# Sevens — Intent and Mental Model

What sevens is, what it's trying to achieve, and the ideas behind its design.
Written as a companion to `responsibilities.md` -- that file catalogs *what the
code does*; this one explains *what it's for and why it's shaped this way*.

---

## What Sevens Is

Sevens is a local-first thinking tool. You write markdown files about whatever
you're working through -- a project, a research topic, an argument, a plan --
and sevens turns them into a structured knowledge graph that you can explore,
query, and grow with the help of AI.

The name comes from cognitive science: people can hold about seven things (plus
or minus two) in working memory at once. When a topic grows too large to hold
in your head, you break it into five to nine subtopics. The result is a tree
of ideas where each level is small enough to think about clearly.

Sevens is not a note-taking app. It's a tool for *organized thinking* -- the
kind where structure emerges through iterative decomposition, where AI helps
you see what you're missing, and where the relationships between ideas are as
important as the ideas themselves.

---

## The Core Loop

The fundamental workflow is:

1. Write markdown files with ideas, arguments, brainstorms
2. Sync those files into a structured graph
3. Use AI functions to analyze, decompose, challenge, and extend your thinking
4. Review and approve AI-generated changes
5. The graph grows; repeat

This loop is human-paced and human-directed. The AI proposes; the human
approves, rejects, or revises. Every change is tracked and reversible.

---

## The Triples Data Model

Everything in sevens is stored as simple three-part facts:

```
(subject, predicate, object)
```

A node's content, its parent relationship, its wiki-links, its character count,
the history of AI operations applied to it, the state of paused workflows --
all stored as triples in the same table.

Why triples instead of a richer schema? Because it means every piece of
information is queryable in the same way. Structural questions ("what are the
children of this note?"), content questions ("which notes mention governance?"),
and operational questions ("what functions have been applied to this note?")
are all queries over the same facts.

The triples model also means the schema never needs migrating. New kinds of
information are just new predicates. The database grows by accumulating facts,
not by altering its shape.

---

## Category 𝒦: The Epistemic Framework Behind the Design

Sevens is informed by an original epistemic framework called Category 𝒦
(documented in full in `docs/CATEGORY-K.md`). The framework derives the
structure of knowing from a single axiom: **knowledge is synchronic binary
relation**. Everything that follows -- points, partitions, time, observers,
types, composition -- is derived from this.

The practical design principles that feed into sevens:

### Dumb Morphisms, Smart Objects (The Intermediary Principle)

Every semantically meaningful relationship is a path through intermediate
objects. Morphisms (edges, connections) are anonymous and binary. Objects
(nodes) carry all the semantic weight.

"Alice knows Bob" is not a single rich labeled edge. It is:

```
Alice :rel "acquaintance" :rel Bob
```

Two anonymous morphisms, one intermediate object carrying the specificity. The
complexity is always in the nodes, never in the edges.

This is exactly what the triples model implements. `(subject, predicate,
object)` -- the predicate is itself an intermediate point on the path. When a
relationship seems too complex for a binary edge, you don't make the edge
richer. You add objects to the path. This is the decomposition principle.

### Arrow Composition

In a category, if you can follow arrow A from X to Y, and arrow B from Y to Z,
then the composition A-then-B gets you from X to Z.

In sevens, each predicate is an arrow. "parent" forward gives you a node's
parent. "parent" backward gives you all children. Composing "parent forward,
then parent backward" gives you siblings.

Traversals are chains of arrows -- path specifications:

- Grandparent: follow "parent" twice
- Siblings: "parent" forward, then "parent" backward, excluding self
- Children's wiki-links: "parent" backward, then "wiki-link" forward

Functions declare what context they need as path specs, and the system walks
those paths to gather the right information. No special cases needed.

### Types as Subgraph Shapes

A type in 𝒦 is a tree-shaped subgraph. A value is a leaf. Type checking is a
graph operation: are all the required nodes present?

In sevens, a note is not "a discussion note" because of a type field. It's
recognized as a discussion note because it has a parent, its content has
conversation-format turns, and its title starts with "Discussion:". Functions
that need "the discussion child" look for these structural characteristics,
not a type tag.

Types are emergent, not declared. The system identifies what something is by
examining the subgraph shape around it.

### Time (T) and the Two Channels

In 𝒦, time (T) is a distinguished zero object -- the thing that mediates all
change. Every new relation must route through T. An observer has exactly two
modes of epistemic change:

- **Channel 1 -- Recognition**: traversing structure already in your partition.
  No partition change. You're seeing connections that were already there.
- **Channel 2 -- Being informed**: something new enters your partition from
  outside, via T. The partition changes. You know something you didn't before.

In sevens:
- Functions like `notice`, `bridge`, and `relate` are **Channel 1** -- they
  help you see connections within what you already have. No new nodes are
  created. The AI traverses your existing graph and surfaces patterns.
- Functions like `elaborate`, `decompose`, and `scaffold` are **Channel 2** --
  they bring genuinely new structure into the graph. New nodes are created.
  The partition grows.

The **suspension model** is the mechanism of T-crossing. The pipeline pauses
(suspends at T), the human reviews (the tick happens), and the partition
changes (new nodes enter the graph on approval). The human's approval is what
makes the diachronic transition real. Without it, the proposed change is just
a possibility -- not yet knowledge.

### Witnesses and Durability

A witness in 𝒦 is a subgraph that survives passthrough of T -- it persists
across ticks. Knowledge that holds up over time is well-typed. Knowledge that
dissolves when context changes was never properly witnessed.

In sevens, the operation log and git history are the witness mechanism. A
function's output that gets accepted and persists in the graph has been
witnessed. One that gets rejected or reverted was ill-typed -- it didn't
survive the human's scrutiny.

### Relationships Are First-Class, Composable, and Queryable

This is not a storage optimization. It's a design principle drawn from the
deeper framework: the structure of the knowledge graph is not imposed by a
schema -- it emerges from the facts, and you navigate it by composing the
relationships those facts express. The complexity lives in the objects. The
morphisms stay dumb.

---

## Functions as Composable Transformations

A function in sevens is a named AI operation with three parts:

1. **What context it needs** -- which arrows to follow to gather information
   (path specs, role-based requires, context policies)
2. **What instructions to give the AI** -- the prompt template, persona, and
   system prompt
3. **What to do with the result** -- create files, edit files, or just display
   text

Functions are defined as small configuration files with prompt templates. They
control what the AI sees (context policy: minimal, neighborhood, or full) and
how the AI behaves (persona: critic, facilitator, builder). The same note
processed by different functions gets genuinely different perspectives.

Functions compose. A multi-step function is a pipeline where each step's output
feeds the next step's input. A composed function can delegate to another
function or map a function over a set of related nodes. This is composition at
a higher level -- instead of composing arrows in the graph, you compose
operations on the graph.

---

## Human-in-the-Loop Pipelines

Some operations need human review before they take effect. Decomposing a note
into children, for example, works in two phases: the AI suggests how to break
it up, then the human approves (or revises), then the AI generates the content.

Between phases, the pipeline *suspends*. The entire state of the paused
operation -- what function was running, what step it was on, what the AI
produced -- is saved as triples in the database. You can close your terminal,
come back the next day, and resume exactly where you left off.

This suspension/resume model is the operational heart of sevens. It makes AI
assistance *human-paced* -- you're never rushed to approve something, and you
can always revise or reject. The pipeline is a state machine that moves forward
only with your explicit consent.

---

## Agent Mode and CLI Passthrough

Sevens can work in two modes:

**Standalone**: you run `sevens apply` and sevens calls the LLM directly.

**Agent mode**: you're already working inside an AI assistant (like Claude Code),
and the assistant uses sevens as a tool. Instead of sevens calling the AI, the
AI calls sevens.

In agent mode, `sevens prepare` compiles a function application into a
checklist: what notes to read, what instruction to follow, and how to submit
the response. The AI reads the notes, does its analysis, and submits the result
via `sevens submit`. Sevens handles the graph, version control, and workflow
state. The AI handles thinking.

This is a deliberate architectural separation. Sevens is the **graph layer and
workflow engine**; the AI is the **intelligence layer**. They communicate
through structured CLI commands. This means sevens doesn't care where the AI
lives -- it could be a local model, an API, or a coding assistant that's
already in your terminal.

---

## Templates as Deterministic Structure

Not everything needs AI. Templates are pre-built patterns for creating notes
with known structure:

- A pros/cons analysis creates four notes with typed relationships
- A daily note creates a dated journal entry
- An inbox capture creates a quick entry under a container node

Templates are the deterministic complement to AI functions. Where functions
produce emergent, unpredictable results that need review, templates produce
predictable structure that's correct by construction. Together they make
sevens a mixed-mode tool: deterministic where structure should be deterministic,
AI-mediated where judgment and synthesis help.

---

## Version Control as Audit Trail

Sevens uses git to track every change. Every sync, every accepted AI operation,
every template instantiation produces a commit with a descriptive message. If
something goes wrong, you can revert. The git history is an audit trail of how
your thinking evolved, with each commit identifying what function was applied
and to which note.

---

## The Data Authority Question

As sevens has developed, a design tension has emerged: **is the database a
cache of the markdown files, or is it becoming the authoritative source of
truth?**

Currently it's a hybrid. Markdown files are still the source of truth for note
content -- you can edit them in any text editor and `sevens sync` will pick up
the changes. But the database is already the source of truth for operational
state: suspensions, logs, session focus, block identity. And the richer the
graph operations become (block-level diffing, stable block identity tracking,
cross-walk output), the more the database carries information that doesn't
exist in the files.

The emerging direction is: **one primary model in the database, with markdown
and git as projections**. Markdown would be the most important projection
because it's human-editable and durable. Git would be the most important
transport and audit projection. But the database would be where operations are
defined, not against a particular projection of the data.

This isn't resolved yet. It's the central architectural question for the next
phase of development.

---

## What It Looks Like In Practice

### Syncing and exploring the graph

```
$ sevens sync
[sync] scanned 34 files, 34 nodes
[sync] populated 847 triples

$ sevens overview
The Commons
├── Commons Governance Models
│   └── Governance Evolution Pathways
├── Commons Revenue and Sustainability
│   ├── Membership Models and Pricing
│   ├── Grant Funding Strategy
│   └── Revenue-Generating Programming
├── Existing Community Infrastructure
│   ├── Informal Sharing Networks
│   ├── Integration Without Displacement
│   └── Community Asset Mapping
├── Lending Infrastructure Design
└── Multi-Use Space Design

$ sevens walk "Lending Infrastructure Design"
Lending Infrastructure Design
parent: The Commons
children: (none)
siblings: Commons Governance Models, Commons Revenue and Sustainability, ...
cross-refs: (none)
────────────────────────────────────────────────────────────
The technical challenge of creating unified lending systems
that work across completely different domains — a book and a
table saw have different checkout periods, care requirements,
liability profiles...
```

### Applying an AI function

```
$ sevens apply notice "The Commons"
[notice] notice → "The Commons"
[cost] 2847 tokens, ~$0.0043 (auto-approved, below $0.01)
[notice] ····················

**Gaps** — There's no discussion of how the institution handles
failure modes. What happens when a tool is returned broken? When
seeds don't germinate? When a member doesn't return something?
The lending model is all happy path.

**Tensions** — The governance section wants distributed authority
but the revenue model assumes central management of grants and
membership fees. These pull in opposite directions.

**Assumptions** — The document assumes a physical neighborhood
with walkable density. None of this works in a suburb.
```

### Decomposing a node (multi-step pipeline with gate)

```
$ sevens apply decompose "The Commons"
[decompose] suggest → "The Commons"
[cost] 3102 tokens, ~$0.0047 (auto-approved, below $0.01)
[decompose] ····················

Proposed children:
  1. Commons Governance Models
     "How decisions get made and who has authority"
  2. Commons Revenue and Sustainability
     "Financial models that don't require perpetual grants"
  3. Lending Infrastructure Design
     "The technical systems for tracking loans across domains"
  ...

[pending] Review with: sevens accept "The Commons"
                    or: sevens accept "The Commons" --with "feedback"
                    or: sevens reject "The Commons"

$ sevens accept "The Commons" --with "Add a node about existing community infra"
[decompose] suggest (revision) → "The Commons"
[decompose] ····················

Revised children:
  1. Commons Governance Models
  2. Commons Revenue and Sustainability
  3. Existing Community Infrastructure    ← added
  4. Lending Infrastructure Design
  5. Multi-Use Space Design

$ sevens accept "The Commons"
[decompose] generate → "The Commons"
[decompose] ····················
  + Commons Governance Models
  + Commons Revenue and Sustainability
  + Existing Community Infrastructure
  + Lending Infrastructure Design
  + Multi-Use Space Design
[git] committed: sevens: apply decompose to "The Commons"
```

### Agent mode (CLI passthrough for external AI)

```
$ sevens prepare notice "Lending Infrastructure Design"
[task] notice → "Lending Infrastructure Design"

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
  can't easily see from inside their own thinking. Look for:
  recurring themes, conspicuous absences, contradictions,
  assumptions, unacknowledged relationships...

[output] text
[submit]
  $ sevens submit "Lending Infrastructure Design" --function notice \
      --step notice --output text --response-file /tmp/notice-notice.txt
```

### Templates (deterministic structure creation)

```
$ sevens new --template daily-note
[new] Created "2026-04-10"
[git] committed: sevens: new from template daily-note

$ sevens capture "insurance liability model" --parent "Lending Infrastructure Design"
[new] Created "insurance liability model"
[git] committed: sevens: capture "insurance liability model"
```

### Discussion mode (REPL multi-turn conversation)

```
$ sevens repl "Lending Infrastructure Design"
Lending Infrastructure Design> discuss
[discuss] discuss → "Lending Infrastructure Design"
[discuss] ····················

**[agent 2026-04-10 14:32]** The lending model treats all items
as equivalent once checked out. But a $15 book and a $400 table
saw have fundamentally different risk profiles. How do you think
about liability when the replacement costs vary by 30x?

[you]> The insurance model might be tiered — small items on trust,
       big items require a deposit or membership level
[discuss] ····················

**[agent 2026-04-10 14:33]** That creates an interesting tension
with the egalitarian framing. If access to expensive tools requires
a higher membership tier, you're re-introducing the economic barrier
the Commons was meant to dissolve. Is there a model that doesn't
gate access by ability to pay?

[you]> .end
[git] committed: sevens: discussion on "Lending Infrastructure Design"
```

### Session and focus

```
$ sevens focus "The Commons" --include "Lending Infrastructure Design"
focused: The Commons (+1 context node)

$ sevens status
focus: The Commons
root:  ~/Documents/commons-project
since: 2026-04-10T14:00:00Z
includes:
  - Lending Infrastructure Design

$ sevens apply notice .
[notice] notice → "The Commons"
...
```

---

## Who Sevens Is For

Sevens is for people who think by writing and want structure to emerge from
that writing rather than being imposed upfront. The intended user is
comfortable with a terminal, has ideas that are too complex for a single
document, and wants AI to help them see patterns, gaps, and connections --
not to generate content for them.

The tool should feel trustworthy: every AI action is reviewable, every change
is reversible, and the system never modifies your work without explicit
approval.
