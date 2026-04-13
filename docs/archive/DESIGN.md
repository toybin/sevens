# Sevens -- Design Document

This document explains the ideas behind sevens and why the system is built the way it is. It walks through a single extended example to show how the pieces fit together, and introduces some concepts from mathematics along the way -- not because you need a math background to use sevens, but because these concepts explain why certain design choices were made and why the system behaves the way it does.

## The Problem

You have a big messy idea. Maybe it's a project plan, a research topic, a book outline, a business concept. You've written about it, probably in multiple documents, and the relationships between parts are mostly in your head. You can hold some of it in working memory, but as it grows, pieces start falling off the edges.

The usual tools don't help much. A single long document buries structure in linear text. A folder of files loses the relationships between them. An outliner enforces hierarchy but doesn't capture cross-references or allow AI-assisted analysis.

Sevens is an attempt to solve this by making the structure explicit, the relationships queryable, and the AI assistance composable.

## The Example: A Neighborhood Lending Institution

Throughout this document, we'll work with a real example: a braindump about creating a neighborhood institution that combines a library, tool library, seed bank, and repair cafe under one roof.

The braindump starts as a single long file -- about 3,000 characters of unstructured thinking about governance, space design, revenue, staffing, and community building. By the end, it becomes a structured graph of 30 nodes across three levels, with typed relationships, operational history, and AI-generated analysis.

## Everything Is a Fact

The first design decision: store everything as simple facts.

When sevens reads your markdown file, it extracts dozens of facts about it:

```
"The Commons" has a parent of (none -- it's a root)
"The Commons" has content "Ok so I keep coming back to this idea..."
"The Commons" has a file path "/Users/.../the-commons.md"
"The Commons" has a character count of 3847
"The Commons" has 3 headings
"The Commons" links to "Kubernetes Concepts" via a wiki link
```

Each fact has three parts: a subject (which thing we're talking about), a relationship (what we're saying about it), and a value (the actual information). In the database, these are stored as rows in a single table with three columns: subject, predicate, object. We call these "triples."

Everything goes into this same table. Content, structure, cross-references, word counts, the history of which AI functions have been applied, even the state of paused AI operations. One table, one format.

Why? Because it means every piece of information is queryable in exactly the same way. You don't need different tools to ask "what are the children of this note?" (a structural question) versus "which notes mention governance?" (a content question) versus "what functions have been applied to this note?" (a history question). They're all just queries over the same facts.

## Arrows and Composition

Here's where the math starts to help.

Think of each fact as an arrow pointing from the subject to the value, labeled with the relationship:

```
Container Strategy --parent--> EHE Modernization
EHE Modernization --parent--> Program Portfolio
```

The first arrow says "Container Strategy's parent is EHE Modernization." The second says "EHE Modernization's parent is Program Portfolio." These are two separate facts.

But you can compose them. Following both arrows in sequence tells you: "Container Strategy's grandparent is Program Portfolio." You didn't store that fact -- you derived it by following two arrows in a chain. This is called composition: combining two relationships to get a third.

In mathematics, a system of objects connected by composable arrows is called a "category." The objects are the things (nodes, in our case). The arrows are the relationships (the predicates in our triples). And the composition rule says: if you can follow arrow A to get from X to Y, and arrow B to get from Y to Z, then the composition A-then-B gets you from X to Z.

This is not just abstract. Every query sevens runs is a composition of arrows.

### Finding Siblings

Suppose you want to find the siblings of "Container Strategy" -- the other children of the same parent. There's no "sibling" arrow stored in the database. Instead, sevens composes two arrows:

1. Follow "parent" forward from Container Strategy to EHE Modernization
2. Follow "parent" backward from EHE Modernization to find all notes that have EHE Modernization as their parent
3. Remove Container Strategy from the result (it's a sibling of itself, which isn't useful)

Step 2 is the reverse direction -- instead of "which parent does this note have?" it asks "which notes have this as their parent?" Reversing an arrow is called taking its inverse. So the sibling query is: "parent, then parent-inverse, excluding self."

Sevens lets you express this in function definitions using a notation called a "path spec":

```
["node/parent" "node/parent~"]
```

The `~` suffix means "go backwards." This path spec says: follow the parent arrow forward, then follow the parent arrow backward. The result is all siblings.

