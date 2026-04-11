# Sevens — Triples + PRQL Design

**Date**: 2026-04-07

## Architecture

- **Storage**: SQLite with a single `triples` table (subject, predicate, object)
- **Query**: PRQL compiles to SQL. User-writable queries alongside function definitions.
- **Persistence**: SQLite file is inspectable by standard tools (DB Browser, sqlite3 CLI, Datasette)
- **Inspiration**: Datomic/Datascript philosophy (immutable facts, query-driven), triples model from ahyatt/triples, PRQL for ergonomic query syntax

## The Triples Table

```sql
CREATE TABLE triples (
    subject TEXT NOT NULL,
    predicate TEXT NOT NULL,
    object TEXT NOT NULL,
    PRIMARY KEY (subject, predicate, object)
);

-- Indexes for reverse lookups
CREATE INDEX idx_predicate_object ON triples (predicate, object);
CREATE INDEX idx_object ON triples (object);
```

Everything is a string. Type interpretation lives in the query/application layer, not the schema. This is the Datomic approach — the database is a collection of facts, not a collection of rows.

## Everything as Triples

### Node Identity & Structure

```
("Container Strategy"    :node/root          "/Users/.../sevens-sandbox")
("Container Strategy"    :node/parent        "EHE Modernization")
("Container Strategy"    :node/file-path     "/Users/.../container-strategy.md")
("Container Strategy"    :node/content       "# Container Strategy\n\nWe're migrating...")
("Container Strategy"    :node/max-chars     "2000")
```

The tree is just `:node/parent` triples. Children are the reverse query: all subjects where `:node/parent` = X.

### Cross-References

```
("Container Strategy"    :ref/wiki-link      "Kubernetes Concepts")
("Container Strategy"    :ref/wiki-link      "Cortex Platform")
```

Future typed references:
```
("Container Strategy"    :ref/depends-on     "Kubernetes Concepts")
("Container Strategy"    :ref/contradicts    "Legacy Architecture")
("Container Strategy"    :ref/elaborates     "EHE Modernization")
```

### Context Files

```
("Container Strategy"    :context/file       "~/Documents/sevens-context/architecture.md")
("Multi-Use Space Design" :context/file      "~/Documents/sevens-context/portland-zoning-notes.md")
```

Global context files:
```
("__global__"            :context/file       "~/Documents/sevens-context/author-bio.md")
```

### Root Configuration

```
("/Users/.../sandbox"    :root/alias         "sandbox")
("/Users/.../sandbox"    :root/max-chars     "5000")
("/Users/.../sandbox"    :root/path          "~/Documents/sevens-sandbox")
```

### Frontmatter Extensions (Duck Typing)

Any frontmatter field becomes a triple. The `type: discussion` in a discussion node:
```
("Discussion: The Commons"  :meta/type       "discussion")
```

Custom frontmatter fields users add:
```
("Evidence and Precedents"  :meta/status     "draft")
("Evidence and Precedents"  :meta/priority   "high")
("Commons Governance Models" :meta/confidence "0.6")
```

Duck typing = predicate queries. "Give me all nodes with `:meta/type` = discussion" is a one-line PRQL query. No type registry.

### Function Application Lineage

Every function application becomes triples on the target node:

```
("Container Strategy"    :lineage/fn         "decompose")
("Container Strategy"    :lineage/step       "generate")
("Container Strategy"    :lineage/status     "accepted")
("Container Strategy"    :lineage/commit     "a1b2c3d")
("Container Strategy"    :lineage/timestamp  "2026-04-07T17:02:16Z")
("Container Strategy"    :lineage/produced-by "decompose:generate:2026-04-07T17:02:16Z")
```

For multi-application history, the subject could be a compound key or the lineage entries could use unique IDs:

```
("lineage:001"           :lineage/target     "Container Strategy")
("lineage:001"           :lineage/fn         "decompose")
("lineage:001"           :lineage/step       "suggest")
("lineage:001"           :lineage/status     "accepted")
("lineage:001"           :lineage/timestamp  "2026-04-07T17:00:33Z")

("lineage:002"           :lineage/target     "Container Strategy")
("lineage:002"           :lineage/fn         "decompose")
("lineage:002"           :lineage/step       "generate")
("lineage:002"           :lineage/status     "accepted")
("lineage:002"           :lineage/commit     "dfc18e1")
("lineage:002"           :lineage/timestamp  "2026-04-07T17:02:16Z")
```

