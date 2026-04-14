# Picker expressions: declarative output dispatch

## What this is

A small, closed-vocabulary expression language for declaring that a
function's output type depends on runtime state — KB contents, the
target node's type, or a prior step's output. Evaluated at dispatch
time, before the LLM is called. Used to express conditional output
shape *as data in an EDN config file*, not as a Go hook.

Lives in `internal/function/picker` and is loaded from a function's
EDN declaration via `internal/function.ParseOutputPicker`.

## Why

Sevens functions normally declare a static output type:

```edn
{:name "sharpen"
 :input "node"
 :output "edit"}
```

But some functions need conditional dispatch — `discuss` should
produce a `create` op the first time it runs on a node and an `edit`
op on subsequent runs. A picker lets the function declare that
decision as part of its type signature:

```edn
{:name "discuss"
 :input "node"
 :output-picker
 {:alternatives [:create :edit]
  :expr (if (exists-node? (concat "Discussion - " target-title))
            :edit
            :create)}}
```

At load time sevens:

1. Parses the picker expression into an AST.
2. Verifies `possibleReturnTypes` of the expression is a subset of
   the declared `:alternatives`. A picker that could return a type
   not in its alternatives is a load-time error.

At call time sevens:

1. Builds an `EvalContext` from the live KB, target, and prior steps.
2. Evaluates the expression — returns a single `TypeName`.
3. Checks the returned name against `:alternatives`. A picker that
   lies is a runtime error.
4. Uses the resolved type to pick the schema instruction for the
   LLM *and* the validator for the response. Prompt and parser
   share one source of truth.

## Shape

Every picker has two required keys:

- `:alternatives` — a vector of type keywords the expression is
  allowed to return. Every element must name a real type (primitive
  or kernel-registered subtype). Load-time static analysis verifies
  the expression can only return values from this set.
- `:expr` — the expression, described below.

Optional:

- `:name` — a human-readable name for the picker, surfaced in error
  messages. Defaults to empty.

## Vocabulary (12 constructors)

### Literals

- **`:type-name`** (EDN keyword) — a type literal. Evaluates to that
  type. Example: `:edit`, `:create`, `:discussion-turn`.
- **`"string"`** (EDN string) — a string literal. Used as input to
  `concat`, `exists-node?`, etc.

### Bare primitives

- **`target-title`** — the title of the target node the function is
  being applied to. Evaluates to the string form.

### Unary and binary operators

- **`(if cond then else)`** — cond must evaluate to a boolean. If
  true, returns the result of `then`; otherwise `else`.
- **`(and a b)`**, **`(or a b)`**, **`(not x)`** — boolean
  combinators. Operands must be booleans.
- **`(= a b)`** — equality. Operands must be the same type
  (both strings, both type literals, or both booleans).

### String construction

- **`(concat part1 part2 ...)`** — joins string expressions. All
  parts must evaluate to strings.

### KB queries

- **`(exists-node? title-expr)`** — returns true if a node with the
  given title exists in the KB. `title-expr` must evaluate to a
  string.
- **`(has-type? :type-name)`** — returns true if the target node is
  known to conform to the given type (via the conformance index).
  Argument must be a type keyword literal.

### Pipeline state

- **`(prior-output-type n)`** — returns the `TypeName` that step `n`
  of the current pipeline produced. `n` is an integer index, 0-based.
  Only meaningful in multi-step pipelines. Note: this is the *one*
  constructor whose return set cannot be statically bounded, so
  `PossibleReturnTypes` for expressions using it returns `nil, false`
  and the load-time check is deferred to runtime.

## Example: discuss

```edn
{:name "discuss"
 :description "Create or continue a Discussion child node on the target"
 :input "node"
 :output-picker
 {:alternatives [:create :edit]
  :expr (if (exists-node? (concat "Discussion - " target-title))
            :edit
            :create)}}
```

Reads: "If a node titled `Discussion - <target>` already exists in
the KB, the output is an edit op (append to it); otherwise it's a
create op (start a new discussion child)."

## Example: conditional dispatch with conformance

```edn
{:name "analyze"
 :input "node"
 :output-picker
 {:alternatives [:observation :task-edit]
  :expr (if (has-type? :task)
            :task-edit
            :observation)}}
```

Reads: "If the target conforms to `task`, produce a task-edit; else
produce an observation." Useful for functions that want to behave
differently based on the target's type.

## Static analysis guarantee

For every picker whose expression uses only constructors with
bounded return sets (everything except `prior-output-type`), the
load-time check enumerates every possible final type literal and
verifies it is in `:alternatives`. This means:

- A picker that *could* return a type not in its alternatives is
  rejected at load time. The error names the extra types.
- A picker that declares alternatives it can't reach is fine — the
  declaration is an upper bound, not an equality.
- Pickers using `prior-output-type` defer to runtime checking (the
  only way a picker can "lie" about its declared alternatives).

## What this language does not do

- **No user-defined primitives.** The vocabulary is fixed. Adding
  new primitives means extending the Go kernel (new `Expr`
  constructor, new evaluator clause, new parser case), not writing
  EDN.
- **No general lambda / no recursion.** Every expression terminates
  in a finite number of evaluator steps.
- **No side effects.** The evaluator is pure over
  `(KB, EvalContext)`. Calling a picker twice with the same inputs
  always returns the same output.
- **No string manipulation beyond concat.** No regex, no slicing,
  no substitution.

If you find yourself wanting something not in the vocabulary, the
answer is usually "add the primitive to the kernel" rather than
"escape hatch to Go code." The closed vocabulary is what makes
static analysis decidable.

## Where the code lives

- AST and evaluator: `internal/function/picker/picker.go`
- EDN parser: `internal/function/picker_parse.go`
- Executor integration: `internal/function/executor.go` (search for
  `routedType`)
- Haskell spec: `docs/sketch/PickerExpressions.hs`
