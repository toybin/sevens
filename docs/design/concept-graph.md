# Concept: Graph

Status: draft v3 -- working through via discussion
Layer: 1 (K triples)

---

## Name and Type Parameters

```
concept Graph
```

No type parameters. Nodes and predicates are the only primitives.

---

## Purpose

To store and retrieve knowledge as typed relationships between entities,
where all information -- content, structure, metadata -- is represented
uniformly as triples.

**Discussion notes**:

The purpose is deliberately low-level. The graph doesn't know about documents,
blocks, trees, roots, or the 7±2 constraint. It knows subjects, predicates,
and objects. Higher-level concepts give shape to this substrate by
establishing predicate conventions and structural invariants.

This mirrors the Category K architecture: the graph is the floor. Binary
relations compose into domain-specific triples where morphisms acquire
semantic weight through intermediate objects. The types-and-witnesses
structure from K holds -- a well-typed subgraph is a pattern of triples
that satisfies structural expectations, and a witness is a pattern that
persists across changes (ticks).

---

## Operational Principle

If you assert a triple (subject, predicate, object), you can later retrieve
it by any of its components: by subject, by predicate, by (subject,
predicate) pair, or by (predicate, object) pair. If you assert a triple
that already exists, nothing changes (idempotent). If you retract a triple,
it is gone and subsequent queries will not find it.

**Note**: composition (path walking) and functional-predicate semantics
(`set`) belong to the GraphOps concept (layer 2), not here. This concept
provides the storage substrate that makes composition possible but does
not implement it.

---

## State

```
state:
  a set of Triples with
    a subject String
    a predicate String
    a object String
    unique on (subject, predicate, object)
```

That's it. All structure is emergent from predicate conventions established
by higher-level concepts.

**Design invariants**:

- **Uniformity**: every piece of information is a triple. No side tables, no
  special storage for "content" vs. "structure" vs. "metadata." This is the
  intermediary principle: the complexity is in the objects (values in subject
  and object positions), never in the edges (which are always the same shape).

- **Open predicate space**: new kinds of information are new predicates. The
  graph never needs schema migration. This is what makes it extensible
  without coordination -- any concept layered above can introduce its own
  predicates without modifying the graph.

- **Thinness**: between any (subject, predicate, object) tuple, the relation
  either holds or it doesn't. No weights, no multiplicity, no metadata on
  the relation itself. If you need to annotate a relationship, you add an
  intermediate node (the intermediary principle again).

**Note on what this state does NOT include**:

No notion of "node" vs. "block" vs. "root." No parent/child hierarchy. No
content field. These are all predicate conventions that higher concepts
establish. The graph sees `("node:abc123:MyNote", "node/parent",
"node:abc123:ParentNote")` as the same kind of thing as
`("node:abc123:MyNote", "node/content", "Some text here")`. Both are
triples. The graph doesn't privilege either.

---

## Actions

```
actions:
  assert (subject: String, predicate: String, object: String): ()
    requires: --
    effects: adds the triple if not already present. Idempotent.

  retract (subject: String, predicate: String, object: String): ()
    requires: the triple exists.
    effects: removes the triple.

  retractBySubject (subject: String): ()
    requires: --
    effects: retracts all triples with the given subject.

  assertBatch (triples: set Triple): ()
    requires: --
    effects: asserts all triples. Idempotent per triple.
```

These are CRUD operations on triples. There is no domain logic here.
The graph doesn't validate whether a predicate is meaningful or whether
a subject follows any naming convention. It stores and retrieves.

---

## Queries

```
queries:
  bySubject (subject: String): (triples: set Triple)
  byPredicate (predicate: String): (triples: set Triple)
  bySubjectPredicate (subject: String, predicate: String): (objects: set String)
  byPredicateObject (predicate: String, object: String): (subjects: set String)
  search (predicate: String, substring: String): (subjects: set String)
    -- find subjects whose object for the given predicate contains
    -- the substring
```

---

## What This Concept Does NOT Do

The following are responsibilities of concepts layered above:

- **Naming conventions for subjects** (e.g., `node:<hash>:<title>`) --
  established by whatever concept manages node identity
- **Predicate vocabularies** (e.g., `node/parent`, `node/content`) --
  established by domain concepts (PKM structure, projection, etc.)
- **Structural invariants** (tree shape, 7±2 constraint, no orphans) --
  enforced by domain concepts as policies on top of the graph
- **Parsing and projection** (markdown to triples, triples to markdown) --
  the Projection concept's job
- **AI operations** -- the Function concept's job
- **Human approval gates** -- the Suspension concept's job

---

## Relationship to Category K

The graph concept is the computational realization of K's single axiom:
knowledge is synchronic binary relation. Each triple is a binary relation
holding at a moment. The set of all triples at a given moment is a
partition -- an equivalence structure over subjects.

K's intermediary principle maps directly: every semantically meaningful
relationship is a path through intermediate objects. `(Alice, :rel,
"acquaintance")` and `("acquaintance", :rel, Bob)` -- two triples, one
intermediate object. The graph stores both the same way.

What the graph concept does NOT capture from K: time (T), ticks,
witnesses, observer partitions. These are concerns of concepts that
operate on the graph diachronically -- functions that propose changes,
approval gates that accept or reject them, version history that records
what persisted. The graph itself is synchronic: a snapshot of what holds
right now.

---

## Open Questions

1. **Transaction boundaries**: the current actions are individual triple
   operations. Should the graph concept have a notion of atomic multi-triple
   transactions? Sync, for instance, clears all triples for a root and
   reinserts. Without atomicity, this is a window of inconsistency.

2. **`search` placement**: substring search over object values is arguably
   a graph-ops concern, not a bare storage concern. It could move to layer 2.
   Left here for now because it's predicate-unaware -- it doesn't need
   metadata about the predicate, just a string match.
