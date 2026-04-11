# Concept: Function

Status: draft v1 -- working through via discussion
Layer: above KnowledgeBase (layer 3). Operates on the PKM-structured graph.

---

## Name and Type Parameters

```
concept Function [KnowledgeBase]
```

Parameterized by KnowledgeBase. Uses KB's predicate vocabulary and
queries to gather context and apply results, but does not depend on
any particular execution backend (LLM, deterministic code, etc.).

---

## Purpose

To define and execute named transformations on knowledge base nodes,
where each transformation specifies what context to gather from the
graph, what output to produce, and how steps compose into pipelines.

**Discussion notes**:

A function is not "an AI operation." It is a typed arrow: input context
(a subgraph gathered by composing predicates) goes in, structured output
(graph mutations, text, or both) comes out. The transformation between
input and output is a black box to this concept -- it could be an LLM
call, a deterministic computation, a template expansion, or anything
else that satisfies the contract.

This is arrow composition at a higher level than layer 2. Layer 2's
`compose` walks predicate arrows through the triple store. This concept
composes *transformation* arrows over the knowledge base. Same
structural principle, different scale.

The Category K connection: functions are the mechanism of Channel 2
(being informed). They bring genuinely new structure into the graph.
But Channel 1 functions (notice, bridge, relate) also exist -- they
traverse existing structure and surface patterns without creating new
nodes. The distinction between Channel 1 and Channel 2 is in the
output type (text-only vs. graph-mutating), not in the function
machinery.

---

## Operational Principle

If you define a function with input paths, a transformation, and an
output signature, you can apply it to any node. The system gathers
context by walking the specified paths from the target node, passes
that context to the transformation, and produces output conforming to
the declared signature.

A pipeline is a composition of functions, and it is curried. Executing
a pipeline partially applies it: the first step runs, and the result
is a partially applied pipeline waiting at the next step. If a step is
gated, the pipeline enters a review state machine at that point:
pending → (revise | accept | reject). Revision re-executes the current
step with human feedback and returns to pending. Accept advances the
pipeline to the next curry application. Reject terminates.

The review loop is local to each gate -- it doesn't re-run prior steps
or affect future ones. It is "keep re-applying this step until the
human is satisfied." Then the pipeline continues.

---

## State

