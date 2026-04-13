# Concept: Types

## Purpose

Name emergent structural patterns so they can be queried, validated, and used
to drive projection mappings, context gathering, and function selection.

Types are not a separate layer. They are named patterns over predicates that
already exist in the graph. Recognition and specification are the same
operation — both are pattern matching over the predicate space.

## State

```
TypeDefs        map[name] → TypeDef
ValueModels     map[name] → ValueModel (enum, state machine, date parser, etc.)
```

Both are stored as triples. Config files are ergonomic sugar that expand into
triples at load time.

## What a Type Is

A type is a predicate shape. A node or block conforms to a type when its
predicates match the shape. Three sources contribute predicates:

1. **Graph position** — relationships to other nodes (parent, children,
   siblings, cross-refs). Already expressed as triples via path specs.

2. **Frontmatter** — document-level metadata. Predicates attached to the
   node subject. Currently limited to title/parent/role; needs open
   frontmatter → predicate bridge.

3. **Block-level orthography** — property lists and inline signifier atoms
   attached to blocks within a node. Predicates attached to block subjects.
   Specified by the Markdown+ DSL (see `MARKDOWN-ORTHOGRAPHY-DESIGN.md`).

The type definition declares which predicates constitute the shape AND how
those predicates map to each projection layer.

## Type Definition Format

```edn
{:name "task"

 ;; --- Predicate shape ---
 :predicates {:required ["status" "deadline"]
              :optional ["priority" "assignee" "estimate"]}

 ;; --- Structural constraints ---
 :structure {:parent-type "project"       ;; parent must conform to "project"
             :children {:min 0 :max 5}
             :requires-sibling-role nil}

 ;; --- Projection mapping ---
 ;; Declares how predicates render in each layer.
 ;; The type owns the bidirectional mapping: parse and render.
 :projection {;; Layer 2: which predicates are frontmatter fields
              :frontmatter ["status" "deadline" "priority" "assignee"]

              ;; Layer 3: predicate → orthographic binding
              ;; Each entry binds a signifier, a key name, and a value model.
              ;; The triple (signifier, key, value-model) is the atomic unit.
              :orthography {"status"   {:signifier nil         ;; word-key only
                                        :value-model "task-status"}
                            "assignee" {:signifier "@"
                                        :value-model "person"}
                            "priority" {:signifier "!"
                                        :value-model "priority"}
                            "estimate" {:signifier "~"
                                        :value-model "duration"}}}}
```

## Slot Grammar

A property list contains slots separated by `|`. Each slot has one of
three arities:

```
arity-0: token                              "x", "!", "::blocked"
arity-1: signifier-symbol (no whitespace)   "@julian", "#research", "~2h"
arity-2: key WS payload                     "status done", "@ julian", "assignee @julian"
```

**Arity-0**: A standalone token. Resolves through semantic definitions to
a state, flag, or transition. No value.

**Arity-1**: A signifier fused with a symbol (no whitespace between them).
The entire atom is the value — the signifier is part of the identity.
`@julian` is a self-contained reference atom. It is the same `@julian`
that appears inline in prose.

**Arity-2**: An explicit key followed by whitespace and a payload. The key
is either a word-key (`status`) or a signifier used as a key (`@`). The
payload is the value.

### Signifier double duty

A signifier like `@` serves two roles, bound together by the type definition:

- In arity-1 (`@julian`): the `@` is part of the reference atom. The type
  resolves which key this signifier maps to (e.g., `assignee`) and what
  value model applies (e.g., `person`).
- In arity-2 (`@ julian`): the `@` is a shorthand key name. Same key
  binding, but the value is the bare payload `julian`.

The type definition couples these: `{:signifier "@" :value-model "person"}`
on the `assignee` key means `@` always maps to `assignee` whether fused or
spaced. The value model determines how the payload is interpreted.

A signifier COULD be configured to mean different things in each arity, but
in practice you want consistency — `@julian` and `@ julian` should resolve
to the same key.

### Examples

Given a `task` type with the orthography mapping above:

```md
(@julian)                    → assignee = @julian (person reference)
(@ julian)                   → assignee = julian (bare value)
(assignee @julian)           → assignee = @julian (explicit key, reference)
(assignee julian)            → assignee = julian (explicit key, bare value)
(status done)                → status = done (word-key, state machine)
(x)                          → status = done (bare token, alias resolution)
(! urgent)                   → priority = urgent (signifier key, enum)
(!urgent)                    → priority = !urgent (fused atom)
(~2h)                        → estimate = ~2h (fused atom, duration)
(@ julian | status done | !) → three slots in one property list
```

### What this means

- A node conforms to `task` when it has `status` and `deadline` predicates
  and its parent conforms to `project`.
- When rendering a `task` node to markdown, `status` and `deadline` appear
  as frontmatter fields. Inside blocks, `status` renders as the `status`
  word-key and `assignee` renders as `@name`.
- When parsing, the projection mapping runs in reverse: frontmatter field
  `deadline` becomes predicate `deadline` on the node; `(@julian)` on a
  heading becomes predicate `assignee` = `julian` on the block.

## Value Models

Predicates have values. The interpretation of those values — validation,
normalization, valid transitions — is defined by value models. Value models
are graph objects, not parser primitives.

### Enum

A fixed set of allowed values with optional aliases.

```edn
{:name "priority"
 :kind :enum
 :members ["low" "medium" "high" "urgent"]
 :aliases {"!" "urgent"
           "!!" "high"}}
```

Expands to triples:

```
enum:priority    enum/member     "low"
enum:priority    enum/member     "medium"
enum:priority    enum/member     "high"
enum:priority    enum/member     "urgent"
state:urgent     orth/alias      "!"
state:high       orth/alias      "!!"
```

Validation: payload must be a member or resolve through an alias.

### State Machine

An enum with transition constraints.

```edn
{:name "task-status"
 :kind :state-machine
 :states ["todo" "in-progress" "done" "blocked"]
 :transitions [["todo" "in-progress"]
               ["todo" "blocked"]
               ["in-progress" "done"]
               ["in-progress" "blocked"]
               ["blocked" "in-progress"]]
 :aliases {"x" "done"
           " " "todo"
           "::blocked" "blocked"}}
```

Expands to triples:

```
sm:task-status         sm/state       state:todo
sm:task-status         sm/state       state:in-progress
sm:task-status         sm/state       state:done
sm:task-status         sm/state       state:blocked

sm:task-status         sm/transition  trans:todo->in-progress
trans:todo->in-progress  sm/from      state:todo
trans:todo->in-progress  sm/to        state:in-progress
...

state:done             orth/alias     "x"
state:todo             orth/alias     " "
state:blocked          orth/alias     "::blocked"
```

Validation: current value + proposed value must be a valid transition.

### Date

```edn
{:name "date"
 :kind :date
 :format "2006-01-02"}
```

Validation: payload parses as a date in the given format.

### Reference

```edn
{:name "person"
 :kind :reference
 :resolves-to "node"    ;; or "external"
 :signifier "@"}
```

Validation: payload resolves to an existing node (or is recorded as an
unresolved reference for later).

### Custom

Value models are open. Any subgraph shape that provides validation and
normalization can serve as a value model. The four above are the common
cases that get ergonomic config sugar.

## Binding: Predicates to Value Models

A semantic key binds a predicate name to a value model:

```edn
{:key "status"
 :emits-predicate "meta/status"
 :value-model "task-status"}

{:key "deadline"
 :emits-predicate "meta/deadline"
 :value-model "date"}

{:key "assignee"
 :emits-predicate "meta/assignee"
 :value-model "person"}
```

These expand to triples:

```
sem:status      sem/label             "status"
sem:status      sem/emits-predicate   "meta/status"
sem:status      sem/value-model       sm:task-status

sem:deadline    sem/label             "deadline"
sem:deadline    sem/emits-predicate   "meta/deadline"
sem:deadline    sem/value-model       vm:date

sem:assignee    sem/label             "assignee"
sem:assignee    sem/emits-predicate   "meta/assignee"
sem:assignee    sem/value-model       vm:person
```

## Type Conformance

A node conforms to a type when:

1. All required predicates are present (directly on the node or on its
   blocks, depending on the predicate's scope).
2. Structural constraints are satisfied (parent type, child count, etc.).
3. All predicate values are valid according to their bound value models.

Conformance is a query, not an assertion. The graph already has the
predicates; conformance just checks if the pattern matches.

Partial conformance is meaningful: a node may have 3 of 5 required
predicates. This can be reported as "partially conforms" rather than a
binary pass/fail. Functions can use partial conformance to suggest what's
missing.

## Type Recognition vs Specification

These are the same operation:

- **Recognition**: query the graph for nodes whose predicates match a
  pattern. "Find all nodes with status + deadline predicates whose parent
  has type project." This is a graph query.

- **Specification**: assert that a pattern should exist. "Nodes of type
  task must have status + deadline." This adds triples describing the
  pattern. Validation then queries whether existing nodes match.

Both are pattern matching over the same predicate space. The type
definition adds triples that describe the pattern. Conformance checking
queries whether other triples match that pattern. Same machinery.

## Relationship to Context Policies

Context policies (`minimal`, `neighborhood`, `full`) are anonymous types.
They describe a subgraph shape for context gathering:

- `minimal` = target node only
- `neighborhood` = target + parent + siblings + children
- `full` = target + entire subtree

Named types subsume context policies. A function that says `context-policy:
neighborhood` is equivalent to one that says `context-type: neighborhood`
where `neighborhood` is a built-in type. Once types exist, context policies
become sugar over type-scoped queries.

Functions can also scope context by type: "gather all `task` nodes under
the target" instead of "gather all children."

## Relationship to Export / Harvest

Export ("give me context to paste into a GUI LLM") and harvest ("structure
this conversation back into sevens nodes") both operate on typed subgraphs:

- **Export**: resolve a type (or function's context spec) against the graph,
  render the matching nodes. "Export all task nodes under this project" is
  a type-scoped query followed by rendering.

- **Harvest**: the prompt tells the GUI LLM what types of nodes to produce.
  The response format is the type's predicate shape serialized as JSON ops.
  "Create task nodes with status, deadline, and assignee" — the type
  definition specifies what fields to include.

## Primitive Types and Dependent Typing

The type system is fully dependent. There are four primitive output types
that are themselves type definitions:

```
text        displayed, not persisted
create      new node(s)
edit        modify existing node(s)
suggestion  proposed creates (not yet materialized)
```

These are the base types. They are defined as EDN type definitions like any
other type, but they are built-in and carry their own schema instructions
(the prompt material that tells the LLM how to format its response).

User-defined types are **subtypes of these primitives**. A `task` type
extends `create` — it inherits the create schema instruction and adds its
own constructor fields (required predicates, structural constraints). The
schema instruction for `task` is composed: the `create` base instruction +
the `task`-specific fields.

```edn
;; Primitive type (built-in)
{:name "create"
 :primitive true
 :schema-instruction "Respond with a JSON object containing an \"ops\" field
   with an array of create operations. Each operation:
   {\"action\": \"create\", \"title\": \"...\", \"parent\": \"...\",
    \"content\": \"...\", \"extra\": {...}}"}

;; User-defined subtype of create
{:name "task"
 :extends "create"
 :predicates {:required ["status" "deadline"]
              :optional ["priority" "assignee"]}
 :structure {:parent-type "project"}
 :projection {:frontmatter ["status" "deadline" "priority" "assignee"]}}
```

When a function's output step resolves to type `task`, the executor composes
the schema instruction: the `create` primitive's instruction + "Nodes you
create should conform to type task. Required fields in extra: status,
deadline."

### Why dependent types

Every layer is dependent on the layers below it:

- `text` output depends on what the function is analyzing
- `create`/`edit` ops depend on what nodes are being created/modified
- The created nodes' types depend on their parent's type
- The parent's type depends on its position in the graph

There are no static types anywhere. A function signature is not
`decompose : Node → [Node]`. It is
`decompose : Node<T> → [Node<child-of<T>>]`.

The type annotation on a function is not a fixed string — it is a
relationship derived from the target node's context at call time. The
executor resolves the concrete type by examining the graph.

### Schema instructions live in type definitions

The hardcoded schema instructions in the executor are replaced by schema
instructions stored IN the type definitions. The four primitives carry
their own prompt material. Subtypes inherit and extend it. The executor
composes them at call time:

1. Resolve the function step's output type (may be dependent on target)
2. Walk the type chain to the primitive
3. Compose schema instructions: primitive base + subtype constructor
4. Inject into the system prompt

This means prompt format instructions are never in function templates or
in Go code. They live in type definitions — the same place the type's
shape is defined.

## Relationship to Functions

Functions are typed arrows over the primitive types. A function's `output`
declares which primitive type it produces. The `output-type` optionally
narrows it to a specific subtype:

```edn
{:name "notice"
 :steps [{:output "text"}]}                    ;; primitive: text

{:name "decompose"
 :steps [{:output "suggestions"                ;; primitive: suggestion
          :output-type "dimension"}             ;; subtype: dimension
         {:output "ops"                         ;; primitive: create
          :output-type "dimension"}]}           ;; subtype: dimension

{:name "sharpen"
 :steps [{:output "ops"}]}                     ;; primitive: edit (no subtype)
```

When `output-type` is absent, the primitive's schema instruction is used
alone. When present, the subtype's constructor fields are composed with
the primitive's instruction.

This enables type-driven function selection: "what functions can operate on
this node given its current type?"

## Reification

Reification is naming a pattern that already exists. The user observes that
several nodes share a predicate shape, then pins it:

```
sevens type define task --requires status,deadline --parent-type project
```

This adds the type definition triples. It doesn't change any existing nodes.
It just makes the pattern queryable by name.

The inverse is also useful: "what unnamed patterns exist in my graph?"
This is a clustering query over predicate co-occurrence — which predicates
tend to appear together on the same nodes? This can surface candidate types
for reification.

## Implementation Order

1. **Open frontmatter → predicates**. The sync layer extracts arbitrary
   frontmatter fields into `fm/*` predicates. Type definitions declare
   which predicates are frontmatter fields for a given type.

2. **Type definitions**. Config format, expansion to triples, conformance
   checking. This is the named-pattern layer over predicates + structure.

3. **Type-scoped queries**. "Find nodes matching type X" as a first-class
   query. Context policies become sugar over this.

4. **Orthography parser**. Surface parsing of property lists and inline
   atoms. Semantic resolution emits block-level predicates. Type definitions
   declare the predicate ↔ orthography mapping.

5. **Export / harvest**. Type-scoped context rendering and structured
   response import.

Layer 1 is the foundation — everything else needs open predicates. Layer 2
makes patterns nameable and queryable. Layer 3 makes queries composable
with existing context gathering. Layer 4 adds the block-level DSL. Layer 5
builds the external-LLM workflow on top.

## Design Decisions

1. **Predicate namespace**: Type-defined predicates are namespaced.
   `meta/status`, `meta/deadline`, `meta/assignee`. This avoids collisions
   with structural predicates (`node/parent`, `block/content`). The
   namespace prefix distinguishes the predicate's origin: `node/*` for
   graph structure, `block/*` for block structure, `meta/*` for
   type-defined semantics.

2. **Flat subtyping over primitives**: User-defined types extend exactly
   one primitive (`text`, `create`, `edit`, `suggestion`). There is no
   arbitrary inheritance between user types. A node can conform to multiple
   user types independently — `task` and `urgent` are separate subtypes of
   `create`; a node with both shapes conforms to both. The only "extends"
   relationship is subtype → primitive.

3. **Predicate scope by projection layer**:
   - **Frontmatter** → file-level predicates on the node subject.
     `meta/status` in frontmatter attaches to the node.
   - **Property list on a heading** → applies to child blocks within that
     heading's scope. A `(status in-progress)` on `## Implementation`
     means all blocks under that heading inherit `meta/status in-progress`.
     This is scope-based inheritance within the document, not type
     inheritance.
   - **Property list on a list item** → applies to that specific block.

   Frontmatter and block-level predicates are different subjects (node vs.
   block) with the same predicate names. A heading's property list scopes
   downward to its children, providing a natural way to annotate sections
   without repeating predicates on every block.

## Open Questions

1. **Validation strictness**: When a node partially conforms to a type,
   is that an error, a warning, or just information? Different contexts
   may want different answers.

2. **Type-driven rendering**: Should the projection layer use type
   conformance to decide rendering? E.g., render `task` nodes differently
   from `note` nodes in the overview tree. Or is rendering always
   type-agnostic?

3. **Heading scope propagation**: Lazy. Block-level queries resolve
   inherited predicates by walking up the heading scope chain (already
   tracked via `block/scope`). Predicates on a heading are not eagerly
   copied to child blocks. This avoids stale triples when headings are
   edited or reordered.
