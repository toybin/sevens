# Sevens — Feature Brainstorm

**Date**: 2026-04-07

---

## 1. FP Core in Go (samber/lo, samber/mo)

Keep Go as the imperative shell but pull FP idioms into the core logic via `samber/lo` (lodash-style collection ops) and `samber/mo` (monadic types — Option, Result, Either, IO).

Goal: make the graph engine, function composition, and pipeline plumbing more declarative without a full language switch. Haskell or similar may come later for a v2; this buys the conceptual vocabulary now.

**Agent skills installed**: `~/.claude/skills/cc-skills-golang/skills/golang-samber-{lo,mo,ro}`

### Research findings

**samber/lo** — 200+ generic collection functions. Maps directly to tree operations:
- `lo.Map(node.Children, transform)` — map over children
- `lo.Filter`, `lo.Reduce`, `lo.GroupBy` — standard FP toolkit with index access
- `lo.FilterMap` — filter + transform in one pass (useful for "process children, skip failures")
- `lop` subpackage — parallel variants for concurrent child processing
- `loi` — lazy iterators via `iter.Seq[T]` (Go 1.22+)

**samber/mo** — Monadic types: Option, Result, Either, Either3-5, IO, IOEither, Task, Future, State.
- `mo.Result[T]` — natural fit for pipeline step outcomes (Ok/Err)
- `mo.Either[L, R]` — fits gate semantics (Left = pause/reason, Right = proceed)
- `mo.Either3` and above — multi-state pipelines (success / paused / skipped / error)
- `mo.IO[T]` — deferred execution, composable via FlatMap. Pipeline steps as IO actions that don't fire until `.Run()`.
- Pipe1-10 functions for composing without nesting

**Currying**: Neither library provides it natively, but we don't need language-level currying — see §7 (Recursive Dependency Resolution) for the execution model that supersedes this.

