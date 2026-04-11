# Concept: KnowledgeBase

Status: draft v1 -- working through via discussion
Layer: 3 (sevens-specific abstractions)

---

## Name and Type Parameters

```
concept KnowledgeBase [Graph, GraphOps]
```

Parameterized by layers 1 and 2. Uses Graph for triple storage and
GraphOps for predicate metadata, path composition, and functional
predicate operations.

---

## Purpose

To impose the structure of organized thinking onto a graph of triples,
where ideas are nodes in a tree, each level is small enough to reason
about, and relationships between ideas are navigable and queryable.

**Discussion notes**:

This is the concept that makes sevens *sevens*. Below it, you have a
generic triple store with graph operations. Above it, you have functions,
projections, and user-defined semantics. This layer establishes what a
"node" is, what "parent" means, what it means for a knowledge base to be
well-structured.

Identity is defined by morphisms, not the other way around. A node is
not a thing that has a parent and content -- it IS the pattern of
predicates that connect its subject to other subjects. The subject string
(`node:<hash>:<title>`) is a handle for referring to that pattern, not
the identity itself. This is K's "types as subgraph shapes" made
operational.

---

## Operational Principle

If you create a node with a title under a parent, you can navigate to it
from its parent (as a child), from its siblings (as a peer), and from
any cross-referenced node (as a link). If a node grows too complex, you
create children under it, distributing the complexity into parts small
enough to think about. If a node has more than about nine children, the
structure signals that further decomposition is needed at a different
level. The tree is always navigable from any node to any other via parent
and child traversals.

---

## Predicate Vocabulary

The predicates this concept registers with GraphOps, establishing the
PKM domain model:

```
predicates:
  node/title       functional    "human-readable name of a node"
  node/parent      functional    "the node's single parent; inverse: children"
  node/content     functional    "the node's textual body"
  node/file        functional    "path to the node's source file (if projected)"
  node/root        functional    "the root this node belongs to"
  node/char-count  functional    "character count of content"
  node/link        relational    "cross-reference to another node (wiki-link)"
  node/role        relational    "named role within sibling set (e.g., 'pro', 'con')"
  block/node       functional    "the document node this block belongs to"
  block/root       functional    "the root this block belongs to"
  block/content    functional    "the block's textual content"
  block/scope      functional    "the heading scope containing this block"
  block/kind       functional    "heading, paragraph, list-item, task, etc."
  block/path       functional    "structural path within the document"

  -- session predicates (working context)
  session/focus    functional    "the primary node of attention"
  session/include  relational    "additional nodes in working context"
  session/exclude  relational    "nodes excluded from working context"
  session/started  functional    "when this session began"
  session/ended    functional    "when this session ended (if closed)"

  -- log predicates (activity history)
  log/function     functional    "which function was applied"
  log/node         functional    "which node was targeted"
  log/step         functional    "which step produced this entry"
  log/timestamp    functional    "when this operation occurred"
  log/session      functional    "which session this occurred in"
  log/result       functional    "the output or summary of the operation"
```

**Discussion notes**:

This is not an exhaustive list. The open predicate space (layer 1 design
invariant) means new predicates can be added by this concept or by
concepts above. But these are the *core* predicates that define what a
sevens knowledge base looks like.

Session and log entries are nodes in the graph. The operations performed
on the knowledge base are themselves knowledge *in* the knowledge base.
The graph describes its own history. This is the meta-temporal layer:
not what your ideas are, but what you did with them and in what context.

Log entries are **presentational, not operational**. They are the
historical record of what happened -- written by syncs when the Function
concept's pipeline state transitions (accept, reject, revise, complete).
Nothing reads log entries to drive operational behavior. The Function
concept owns its own pipeline state; logs are the durable projection
of transitions for user queries and temporal analysis.

Focus, session, and logging were initially considered as separate
concepts. They collapse into KnowledgeBase predicates because they
share the same purpose (making the knowledge base usable and
self-aware) and the same machinery (triples in the graph, queryable
like everything else).

Nodes and blocks are the same kind of thing. A block is a node. The
`block/*` and `node/*` namespace prefixes are labels on compositions of
the same underlying morphisms -- they tell the tool which compositions
to follow for which operations. At the K level, it's all binary
relations. At the sevens level, the namespace prefix encodes
compositional role.

`overview` follows `node/parent~`. Block-level operations follow
`block/node~`. Same graph, same traversal machinery, different predicate
argument. The namespace IS the filter.

This means `createNode` with block-pattern predicates creates a block.
No separate API surface is needed. The distinction lives entirely in
which predicates you assert, which determines which compositions the
node participates in.

