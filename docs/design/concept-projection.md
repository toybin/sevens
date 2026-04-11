# Contract: Projection

Status: draft v1 -- working through via discussion
Kind: architectural contract (not a user-facing concept)

---

## Why "Contract" Not "Concept"

Projection is transparent to the user. They edit files, not projections.
They run `sevens sync`, not "parse my projection." The user's mental
model is: "I edit markdown files and the graph updates."

But projection is a real architectural boundary with a well-defined
contract that multiple implementations must satisfy. The concept spec
format is useful for describing it precisely, even though it's internal.

---

## Name and Type Parameters

```
contract Projection [KnowledgeBase, Format]
```

Parameterized by KnowledgeBase (the graph it projects) and Format (the
presentational medium: markdown, org-mode, webapp, etc.). Each Format
implementation satisfies the same contract.

---

## Purpose

To make knowledge base content readable and writable through a
human-native medium, with faithful round-trip correspondence to the
graph.

**Discussion notes**:

This is the natural transformation between graph semantics and
orthographic semantics. The graph stores triples. The projection
renders those triples into a form humans can read and edit (files,
web pages, etc.) and parses edits back into graph mutations.

The contract guarantees round-trip fidelity: render then parse
produces the same graph state (modulo ordering and whitespace).
Parse then render produces the same presentational form (modulo
normalization). This is the "natural transformation" property --
the projection commutes with graph operations.

---

## Operational Principle

If you render a node's graph state into a presentational form, edit
that form, and parse the edits back, the graph reflects exactly the
changes you made -- no more, no less. If you modify the graph directly
(via a function or API) and render again, the presentational form
reflects the new state.

If a node's content has internal structure (headings, sections) and the
projection supports block-level tracking, the identity of individual
blocks is preserved across edits -- changing a paragraph's text doesn't
lose its graph identity.

---

## Contract Interface

```
interface Projection [Format]:

  render (node: String, kb: KnowledgeBase): Format
    -- produce the presentational form of a node from its graph state.
    -- for markdown: a .md file with frontmatter and body.
    -- for webapp: a DOM tree or component state.

  parse (source: Format, kb: KnowledgeBase): set Triple
    -- read a presentational form and produce the triples it
    -- represents. Does not write to the graph -- returns triples
    -- for the caller to assert.

  reconcile (old: set Triple, new: set Triple,
             kb: KnowledgeBase): Changeset
    -- given the triples from the previous parse and a new parse,
    -- produce a changeset: which triples to retract, which to
    -- assert, which block identities map to which.
    -- this is where block identity tracking lives -- format-specific
    -- fuzzy matching (shingle anchors, text hashes) to determine
    -- which block in the new parse corresponds to which block in
    -- the old.

  sync (root: String, kb: KnowledgeBase): SyncResult
    -- the full sync operation: scan the presentational surface,
    -- parse everything, reconcile against current graph state,
    -- apply the changeset. This is what `sevens sync` calls.
    -- returns summary of what changed.

  write (node: String, kb: KnowledgeBase): ()
    -- render a node and write it to the presentational surface.
    -- for markdown: write the .md file to disk.
    -- inverse of sync -- graph state pushed out to presentation.

properties:
  round-trip (render then parse):
    parse(render(node, kb), kb) ≈ currentTriples(node, kb)
    -- "≈" because ordering and whitespace may differ

  round-trip (parse then render):
    render(parse(source, kb), kb) ≈ normalize(source)
    -- presentational form is preserved modulo normalization
```

---

## Format-Specific Concerns

Each Format implementation owns concerns that are invisible to the
graph and to other projections:

### Markdown Format

- **Filesystem layout**: which directory, file naming from node title,
  nested directories or flat
- **Frontmatter**: YAML metadata (parent, wiki-links, custom fields).
  An ergonomic shortcut for the user -- avoids parsing the full body
  to discover links. The frontmatter is redundant with graph state;
  it's a projection convenience
- **Block parsing**: decomposing the markdown body into structural
  blocks (headings, paragraphs, list items, tasks) and assigning
  block subjects
- **Block identity tracking**: fuzzy matching (shingle anchors, text
  hashes) to maintain stable block identity across edits. This is
  the hardest part of the markdown projection and entirely
  format-specific
- **Git**: version control of the projected files. Commits on sync,
  on function application, on template execution. The audit trail.
  Git is part of the markdown projection, not a separate concept --
  a webapp projection wouldn't use git
- **File watching**: detecting external edits (user editing in their
  text editor) and triggering re-parse. Not implemented currently
  but a natural extension