This is the triples-design.org pattern for "relationships with metadata" — the lineage event is its own subject with properties.

### Pending / Suspension State

```
("pending:001"           :pending/target     "Container Strategy")
("pending:001"           :pending/fn         "decompose")
("pending:001"           :pending/step       "suggest")
("pending:001"           :pending/step-index "0")
("pending:001"           :pending/raw-output "[{\"title\": \"Child 1\"...}]")
("pending:001"           :pending/summary    "suggest 7 children")
("pending:001"           :pending/timestamp  "2026-04-07T17:00:33Z")
```

Phase 3 serialized continuations:
```
("pending:001"           :pending/resolved   "{\"target\": {\"content-hash\": \"abc123\"}}")
("pending:001"           :pending/remaining  "[{\"step\": \"generate\", ...}]")
("pending:001"           :pending/gate-type  "approve")
("pending:001"           :pending/expects    "[\"approve\", \"reject\", \"revise\"]")
```

### Discussion Threads

```
("Discussion: The Commons"  :discuss/parent-node    "The Commons")
("Discussion: The Commons"  :discuss/turn-count     "8")
("Discussion: The Commons"  :discuss/last-speaker   "agent")
```

Individual turns (if we want turn-level queryability):
```
("discuss-turn:001"      :turn/thread        "Discussion: The Commons")
("discuss-turn:001"      :turn/speaker       "agent")
("discuss-turn:001"      :turn/content       "What if the governance evolution...")
("discuss-turn:001"      :turn/index         "0")
("discuss-turn:001"      :turn/timestamp     "2026-04-07T16:58:56Z")

("discuss-turn:004"      :turn/thread        "Discussion: The Commons")
("discuss-turn:004"      :turn/speaker       "user")
("discuss-turn:004"      :turn/content       "On the governance question — you're right...")
("discuss-turn:004"      :turn/index         "5")
("discuss-turn:004"      :turn/responds-to   "discuss-turn:001")
```

### Content-Derived Facts (Computed on Sync)

```
("Container Strategy"    :content/char-count     "487")
("Container Strategy"    :content/token-estimate  "122")
("Container Strategy"    :content/heading-count   "3")
("Container Strategy"    :content/wiki-link-count "2")
("Container Strategy"    :content/has-questions   "false")
```

### Validation State (Computed on Sync)

```
("Container Strategy"    :validation/status       "ok")
("Existing Community Infrastructure" :validation/status "overflow")
("Existing Community Infrastructure" :validation/issue  "7620/5000 chars")
("The Commons"           :validation/status       "overflow")
("The Commons"           :validation/issue        "10 children > 9")
```

### Focus Sessions

```
("__session__"           :session/node       "The Commons")
("__session__"           :session/root       "/Users/.../sevens-sandbox")
("__session__"           :session/created    "2026-04-07T16:56:34Z")
("__session__"           :session/include    "Multi-Use Space Design")
("__session__"           :session/include    "Staffing and Volunteer Management")
("__session__"           :session/exclude    "Discussion: The Commons")
```

### Function Definitions (Functions as Data)

Functions themselves could be triples — queryable, composable:

```
("fn:bridge"             :fn/name            "bridge")
("fn:bridge"             :fn/description     "Write connecting narrative between siblings")
("fn:bridge"             :fn/output          "ops")
("fn:bridge"             :fn/requires        "target:node")
("fn:bridge"             :fn/requires        "siblings:node[]")
("fn:bridge"             :fn/requires        "parent:node:optional")
("fn:bridge"             :fn/prompt-file     "bridge.md")
("fn:bridge"             :fn/context-file    "templates/bridge-examples.md")
```

Multi-step pipelines:
```
("fn:decompose"          :fn/name            "decompose")
("fn:decompose:suggest"  :step/fn            "fn:decompose")
("fn:decompose:suggest"  :step/index         "0")
("fn:decompose:suggest"  :step/output        "suggestions")
("fn:decompose:suggest"  :step/gate          "approve")
("fn:decompose:suggest"  :step/prompt-file   "decompose.suggest.md")
("fn:decompose:generate" :step/fn            "fn:decompose")
("fn:decompose:generate" :step/index         "1")
("fn:decompose:generate" :step/output        "ops")
("fn:decompose:generate" :step/requires-ref  "fn:decompose:suggest")
```

### Model Profiles

