# Session handoff — 2026-04-14

This is the state of sevens after the multi-session push that ended on
2026-04-14. Read this first when starting a new session. It captures
(a) what works, (b) what's live vs. legacy, (c) the unresolved design
direction, and (d) the concrete next-steps punch list.

## What works end to end

Every default function in `defaults/functions/` has been exercised
against the live vault (`~/Documents/sevens`) with the codex backend
and the kernel validator on the hot path:

- **Polymorphic dispatch**: `discuss` is the only currently-polymorphic
  function. Its EDN declares an `:output-picker` that routes between
  `:create` (first call — start a new Discussion child) and `:edit`
  (subsequent calls — append to the existing discussion file). The
  picker expression is pure data; no Go-side hook.
- **Static ops functions**: sharpen, bridge, elaborate, trim, scaffold,
  summarize, merge, relate — all produce edit ops, all validated by
  the kernel before materialization.
- **Text functions**: notice, challenge, contradict, thesis, audit —
  produce markdown, displayed via `ui.RenderMarkdownOrPlain`.
- **Suggestion functions**: synthesize, decompose — produce structured
  suggestions, displayed via `formatSuggestions`.
- **Multi-step with delegation**: audit delegates its first step to
  notice via `:fn "notice"`; the executor substitutes the delegated
  function's backend config at dispatch time.
- **Multi-step with gate**: decompose suggests, gates for approve,
  then generates create ops on accept.
- **Distill**: handles the "nothing to distill" case cleanly — empty
  `{"ops":[]}` envelopes are recognized and displayed as "reported
  no changes" rather than printing raw JSON.

Observability: `sevens discuss --dry-run <target>` and
`sevens apply <fn> <target> --dry-run` both print three sections
(picker resolution, composed system prompt, user prompt) without
calling the LLM. Useful for debugging prompts.

## Packages introduced in this push

Two new load-bearing packages. Both parallel the existing tree; the
old `internal/types` package still exists as a fallback but is mostly
dead.

### `internal/types/kernel`

The runtime type kernel. Owns primitive type definitions, schema
composition, and validation.

- **Primitive types** — the four wire-level operation types (`text`,
  `create`, `edit`, `suggestion`) are defined as structured EDN files
  under `internal/types/kernel/primitives/`, embedded at compile time
  via `//go:embed`. Each file declares:
  - `:envelope` with `:kind` (scalar or array), `:wrapper` (top-level
    JSON key), `:item-constants` (routing tags like action=create),
    `:item-fields` (per-item shape), and for scalar envelopes a
    `:scalar-field`.
  - `:example` — hand-written EDN data that gets `json.Marshal`'d at
    load time so the example shown to the LLM is canonically valid
    JSON by construction. Replaces the previous bug-prone hand-typed
    JSON strings in Go source.
- **Schema rendering** — `renderPrimitivePrompt` walks the envelope
  structure and produces the "You MUST respond with a JSON object..."
  preamble + field list + example for the prompt. Same structured
  data also drives `LocalFields` for the validator — one source, two
  derivations.
- **Validation** — `Registry.Validate(kb, typeName, value)` walks the
  shape and runs refinements. Refinements are either `Intrinsic` (no
  KB) or `Contextual` (KB-dependent).
- **Subsumption and chain walks** — `Ancestors`, `IsSubtype`,
  `RootPrimitive`, `ChildTypeOf`, `SubtypesOf`, `ComposedShape`,
  `CollectRefinements`. All cycle-guarded (a malformed `task → task`
  chain is broken at the first repeat, no infinite loop).
- **Type family groundwork** — `DerivedType.ChildType` field and
  `ChildTypeOf` walker for the `Node<T> → Node<child-of<T>>` pattern
  from the Haskell sketch. Not yet used by any function.

### `internal/function/picker`

The EDN-declarable expression language for dependent output type
pickers. Full twelve-constructor vocabulary:

```
LitType, LitStr, If, And, Or, Not, Eq, Concat,
TargetTitle, ExistsNode, HasType, PriorOutputType
```

- **AST and evaluator** — `picker.Expr` sealed interface, `Eval`
  returns a `Value` in an `EvalContext{KB, TargetTitle, TargetConforms,
  PriorOutputType}`.
- **Static analysis** — `PossibleReturnTypes(expr)` enumerates the
  types an expression can return. Load-time `CheckDeclaration` verifies
  the declared alternatives is a superset of this. Runtime rejects
  pickers whose evaluator returns a type not in the declared set.
- **EDN parser** — `function.ParseOutputPicker` and
  `function.ParsePickerExpr` in `internal/function/picker_parse.go`
  handle the surface syntax (s-expression form).
- **Reference doc** — `docs/picker-language.md` covers every
  constructor, static analysis guarantee, and the discuss example.

## Live vs. dead (or dormant) code

### On the hot path

- `internal/types/kernel` — `NewPrimitivesRegistry` is called as the
  package-level `function.primitiveRegistry` variable. Every LLM step
  goes through `kernel.Validate` for parse-time shape checking, and
  every step's system prompt is composed via `kernel.SchemaInstruction`
  (for primitives) when no subtype is declared.
- `internal/function/picker` — evaluated in `executeStep` Phase 1 when
  a step has `OutputPicker != nil`. Currently used by `discuss`.
- `internal/function/opvalidate.go:ValidateOps` and `ValidateOpsAgainst`
  — run on every `ShapeFileOps` step's output before materialization.
  Routed via the picker when present, otherwise per-op primitive check.
- `internal/function/preview.go:PreviewStepPrompt` — called from both
  dry-run paths to mirror the executor's dispatch logic without
  actually calling the LLM.
- `internal/projection/edn` — reads and writes function definitions,
  including the new `PredFnOutputPicker` triple that carries the
  picker's serialized EDN form so graph-loaded functions retain their
  polymorphism.

### Dormant (defined but unused by any current function)

- `internal/types/kernel`'s `DerivedType` with `ChildType` — the
  type-family mechanism for input-dependent outputs. Tests cover it
  but no function uses `:output :child-of-input` yet.
- `kernel.ContextualRefinement` — the KB-dependent refinement type.
  Kernel has no built-in refinements that use it (only `Intrinsic`
  ones fire today). The Haskell sketch uses it heavily for
  `discussion-turn.old-text-is-suffix-of-last-line`; no Go
  implementation has registered such a refinement.
- `picker.PriorOutputType` — the only picker constructor that's
  dynamically-bounded (unbounded `PossibleReturnTypes`). No
  multi-step function currently uses it. `TransformResult.ResolvedType`
  threading is in place to support it.

### Legacy (intended for eventual removal)

- `internal/types` (the non-kernel package) — still exists, still
  loaded. Its `ComposeSchemaInstruction` is only called as a final
  fallback in `executor.executeStep` and `preview.go` when the
  current step's `:output-type` is a user-defined subtype the kernel
  doesn't know about. Since no default function uses `:output-type`,
  this path is effectively dead on the current function set but not
  yet safe to delete.
- `defaults/types/{create,edit,text,suggestion}.edn` — duplicates of
  the embedded kernel primitives in an older format. Still read by
  the legacy composer. Safe to leave for now; delete when the legacy
  `internal/types` package is retired.
- `defaults/types/note.edn`, `task.edn` — legacy user-type definitions
  in the old format. Loadable by `internal/types.LoadAllTypeDefs` but
  not by the kernel. `task.edn` had its `project` parent-type
  constraint removed in the 2026-04-13 code review pass because no
  `project` type existed.
- `defaults/types/sevens-*.edn` — context-policy types in the flag-
  based `:gather` shorthand. Used by the legacy `context-policy`
  mechanism, not by the kernel or picker layer.

## Known bugs and debt (still open)

From the five-agent code review on 2026-04-13, the "must fix" set
(1–7) were all closed in commit `eb63d24`. Remaining:

### Cleanup

- `Refinement` interface has no sealing mechanism. `TypeDef` has
  `isTypeDef()` to prevent outside implementations; `Refinement` is
  open. Inconsistency. (Punch list item #13.)
- `checkFields` in `kernel.go` rejects empty `VMap` on required
  fields. The Haskell spec only checks `VAbsent` and empty `VString`.
  Benign today because no primitive has a required map field. (#14.)
- `Value.Fields` is exported but never read outside the kernel
  package. Could be unexported. (#15.)

### Docs staleness

- `docs/ARCHITECTURE.md` was updated in this handoff pass to mention
  `internal/types/kernel` and `internal/function/picker`. Before that
  it missed both.
- `--dry-run` flag help text still says "Print rendered prompt without
  calling LLM" in both `cmd/sevens/main.go` and
  `cmd/sevens/pipeline_cmds.go`. It now shows picker + system prompt.
  Minor cosmetic.

### Test gaps

- No EDN round-trip test for a function with an `:output-picker`.
  `TestLoadFunctionRoundTrip` loads `notice` which has no picker. A
  regression in `ednencode.Marshal` / `ParseOutputPicker` round trip
  would not be caught.
- No test exercises `AcceptPipeline` or `ApplyFunction` with a picker-
  equipped function. All picker tests live in
  `internal/workflow/discussion_test.go`; the general workflow tests
  use static-output functions.

## Open design direction: types as predicate bags

The 2026-04-14 session closed with a design conversation that reframes
the type system, *not yet implemented*. Capturing it here so the next
session starts with the framing rather than rediscovering it.

### The observation

A type is a collection of predicates. Predicates can be:

- **Field predicates** — has `status`, has `title`
- **Content predicates** — title starts with `"Discussion - "`
- **Positional predicates** — has a parent with content X, has at
  least one child
- **Graph-query predicates** — reachable via path `node/parent →
  node/content`, bound to template variable `"parent"`
- **Refinement predicates** — `old_text` is a suffix of the last line
  of the resolved file

They're all just predicates. Some are intrinsic to the value, some
walk the graph, some reference other nodes. The distinction between
"a type" and "a context policy" and "a function's `:context` block"
evaporates — a context policy is a type whose predicates happen to
include positional ones, and a function's `:context` is the same
thing inlined.

### The unification

A function's input type contains everything the function needs from
the target and its neighborhood. The executor "gathers context" by
**evaluating the type's predicates** against the graph — no separate
`:context` mechanism, because positional predicates are members of
the predicate set.

```edn
;; Before (what we have now):
{:name "notice"
 :context
 [{:path ["node/parent"]          :with ["node/content"] :as "parent"}
  {:path ["node/parent~"]         :with ["node/content"] :as "children"}
  {:path ["node/parent" "node/parent~"] :exclude-self true :with ["node/content"] :as "siblings"}]
 :input "node"
 :output "text"}

;; After (unified):
;; defaults/types/node-neighborhood.edn
{:name "node-neighborhood"
 :extends "node"
 :predicates
 [{:path ["node/parent"]          :with ["node/content"] :as "parent"}
  {:path ["node/parent~"]         :with ["node/content"] :as "children"}
  {:path ["node/parent" "node/parent~"] :exclude-self true :with ["node/content"] :as "siblings"}]}

;; defaults/functions/notice.edn
{:name "notice"
 :input "node-neighborhood"
 :output "text"}
```

The function becomes a typed arrow: `NodeNeighborhood → Text`.
Context is a property of the input type, not the function. Multiple
functions that need the same neighborhood share one type definition.
Adding a new neighborhood shape is adding a type, not editing every
consumer.

### Implementation direction

The earlier categorization (primitive / semantic / input) was wrong.
There is one kind of type — a predicate bag — with different predicate
flavors. The kernel loader needs to:

1. Read user-defined types from `defaults/types/*.edn` (currently
   only reads primitives from the embedded primitives dir).
2. Understand positional predicates as a predicate kind. Today's
   `FieldSpec` covers field predicates only; extend or add sibling
   types for the others.
3. Evaluate positional predicates against the KB at dispatch time,
   producing the template-variable bindings the prompt renderer
   consumes.
4. Retire `:context` on function definitions in favor of named
   input types. Inline `:context` can remain as syntactic sugar
   for an anonymous input type.

### Prerequisite

The 2026-04-13 primitive EDN rewrite (commit `aeb875c`) is a
prerequisite — it's what taught the kernel to load structured
definitions from EDN. The unification extends that loader to read
the full type set, not just primitives.

## Proposed next-session work, in order

1. **Context-type unification** (the big one). Design the predicate-
   bag EDN format; extend the kernel loader to read
   `defaults/types/*.edn`; extend `ResolveContext` to read positional
   predicates from the input type; migrate a single function (notice
   is a good candidate) to the new form as a proof of concept;
   migrate the rest; delete `:context` field from `ednformat.Function`.
   Big — probably a full session on its own.

2. **Port user types to the kernel registry** (subset of #1). If the
   full unification is too big, start by teaching the kernel to load
   non-primitive types from `defaults/types/` in the new EDN format.
   This lets functions declare `:output-type "chat"` or similar and
   have the kernel validate against it, without touching `:context`.

3. **Refinement-driven discussion invariants**. Once user types load
   into the kernel, define `discussion-turn extends edit` with the
   `old-text-is-suffix-of-last-line` contextual refinement from the
   Haskell sketch. Wire up a `kernel.KB` adapter that reads file
   content (current adapter only returns presence).

4. **Test gaps from the code review**: round-trip picker test,
   workflow test with picker-equipped function, cycle test for
   derived types with broken parents.

5. **Cleanup from the code review**: seal `Refinement`, fix
   `checkFields` VMap check, unexport `Value.Fields`, update
   `--dry-run` flag help text.

## Key documents

- `docs/sketch/*.hs` — Haskell specs. Still the source of truth for
  the design. 8 files: TypesKernel, GoSubstrate, ConformanceIndex,
  FunctionContracts, PipelineComposition, PipelineGates,
  PickerExpressions, InputDependence.
- `docs/picker-language.md` — reference for the picker DSL.
- `docs/design/concept-types.md` — original concept design. The
  unification direction above is what concept-types was pointing
  at all along.
- `docs/ARCHITECTURE.md` — package graph and layer model. Updated
  in this handoff pass to mention kernel and picker.
- `docs/WALKTHROUGH.md` — user-facing tutorial. Not updated in this
  push; may have stale examples.

## Commits from the push

Most recent (top of `main`, 2026-04-14):

```
aeb875c Load kernel primitive types from embedded EDN
eb63d24 Code review pass: fix 7 bugs across kernel, executor, display
81bdb13 Test picker_glue helpers
7b33848 Include offending old_text in exact-match errors
001d3b9 Regression test for :fn delegation
2c9d81f Display suggestions in completed-result branch
7f6941e Handle empty-envelope LLM responses cleanly
b743c7f Honor :fn delegation in the executor
f40869c Track ResolvedType on TransformResult
9008dbd Extend picker/schema preview to apply --dry-run
55cfd09 Enrich discuss --dry-run with picker + schema inspection
2826c06 Route bare primitives through kernel composer
bdadd84 Kernel SchemaInstruction produces real prompt material
7095b8a Integration tests for discuss picker routing
3879c94 Make discuss polymorphic via EDN-declared output picker
3c4a203 Wire kernel validator into function executor for FileOp shapes
f51e9b1 Add type family support to kernel: ChildType, ChildTypeOf, SubtypesOf
f08dc9e Add internal/function/picker package
ddaae96 Add internal/types/kernel package
fa58464 Add Haskell spec sketches
0762b93 Buffer codex stderr
```

Review any of these in isolation for context on a specific change.