- **DSL extensions**: the custom markup DSL that adds typed property
  lists to certain markdown nodes. The presence of DSL syntax is what
  tells the projection to create block-level structure for a document

### Org-Mode Format (hypothetical)

- Org-native syntax for properties, links, headings
- Different block structure (org headings are hierarchical, not flat)
- Org-specific export/tangle concerns

### Webapp Format (hypothetical)

- DOM rendering of graph state
- Interactive editing with immediate graph updates (no sync step)
- URL routing from graph structure
- No git (version history could be graph-native instead)

---

## State

The projection contract itself is stateless -- it's a set of pure(ish)
functions. But Format implementations may carry state:

```
format-specific state (markdown example):
  a rootPath String
    -- filesystem directory containing the projected files
  a config RootConfig
    -- .sevens.edn settings (ignore patterns, etc.)
  a blockIdentityCache map of (String -> BlockIdentity)
    -- cached identity mappings for reconciliation performance
```

This state belongs to the format implementation, not to the contract.
Different formats have different state needs.

---

## What This Contract Does NOT Do

- **Own any graph state**: the graph is authoritative. The projection
  reads and writes graph state via KnowledgeBase, but stores nothing
  in the graph that isn't a node/block/predicate.
- **Define predicates**: the projection uses KnowledgeBase predicates
  (node/content, node/parent, block/*, etc.) but doesn't define them.
  Exception: `node/file` is arguably a projection predicate -- it maps
  a node to its projected file path. Whether this predicate belongs to
  KnowledgeBase or Projection is an open question.
- **Determine node identity**: subject construction
  (`node:<hash>:<title>`) is KnowledgeBase's job. The projection
  discovers identity (parsing a file to find its title and root) and
  constructs subjects using KB's scheme.
- **Apply functions or manage pipelines**: the projection syncs content
  in and out. What happens to that content (AI operations, validation,
  etc.) is above its concern.

---

## Relationship to Category K

The projection is the natural transformation between orthographic
semantics and domain semantics. K says knowledge is binary relation.
The graph stores those relations as triples. The projection encodes
those relations in a human-readable form where the user "computes on
them in their head" (as discussed).

The key insight from our earlier discussion: markup languages and DSLs
encode semantics that the user computes on mentally. LLMs extend the
computable boundary -- natural language in the projected form is now
also operationally computable. The projection doesn't need to be a
formal language to be useful; it needs to faithfully represent the
graph state in whatever medium the user prefers.

The natural transformation property (round-trip fidelity) is the formal
guarantee that the projection is faithful. If it breaks -- if parsing
a rendered form produces different triples than what was rendered --
the projection has introduced or lost information. The graph and the
user's view have diverged.

---

## Open Questions

1. **`node/file` ownership**: is the file path a KnowledgeBase predicate
   or a projection predicate? It's only meaningful for file-based
   projections. But the current system uses it throughout (file
   operations in functions, git commits). If it's a projection
   predicate, other projections would use their own location predicates
   (e.g., `node/url` for a webapp).

2. **Resolved: sync direction and data authority.** The DB is
   authoritative for persistence (saved state). Files on disk are
   working state (like an unsaved form in a browser). Git is
   checkpoint history. Sync is the "save" operation: working state →
   saved state. Write is the "refresh" operation: saved state →
   working state. Both directions exist and are equally valid.
   The hard case: user has unsaved file edits AND a function modifies
   the graph. Write must merge, not overwrite. The reconcile operation
   in the contract handles this -- it's a three-way merge between
   the last synced state, the current file, and the current graph.
   **On irreconcilable conflict, file wins.** The user's working state
   is sacred. The DB reconstructs from the file -- but only back to
   the last reconcilable point, not the entire history. Find the most
   recent state where file and graph agreed (the common ancestor, like
   git's merge base), rebuild forward from there. Full re-sync from
   scratch is the fallback of last resort, not the default recovery.

3. **Incremental sync**: the current system clears all triples for a
   root and reinserts on sync. With reconcile, incremental sync
   becomes possible (only update what changed). Is incremental sync
   part of the contract or an optimization?

4. **Multiple projections of the same graph**: can a knowledge base
   have both a markdown projection and a webapp projection
   simultaneously? If so, edits in one must propagate to the other
   via the graph. The graph is the mediator. But conflict resolution
   (two projections edited the same node differently) is unaddressed.

5. **Block identity as a contract requirement**: must every projection
   support block-level tracking? A simple projection (e.g., plain text
   files) might not support blocks at all. Should the contract make
   block reconciliation optional, with a capability flag?