**Rough edges**: No HKTs (can't abstract over "any monad"), parameterized methods on generic types don't work (use subpackage standalone functions instead), type inference sometimes needs explicit params.

---

## 2. Structural Duck-Typed Function Signatures

Functions declare their inputs as a list of **structural roles** relative to a target node:

- **target** — the focused node
- **parent** — target's parent
- **child[N]** — target's Nth child (arbitrary indexing)
- **sibling[N]** — target's Nth sibling
- **prev** — result of the last operation on a given node

### Core design principle: structural typing, not nominal

A node's "type" in a function signature is its **structural position** relative to the target, not a label. Semantic types (like "discussion node") emerge as **interfaces** — duck typing. A node is a discussion node if it implements the discussion protocol (right frontmatter fields, conversation structure), not because someone tagged it.

This means:
- The base type is always **parent/child/sibling** — structural position in the tree
- Semantic types **wrap or depend on** the base structural type
- A "discussion" type is just a child that quacks like a chat context
- Types are **ad-hoc and contextual** — determined by the operation and target node, not fixed globally
- Go interfaces are the natural encoding: a function declares what behaviors it needs from its inputs, and any node satisfying those behaviors can fill the role

### What this looks like

A function like `compare` is typed `(target, sibling[0]) → text`. `merge` is `(target, child[0..N]) → ops`. `decompose` is `(target) → suggestions → (target, prev) → ops`.

Currying lets you partially apply — bind the target, supply children lazily as the pipeline advances. Gates produce partially-applied functions: the pipeline pauses with some args bound, resumes when the human provides the remaining input (approval, selection, revision).

### Open questions
- How to express variable arity (e.g., "all children" vs. "child[0..2]")?
- Encoding in EDN function definitions vs. a richer DSL?
- See §7 for how currying, gates, and partial application are unified under recursive dependency resolution.

---

## 3. Chat/Discuss Node Type

A `discuss` function creates a typed child node that acts as an interactive chat context for its parent (the target node). This is the canonical example of duck-typed semantic nodes — not a special class, just a child that implements the chat interface.

The chat node would:
- Be identifiable by structural behavior: has conversation-shaped content, frontmatter signals (e.g., `type: discussion` as a hint, not a hard constraint)
- Support back-and-forth: user writes in the file, runs discuss again, LLM responds in-place
- Stay scoped to its parent — the parent's content is always the grounding context
- Accumulate conversation history within the file itself

Any function that expects "a child with conversation history" can operate on it — the discuss function creates it, but other functions can read it if they declare the right input interface.

---

## 4. New Functions

### merge
Combine multiple nodes (siblings? arbitrary set?) into a single node. Inverse of decompose. Typed: `(target, child[0..N]) → ops` or `(sibling[0], sibling[1], ...) → ops`. Needs the variable-arity input typing from §2.

### discuss
See §3. Creates or continues a chat-style child node. Typed: `(target) → ops` (creates child) or `(target, child["discussion"]) → ops` (continues conversation, where the child is located by interface match, not index).

### "what do you notice?"
Open-ended analysis. LLM examines a node and its neighborhood, surfaces patterns, gaps, contradictions, implicit assumptions. Typed: `(target, parent?, children?, siblings?) → text`. Optional inputs — takes whatever context is available.

### synthesize (inspired by nodepad)
Cross-node pattern detection. Takes a weighted sample of nodes (recent + random, like nodepad's ghost note generation) and finds non-obvious connections. Typed: `(target, child[sample]) → suggestions`. Output is ephemeral by default — suggestions that the user claims or discards.

### relate
Discover or strengthen cross-references. Examines a node's content against the broader graph and proposes wiki links. Typed: `(target, graph-context) → ops`.

### Nodepad-inspired patterns worth considering
- **Ghost notes / ephemeral nodes**: System-generated hypothesis nodes that auto-expire if not claimed. Could be a gate type: suggestion materializes as a temporary node, gets promoted or discarded.
- **View functions**: Same tree, different lens. "Show me this subtree organized by X" without mutating structure. Output type: `text` (a rendered view, not ops).
- **Enrichment pipelines**: Multi-step metadata accumulation on a node (classify → annotate → score confidence). Each step adds to the node's frontmatter rather than replacing content.
- **Confidence/provenance as metadata**: Nodes that were LLM-generated could carry confidence scores and lineage info in frontmatter.

---

## 5. Pre-Call Gates (Confirm, Cost, Readiness)

Add a gate *before* each LLM call (distinct from the existing post-output `:approve` gate). The prompt is fully compiled, then passes through one or more pre-call checks before firing:

### Cost confirmation
- Hit the token-counting / cost-estimation endpoint with the rendered prompt
- Show estimated cost, wait for user confirm
- Especially valuable for large context windows or expensive models

### Readiness check
- A cheap/fast model (e.g., Haiku) reviews the compiled prompt + target content
- Gives advisory feedback: "this node is too sparse to get good results — flesh it out first", "the prompt is asking for X but the context doesn't contain Y", etc.
- User can proceed anyway or revise first

### General pre-call gate framework
Cost and readiness are instances of a general pattern: **pre-call gates** that inspect the compiled prompt and advise/block before the main LLM call. Others could include: context-length warnings, duplicate-work detection (has this exact call been made before?), etc.

### Connection to the type system
Pre-call gates are themselves typed steps in the pipeline. A cost gate is typed `(compiled-prompt) → Either[block-reason, proceed]`. A readiness gate is `(compiled-prompt, target) → Either[advisory, proceed]`. They compose with the same machinery as any other pipeline step — `mo.Either` is the natural encoding.

---

## 6. Dynamic Model Selection & API Config

Move away from a single configured model. Instead:
- Functions (or steps) can declare a preferred model tier (fast/cheap vs. capable/expensive)
- User can override at call time (`--model`)
- Pre-call gates (§5) could recommend a model based on task complexity
- Config supports named model profiles, not just one `{:model "..."}` entry
- Readiness gate could downgrade: "this is a simple task, Haiku would suffice"

### API key in config

Currently config uses `:api-key-env` (indirection through env var). Add support for `:api-key` directly in EDN so that development/testing can just work without environment setup:

```clojure
{:llm {:provider "anthropic"
       :model "claude-sonnet-4-20250514"
       ;; direct key — for dev/testing
       :api-key "sk-ant-..."
       ;; OR env var indirection — for production/shared configs
       :api-key-env "ANTHROPIC_API_KEY"}

 :models
 {:fast {:provider "anthropic" :model "claude-haiku-4-5-20251001"}
  :capable {:provider "anthropic" :model "claude-sonnet-4-20250514"}
  :powerful {:provider "anthropic" :model "claude-opus-4-6"}}

 :context-gathering {:default-model :fast}
 :readiness-gate {:default-model :fast}}

---

## 7. Execution Model: Recursive Dependency Resolution

**This is the core execution model.** It unifies currying, gates, pipelines, and composition into one mechanism.

### The idea

Function application is **lazy, demand-driven evaluation of a dependency graph**. The EDN definition declares what a function needs, not the order it runs in. The runtime recursively resolves dependencies until something bottoms out (a raw node read, a cached result, a literal) or suspends at a gate.

"Currying" isn't a language feature — it's **partial evaluation of the dependency graph**. When a gate fires, the runtime has resolved some inputs but not all. It captures the closure (resolved inputs + remaining dependency tree) and suspends. When the human resumes (accept, revise, select), the runtime picks up where it left off with the new input bound.

### How it works

1. User calls `sevens apply decompose "My Node"`
2. Runtime loads the function definition, sees it needs `target`
3. `target` resolves immediately (read node from DB) — **bottoms out**
4. Step `suggest` runs: all inputs resolved → call LLM → produce suggestions
5. `:approve` gate fires → **suspend**. The closure captures `{target: resolved, suggest.output: resolved}` and the remaining dependency tree (`generate` still needs to run)
6. User calls `sevens accept "My Node"`
7. Runtime resumes the suspended computation. `generate` needs `target` (already resolved) and `suggest.output` (already resolved) → call LLM → produce ops
8. `:cost-confirm` pre-gate could fire before the LLM call → **suspend again** if cost too high
9. User confirms → ops execute → **bottoms out** with concrete file mutations

### Dependent typing

The output type of a step can depend on the *value* of its inputs. `decompose.suggest` on a node with 3 children might produce 4 suggestions (fill to 7); on a node with 0 children it might produce 7. The `generate` step receives whatever `suggest` actually produced — its input type is dependent on the runtime value, not a fixed schema.

This is dependently typed in the practical sense: the shape of the computation is determined by the data flowing through it, not just the declared types.

### EDN encoding

```clojure
{:name "decompose"
 :steps
 [{:name "suggest"
   :requires [{:role "target" :type "node"}]
   :output "suggestions"
   :post-gate :approve}
  {:name "generate"
   :requires [{:role "target" :type "node"}
              {:ref "suggest" :as "prev"}]
   :output "ops"
   :pre-gate :cost-confirm}]}
```

`:requires` declares the dependency graph. `:ref "suggest"` means "the output of the step named suggest" — a dependency on a prior computation, not a node role. The runtime topologically sorts and resolves.

### Composed / nested functions

A function can depend on the output of *another function*, not just its own steps:

```clojure
{:name "decompose-and-elaborate"
 :steps
 [{:name "split"
   :requires [{:role "target" :type "node"}]
   :fn "decompose"            ;; delegates to the full decompose function
   :output "ops"}
  {:name "enrich"
   :requires [{:ref "split" :as "prev"}
              {:role "target.children" :type "node[]"}]
   :fn "elaborate"            ;; maps elaborate over the new children
   :map-over "target.children"
   :output "ops"}]}
```

`split` bottoms out when `decompose` completes (including its own internal gates — the user approves the decomposition before `enrich` even starts resolving). `enrich` maps `elaborate` over the children created by `split`. Each `elaborate` call is its own dependency subtree that resolves independently.

### Go encoding

```go
// Thunk — a lazy value that either resolves or suspends
type Thunk[T any] func() mo.Either[Suspension, T]

// Suspension — a paused computation with captured environment
type Suspension struct {
    Gate      Gate                    // what kind of pause (approve, cost, readiness)
    Resolved  map[string]any          // inputs already computed
    Remaining []Dependency            // what still needs resolving
    Resume    func(input any) Thunk[any]  // continuation
}

// The resolver: recursively evaluate until bottom or suspension
func Resolve[T any](thunk Thunk[T]) mo.Either[Suspension, T] {
    return thunk()
}
```

`mo.Either[Suspension, T]` is the universal return type. Every step in every pipeline either produces a value (`Right`) or suspends (`Left`). The runtime is just a loop: resolve, check if suspended, if so serialize the suspension to the log and exit. On resume, deserialize, call `Resume` with the human's input, and keep resolving.

### What this subsumes

- **Currying**: partial evaluation with captured closure
- **Gates (pre and post)**: suspension points in the dependency graph
- **Pipelines**: linear dependency chains (step N depends on step N-1)
- **Composition**: nested dependency graphs (function A depends on function B's output)
- **Map/filter/fold**: higher-order steps that resolve a function over a collection of nodes
- **Revision loops**: re-entering a suspended computation with new input, same resolved context
- **Arity promotion**: user provides arity-1, LLM gathers arity-N via context exploration
- **Semantic pattern matching**: LLM-as-resolver replaces static selector DSL for emergent type matching

### Suspension serialization

The log becomes the **serialized continuation**, not just a journal. When the runtime suspends, it writes an entry that captures enough to resume:

```clojure
{:event "suspended"
 :function "decompose"
 :step "suggest"
 :gate {:type :approve :expects ["approve" "reject" "revise"]}

 :resolved
 {"target" {:title "My Node"
            :content-hash "abc123"
            :git-commit "a1b2c3d"
            :resolved-at "2026-04-07T14:30:00Z"}
  "suggest.output" {:raw "[ ... ]"
                    :parsed [{:title "Child 1" ...}]}}

 :remaining
 [{:step "generate"
   :requires ["target" "suggest.output"]
   :pre-gate :cost-confirm
   :output "ops"}]

 :resume {:on-approve {:advances-to "generate"}
          :on-revise {:re-runs "suggest" :appends-to "revision-history"}
          :on-reject {:discards-walk true}}}
```

**Resolved values are stored by reference** — content hashes and git commits, not inline content. On resume, the runtime can detect staleness (file changed since suspension) and decide per-role whether to re-read or use the snapshot.

**Snapshot vs. live semantics** are per-role:
- Step outputs (`suggest.output`) are always snapshots — they're the thing the user approved
- Node reads (`target`) can be either — the function definition could declare `:refresh-on-resume true` for roles where the user is likely to have edited the content between suspension and resume

### Partial walk application

The user can apply a **subset** of the remaining walk. The frozen suspension is the full remaining dependency tree from the original call. The user's options:

- **Resume all**: `sevens accept` — advance the full walk, hitting the next gate or bottoming out
- **Resume partial**: `sevens accept --steps generate` — advance only the named steps, freeze the rest. The remaining walk shrinks.
- **Discard**: `sevens reject` — throw away the frozen walk entirely. The node keeps whatever content the completed steps produced.

After any of these, the node's content has a **typed provenance** from the log:

```clojure
;; A node whose decompose walk was partially applied
{:title "My Node"
 :lineage [{:fn "decompose" :step "suggest" :status :accepted :commit "a1b2c3d"}
           {:fn "decompose" :step "generate" :status :discarded}]}
```

### The log as type context

A node's effective type is its structural position **plus its operational lineage**. The log records which functions produced the node's current content, which steps completed, and which were discarded. This lineage is itself a type — duck typing extends to "what has been done to this node."

A function can declare it needs a node with a particular lineage:

```clojure
{:name "elaborate"
 :requires [{:role "target"
             :type "node"
             :expects-lineage {:fn "decompose" :step "generate" :status :accepted}}]}
```

This says: "I operate on nodes that were produced by a completed decompose." The runtime checks the log. If the target was decomposed but `generate` was discarded, `elaborate` knows it's working with machine-suggested-but-not-generated content and can adjust.

More commonly, the lineage check is soft — advisory, not blocking. The function's prompt template can reference `{{lineage}}` to give the LLM context about what produced the content it's looking at. The hard version (`:expects-lineage` as a precondition) is for pipelines where step ordering matters.

### Re-entry and continuation

A new function call on a node with a frozen walk doesn't destroy the walk — it's a separate evaluation. The node can have multiple concurrent frozen walks (e.g., a paused `decompose` and a separate `discuss` in progress). Each is an independent suspended computation in the log.

But a new call *can* reference the frozen walk. If you call `elaborate` on a node that has a frozen `decompose` walk, `elaborate` might declare a dependency on `decompose.suggest.output` — "I want to see what decomposition was proposed." The runtime finds it in the log's resolved values and binds it, even though `decompose` is still suspended. Cross-walk dependency resolution.

### LLM-driven context gathering (arity promotion)

The user specifies **intent** (arity-1: "elaborate this node"). The LLM resolves **dependencies** (arity-N: "I need the parent, two siblings, and the discussion child to do this well"). The function definition is a contract declaring what the LLM is *allowed* to gather, not what the user must specify.

**Three kinds of leaves in the dependency tree:**
- **DB reads** — structural, immediate (node content, children list)
- **Human input** — gates, explicit suspension
- **LLM judgment** — the LLM decides what it needs, runtime fetches it

The execution flow:

1. User: `sevens apply elaborate "Container Strategy"`
2. Runtime: resolves `target`, sees `:context-gathering` is enabled
3. Runtime gives the LLM (cheap/fast model) the target node + its neighborhood summary (parent title, children titles, sibling titles, cross-refs) + graph navigation tools
4. LLM navigates for a few turns: "walk the parent," "walk sibling Terraform Cleanup," "walk child Docker Migration — it overlaps with this node"
5. Runtime freezes the gathered context. Total node set is now known.
6. Pre-call gates fire (cost check against the full context, readiness check)
7. Main LLM call with full context

**EDN encoding:**

```clojure
{:name "elaborate"
 :requires [{:role "target" :type "node"}]
 :context-gathering
 {:enabled true
  :model :fast                        ;; cheap model for gathering phase
  :tools ["walk" "overview"]          ;; what graph operations the LLM can use
  :max-turns 5                        ;; bound the exploration
  :max-nodes 10                       ;; bound the context window
  :prompt "gather-context.md"}}       ;; instruction template for the gathering phase
```

The `:tools` field is the key constraint — the LLM can read the graph but not mutate it. It's a read-only exploration phase. The gathering prompt tells the LLM what the main function will do and asks it to collect what it needs.

**This subsumes the selector/pattern-matching approach.** Instead of the user writing pattern definitions in EDN (`{:match "sub-design" :signals [...]}`), the LLM *is* the pattern matcher. It looks at the children, recognizes which ones are relevant sub-designs, and requests them. The emergent typing happens inside the LLM's judgment, not in a DSL.

**Symmetric tooling.** The same graph navigation tools (`walk`, `overview`) are available to the LLM during context gathering and to the user via the CLI. The LLM auto-gathers by default; the user can make the same calls manually with flags (`--include "Terraform Cleanup" --include-children`). Same interface, different callers, different defaults. A power user can skip the gathering phase entirely and hand-specify context; a casual user lets the LLM figure it out.

**The user can still constrain it** — `:max-nodes 10` and `:max-turns 5` bound cost. `:tools ["walk"]` (without "overview") limits scope to the local neighborhood. A function that should only look at direct children would use `:tools ["walk"]` with `:max-nodes` equal to the child count.

**Context gathering as a serialized step:**

The gathered context becomes a resolved value in the log, just like any other:

```clojure
{:resolved
 {"target" {:title "Container Strategy" :content-hash "abc123"}
  "gathered-context"
  {:nodes ["EHE Modernization" "Terraform Cleanup" "Docker Migration"]
   :rationale "Parent for framing, sibling for contrast, child for overlap detection"
   :model "haiku"
   :turns 3
   :gathered-at "2026-04-07T15:00:00Z"}}}
```

The rationale is logged — you can see *why* the LLM pulled in each node. This is provenance for the context, not just the output. On resume or re-run, the user can override: "no, don't include Terraform Cleanup, it's not relevant."

### Satisfiability check

Since all functions are defined in EDN, the runtime pre-composes the entire dependency chain at definition load time:

1. Expand all `:fn` references (nested function calls) into their full step graphs
2. Topologically sort the combined graph
3. Verify every leaf is either a node read (bottoms out) or a gate (explicit suspension)
4. If any path doesn't terminate → reject the definition as unsatisfiable
5. Gates are expected leaves, not errors — they're the points where the computation *should* yield

This is a static check. Runtime doesn't need cycle detection because the definition was validated at load time.

---

## 8. Focus & Prompt Cache

Every LLM call currently assembles the prompt from scratch — stateless, no memory of prior calls on the same node or neighborhood. This wastes tokens (re-sending the same parent/sibling context) and wastes coherence (the LLM loses any reasoning it built up about the local structure).

### The focus command

`sevens focus "Container Strategy"` pins a node and its resolved context into a **session**. The session is a cached prompt prefix that subsequent calls reuse:

```
sevens focus "Container Strategy"     # pins target + gathers context
sevens apply elaborate .              # reuses cached prefix, only sends the elaborate instruction
sevens apply decompose child[2] .     # same cached neighborhood, different function
sevens apply discuss .                # still in focus — LLM already "knows" the node
sevens unfocus                        # release the session
```

The `.` is shorthand for "the focused node." Functions within a focus session share the same context prefix — the LLM is making **incremental modifications** to a context it already understands, not rebuilding from zero.

### What the cache contains

The prompt cache is a **layered prefix**:

1. **System prompt** — global rules, output format (rarely changes)
2. **Graph context** — the focused node's content, parent, children, siblings, cross-refs (changes when the user edits files or the graph mutates)
3. **Operational context** — lineage, pending walks, discussion history (grows over the session)
4. **Gathered context** — nodes the LLM (or user) pulled in during context gathering (session-specific)

Layers 1-2 are the stable prefix — they can use the Anthropic API's prompt caching (cache the system + graph context, only send function-specific instructions as new tokens). Layer 3 grows but is append-only within a session. Layer 4 is set once during focus and reused.

### Cache invalidation

The cache is **content-addressed**. Each node in the cached context has a content hash. On each call within the session:

- Runtime re-hashes the focused node and its neighbors
- If any hash changed (user edited a file, a prior function mutated content), the affected cache layer is rebuilt
- Unchanged layers keep their cache position — the API's prompt caching handles the prefix matching

This means the user can edit files mid-session and the cache stays coherent. If they edit the focused node, the graph context layer refreshes but the system prompt layer stays cached. If nothing changed, the full prefix is reused.

### Connection to context gathering

Focus and context gathering (§7) compose naturally:

- `sevens focus "Container Strategy"` triggers context gathering — the LLM (or user) selects which neighbors to include
- The gathered set becomes the session's cached context
- Subsequent `apply` calls within the focus reuse that gathered set
- The user can adjust mid-session: `sevens focus --include "Terraform Cleanup"` adds a node to the cached context, `sevens focus --exclude "Docker Migration"` removes one

### Connection to the execution model

A focus session is itself a resolved value in the dependency tree. When a function's `:requires` asks for `target` and context, the resolver checks if there's an active focus session. If so, it binds from the cache instead of re-reading and re-gathering. The focus session is a **memoization layer** in the resolver.

### Anthropic prompt caching

The Anthropic API supports explicit cache breakpoints in messages. The focus session maps directly:

```
[system message — cached]
[graph context: focused node + neighbors — cached]
[operational context: lineage, session history — cached, grows]
[function instruction: "elaborate this node" — new, varies per call]
```

Each call within a focus session hits the cache for the stable prefix. The cost savings compound — the more calls you make within a focus, the more you amortize the context.

### EDN config

```clojure
{:focus
 {:auto-gather true                  ;; run context gathering on focus
  :cache-ttl 3600                    ;; seconds before stale session expires
  :max-session-nodes 20              ;; bound cached context size
  :invalidation :content-hash}}      ;; or :manual for no auto-refresh
```

---

## Open Threads

- **Ephemeral nodes**: Ghost notes / suggestion nodes that auto-expire. Do they live in the graph (with a TTL in frontmatter)? Or in the log (as pending suggestions that can be materialized)?
- **Gathering override UX**: The user should be able to review and edit the gathered context before the main call fires. This is naturally a gate — the gathering phase produces a context set, the user approves/prunes it, then the main call runs. But how does this interact with cost gates? Is it gather → approve-context → cost-check → approve-cost → call? That's a lot of gates for a single function application. Focus sessions may simplify this — gather once on focus, reuse for all calls.
- **Concurrent frozen walks**: A node can have multiple suspended computations. How do they interact? Can they conflict (two walks both want to edit the same file)? Probably need a lock or conflict detection at resume time.
- **Lineage garbage collection**: As nodes evolve, their lineage grows. When is it safe to prune old lineage entries? Probably after the user explicitly "seals" a node's current state.
- **Focus and frozen walks**: If you focus a node that has a frozen walk, does the session include the walk's resolved values in the cached context? Probably yes — the LLM should see the pending suggestions when working on the node.
- **Multi-node focus**: Could you focus a subtree instead of a single node? `sevens focus "EHE Modernization" --depth 2` pins the node and two levels of descendants. The cache prefix is larger but the session covers a whole working area.
