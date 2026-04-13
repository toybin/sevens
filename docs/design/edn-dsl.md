Status: Runtime evaluation (match predicates, value models) is implemented. The keyword-typed EDN syntax (`:create` vs `"create"`) is designed but not yet implemented -- the current format uses string-typed EDN.

# EDN as Embedded Functional DSL

## Context

The function and type definitions in sevens have been drifting toward being
a program: match expressions, type constructors, predicates, control flow,
state machines. Rather than fighting this, we embrace it: EDN IS the
embedded language. Go IS the interpreter.

Everything must be valid EDN parseable by `olympos.io/encoding/edn`. No
invented syntax. The expressiveness comes from conventions on how to
interpret standard EDN structures.

## EDN Primitives and Their DSL Roles

| EDN form | DSL role | Example |
|----------|----------|---------|
| Keywords | Built-in form names, primitive types | `:match`, `:create`, `:edit`, `:text`, `:suggestion`, `:gate`, `:approve` |
| Symbols | References to user-defined names | `task`, `project`, `person`, `neighborhood` |
| Strings | Literal values, templates | `"Discussion - {{title}}"`, `"todo"` |
| Vectors | Ordered sequences, applied types, match clauses | `[:create task]`, `[:suggest :generate]` |
| Maps | Named bindings, struct-like definitions | `{:name :notice :output :text}` |
| Lists | Reserved for future evaluation forms | `(>> step1 step2)` if needed |
| Sets | Unordered collections | `#{:status :deadline}` for required predicates |
| Tagged elements | Domain constructors | `#sevens/query`, `#sevens/predicate` if needed |

## Keywords vs Symbols

Keywords (`:create`, `:text`) are sevens-defined, built-in. They are the
language's reserved words. The Go interpreter knows what to do with every
keyword.

Symbols (`task`, `project`, `person`) are user-defined. They reference
things in the config — type definitions, value models, other functions.
The Go interpreter resolves them by looking them up.