```
state:
  a set of FunctionDefs with
    a name String (unique)
    a steps seq StepDef
    a contextPolicy one of (minimal, neighborhood, full)
    a outputType one of (text, file-ops, mixed)

  a set of StepDefs with
    a name String
    a requires set of Require
    a pathSpecs set of PathSpec
    a inputSignature Signature
    a outputSignature Signature
    a gate lone GateSpec
    a controlFlow lone ControlFlow
    a composedOf lone FunctionDef
    a mapOver lone PathSpec
    a backendSpec BackendSpec

  a set of ControlFlows with
    a kind one of (sequence, loop, branch)
    a loopTermination lone one of (user, condition)
      -- user: loop until the user explicitly ends (.end)
      -- condition: loop until output satisfies a predicate
    a loopAccumulatorPolicy lone one of (replace, append)
      -- replace: each iteration's result replaces the prior
      -- append: each iteration's result appends to an
      --   accumulating result (e.g., conversation transcript)
    a branchCondition lone String
      -- for branch: predicate on the step's output that
      -- determines which subsequent step to take
    a branchTargets lone map of (String -> StepDef)
      -- for branch: output value -> next step
      -- NOTE: FlowBranch is defined in types but not exercised in
      -- tests or the pipeline executor.

  a set of Signatures with
    a shape one of (text, structured, file-ops)
    a schema lone String
      -- for structured output: the JSON schema or equivalent
      -- that the result must conform to. Backend-agnostic.

  a set of GateSpecs with
    a revisable Boolean
      -- whether revise is available at this gate
    a historyPolicy one of (none, latest, full)
      -- none: backend sees only current context + feedback, no
      --   prior attempts. Stateless retry.
      -- latest: backend sees the most recent attempt + feedback.
      --   Enough for simple corrections.
      -- full: backend sees the complete revision chain --
      --   seq of (attempt, feedback) pairs in order. Required
      --   when feedback references specific parts of prior
      --   attempts ("the third child doesn't belong").
    a cancelable Boolean
      -- whether cancel (discard entire pipeline) is available
    a autoAccept Boolean
      -- if true, skip review and apply immediately
    a rollbackOnReject Boolean
      -- if true, rejecting undoes prior accepted steps
      -- NOTE: declared but not yet enforced in the pipeline executor.

  -- pipeline state (live, mutable, owned by this concept)
  a set of Pipelines with
    a id String (unique)
    a functionName String
    a target String
    a currentStep Int
    a state one of (Running, Pending, Accepted, Looping,
                     Rejected, Cancelled, Completed)
    a currentResult lone TransformResult
    a accumulator lone TransformResult
      -- for looping steps: the accumulated result across iterations
    a revisionChain seq RevisionEntry
      -- for gated steps: the history of (attempt, feedback) pairs
    a priorStepResults seq TransformResult
      -- accepted results from earlier steps in the pipeline

  a set of RevisionEntries with
    a attempt TransformResult
    a feedback String

  a set of BackendSpecs with
    a kind one of (llm, deterministic, agent)
    a promptTemplate lone String
      -- for llm: the template to render with resolved context
    a persona lone String
      -- for llm: behavioral framing
    a systemPrompt lone String
      -- for llm: system-level instructions
    a handler lone String
      -- for deterministic: reference to the code that executes

  a set of PathSpecs with
    a name String
    a path seq String
      -- sequence of predicates to walk (with ~ for inverse)
    a fetchPredicates set of String
      -- additional predicates to retrieve at terminal nodes

  a set of Requires with
    a role String
      -- semantic name: "target", "parent", "siblings", "history"
    a pathSpec PathSpec
      -- how to resolve this role from the target node
    a optional Boolean
```

**Discussion notes**:

The state is function *definitions*, not executions. A function
definition is a recipe: what to gather, how to transform, what to
produce. The execution state (resolved context, in-flight prompts,
partial results) is transient and belongs to whichever mechanism is
running the function.

**PathSpec** is the key structure. It's how a function declares its
input type -- which arrows to compose from the target node to reach
the context it needs. `["node/parent", "node/parent~"]` means
"go to parent, then find all children of parent" -- i.e., siblings.
The path vocabulary comes from KnowledgeBase (layer 3), but the
composition mechanism is generic.

**Require** assigns semantic names to resolved paths. A function
doesn't say "walk node/parent and give me whatever's there." It says
"I need the 'parent' role resolved via this path spec." The role name
is used in prompt rendering -- `{{parent.content}}` substitutes the
resolved parent's content.

**StepDef** is one step in the pipeline. Steps can be:
- **Direct**: has a promptTemplate, resolved and executed.
- **Composed**: delegates to another function (`composedOf`).
- **Map-over**: runs a sub-pipeline for each node found via a path
  (`mapOver`).
- **Gated**: pipeline suspends after this step for human review.

The step definition separates the **signature** (input/output contract,
backend-agnostic) from the **backend spec** (how to fulfill that
contract for a specific execution mechanism). The signature is what
makes composition type-checkable: step N's output signature must be
compatible with step N+1's input requirements. The backend spec is
what a particular executor uses to produce conforming output.

For LLM backends, the prompt template communicates the contract to
the model: "given this context, return JSON matching this schema."
For deterministic backends, a handler reference points to code that
produces conforming output directly. For agent mode, the signature
alone may suffice -- the external AI reads the contract and figures
out how to fulfill it.

The function concept never inspects the backend spec. It validates
signatures for compatibility and delegates execution.

---

## Actions

