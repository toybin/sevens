# Concept: GraphOps

Status: draft v1 -- working through via discussion
Layer: 2 (sevens-agnostic graph-conscious operations)

---

## Name and Type Parameters

```
concept GraphOps [Graph]
```

Parameterized by Graph -- it operates on a triple store but doesn't own
one. This is the layer that knows about graph structure (paths,
directionality, predicate properties) without knowing about any particular
domain (PKM, knowledge management, or sevens).

---

## Purpose

To navigate and manipulate a graph of triples using predicate metadata,
path composition, and structural operations, independent of any domain
vocabulary.

**Discussion notes**:

This is the categorical machinery. The graph concept (layer 1) stores
binary relations. This concept knows that predicates compose, that some
are functional (at most one value) and some are relational (set-valued),
that predicates can be traversed in reverse, and that subgraphs can be
identified and operated on as units.

Reusable outside sevens. A Haskell triple store with this layer on top
would give you graph manipulation for any domain, not just PKM.

---

## Operational Principle

If you register a predicate as functional and then set a value for it,
any previous value for the same (subject, predicate) pair is replaced.
If you compose a path of predicates [r, s, t] starting from a subject,
you reach all subjects connected by following r, then s, then t -- with
each step optionally reversed (inverse traversal). If you identify all
subjects reachable from a root via a membership predicate, you can
operate on that subtree as a unit.

---

## State

```
state:
  a set of PredicateSpecs with
    a name String (unique)
    a multiplicity one of (functional, relational)
    a inverse lone String
    a symmetric Boolean
    a transitive Boolean
```

This is the metadata that layer 1 doesn't carry. Each PredicateSpec
declares properties of a predicate that graph operations need to behave
correctly:

- **functional vs. relational**: determines whether `set` (replace) or
  `assert` (accumulate) is the correct write operation. `node/parent` is
  functional -- a node has at most one parent. `node/link` is relational
  -- a node can link to many others.

- **inverse**: the name used when traversing the predicate backward.
  If `parent` has inverse `children`, then walking `parent~` from a
  subject finds everything that has that subject as its `parent` object.
  This may or may not be a predicate that exists in the store -- it could
  be a virtual traversal direction.

- **symmetric**: if (A, r, B) then (B, r, A). Saves the layer above
  from asserting both directions.

- **transitive**: if (A, r, B) and (B, r, C) then (A, r, C). Relevant
  for reachability queries.

**What this state is NOT**: it is not a schema in the traditional sense.
The graph (layer 1) doesn't enforce these specs. Layer 2 uses them to
provide correct operations. Asserting a triple with an unregistered
predicate is fine at layer 1 -- layer 2 just won't know its properties.

**Bootstrap question (resolved)**: predicate specs are stored in-memory
(Go map), not as triples. Avoids the bootstrap problem. Specs are
registered programmatically during KB initialization, not via graph
mutations.

---

## Actions

```
actions:
  registerPredicate (spec: PredicateSpec): ()
    requires: no spec with the same name exists (or updates existing).
    effects: adds or updates the predicate specification.

  set (subject: String, predicate: String, object: String): ()
    requires: predicate is registered as functional, OR predicate has
    no registered spec (unregistered predicates are allowed; no error
    is raised). Errors only if a spec exists AND the predicate is
    Relational (multiplicity mismatch).
    effects: retracts all triples with the given (subject, predicate)
    via Graph.retract, then asserts the new triple via Graph.assert.
    Ensures functional predicates have at most one value.

  retractSubgraph (root: String, membershipPredicate: String): ()
    requires: --
    effects: finds all subjects reachable from root by following
    membershipPredicate, then retracts all triples for each subject
    via Graph.retractBySubject. This is the "clear a root" operation
    generalized.
```

**Note**: `set` is a layer 2 action because it encodes predicate metadata
knowledge. Layer 1's `assert` is unconditionally additive. Layer 2's
`set` is conditionally replacing, based on the predicate's registered
multiplicity.

---

## Queries

```
queries:
  lookup (subject: String, predicate: String): (object: lone String)
    -- convenience for functional predicates; returns at most one value.
    -- uses Graph.bySubjectPredicate, returns first (or only) result.

  compose (start: String, path: seq String): (terminals: set String)
    -- walk a sequence of predicates from a starting subject.
    -- each step: if predicate ends with ~, traverse inverse
    -- (use Graph.byPredicateObject with the subject as object).
    -- otherwise traverse forward (use Graph.bySubjectPredicate).
    -- deduplicate and return terminal subjects.

  reachable (start: String, predicate: String): (subjects: set String)
    -- transitive closure: all subjects reachable from start by
    -- following predicate recursively.

  subgraph (root: String, membershipPredicate: String): (triples: set Triple)
    -- all triples whose subjects are reachable from root via the
    -- membership predicate. The "view" of a subtree.
```

---

## What This Concept Does NOT Do

- **Define predicate vocabularies**: it doesn't know that `node/parent`
  exists or what it means. Layer 3 registers specific predicates.
- **Enforce structural invariants**: tree shape, acyclicity, degree
  constraints -- these are domain policies, not graph operations.
- **Subject naming conventions**: `node:<hash>:<title>` is layer 3.
- **Domain-specific queries**: walk, overview, siblings -- these compose
  layer 2 operations with layer 3 vocabulary knowledge.

---

## Relationship to Category K

This is where K's "morphisms acquire semantic weight" begins. Layer 1
gives us bare binary relations. Layer 2 gives predicates *properties* --
functional, symmetric, transitive, invertible. These properties are
exactly the properties of morphisms in category theory.

Path composition here IS arrow composition. The `compose` query is the
computational realization of following morphisms through intermediate
objects. The intermediary principle becomes operational: to traverse a
complex relationship, you compose simple predicate steps.

The inverse traversal (~) corresponds to the observation in K that
synchronic relations are symmetric at the floor but can be traversed
asymmetrically through intermediate objects. `parent` forward and
`parent~` (children) backward are the same morphism traversed in
different directions.

---

## Open Questions

1. **Bootstrap problem**: Resolved: predicate specs are stored in-memory
   (Go map), not as triples. Avoids bootstrap problem. Specs are registered
   programmatically during KB initialization.

2. **Symmetric auto-assertion**: if a predicate is symmetric, should `set`
   or `assert` automatically create the reverse triple? Or should that be
   a sync-level concern? Putting it here makes the concept smarter but
   potentially surprising.

3. **Transitive closure**: `reachable` implies recursive graph traversal.
   In a large graph this is expensive. Should the concept define this
   operation or leave it to callers who know the expected depth?

4. **Scope of predicate metadata**: the current spec has five properties
   (name, multiplicity, inverse, symmetric, transitive). Are there others
   that graph-level operations need? Ordered (for seq-like predicates)?
   Deprecated? Scoped-to-a-namespace?