This distinction matters: a keyword is always valid (it's part of the
language). A symbol may fail to resolve (the user hasn't defined it yet).

## Function Definitions

### Current (string-typed, flat)

```edn
{:name "notice"
 :description "Surface patterns..."
 :agent {:persona "analyst"
         :system-prompt "..."
         :context-policy "neighborhood"}
 :context [{:path ["node/parent"] :with ["node/content"] :as "parent"}]
 :input "node"
 :output "text"}
```

### Proposed (keyword/symbol-typed, structured)

```edn
{:name :notice
 :description "Surface patterns..."
 :persona :analyst
 :system-prompt "..."
 :context {:policy :neighborhood}
 :input :node
 :output :text}
```

Changes:
- `:output :text` — keyword, not string. References the primitive type.
- `:persona :analyst` — top-level binding, not nested in `:agent`.
- `:context {:policy :neighborhood}` — structured, not a string.
- `:input :node` — keyword for the base input type.

### Applied types (subtyped output)

When a function produces a specific subtype:

```edn
{:name :decompose
 :pipeline [:suggest :generate]
 :steps {:suggest {:input :node
                   :output :suggestion
                   :gate :approve}
         :generate {:input :suggestion
                    :output [:create task]}}}
```

`[:create task]` is an applied type: the primitive `:create` parameterized
by the user-defined symbol `task`. The Go interpreter reads this as
"output shape is create, output type is task."

A non-parameterized output is just the keyword: `:text`, `:edit`.

### Conditional output (match)

When the output type depends on graph state:

```edn
{:name :discuss
 :persona :facilitator
 :system-prompt "Ask probing questions..."
 :context {:policy :neighborhood}
 :input :node
 :output [:match
          [:child-exists "Discussion - {{title}}"  :edit]
          [:otherwise                               :create]]}
```

`[:match ...]` is a form. Each clause is a vector: `[predicate args... result-type]`.
The Go interpreter evaluates predicates against the graph and returns the
first matching result type.

### Pipelines (multi-step composition)

```edn
{:name :decompose
 :pipeline [:suggest :generate]
 :steps {:suggest {:input :node
                   :output :suggestion
                   :gate :approve}
         :generate {:input :suggestion
                    :output [:create task]}}}
```

`:pipeline` declares the composition order. `:steps` is a map of step
definitions keyed by name. This replaces the current ordered vector of
step maps, making the composition explicit and the steps addressable.

### Deterministic functions (templates)

```edn
{:name :daily-note
 :backend [:deterministic :create-node]
 :title "{{date}}"
 :parent inbox
 :parent-template inbox-root
 :params [{:name :summary}]}
```

`[:deterministic :create-node]` is an applied backend form. `:title`,
`:parent` are direct bindings instead of nested in a JSON config string.
`inbox` and `inbox-root` are symbols referencing other nodes/functions.

## Type Definitions

### Current

```edn
{:name "task"
 :extends "create"
 :predicates {:required ["status" "deadline"]
              :optional ["priority" "assignee"]}
 :structure {:parent-type "project"}
 :projection {:frontmatter ["status" "deadline" "priority" "assignee"]
              :orthography {"status" {:value-model "task-status"}
                            "assignee" {:signifier "@" :value-model "person"}}}}
```

### Proposed

```edn
{:name task
 :extends :create
 :predicates {:required #{:status :deadline}
              :optional #{:priority :assignee :estimate}}
 :structure {:parent-type project}
 :projection {:frontmatter #{:status :deadline :priority :assignee}
              :orthography {:status   {:value-model task-status}
                            :assignee {:signifier "@" :value-model person}
                            :priority {:signifier "!" :value-model priority}
                            :estimate {:signifier "~" :value-model duration}}}}
```

Changes:
- `task`, `project`, `person` are symbols — user-defined references.
- `:create`, `:status`, `:deadline` are keywords — built-in / structural.
- Sets `#{}` for unordered collections (required predicates, frontmatter fields).
- No strings where references are intended.

### Primitive types

```edn
{:name :text
 :primitive true}

{:name :create
 :primitive true}

{:name :edit
 :primitive true}

{:name :suggestion
 :primitive true}
```

Schema instructions are NOT stored in the EDN. They are derived by the Go
renderer from the type's structure. The primitive types have no predicates
or projection — their schema is hardcoded in Go because it IS the Go
interpreter's output format.

## Predicates (Match Conditions)

Predicates for `:match` forms are vectors where the first element is a
keyword naming the predicate:

```edn
[:child-exists "Discussion - {{title}}"]   ;; node has child with this title
[:has-content "{{title}}"]                 ;; node has non-empty content
[:has-blocks "{{title}}" :heading]         ;; node has blocks of this kind
[:children-count 0]                        ;; node has exactly N children
[:conforms task]                           ;; node conforms to this type
[:otherwise]                               ;; always true (fallback)
```

Each predicate keyword maps to a Go function that evaluates it against the
graph. New predicates = new Go code, but composition happens in EDN.

## Value Models

Value models define how predicate values are validated and normalized:

```edn
;; Enum
{:name priority
 :kind :enum
 :members [:low :medium :high :urgent]
 :aliases {"!" :urgent "!!" :high}}

;; State machine
{:name task-status
 :kind :state-machine
 :states [:todo :in-progress :done :blocked]
 :transitions [[:todo :in-progress]
               [:todo :blocked]
               [:in-progress :done]
               [:in-progress :blocked]
               [:blocked :in-progress]]
 :aliases {"x" :done " " :todo "::blocked" :blocked}}

;; Date
{:name deadline-date
 :kind :date
 :format "2006-01-02"}

;; Reference
{:name person
 :kind :reference
 :resolves-to :node
 :signifier "@"}
```

## Context Specifications

Context gathering is expressed as either a named policy or explicit queries:

```edn
;; Named policy (sugar)
:context {:policy :neighborhood}

;; Explicit queries
:context {:queries [{:path [:node/parent]
                     :with [:node/content]
                     :as :parent}
                    {:path [:node/parent :node/parent~]
                     :exclude-self true
                     :with [:node/content]
                     :as :siblings}]}

;; Type-scoped query
:context {:type task
          :scope :subtree}
```

## Summary of Conventions

1. **Keywords** = language built-ins (Go knows about these)
2. **Symbols** = user-defined references (resolved from config)
3. **Vectors** = applied forms, ordered sequences, match clauses
4. **Maps** = named bindings
5. **Sets** = unordered collections (predicate sets, field sets)
6. **Strings** = literal values, templates with `{{var}}` interpolation

## EDN as Projection

The critical architectural insight: EDN config files are a PROJECTION of
the triple store, exactly the same way markdown files are a projection of
the triple store.

```
Markdown files  ←→  triple store  ←→  EDN config files
(nodes, blocks)     (source of        (types, functions,
                     truth)            value models)
```

The flow for EDN is the same as for markdown:

1. User writes/edits `task.edn`
2. Sync parses the EDN, expands into triples, stores in the DB
3. The triple store is the source of truth at runtime
4. Type checking, function loading, schema resolution — all graph queries
5. Rendering back to EDN is the reverse projection

### Type definitions as triples

```
type:task          type/name          "task"
type:task          type/extends       type:create
type:task          type/requires      "status"
type:task          type/requires      "deadline"
type:task          type/optional      "priority"
type:task          type/optional      "assignee"
type:task          type/parent-type   type:project
type:task          type/frontmatter   "status"
type:task          type/frontmatter   "deadline"
type:task          type/frontmatter   "priority"
type:task          type/frontmatter   "assignee"
type:task          type/orth-key      "status"
type:task          type/orth-signifier-assignee  "@"
type:task          type/orth-model-assignee      type:person
```

### Function definitions as triples

```
fn:notice          fn/name            "notice"
fn:notice          fn/output          type:text
fn:notice          fn/persona         "analyst"
fn:notice          fn/context-policy  "neighborhood"
fn:notice          fn/prompt          "...template..."
fn:notice          fn/system-prompt   "..."

fn:decompose       fn/name            "decompose"
fn:decompose       fn/pipeline        "suggest,generate"
fn:decompose       fn/step-suggest    step:decompose:suggest
fn:decompose       fn/step-generate   step:decompose:generate

step:decompose:suggest   step/input    type:node
step:decompose:suggest   step/output   type:suggestion
step:decompose:suggest   step/gate     "approve"
step:decompose:suggest   step/prompt   "...template..."

step:decompose:generate  step/input    type:suggestion
step:decompose:generate  step/output   type:create
step:decompose:generate  step/prompt   "...template..."
```

### Value models as triples

```
vm:task-status     vm/kind            "state-machine"
vm:task-status     vm/state           "todo"
vm:task-status     vm/state           "in-progress"
vm:task-status     vm/state           "done"
vm:task-status     vm/state           "blocked"
vm:task-status     vm/transition      "todo->in-progress"
vm:task-status     vm/alias-done      "x"
vm:task-status     vm/alias-todo      " "
```

### What this means

- Type checking at runtime is a graph query, not file loading
- Function execution resolves its definition from the graph
- The EDN files are editable projections, like markdown files
- A single sync pipeline handles both projections
- The Projection contract (`Sync`, `Write`, `WriteAll`) applies to EDN too

### EDN Projection contract

The EDN projection implements the same interface as the markdown projection:

- **Sync**: read `.edn` files from config dir, parse, expand to triples,
  reconcile against current graph state
- **Write**: render a type/function/value-model from graph state back to EDN
- **WriteAll**: render all config objects back to EDN files

This is a second implementation of the Projection concept, specialized for
the config surface instead of the content surface.

### Curry-Howard correspondence

The EDN config files are the "proof terms" — they express the structure.
The triples are the "propositions" — the facts that the structure asserts.
The projection is the correspondence between the two representations.

A type definition in EDN is a specification. The same type as triples is
the elaborated record. Syncing EDN to triples is elaboration. Rendering
triples to EDN is extraction. Same data, different representations
connected by a faithful bidirectional mapping.

## Implementation Strategy

### Phase 1: EDN Projection (new package)

Create `internal/projection/edn/` implementing the Projection contract
for EDN config files. Handles types, functions, and value models.

Sync reads `.edn` files → expands to triples with `type/*`, `fn/*`,
`step/*`, `vm/*` predicates → stores in the triple store.

### Phase 2: Runtime from graph

Replace `types.LoadTypeDef` (reads files) with graph queries. Replace
`function.LoadFunction` (reads files) with graph queries. All runtime
resolution goes through the triple store.

### Phase 3: EDN rendering

Render types/functions/value models back to EDN from graph state. This
enables `sevens config show` to display the canonical form, and enables
round-trip editing.

### Phase 4: Interpreter forms

Implement the keyword-dispatched interpreter for forms like `:match`,
applied types `[:create task]`, and predicates `[:child-exists ...]`.
These are evaluated against the graph at runtime.

## Migration Path

The current file-based loading continues to work during migration. The
EDN projection is additive — it writes triples alongside the existing
ones. Once all consumers read from the graph instead of files, the file
loaders become the projection's parse step only.

## What This Enables

- Functions as well-typed arrows: `{:input :node :output [:create task]}`
- Conditional output types: `[:match [:child-exists ...] :edit :create]`
- Type-scoped context: `{:context {:type task :scope :subtree}}`
- Composable pipelines: `{:pipeline [:suggest :generate]}`
- Templates as applied constructors: `[:deterministic :create-node]`
- Value models as graph objects: enums, state machines, dates, references
- All expressible in valid, parseable EDN
- All stored as triples, queryable uniformly
- Config editing is just another projection surface