**Open question**: should `node/file` exist at this layer? It's a
projection concern -- it maps graph state to a filesystem artifact. If
the graph is authoritative and markdown is a projection, the file path
is projection metadata, not PKM structure. But the current system uses
it heavily for file operations. This might be a projection-concept
predicate that this concept doesn't define but does tolerate.

---

## Subject Identity

Subjects are scoped strings encoding the root and title:

```
node:<6-byte-sha1-of-root>:<title>
block:<6-byte-sha1-of-root>:<nodeTitle>:<path>
```

The root hash provides namespace isolation -- two roots can have nodes
with the same title without collision. The title (or title+path for
blocks) provides human-readable identity within the namespace.

**Key property**: identity is constructable from (root, title) without
querying the graph. You can compute a subject string and assert triples
about it without first looking anything up. The `node/title` predicate
provides the reverse lookup -- from subject back to human-readable name.

**Discussion notes**:

This identity scheme is one design choice. Others are possible:
content-addressed (hash of content), UUID-based, or path-based
(filesystem-derived). The current scheme ties identity to title, which
means renaming a node changes its identity -- all triples must be
rewritten. This is a known cost of title-based identity.

Whether this belongs at layer 3 or layer 4 (user-defined) is debatable.
The scheme is sevens-specific, not a general graph concern. But it's
also not user-defined -- it's baked into how sevens constructs subjects.
Layer 3 feels right.

---

## State

```
state:
  -- the predicates above, registered with GraphOps as PredicateSpecs
  -- the subject identity scheme (algorithmic, not stored)
  -- structural parameters:
  a maxChildren Int (default 9)
  a maxContentLength Int (default configurable)
```

The actual node and relationship data lives in the graph as triples.
This concept does not duplicate that state. It provides the
*interpretation* -- the vocabulary, naming conventions, and structural
constraints that give meaning to patterns of triples.

The `maxChildren` and `maxContentLength` are the 7±2 constraint made
configurable. They parameterize validation, not storage.

---

## Actions

```
actions:
  createNode (root: String, title: String, content: String,
              parent: lone String): (subject: String)
    requires: no node with the same (root, title) exists.
    effects: computes the subject from (root, title). Asserts triples
    for node/title, node/content, node/root, node/char-count. If parent
    is provided, asserts node/parent. Returns the subject.

  deleteNode (subject: String): ()
    requires: subject exists in the graph.
    effects: retracts all triples for this subject via
    GraphOps.retractSubgraph (if it has dependents) or
    Graph.retractBySubject (if leaf).

  moveNode (subject: String, newParent: String): ()
    requires: both exist. newParent is not a descendant of subject
    (no cycles).
    effects: uses GraphOps.set on node/parent to replace the parent.

  linkNodes (source: String, target: String): ()
    requires: both exist.
    effects: asserts (source, node/link, target).

  unlinkNodes (source: String, target: String): ()
    requires: the link exists.
    effects: retracts (source, node/link, target).

  setContent (subject: String, content: String): ()
    requires: subject exists.
    effects: uses GraphOps.set on node/content and updates
    node/char-count.

  registerRoot (path: String): (rootHash: String)
    requires: path is a valid directory.
    effects: computes the root hash, registers in the root registry.
    Returns the hash for use in subject construction.
```

**Discussion notes**:

These actions are the PKM-domain operations that compose layer 1 and 2
primitives. `createNode` is really "assert a specific pattern of triples
that constitutes a node." `moveNode` is really "set a functional
predicate." The concept adds domain meaning to generic graph operations.

`deleteNode` has unresolved semantics -- see open questions.

---

## Queries

```
queries:
  walk (subject: String): (context: WalkContext)
    -- returns the full local context of a node:
    -- title, content, parent, children, siblings, cross-references,
    -- roles. Composes GraphOps.compose with the predicate vocabulary:
    --   parent:    node/parent forward
    --   children:  node/parent inverse (i.e., node/parent~)
    --   siblings:  node/parent forward, then node/parent~, minus self
    --   links:     node/link forward and inverse

  overview (rootHash: String): (tree: Tree)
    -- all nodes in the root, arranged by parent/child structure.
    -- the tree that `sevens overview` renders.

  resolve (root: String, title: String): (subject: lone String)
    -- find a node by human-readable title within a root.
    -- constructs the subject from (root, title) and verifies it exists.

  children (subject: String): (subjects: seq String)
    -- GraphOps.compose(subject, ["node/parent~"])

  parent (subject: String): (subject: lone String)
    -- GraphOps.lookup(subject, "node/parent")

  siblings (subject: String): (subjects: set String)
    -- GraphOps.compose(subject, ["node/parent", "node/parent~"]) minus self

  validate (rootHash: String): (violations: set Violation)
    -- check all structural invariants:
    --   orphans (nodes with no parent that aren't the root)
    --   oversized (> maxChildren children)
    --   overlength (content exceeds maxContentLength)
    --   missing parents (node/parent points to nonexistent subject)
    --   cycles (reachable from self via node/parent)
```