```
actions:
  define (def: FunctionDef): ()
    requires: no function with the same name exists (or updates
    existing).
    effects: registers the function definition. Validates that
    step composition is well-typed: each step's output signature is
    compatible with the next step's input requirements.

  apply (functionName: String, target: String,
         backend: TransformBackend): (pipeline: Pipeline)
    requires: function exists. Target node exists in KB.
    effects: creates a Pipeline (the curried application). Executes
    the first step:
      1. resolveContext: walk path specs from target
      2. renderPrompt: substitute context into backend spec
      3. backend.execute(prompt) -> result
      4. validate result against step's output signature
    If the step is gated: pipeline state becomes Pending(result).
    If ungated: applyResult and advance to next step.
    Returns the pipeline in its current state.

  advance (pipeline: Pipeline): (pipeline: Pipeline)
    requires: pipeline is in Accepted state.
    effects: applies the accepted result to the knowledge base,
    then executes the next step (same as apply's step execution).
    If no more steps: pipeline state becomes Completed.

  accept (pipeline: Pipeline): (pipeline: Pipeline)
    requires: pipeline is in Pending state.
    effects: pipeline state becomes Accepted. Does NOT apply the
    result yet -- that happens on advance. (Separation allows the
    caller to inspect before side effects.)

  reject (pipeline: Pipeline): (pipeline: Pipeline)
    requires: pipeline is in Pending state.
    effects: pipeline state becomes Rejected (terminal).

  revise (pipeline: Pipeline, feedback: String,
          backend: TransformBackend): (pipeline: Pipeline)
    requires: pipeline is in Pending state. Current gate is revisable.
    effects: appends (currentResult, feedback) to the gate's
    RevisionChain. Re-executes the current step with:
      - original resolved context (always)
      - feedback (always)
      - revision history per historyPolicy:
          none:   nothing -- stateless retry with feedback only
          latest: the most recent (attempt, feedback) pair
          full:   the complete RevisionChain in order
    Validates new result against the same output signature.
    Pipeline state returns to Pending with the new result.

  cancel (pipeline: Pipeline): (pipeline: Pipeline)
    requires: pipeline is in Pending state. Current gate is cancelable.
    effects: pipeline state becomes Cancelled. If gate has
    rollbackOnReject, undoes results of prior accepted steps.
```

**Pipeline as state machine**:

```
pipeline states:
  Running     -- step is executing (transient)
  Pending     -- gated step produced a result awaiting review
  Accepted    -- human approved the pending result
  Rejected    -- human rejected current step (terminal)
  Looping     -- in a loop, accumulating results across iterations
  Cancelled   -- entire pipeline discarded (terminal)
  Completed   -- all steps executed and applied (terminal)

transitions:
  apply      -> Running -> Pending    (if gated, not autoAccept)
  apply      -> Running -> advance    (if ungated or autoAccept)
  apply      -> Running -> Looping    (if loop, after first iteration)
  accept     -> Pending -> Accepted
  reject     -> Pending -> Rejected   (+ rollback if configured)
  cancel     -> Pending -> Cancelled  (+ rollback if configured)
  revise     -> Pending -> Running -> Pending (if revisable)
  advance    -> Accepted -> Running -> Pending | Looping | Completed
  continue   -> Looping -> Running -> Looping (next iteration)
  end        -> Looping -> advance   (break loop, continue pipeline)
  cancel     -> Looping -> Cancelled (discard loop and pipeline)

gate configuration determines which transitions are available:
  revisable:        enables revise transition
  historyPolicy:    controls revision chain visibility
  cancelable:       enables cancel transition
  autoAccept:       skips Pending, goes directly to advance
  rollbackOnReject: reject/cancel undoes prior accepted steps

loop configuration determines iteration behavior:
  loopTermination:       user (.end) or condition (output predicate)
  loopAccumulatorPolicy: replace or append across iterations
```

The pipeline IS the curried application. Each state is a partial
application point. The gate and control flow configuration at each
step determines which sub-machine transitions are available. Different
functions can have different review ergonomics and iteration patterns
without changing the pipeline machinery.

