# sevens

Sevens is a tool for organizing your thinking. You write markdown files, and sevens turns them into a structured knowledge graph that you can explore, analyze, and grow with the help of AI.

The name comes from a finding in cognitive science: people can hold about seven things (plus or minus two) in working memory at once. Sevens uses this as a design principle -- when a topic gets too big, break it into five to nine subtopics. The result is a tree of ideas where each level is small enough to think about clearly.

## What It Looks Like

You start by writing a markdown file. This is just a text file with a small header at the top:

```markdown
---
title: The Commons
---

Ok so I keep coming back to this idea of a neighborhood that has one place
that's a library but also a tool library and a seed bank and a repair cafe
all mashed together...
```

The header (the part between the `---` lines) tells sevens the title of this note. Everything below it is your content -- your thoughts, questions, brainstorms, whatever you're working through.

Run `sevens sync` in your terminal, and sevens reads all your markdown files and builds a database from them. Now you can ask sevens to help you work with what you've written.

## Functions: Asking AI to Help

Sevens comes with a set of "functions" -- named operations that use AI to do something useful with your notes. For example:

```
$ sevens apply notice "The Commons"
```

This runs the "notice" function on your note called "The Commons." The AI reads your note and points out things you might have missed -- patterns, gaps, assumptions, contradictions. It prints its observations directly in your terminal.

Other functions do different things:

- **decompose** reads a long, dense note and suggests how to break it into smaller, focused child notes
- **elaborate** takes a sparse note and fleshes it out with deeper detail and follow-up questions
- **discuss** creates a conversation thread where the AI asks probing questions about your note, and you can write responses directly in the file
- **challenge** plays devil's advocate, finding the weakest points in your thinking
- **bridge** looks at sibling notes (notes that share the same parent) and writes about the connections between them
- **synthesize** looks across a set of related notes and finds patterns that aren't obvious from reading any single note

Each function is defined by a small configuration file and a prompt template. You can modify existing functions or create your own.

## The Tree

When you decompose a note, sevens creates child notes -- new markdown files whose headers point back to the parent:

```markdown
---
title: Lending Infrastructure Design
parent: "[[The Commons]]"
---

# Lending Infrastructure Design

The technical challenge of creating unified lending systems
that work across completely different domains...
```

The `parent` field creates the tree structure. Over time, your notes form a hierarchy:

```
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
```

This is just a view -- the files can live anywhere on disk. The tree comes from the `parent` fields, not from folders.

## Pipelines and Approval

Some functions have multiple steps. Decompose, for example, works in two phases:

1. First, it suggests child topics and pauses for your review
2. You approve (or revise), and then it generates the actual content for each child

When sevens pauses, it saves its state. You can close your terminal, think about it overnight, and come back the next day. Running `sevens accept "The Commons"` picks up exactly where it left off.

If you don't like the suggestions, you can give feedback:

```
$ sevens accept "The Commons" --with "Add a node about existing community infrastructure"
```

The AI re-runs the step with your feedback and produces a revised suggestion.

## Focus and Context

When you're working on a particular note, you can "focus" on it:

```
$ sevens focus "The Commons"
```

Now you can use `.` as shorthand for that note in any command:

```
$ sevens apply notice .
$ sevens apply discuss .
```

You can also include other notes as extra context:

```
$ sevens focus "Discussion: The Commons" --include "The Commons" --include "Lending Infrastructure Design"
```

This tells sevens to make those notes' content available when running functions on the focused note.

## Templates

Templates let you create notes with pre-built structure. Instead of starting from a blank file, you stamp out a pattern:

```
$ sevens new --template pros-cons --set topic="Co-op Governance" --parent "The Commons"
```

This creates four notes at once -- an analysis root, a pros note, a cons note, and a synthesis note -- with the relationships between them already defined. The pros note is marked as "support," the cons note as "counterexample," and the synthesis as "continuation." Functions can see these relationships and use them.

Other templates:

- **daily-note** creates a dated journal entry
- **journal-entry** creates an entry under today's date (creating the date note if needed)
- **decision-record** creates a structured decision document with context, options, and consequences
- **braindump** creates a blank note for free-form thinking

You can create your own templates.

## Typed Relationships

Notes that share a parent (siblings) can have named relationships. When you look at a note's neighborhood, sevens shows these:

```
children: Pros [support], Cons [counterexample], Synthesis [continuation]
```

This isn't just a label -- functions use it. A function that needs "all the counterarguments" can ask for siblings with the "counterexample" role. A function that wants to continue a thread can look for the "continuation" sibling.

These roles are set by templates, by adding `sibling-role: support` to a note's header, or by the AI when it creates notes.

## Agent Personas

Different functions behave differently because they use different AI "personas":

- The **challenge** function uses a "critic" persona with instructions to be sharp and adversarial
- The **discuss** function uses a "facilitator" persona with instructions to be curious and Socratic
- The **elaborate** function uses a "builder" persona with instructions to be constructive

Each function can also control how much context the AI sees. The challenge function deliberately uses "minimal" context -- it only sees the target note, not its siblings or parent. This prevents it from being influenced by surrounding context and forces it to evaluate the note on its own terms. The bridge function uses "full" context because its entire job is to find connections between siblings.

## How Everything Is Stored

Under the hood, sevens stores everything as "triples" -- simple three-part facts:

```
(Container Strategy    has parent       EHE Modernization)
(Container Strategy    has content      "# Container Strategy...")
(Container Strategy    links to         Kubernetes Concepts)
(Container Strategy    has char count   487)
(Pros                  has role         support)
```

Subject, relationship, value. That's it. Every piece of information -- content, tree structure, cross-references, word counts, function history, even the state of paused pipelines -- is stored as one of these facts.

This matters because it means everything is queryable in the same way. You can ask:

```
$ sevens search "governance"
```

And it searches across titles and content. Or run a direct query:

```
$ sevens query "SELECT subject AS node, object AS links_to
  FROM triples WHERE predicate = 'ref/wiki-link' LIMIT 10"
```

## Exploring Your Graph

Beyond applying functions, sevens gives you several ways to look at your knowledge graph:

- `sevens overview` prints the full tree as an ASCII diagram
- `sevens walk "Node Title"` shows a note's content plus its parent, children, siblings, and cross-references
- `sevens tree "Node Title"` shows the subtree rooted at a specific note
- `sevens search "query"` finds notes by title or content
- `sevens log "Node Title"` shows the history of AI operations on a note
- `sevens status` shows what note you're focused on and what context is active
- `sevens pending` shows notes with AI suggestions waiting for your review

## Working with an AI Agent

Sevens can work in two ways:

**Standalone**: You run `sevens apply` and sevens calls the AI directly. This requires an API key.

**Agent mode**: If you're already working inside an AI assistant (like Claude Code or ChatGPT), the assistant can use sevens as a tool. Instead of sevens calling the AI, the AI calls sevens:

```
$ sevens prepare notice "The Commons"
```

This prints a checklist of commands the AI should run -- what notes to read, what instruction to follow, and how to submit its response:

```
[task] notice --> "The Commons"

[read] target
  $ sevens walk "The Commons"

[read] parent
  (none -- root node)

[instruction]
  Surface patterns, gaps, tensions, and implicit assumptions...

[submit]
  $ sevens submit "The Commons" --function notice --output text --response-file /tmp/response.txt
```

The AI reads the notes, does its analysis, and submits the result. Sevens handles logging, version control, and the knowledge graph. The AI handles thinking.

This is useful when you have access to an AI through your workplace (like ChatGPT Enterprise) and don't need a separate API key.

## Version Control

Sevens uses git to track changes. Every time you sync or accept AI-generated changes, sevens commits automatically. Every commit message identifies what function was applied and to which note:

```
sevens: apply decompose to "The Commons"
sevens: apply distill to "Discussion: The Commons"
sevens: apply bridge to "Commons Revenue and Sustainability"
```

If something goes wrong, you can undo:

```
$ sevens revert "The Commons"
```

This finds the last AI-applied change to that note and reverts it using git.

## Getting Started

Build sevens (requires Go 1.22 or later):

```
$ go build -o sevens ./cmd/sevens/
```

Initialize a new knowledge graph in a directory:

```
$ sevens init ~/Documents/my-brain --alias brain
```

Create some markdown files with `title` headers, then:

```
$ sevens sync
$ sevens overview
```

You'll see your notes as a tree. Start exploring with `sevens walk`, thinking with `sevens apply notice`, and growing with `sevens apply decompose`.