---

## Structural Invariants

These are not enforced by the actions (actions don't refuse to create
an 11th child). They are checked by `validate` and surfaced to the user
or to higher-level concepts that want to drive behavior based on
structural health.

- **Tree property**: each node has at most one parent (enforced by
  `node/parent` being functional).
- **Acyclicity**: no node is its own ancestor via `node/parent`.
- **The 7±2 constraint**: no node should have more than `maxChildren`
  children. This is the tool's namesake. It drives decomposition
  behavior -- when a node exceeds the threshold, the system (or the
  user) should decompose further.
- **Content length**: nodes beyond `maxContentLength` signal need for
  decomposition into children or blocks.
- **Orphan detection**: every node except the root should be reachable
  from the root via `node/parent~` traversal.

**Discussion notes**:

The 7±2 constraint is deliberately advisory, not enforced. The concept
surfaces violations; concepts above (functions, UI) can act on them.
This keeps the knowledge base concept from being opinionated about
*how* to fix structural problems -- it just identifies them.

Should the constraint parameters be per-root? Per-node? Global? The
current system uses global defaults. Per-root would let different
projects have different structural expectations.

---

## What This Concept Does NOT Do

- **Parse or render any presentational format** -- no markdown, no org,
  no HTML. Content is a string. The Projection concept handles format.
- **Run AI operations** -- functions, prompts, LLM calls are above.
- **Manage approval workflows** -- suspension/accept/reject is above.
- **Define user-level types** -- the DSL, custom typed properties, and
  user-defined predicates are layer 4.
- **Track history** -- operation logs, git commits, witnesses are
  separate concepts.

---

## Relationship to Category K

This is where K's derived structure becomes concrete:

- **Equivalence classes**: the children of a node form an equivalence
  class in the synchronic sense -- they are the things currently related
  to the parent, right now. The partition at any moment is the tree
  structure.

- **Types as subgraph shapes**: a "node" is recognized by its predicate
  pattern (has node/title, node/root, optionally node/parent and
  node/content). A "block" is recognized by a different pattern (has
  block/node, block/path, block/content). No type field. The type IS
  the shape.

- **The 7±2 constraint**: a structural health heuristic grounded in
  cognitive science, applied as a validation rule on equivalence class
  size. When a partition level exceeds what a human can hold in working
  memory, the structure needs further decomposition.

- **Witnesses**: a node that persists across edits, syncs, and AI
  operations is a well-witnessed subgraph. One that gets deleted or
  restructured was not durable. The validation query identifies nodes
  whose structural context is degraded (missing parents, orphaned
  children) -- potential witnesses that are becoming ill-typed.

---

## Open Questions

1. **Delete semantics**: when a node with children is deleted, what
   happens to the children? Options: cascade (delete them too), orphan
   (remove their parent predicate, validation catches them), refuse
   (require children to be moved or deleted first), reparent (attach
   to grandparent). This might vary by use case, suggesting it should
   be parameterized or a sync-level decision.

2. **Rename semantics**: renaming a node changes its subject (since
   subjects are derived from title). All triples referencing the old
   subject must be rewritten. Is this an action of this concept, or
   should identity be decoupled from title?

3. **Resolved: block/node predicate namespaces.** Separate namespaces,
   same underlying thing. `block/*` and `node/*` are labels on
   compositions of the same morphisms. The namespace prefix tells the
   tool which compositions to follow. No unification needed -- the
   separation is the compositional filtering mechanism.

4. **Root as node**: we said a root is "just the starting node." But
   roots currently have config (`.sevens.edn`) and a filesystem
   location. Is root config part of this concept's state (stored as
   triples on the root node) or a projection concern (the filesystem
   materialization of root metadata)?

5. **Predicate extensibility**: this concept defines the core predicate
   vocabulary. Layer 4 (user-defined) adds more. How do user predicates
   get registered with GraphOps? Does this concept provide an
   `extendVocabulary` action, or does layer 4 talk to GraphOps directly?
