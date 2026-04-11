# Sevens — Roadmap

**Date**: 2026-04-07

## Phase 1: Triples Cutover (NOW)

Replace the relational `nodes`/`cross_refs` tables with reads from the `triples` table. The triples table is already populated on sync. This phase makes it the primary store.

- [x] **1a. Rewrite `BuildWalk` to read from triples**
  - `GetObject(db, title, "node/content")` instead of `SELECT content FROM nodes`
  - `GetObject(db, title, "node/parent")` for parent
  - `GetSubjects(db, "node/parent", title)` for children (inverse of parent)
  - `GetObjects(db, title, "ref/wiki-link")` for cross-refs
  - Siblings via `ComposeInverse(db, title, "node/parent", "node/parent")`

- [x] **1b. Rewrite `BuildOverview` to read from triples**
  - `GetPredicateTriples(db, "node/parent")` gives all parent-child edges
  - `GetPredicateTriples(db, "ref/wiki-link")` gives all cross-refs

- [x] **1c. Rewrite `ResolveContext` to use triple primitives**
  - Replace `BuildWalk` calls with `GetObject`/`GetSubjects`/`Compose`
  - Parent: `GetObject` + `GetObject` for content
  - Siblings: `ComposeInverse` + map `GetObject` for content
  - Children: `GetSubjects` + map `GetObject` for content

- [x] **1d. Rewrite validation to query triples**
  - Orphans: nodes with no parent AND no children
  - Missing parents: nodes whose parent value doesn't match any subject with `node/root`
  - Overflow: group by parent, count > 9
  - Length violations: compare `content/char-count` triple against resolved `max-chars`

- [x] **1e. Drop old tables**
  - Remove `nodes` and `cross_refs` from `InitSchema`
  - Remove `PopulateDB` from sync path
  - Remove `ClearRoot`
  - Clean up `store.go`

- [x] **1f. Verify: all existing commands work against triples**
  - `sync`, `overview`, `walk`, `tree`, `search`, `query`
  - `apply` with all 12 functions (resolver reads from triples)
  - `accept`, `reject`, `pending`, `log`
  - `focus`, `unfocus`, `status`

## Phase 2: Path Specs + Morphism Resolver

Replace the hardcoded `:requires [{:role "siblings"}]` system with composable morphism path specifications.

- [x] **2a. Path spec type in EDN**
  - `{:path [:node/parent :node/parent⁻¹] :exclude-self true :with [:node/content] :as "siblings"}`
  - Parser: convert EDN path specs into Go structs

- [x] **2b. Path evaluator**
  - Walk a path spec by calling `Compose`/`ComposeInverse`/`GetObject` in sequence
  - Each step in the path is a morphism; the evaluator composes them
  - `:with` fetches additional predicates from terminal objects

- [x] **2c. Update function definitions**
  - Replace `:requires` with `:context` path specs on all 9 functions
  - Same information, different representation — composable instead of hardcoded roles

- [x] **2d. Backward compat**
  - Functions without `:context` fall back to old `:requires` behavior
  - Functions without either fall back to target-only (legacy path)

## Phase 3: Lineage + Pending as Triples

Move the EDN log files and pending state into the triples store.

- [x] **3a. Lineage events as triple subjects**
  - `lineage:<uuid>` as subject with predicates: `/target`, `/fn`, `/step`, `/status`, `/timestamp`, `/commit`, `/raw-output`, `/summary`
  - Replace `AppendLog` with `InsertTriples`
  - Replace `ReadLog` with `GetSubjects(db, "lineage/target", nodeTitle)` + fetch predicates

- [x] **3b. Pending as triple queries**
  - A pending suggestion is a lineage subject where `/status` = "suggested" and no later "accepted"/"rejected"/"completed" exists for the same target
  - Replace `FindPending`/`ListPending` with triple queries

- [x] **3c. Drop EDN log files**
  - Remove `log.go` append/read functions
  - Remove `~/.config/sevens/log/` directory

## Phase 4: Dependency Graph Execution

Replace the linear pipeline runner with dependency graph evaluation (BRAINSTORM §7).

- [x] **4a. Step dependency declarations**
  - Steps declare `:ref` dependencies on other steps
  - Topological sort at function load time
  - Satisfiability check: every leaf is a graph read or a gate

- [x] **4b. Composed/nested functions**
  - `:fn "decompose"` delegates to another function's full pipeline
  - Inner function's gates suspend the outer pipeline

- [x] **4c. Map-over**
  - `{:map-over {:path [:node/parent⁻¹]} :fn "elaborate"}` — apply function to each child
  - Each child gets its own sub-pipeline with independent gates

- [x] **4d. mo.Either integration**
  - `mo.Either[Suspension, T]` as universal return type
  - Thunks for lazy evaluation
  - Pipeline runner as recursive evaluator

## Phase 5: Serialized Continuations

The log becomes the suspended computation, not just a journal.

- [x] **5a. Suspension as triples**
  - `pending:<id>` subject with `/resolved`, `/remaining`, `/gate-type`, `/expects`
  - Resolved values stored by content hash reference

- [x] **5b. Resume from serialized state**
  - `sevens accept` deserializes the continuation and resumes evaluation
  - Snapshot vs. live semantics per-role

- [x] **5c. Partial walk application**
  - `sevens accept --steps generate` advances subset of remaining walk
  - `sevens reject` discards the frozen walk, preserves completed steps

- [x] **5d. Cross-walk dependency resolution**
  - A new function call can reference resolved values from a different frozen walk on the same node

## Phase 6: Remaining Features

In priority order, independent of above phases:

- [x] System prompt parameterized by output type
- [x] `--model` flag wired to named profiles
- [x] Fuzzy edit matching with confirmation fallback
- [x] `sevens revert <node>` (git checkout via log commit hash)
- [ ] Ghost/ephemeral nodes (suggestion → temporary node → promote or expire)
- [ ] Prompt caching (Anthropic API cache breakpoints in focus sessions)
- [ ] Multi-node function inputs (`sevens apply compare "A" "B"`)
- [ ] LLM-driven context gathering (cheap model writes morphism paths)