**Discussion as a function**: a single looping step with user
termination, append accumulator, gated within each iteration. The
user's response is the continue action's input. `.end` breaks the
loop and commits. `.cancel` breaks the loop and rolls back. The
discussion node and its turns are the accumulating TransformResult.

**Discussion notes**:

`TransformBackend` is the abstraction over execution mechanisms.
The function concept defines the interface:

```
interface TransformBackend:
  execute (prompt: RenderedPrompt): (result: TransformResult)
```

An LLM backend sends the prompt to an API and parses the response.
A deterministic backend ignores the prompt and computes directly from
the resolved context. An agent-mode backend returns the prompt as a
checklist for an external AI to execute. The function concept doesn't
care which.

Pipeline state is persisted as triples in the graph (using function-owned
predicates like `pipeline/state`, `pipeline/step`, `pipeline/result`,
etc.). This is what lets you close the terminal, come back later, and
resume -- the curried pipeline remembers where it paused.

Pipeline state is **operational**, not presentational. It drives the
state machine. Log entries (KnowledgeBase `log/*` predicates) are
written as side effects of pipeline transitions -- they're the
historical record for the user and for temporal queries. Nothing reads
log entries to determine pipeline behavior. The pipeline owns its own
state; the log is a projection of what happened.

Sync: when a pipeline transitions (accept, reject, revise, complete),
a log entry is asserted in the knowledge base recording the transition,
timestamp, function, target, step, and result summary.

---

## Queries

```
queries:
  list (): (names: set String)
    -- all defined function names

  describe (functionName: String): (def: FunctionDef)
    -- the full definition of a function

  stepsFor (functionName: String): (steps: seq StepDef)
    -- the ordered steps of a function's pipeline

  requiredContext (functionName: String, stepIndex: Int):
      (requires: set Require)
    -- what roles need to be resolved for a step

  availableFunctions (target: String): (names: set String)
    -- which functions are applicable to a given node,
    -- based on whether their context requirements can be
    -- satisfied (e.g., a function requiring siblings is not
    -- applicable to a root node with no siblings)
```

---

## What This Concept Does NOT Do

- **Execute transformations**: the actual LLM call or deterministic
  computation is external. The function concept orchestrates but
  delegates execution to a backend.
- **Own the log**: log entries are KnowledgeBase predicates. This
  concept writes them as side effects of pipeline transitions (via
  sync), but doesn't query or manage them.
- **Define the transformation contract beyond prompt/result**: the
  concept doesn't specify how an LLM should be called, what model to
  use, what temperature, etc. Those are backend configuration, not
  function definition.

---

## Composition

Functions compose in three ways, all within the pipeline mechanism:

**Sequential steps**: step 1 output feeds step 2 input. The output of
a text step becomes available as `{{prior_output}}` in the next step's
prompt template. The output of a file-ops step modifies the graph,
which changes what the next step's context resolution finds.

**Delegation (composedOf)**: a step delegates to another function
entirely. The composed function runs its own pipeline against the same
(or a derived) target. This is function-level arrow composition.

**Map-over (mapOver)**: a step runs a sub-pipeline for each node found
by a path spec. "For each child, run the elaborate function." This is
a functor -- mapping a function over a structure.

**Loop**: a step repeats until a termination condition (user action or
output predicate). Each iteration's result either replaces or appends
to an accumulator. This is where discussion lives -- a loop with user
termination and an appending accumulator.

**Branch**: based on step output, the pipeline takes different paths.
"If more than 7 children proposed, run a consolidation step first."

**Type checking at definition time**: when a function is defined, the
system can validate that step composition is well-typed. A step that
produces text followed by a step that expects file-ops input is a
type error. A composed step whose target function doesn't exist is a
reference error.

---

## Relationship to Category K

Functions are the operational mechanism of T-crossing. The graph is
synchronic -- a snapshot. A function introduces diachronic change:
new triples that didn't exist before.

- **Channel 1 functions** (notice, bridge, relate): output type is
  text. They traverse existing structure (Channel 1 -- recognition)
  and surface patterns. The graph doesn't change. The observer's
  understanding changes, but the partition stays the same.