### Why This Matters

The path spec system means you can express any traversal of the graph as a chain of arrows. Some examples:

- Grandparent: `["node/parent" "node/parent"]` -- follow parent twice
- All children: `["node/parent~"]` -- follow parent backward
- Wiki links from children: `["node/parent~" "ref/wiki-link"]` -- children, then their links
- Siblings' wiki links: `["node/parent" "node/parent~" "ref/wiki-link"]` -- parent, then all its children, then their links

Each of these is a composition. The system doesn't need a special case for "grandparent" or "siblings' links" -- it just composes arrows.

## Functions and What They See

A function in sevens is a named AI operation. It has three parts:

1. **What context it needs** -- which arrows to follow to gather information
2. **What instructions to give the AI** -- the prompt template
3. **What to do with the result** -- create files, edit files, or just display text

The context part uses path specs. Here's the definition for the "bridge" function, which writes about connections between sibling notes:

```
name: bridge
description: Write connecting narrative between siblings

context:
  - path: parent, then parent-inverse (excluding self)
    fetch: content of each result
    call it: "siblings"

  - path: parent
    fetch: content
    call it: "parent"

output: file edits
```

When you run `sevens apply bridge "Container Strategy"`, sevens:

1. Follows the path specs to fetch the parent and all siblings with their content
2. Plugs that content into the prompt template
3. Sends the prompt to the AI
4. Gets back a set of edits to apply to the note

The function definition controls what the AI sees. This is a deliberate design choice.

### Context Policies

Different functions should see different amounts of information. Consider two functions:

- **challenge** (devil's advocate) should see only the target note. If it sees the parent's framing or the sibling's supporting evidence, it might soften its critique. We want it to evaluate the note on its own terms.

- **bridge** (connecting narrative) needs to see all siblings. Its entire job is to find relationships between them. Without sibling content, it can't do anything.

Sevens formalizes this as "context policies":

- **minimal**: The AI sees only the target note. No parent, no siblings, no children.
- **neighborhood**: The AI sees the target note plus the titles and relationships of nearby notes, but not their full content. It knows what exists without reading everything.
- **full**: The AI sees everything the path specs resolve -- full content of parent, siblings, children, whatever the function asks for.

Each function declares its policy. The challenge function uses minimal. The bridge function uses full. The summarize function uses minimal (so it compresses what's there, not what's nearby). The notice function uses full (so it can spot patterns across the neighborhood).

## Personas

Beyond controlling what the AI sees, functions control how the AI behaves. Each function can declare a "persona" -- a different system prompt that shapes the AI's response style:

- The **challenge** function tells the AI: "You are a sharp, constructive critic. You find weaknesses and push on them."
- The **discuss** function tells the AI: "You are a curious, Socratic thinker. You ask probing questions."
- The **elaborate** function uses the default behavior -- helpful and constructive.

This means the same note can be processed by different functions and get genuinely different perspectives. Running `notice`, then `challenge`, then `discuss` on the same note gives you three distinct viewpoints: an analyst noticing patterns, a critic finding weaknesses, and a facilitator drawing out questions.

## Pipelines and Pausing

Some operations take multiple steps. Decomposing a long note works in two phases:

1. The AI suggests how to break it up (titles and rationale for each child)
2. You review and approve (or revise)
3. The AI generates the actual content for each child

Between steps 1 and 2, the pipeline pauses. Sevens saves the entire state of the paused operation -- what function was running, what step it was on, what the AI produced -- as facts in the database. You can close your terminal, come back the next day, and `sevens accept` resumes exactly where it left off.

This state is stored using the same fact system as everything else. A paused operation is just a set of triples:

```
"suspension:XYZ" has target "The Commons"
"suspension:XYZ" has function "decompose"
"suspension:XYZ" has step "suggest"
"suspension:XYZ" has status "pending"
"suspension:XYZ" has output "[{title: 'Lending Infrastructure'...}]"
```

When you accept, sevens finds the pending suspension, reads the saved output, and continues the pipeline. When the operation completes, the suspension's status changes to "accepted" -- another fact update.

### Why Save State as Facts?

Because it means the same query tools work on operational state as on content. "Which notes have pending suggestions?" is a query over suspension facts. "What functions have been applied to this note?" is a query over log facts. You don't need separate systems for content management and workflow management -- they're the same thing.

## Composed Functions

Functions can be built from other functions. The "deepen" function combines two operations:

1. Decompose a note into children
2. Elaborate each child

The second step uses "map-over" -- it runs the elaborate function on each child separately. If decompose creates six children, elaborate runs six times, once for each child.

This is composition at a higher level. Instead of composing arrows in the graph (subject-predicate-object), we're composing operations on the graph (decompose, then map elaborate over results). The same principle -- combine simple pieces into complex ones -- works at both levels.

## Templates and Typed Relationships

Sometimes you want to create structure without AI involvement. Templates are pre-built patterns for notes:

```
$ sevens new --template pros-cons --set topic="Co-op Governance" --parent "The Commons"
```

This creates four notes: an analysis root, a pros note, a cons note, and a synthesis note. Each child has a named relationship:

- Pros is marked as "support"
- Cons is marked as "counterexample"
- Synthesis is marked as "continuation"

These aren't just labels. They're stored as facts and visible to functions. When the bridge function looks at siblings, it sees:

```
children: Pros [support], Cons [counterexample], Synthesis [continuation]
```

It knows not just what notes exist, but what role each plays. And crucially, if you run a function that creates child notes (like discuss), it can check whether a child with a particular role already exists before creating a duplicate.

## Objects Are What They Do

There's a principle in the mathematical framework we've been drawing on: an object is characterized by its relationships, not by an inherent label.

In sevens, a note doesn't have a fixed "type." It isn't declared as a "discussion node" or a "decision record." Instead, it's recognized as a discussion node because of the facts about it:

- It has a parent (the note being discussed)
- Its content has a conversation format (agent and user turns)
- Its title starts with "Discussion:"

A function that needs "the discussion child" doesn't check a type field. It looks for a child that has these characteristics. Any note that matches the pattern qualifies.

This means types are emergent, not declared. You don't have to decide up front what kind of note you're creating. The structure emerges from the relationships -- and the system figures out what it's looking at by examining those relationships.

This connects back to the idea of objects characterized by their arrows. In the mathematical framework, what matters about an object is what arrows point in and out of it -- not any intrinsic property. In sevens, what matters about a note is what facts connect it to other notes -- not a type label in a registry.

## Agent Mode

Sevens can run AI functions directly (standalone mode) or serve as a tool for an external AI assistant (agent mode).

In agent mode, `sevens prepare` compiles a function application into a checklist:

```
[task] notice --> "Container Strategy"

[read] target
  $ sevens walk "Container Strategy"

[read] siblings (9 nodes)
  $ sevens walk "Terraform Cleanup" --depth 0
  ...

[instruction]
  Surface patterns, gaps, tensions...

[submit]
  $ sevens submit "Container Strategy" --function notice --output text --response-file /tmp/response.txt
```

The AI assistant reads the notes (each one is a short command), follows the instruction, and submits its response. Sevens handles the graph operations, version control, and fact storage. The AI handles thinking.

This separation matters when you have access to an AI through your workplace and don't want to pay for a separate API. Sevens is the graph layer; the AI is the intelligence layer. They communicate through structured CLI commands.

## The Shape of the System

Stepping back, here is how the pieces relate:

**Storage**: Everything is a fact (triple). One table, three columns. No schema to migrate.

**Structure**: The tree comes from "parent" facts. Siblings, ancestors, descendants -- all derived by composing arrows. Path specs are the composition language.

**Functions**: Named AI operations that declare what context they need (path specs), how the AI should behave (persona and context policy), and what to do with the result (create files, edit files, display text).

**Pipelines**: Multi-step functions that pause for human review. State is saved as facts and survives process restarts.

**Templates**: Pre-built structural patterns for creating groups of notes with typed relationships.

**Agent mode**: The AI assistant uses sevens as a tool. `prepare` compiles tasks; `submit` ingests results. The graph layer and the intelligence layer are separate.

**Version control**: Git tracks every change. Every AI operation commits with a descriptive message. Revert undoes the last operation.

The mathematical insight that ties it together: arrows compose. Small relationships combine into complex queries. Simple functions combine into multi-step pipelines. Individual notes combine into structured trees. The system is built from one pattern applied at every level.
