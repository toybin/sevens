# Project: `sevens`

A Go CLI tool that serves as a context server for AI agents operating over a tree-structured knowledge graph of markdown files. The tool is the agent's only interface to the graph — the agent never reads files directly.

## Design Philosophy

The knowledge graph enforces a cognitive constraint: each node has at most one parent and 5-9 children (based on Miller's 7±2 chunking theory). When a node exceeds this bound, it should be decomposed into sub-nodes. The tree structure emerges from YAML frontmatter declarations in markdown files, not from the filesystem layout. Files can live in any directory structure.

## Data Model

- **Node** = one `.md` file, anywhere in the project directory
- **Node ID** = `title` field from YAML frontmatter (must be unique across the graph)
- **Tree edges** = `parent` field in frontmatter. Root nodes omit `parent`.
- **Cross-references** = `[[wiki links]]` in the markdown body (not tree edges, just references)
- **Constraints**: max 1 parent per node, soft bound of 5-9 children per node

Frontmatter schema:

```yaml
---
title: Some Node Title
parent: Parent Node Title
---
```

## Architecture

- Markdown files are the source of truth
- Turso (libSQL) database is a derived cache, rebuilt on sync
- The tool is read-only against the markdown files — it never writes them
- All CLI output to stdout is EDN (Extensible Data Notation) using keyword keys (`:title`, `:parent`, etc.)
- Validation and diagnostics go to stderr as human-readable text

## Dependencies

Use only these dependencies plus the Go standard library:

- **CLI framework**: Kong — https://github.com/alecthomas/kong
- **Markdown parsing**: Goldmark — https://github.com/yuin/goldmark
- **Frontmatter extraction**: goldmark-frontmatter — https://github.com/abhinav/goldmark-frontmatter
- **Wiki link extraction**: Either use the goldmark-wikilink extension or a simple regex over the markdown body (`\[\[([^\]]+)\]\]`). Use whichever is simpler.
- **Database**: Turso Go client — `github.com/tursodatabase/turso-go`. Uses standard `database/sql` interface. For v0, just use a local SQLite file, no remote Turso sync.
- **EDN output**: go-edn — `olympos.io/encoding/edn`. Use keyword types for all map keys. API mirrors `encoding/json`.

## Reference Documentation

Read these before starting implementation:

- **Turso docs (start here)**: https://docs.turso.tech/introduction
- **Turso Go quickstart**: https://docs.turso.tech/connect/go
- **Turso vector search (future, skim for awareness)**: https://docs.turso.tech/guides/vector-search
- **Go module layout**: https://go.dev/doc/modules/layout
- **Go style guide**: https://google.github.io/styleguide/go/guide
- **Go best practices**: https://google.github.io/styleguide/go/best-practices
- **Kong CLI README**: https://github.com/alecthomas/kong/blob/master/README.md
- **Goldmark README**: https://github.com/yuin/goldmark/blob/master/README.md
- **goldmark-frontmatter**: https://github.com/abhinav/goldmark-frontmatter
- **go-edn introduction**: https://github.com/go-edn/edn/blob/v1/docs/introduction.md
- **Open Agent Skills specification**: https://openagentskills.dev/docs/specification
- **Writing SKILL.md**: https://openagentskills.dev/docs/writing-skill-md
- **OpenCode agent skills**: https://opencode.ai/docs/skills/
- **OpenCode agents**: https://opencode.ai/docs/agents/

## CLI Commands

### `sevens sync [--root <path>]`

Scan all `.md` files under root (recursively, default `.`), parse frontmatter from each, extract `[[wiki links]]` from body, rebuild the Turso DB. After rebuilding, print a validation report to stderr:

- Orphan nodes (no parent declared, and also no children — true isolates)
- Missing parents (parent title doesn't match any node's title)
- Duplicate titles
- Branching overflow (any node with >9 children)
- Branching underflow is NOT an error — nodes with 0-4 children are fine, they're just not full

### `sevens overview [--root <path>]`

Print to stdout an EDN representation of the full tree structure. Titles only, no content. Include for each node: title, parent title (or nil), list of children titles, child count, and list of wiki-link cross-references found in its body.

```clojure
{:nodes
 [{:title "EHE Modernization"
   :parent nil
   :children ["Container Strategy" "Terraform Cleanup" "Cortex Platform"]
   :child-count 3
   :cross-refs ["Kubernetes Concepts"]}]
 :validation
 {:orphans []
  :missing-parents []
  :duplicate-titles []
  :overflow []}}
```

### `sevens walk <node-title> [--depth N] [--root <path>]`

Print to stdout an EDN map containing:

- The node's full markdown content (everything after frontmatter)
- Its parent title (or nil)
- Its children (titles only)
- Its siblings (titles only — other children of the same parent)
- Wiki-link cross-references found in its body
- A list of all node titles that were NOT included in this response, so the agent can decide what to walk next

Default `--depth` is 1 (the node itself plus its immediate children's titles). Depth 0 means just the node, no children listed.

```clojure
{:node
 {:title "Container Strategy"
  :parent "EHE Modernization"
  :content "## Current State\n\nWe're running legacy Java apps on EC2..."
  :children ["Docker Migration" "K8s Rollout" "Image Pipeline"]
  :siblings ["Terraform Cleanup" "Cortex Platform"]
  :cross-refs ["Kubernetes Concepts" "Cortex Platform"]}
 :unwalked
 ["Docker Migration" "K8s Rollout" "Image Pipeline"
  "Terraform Cleanup" "Cortex Platform" "Kubernetes Concepts"]}
```

## DB Schema

Two tables:

```sql
CREATE TABLE nodes (
  title TEXT PRIMARY KEY,
  parent TEXT,
  file_path TEXT NOT NULL,
  content TEXT NOT NULL
);

CREATE TABLE cross_refs (
  source_title TEXT NOT NULL,
  target_title TEXT NOT NULL,
  PRIMARY KEY (source_title, target_title)
);
```

## Code Structure

Keep it flat and simple:

```
sevens/
  main.go          -- kong CLI definition, command dispatch
  sync.go          -- file scanning, frontmatter parsing, DB rebuild, validation
  graph.go         -- tree queries (walk, overview) against the DB
  db.go            -- Turso/DB connection setup, schema init
  go.mod
  go.sum
```

## Agent Skill

After building the CLI tool, create an agent skill following the Open Agent Skills specification. The skill should be placed at `.opencode/skill/sevens/SKILL.md` (or `.claude/skills/sevens/SKILL.md` for Claude Code compatibility).

The skill document should contain:

1. **Tool API reference** — what each `sevens` command returns, when to call it, output format (EDN with keyword keys)
2. **The structural rules** — one parent, 5-9 children, title as ID, frontmatter schema
3. **Decomposition heuristics** — when to suggest breaking a node (e.g., >2000 words and no children, >9 children, content covering clearly distinct topics)
4. **Many examples of scaffolding sections** that could appear within nodes — questions, prompts, structural patterns — without declaring a fixed taxonomy of node types. Examples should cover a range: strategy nodes, concept nodes, decision records, process descriptions, troubleshooting guides, reference material, etc. Show what kinds of sections and questions naturally appear in each, but frame them as examples to pattern-match on, not types to declare.
5. **Worked examples** of the full brain-dump → decompose → elaborate cycle, showing the agent reading a dense node via `sevens walk`, proposing 5-7 children with titles and scaffolding questions, and the user accepting/editing before the agent creates the files.
6. **Elaboration heuristics** — when a node has sparse content and few children, suggest follow-up questions rather than decomposition. Include examples of good follow-up questions that draw out implicit knowledge.

The skill should instruct the agent to use `sevens` commands as its primary interface to the knowledge graph, and to use its own file-writing capabilities (not sevens) to create new nodes when the user approves a decomposition.

## What NOT to Build

- No semantic search / embeddings (future — Turso has native vector search for when we're ready)
- No LLM integration in the CLI tool (that's the agent skill layer)
- No file writing in the CLI — the tool is read-only
- No sub-heading node support (future — for now one file = one node)
- No web UI
- No MCP server (future, maybe)

## Workflow

**Do not start writing code immediately.**

### Phase 1: Research and Design (do this first)

1. Spin up subagents to read all reference documentation listed above — Turso docs, Kong, Goldmark, goldmark-frontmatter, go-edn, the Go module layout, the Go style guides, and the agent skills specification. Each subagent should summarize what's relevant to this project.
2. Based on the research, produce a **high-level design document** for my review. This should cover:
   - Data flow: how markdown files become DB rows, how queries traverse the tree
   - Key type definitions (Go structs for nodes, validation results, CLI output shapes)
   - How EDN serialization works with go-edn's keyword types and struct tags
   - How goldmark + goldmark-frontmatter + wiki link extraction compose together
   - The DB lifecycle: when sync runs, what it drops/recreates, how walk/overview query
   - The agent skill structure: what files, what goes in SKILL.md vs references/
   - Any design decisions or ambiguities that need my input
3. **Stop and wait for my review.** Do not proceed to implementation until I've approved the design.

### Phase 2: Implementation

Once the design is approved, spin up subagents to implement in parallel where possible. Each file in the code structure (`db.go`, `sync.go`, `graph.go`, `main.go`) is a natural unit of work, though they share type definitions. The agent skill document is a separate workstream that can proceed in parallel with the CLI implementation.