- **Channel 2 functions** (elaborate, decompose, scaffold): output
  type is file-ops. They bring genuinely new structure into the graph
  (Channel 2 -- being informed). The partition grows. New nodes enter
  the equivalence class.

The **gated step** is the T-crossing made explicit. The function
proposes a change (the diachronic possibility). The gate suspends.
The human reviews (the tick happens). On acceptance, the partition
changes. The human's approval is what makes the transition real --
without it, the proposed change is a possibility, not knowledge.

Path specs are arrow composition applied to context gathering. The
function's input type is defined by which arrows to compose from the
target node. This is the same mechanism as layer 2's `compose`, but
applied to "what should the transformation see?" rather than "what
is the query result?"

---

## Open Questions

1. **Resolved: signature vs. backend spec.** The function's signature
   (input/output contract) is backend-agnostic and lives in the
   function definition. The backend spec (prompt template, handler
   reference, etc.) is how a specific executor fulfills that contract.
   The signature is the source of truth for composition type-checking.
   The backend spec is execution-layer detail.

2. **Context policy**: the current system has three levels (minimal,
   neighborhood, full). These are shortcuts for "which path specs to
   use." Should context policy be eliminated in favor of explicit
   path specs on every function, or does the shorthand serve a real
   purpose (quick definition of simple functions)?

3. **Resolved: revision.** Revision is a function action (`revise`)
   operating on pipeline state at a gate. The RevisionChain is local
   to each gate and controlled by `historyPolicy`. Feed-forward of
   revision-relevant information to later steps is a prompt convention
   (instruct the backend to include it in the accepted result), not
   pipeline machinery.

4. **Agent mode**: agent mode looks like a TransformBackend where
   `execute` returns the prompt as a structured checklist rather than
   calling an LLM. The external AI reads the checklist, does the work,
   and submits a result that gets validated against the output
   signature. `prepare` = create pipeline + render first step for
   external execution. `submit` = provide a TransformResult that
   the pipeline validates and transitions on. Does this fully reduce
   to a backend choice, or does the async/external nature of agent
   mode require pipeline state to handle "waiting for external
   submission" as a distinct state?

5. **Function as arrows between types**: if nodes have types (subgraph
   shapes per K), a function is an arrow between types -- it takes a
   node of type A and produces nodes of type B. Can/should function
   definitions declare their input and output types in terms of
   subgraph shapes? This would enable richer type checking and
   composition validation.

6. **Signature representation**: what does a Signature actually look
   like? For structured output, a JSON schema is one option. But if
   the output is "create child nodes with these properties," the
   schema is really describing a subgraph shape (back to K's types
   as subgraph shapes). The signature format might need to express
   graph-structural constraints, not just data shapes.

---

## Concepts Absorbed

The following things that initially looked like separate concepts
collapse into configurations of the function concept:

- **Suspension**: the Pending/Accepted/Rejected pipeline state at
  gated steps. Not a separate concept -- it's the review sub-machine
  at each curry point. Pipeline state persistence replaces the old
  `suspension:*` triples. **Status: fully absorbed.**

- **Discussion**: a function with a single looping step
  (user-terminated, append accumulator, cancelable). Each iteration
  produces a conversation turn. The transcript is the accumulating
  result. `.end` commits, `.cancel` rolls back. **Status: absorbed
  (looping step implemented).**

- **Templates**: deterministic functions. The template definition is a
  function with a deterministic backend spec instead of an LLM prompt.
  Same signature, same pipeline machinery, same output validation. A
  template that creates multiple nodes with a review step is a gated
  deterministic pipeline. **Status: partially absorbed -- the old
  template system is still active via convert.go bridge. Migration to
  the function concept is incomplete.**

This reduces the concept count significantly. The function concept is
large but coherent: it's all "typed transformations on the knowledge
base, composed as state machines." The variety is in the configuration
(backend spec, gate spec, control flow), not in the machinery.