```
("model:fast"            :model/provider     "anthropic")
("model:fast"            :model/id           "claude-haiku-4-5-20251001")
("model:capable"         :model/provider     "anthropic")
("model:capable"         :model/id           "claude-sonnet-4-20250514")
("model:powerful"        :model/provider     "anthropic")
("model:powerful"        :model/id           "claude-opus-4-6")
```

## PRQL Query Layer

PRQL compiles to SQL. It's readable, composable, and pipe-based. SQLite stays as the storage engine — PRQL is the ergonomic layer on top.

### Why PRQL

- Compiles to standard SQL — SQLite executes it
- Pipe syntax is natural for graph traversal: `from triples | filter ... | join ... | select ...`
- User-writable — function authors can define custom queries
- Composable — queries can reference other queries
- The compiled SQL is inspectable if you want to debug

### Basic Queries

Children of a node:
```prql
from triples
filter predicate == "node/parent" && object == "The Commons"
select subject
```

All siblings of a node:
```prql
let parent = (
  from triples
  filter subject == "Container Strategy" && predicate == "node/parent"
  select object
)
from triples
filter predicate == "node/parent" && object == (from parent | select object)
filter subject != "Container Strategy"
select subject
```

All nodes with a discussion child:
```prql
from triples
filter predicate == "meta/type" && object == "discussion"
join (from triples | filter predicate == "node/parent") [==subject]
select {discussion: subject, parent: object}
```

Node content with lineage:
```prql
let content = (
  from triples
  filter subject == "Container Strategy" && predicate == "node/content"
)
let lineage = (
  from triples
  filter predicate == "lineage/target" && object == "Container Strategy"
  join (from triples | filter predicate == "lineage/fn") [==subject]
  select {event: subject, fn: object}
)
```

Search across all content:
```prql
from triples
filter predicate == "node/content" && (object | text.contains "governance")
select subject
```

### User-Writable Queries in Function Definitions

A function's EDN can include PRQL queries that run before the LLM call to gather context:

```clojure
{:name "bridge"
 :description "Write connecting narrative between siblings"
 :output "ops"

 :queries
 {:siblings
  "from triples
   filter predicate == 'node/parent'
   filter object == {{parent}}
   filter subject != {{target}}
   join (from triples | filter predicate == 'node/content') [==subject]
   select {title: subject, content: object}"

  :parent-content
  "from triples
   filter subject == {{parent}}
   filter predicate == 'node/content'
   select object"}

 :prompt "bridge.md"}
```

The `{{target}}` and `{{parent}}` in queries are bound from the function invocation context. Query results become template variables available in the prompt.

This replaces the `:requires` system with something more powerful — the function author writes exactly the query they need, not just "give me siblings." A function that needs "all siblings that have been decomposed but not elaborated" writes that as a PRQL query joining lineage triples.

### Query Composition

Named queries can reference each other:

```clojure
{:queries
 {:stubs
  "from triples
   filter predicate == 'node/parent' && object == {{target}}
   join (from triples | filter predicate == 'content/char-count') [==subject]
   filter object < '200'
   select subject"

  :stubs-content
  "from (stubs)
   join (from triples | filter predicate == 'node/content') [==subject]
   select {title: subject, content: object}"}}
```

### Pre-Call Queries

The gate framework can use queries too. A readiness gate:

```clojure
{:pre-gates
 [{:name "check-content-length"
   :query "from triples | filter subject == {{target}} && predicate == 'content/char-count'"
   :condition "object > '100'"
   :message "Node has very little content — consider elaborating first"}]}
```

## What This Enables

### Duck Typing as Queries

"Find me a child that implements the discussion interface":
```prql
from triples
filter predicate == "node/parent" && object == {{target}}
join (from triples | filter predicate == "meta/type" && object == "discussion") [==subject]
select subject
```

"Find me nodes that have been noticed but not challenged":
```prql
let noticed = (
  from triples | filter predicate == "lineage/fn" && object == "notice"
  join (from triples | filter predicate == "lineage/target") [==subject]
  select {target: object}
)
let challenged = (
  from triples | filter predicate == "lineage/fn" && object == "challenge"
  join (from triples | filter predicate == "lineage/target") [==subject]
  select {target: object}
)
from noticed
anti_join challenged [==target]
```

### Context Gathering as Queries

Instead of the LLM navigating the graph with tool calls, context gathering becomes a set of PRQL queries that the function (or the user) defines. The queries run, results are injected into the prompt. No extra LLM call for gathering.

For cases where the queries aren't known in advance (truly emergent context needs), the LLM-driven gathering from §7 still applies — but the LLM writes PRQL queries instead of calling `walk`/`overview` tools.

### The Resolver Becomes a Query Runner

The current `ResolveContext` function does hardcoded DB queries based on role names ("parent" → fetch parent, "siblings" → fetch siblings). With PRQL, the resolver is generic: run the queries, bind results to template variables. The function author controls what gets resolved.

### Functions as Data, Queryable

Since function definitions are themselves triples, you can query them:

"Which functions produce ops output?":
```prql
from triples
filter predicate == "fn/output" && object == "ops"
select subject
```

"Which functions require sibling content?":
```prql
from triples
filter predicate == "fn/requires" && (object | text.contains "siblings")
select subject
```

### Graph Analytics

The triples model makes graph analytics trivial:

Depth of each node:
```prql
-- Recursive CTE compiled from PRQL (or raw SQL for recursive queries)
```

Most-connected nodes (by cross-ref count):
```prql
from triples
filter predicate == "ref/wiki-link"
group subject (aggregate {ref_count = count this})
sort {-ref_count}
take 10
```

Function application frequency:
```prql
from triples
filter predicate == "lineage/fn"
group object (aggregate {count = count this})
sort {-count}
```

## Migration Path

1. **Keep the `sync` → rebuild model.** Sync reads markdown files, extracts triples, inserts into SQLite. The filesystem remains source of truth for node content. The DB is a derived triple store.

2. **Lineage/pending/session triples are primary.** They don't come from files — they're written directly to the DB by the runtime. These are the DB-is-source-of-truth facts.

3. **PRQL compilation.** Add a PRQL → SQL compiler as a dependency (go-prql or shell out to prqlc). Queries in EDN are compiled at function load time. Compilation errors are caught early.

4. **Incremental adoption.** The old `nodes` + `cross_refs` tables can coexist with the `triples` table during migration. Old code reads from old tables; new code reads from triples. Once everything's ported, drop the old tables.

## Categorical Foundations

### Triples as Morphisms

A triple `(A, f, B)` is an arrow `A →f→ B` in a category. The subject and object are objects in the category. The predicate is the morphism. The triples table is a hom-set — the collection of all arrows in the system.

Composition is the fundamental operation. If you have:
```
(Container Strategy  :node/parent   EHE Modernization)
(EHE Modernization   :node/parent   Program Portfolio)
```

Then `Container Strategy →ancestor→ Program Portfolio` is the composite `:node/parent ∘ :node/parent`. You don't store it — you compute it by composing arrows.

### Predicates Form a Category

The predicates themselves compose. `:node/parent ∘ :node/parent = :node/ancestor`. `:node/parent⁻¹ = :node/children` (reverse lookup). These aren't different predicates stored in the DB — they're derived from composition and inversion.

The core predicates and their algebraic properties:

| Predicate | Inverse | Composition with self |
|---|---|---|
| `:node/parent` | `:node/child-of⁻¹` (children) | `:node/ancestor` |
| `:ref/wiki-link` | `:ref/linked-from` | transitive closure of references |
| `:lineage/target` | `:lineage/applied-to⁻¹` | (doesn't compose with self meaningfully) |
| `:discuss/parent` | `:discuss/thread-of⁻¹` | (single level) |

A PRQL join is morphism composition. When you write:
```prql
from triples t1
filter t1.predicate == "node/parent"
join (from triples t2 | filter t2.predicate == "node/content") [t1.subject == t2.subject]
```

You're composing `:node/parent⁻¹` with `:node/content` — "from this node, find its children, then get their content." Two arrows composed.

### Objects Are Characterized by Their Morphisms

A node's "type" isn't a label — it's the set of morphisms it participates in. This is the categorical definition: an object is characterized by its arrows in and out.

A node "is a discussion node" if there exist arrows:
```
(X  :meta/type       "discussion")
(X  :discuss/parent  Y)
```

A node "has been analyzed" if there exist arrows:
```
(lineage:N  :lineage/target  X)
(lineage:N  :lineage/fn      "notice")
```

Duck typing is exactly this: check the morphism signature, not a type label. The query "find me a child that implements the discussion interface" is "find objects reachable from target via `:node/parent⁻¹` that also have outgoing `:meta/type → "discussion"` arrows."

### Queries as Morphism Path Specifications

Every PRQL query is really a specification of which morphism paths to traverse. The function's `:queries` block declares paths:

```clojure
{:context
 [;; target →parent→ X →parent⁻¹→ [siblings] →content→ [text]
  {:path [:node/parent :node/parent⁻¹]
   :exclude-self true
   :with [:node/content]
   :as "siblings"}

  ;; target →parent→ X →content→ [text]
  {:path [:node/parent]
   :with [:node/content]
   :as "parent"}

  ;; target →parent⁻¹→ [children] →content→ [text]
  {:path [:node/parent⁻¹]
   :with [:node/content]
   :as "children"}]}
```

Each `:path` is a morphism composition. `:with` selects which additional arrows to traverse from the terminal objects. The resolver walks the composition chain and collects results.

This is equivalent to the PRQL queries but in a declarative morphism language. The two representations are interchangeable — a path spec compiles to PRQL, and a PRQL query can be analyzed as a path spec.

### Functions as Morphisms (Currying and Composition)

Functions themselves are morphisms in a higher category — the category of **transformations on the graph**.

`decompose: Node → Suggestions → Ops` is a composed morphism with a gate (suspension point) in the middle:

```
Node →suggest→ Suggestions →(gate: approve)→ Suggestions →generate→ Ops
```

The gate is a morphism that requires an external input (human judgment) to resolve. Until the human provides input, the composition is **partial** — some arrows are resolved, some are pending.

**Currying is partial morphism composition.** When you call `sevens apply bridge "Container Strategy"`:

1. `bridge` is typed: `(target, siblings[], parent?) → ops`
2. In categorical terms: it's a morphism from a product of objects to ops
3. The runtime begins resolving: `target` binds immediately (the invocation supplies it)
4. Now you have a partially applied morphism: `(siblings[], parent?) → ops` — still needs two more inputs
5. The resolver composes graph morphisms to get siblings: `target →parent→ X →parent⁻¹→ [siblings]`
6. And parent: `target →parent→ X`
7. All inputs resolved → the function morphism can fire (call the LLM)

The partially-applied function at step 4 is a **closure over resolved morphisms**. It carries the resolved `target` and the remaining path specs for `siblings` and `parent`. This is exactly the `Suspension` from §7:

```go
type Suspension struct {
    Resolved  map[string]any       // morphisms already composed
    Remaining []MorphismPath       // paths still to traverse
    Resume    func(input) Thunk    // the continuation
}
```

### Gates as Morphisms to the Human Category

A gate is a morphism from the pipeline to an external category — the **human judgment category**. Objects in this category are decisions: approve, reject, revise, select.

```
Suggestions →(gate: approve)→ HumanDecision →(resume)→ Suggestions'
```

The gate suspends the pipeline (partial composition), serializes the state (resolved morphisms + remaining paths), and waits. When the human provides input, it's a morphism from the human category back into the pipeline category. The composition resumes.

Revision is a loop in this framing:
```
Suggestions →(gate: approve)→ Revision("make titles shorter")
            →(re-run suggest with history)→ Suggestions'
            →(gate: approve)→ Accept
            →(resume)→ Suggestions'
```

Each revision is a morphism `Revision(feedback) → Suggestions'` that re-enters the same step with accumulated context. The revision history is a chain of morphisms in the human category.

### Composed Functions as Functor Application

`decompose-and-elaborate` is a composition of two functors:

```
decompose: Node → [ChildNode]
elaborate: ChildNode → ChildNode'
map(elaborate): [ChildNode] → [ChildNode']
```

The `map-over` operation from the brainstorm is literally a functor — it lifts `elaborate` (which operates on one node) into the category of node collections. `map(elaborate)` applies the morphism pointwise.

In EDN:
```clojure
{:name "decompose-and-elaborate"
 :steps
 [{:name "split"
   :fn "decompose"
   :output "ops"}
  {:name "enrich"
   :fn "elaborate"
   :map-over {:path [:node/parent⁻¹]}  ;; children of target, post-decompose
   :output "ops"}]}
```

The `:map-over` is a functor application. The `:path` declares which morphism path selects the objects to map over. The runtime:
1. Runs `decompose` (which creates new children via ops)
2. Re-syncs the graph (new triples for new nodes)
3. Queries `:node/parent⁻¹` from the target to find the new children
4. Maps `elaborate` over each child — each is an independent sub-pipeline with its own gates

### The Resolver as a Morphism Evaluator

The current `ResolveContext` is a hardcoded set of DB queries. With the categorical framing, it becomes a **morphism evaluator**: given a set of path specs (morphism compositions), traverse the graph and collect terminal objects.

Three kinds of morphisms the evaluator handles:

1. **Graph morphisms** — traverse triples in the DB. `:node/parent`, `:ref/wiki-link`, etc. These are pure DB reads, always terminate.

2. **Pipeline morphisms** — `:ref "suggest"` references a prior step's output. These resolve from the pipeline's accumulated state (already-computed results).

3. **Human morphisms** — gates. These don't resolve until the human acts. The evaluator suspends when it hits one, serializes the partial composition, and exits.

4. **LLM morphisms** — the LLM call itself. Also: context-gathering calls where a cheap model decides which graph morphisms to traverse. These are non-deterministic morphisms — the output depends on the LLM's judgment.

The evaluator is a recursive morphism compositor. It walks the dependency graph (which is itself a category of steps and their requirements), composes morphisms at each node, and either bottoms out (graph read), suspends (gate), or calls an oracle (LLM).

### PRQL as Morphism Composition Syntax

Every PRQL query corresponds to a morphism composition:

| PRQL Operation | Categorical Operation |
|---|---|
| `from triples \| filter predicate == "node/parent"` | Select morphisms of type `:node/parent` |
| `filter object == X` | Restrict to arrows targeting X |
| `filter subject == X` | Restrict to arrows sourced from X |
| `join ... [==subject]` | Compose morphisms on shared objects |
| `anti_join` | Set difference on morphism images |
| `group ... (aggregate)` | Collect objects of a morphism image, apply reduction |
| `sort` | Order objects in the image |

A path spec `[:node/parent, :node/parent⁻¹, :node/content]` compiles to:
```prql
from triples t1
filter t1.predicate == "node/parent" && t1.subject == {{target}}
join (from triples t2 | filter t2.predicate == "node/parent" && t2.subject != {{target}}) [t1.object == t2.object]
join (from triples t3 | filter t3.predicate == "node/content") [t2.subject == t3.subject]
select {title: t2.subject, content: t3.object}
```

The path spec is a declarative morphism composition. PRQL is the procedural spelling. Both are equivalent — the function author can use whichever is clearer for their use case.

### Why This Matters for Sevens

The categorical framing unifies everything from the brainstorm:

| Brainstorm Concept | Categorical Interpretation |
|---|---|
| **Structural duck typing (§2)** | Objects characterized by their morphism signatures |
| **Currying (§7)** | Partial morphism composition with closures |
| **Gates** | Morphisms to the human category |
| **Suspension/continuation** | Serialized partial compositions |
| **Dependency resolution** | Recursive morphism evaluation |
| **Map-over (§7)** | Functor application lifting node→node to [node]→[node] |
| **Composed functions** | Morphism composition in the transformation category |
| **Context gathering** | Morphism path traversal (declarative or LLM-guided) |
| **Log as type context** | Lineage morphisms characterize object history |
| **Focus session** | Memoized morphism evaluation cache |
| **PRQL queries** | Morphism composition syntax |

The triples table is the hom-set. PRQL is the composition language. The resolver is the evaluator. Functions are morphisms. Gates are inter-category arrows. The whole system is one category.

## Open Questions

- **Subject identity**: Node titles as subjects works for now (they're unique within a root). But titles can change. UUIDs are safer but lose readability. Datascript uses auto-incrementing entity IDs. Triples-design.org recommends stable IDs. What's the right choice for sevens?

- **PRQL recursive queries**: PRQL doesn't natively support recursive CTEs (needed for ancestor/descendant queries, depth calculation). Raw SQL escape hatch? Or compute depth at sync time and store as a triple?

- **PRQL Go integration**: Is there a Go PRQL library, or do we shell out to `prqlc`? The prql-compiler crate has C bindings which could work via CGO, but we've been avoiding CGO (Turso purego was chosen for this reason).

- **Triple cardinality**: Some predicates are unique (`:node/content` — one per node) and some are multi-valued (`:ref/wiki-link` — many per node). Do we enforce this in the schema, in the application, or leave it to convention?

- **Object types**: Everything is TEXT in SQLite. Numbers stored as strings. This works but makes numeric comparisons in PRQL require casting. Worth adding a `type` column to the triples table? Or keep it pure and handle in queries?

- **Transaction boundaries**: When sync inserts thousands of triples, it needs to be atomic. SQLite transactions handle this. But when a function applies ops (creates files, writes lineage triples), the file ops and triple writes need to be coordinated. What if file creation succeeds but triple insertion fails?
