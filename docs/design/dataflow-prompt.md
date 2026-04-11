# Dataflow: Prompt Construction Pipeline

Status: draft v1
Not a concept -- documents the data transformation pipeline from
function definition to LLM call.

---

## Why This Matters

The prompt construction pipeline is where three concerns meet:
Function (what context to gather and what template to use), Backend
(how to format for a specific LLM), and the user's content (from
the knowledge base). Getting it wrong produces bad AI output.
The current code has two parallel prompt rendering paths (simple
and full-context) that diverge in subtle ways.

---

## Pipeline Stages

```
Stage 1: Context Resolution (Function concept)
  Input: step.Requires, step.Paths, target node
  Process: walk graph via KB, gather parent/children/siblings/
           cross-refs/history per Require declarations.
           Walk custom PathSpecs via graphops.Compose.
  Output: ResolvedContext

Stage 2: Template Rendering (Function concept)
  Input: step.Backend.PromptTemplate, ResolvedContext
  Process: substitute {{variables}} into template
  Output: rendered prompt text (String)

  Supported variables:
    {{title}}           target node title
    {{content}}         target node body
    {{parent}}          parent node title
    {{children}}        comma-separated child titles
    {{siblings}}        comma-separated sibling titles
    {{prev}}            output from prior pipeline step
    {{instruction}}     ad-hoc user instruction (if function.AdHoc)
    {{timestamp}}       current ISO timestamp
    {{role.title}}      title of node resolved for named role
    {{role.content}}    content of node resolved for named role
    {{pathAs}}          titles resolved from named PathSpec

  Block-specific (when target is a block):
    {{block.path}}      block dotted path
    {{block.kind}}      heading, paragraph, list-item, task
    {{block.text}}      block text content
    {{block.markdown}}  block rendered as markdown
    {{block.scope}}     heading chain as "A > B > C"

Stage 3: System Prompt Assembly (Backend-specific)
  Input: step.Backend.SystemPrompt, step.Backend.Persona,
         agent exploration tier, capabilities
  Process:
    - Start with function's system prompt (if any)
    - Prepend persona framing (if any)
    - Prepend context policy preamble (closed/scoped)
    - Inject capability/MCP server references
  Output: system prompt text (String)

Stage 4: Prompt Packaging (Backend-specific)
  Input: rendered prompt, system prompt, model ID
  Process: construct RenderedPrompt { System, User, Model }
  Output: RenderedPrompt

Stage 5: Transport (Backend implementation)
  Input: RenderedPrompt
  Process:
    - Anthropic API: separate system/user messages, streaming
    - Claude CLI: concatenate system+user with separator, pipe stdin
    - Codex CLI: concatenate system+user, exec with flags
    - Agent mode: format as checklist, return for external execution
    - Deterministic: ignore prompt, compute from context directly
  Output: raw text response (String)

Stage 6: Output Parsing (Function concept)
  Input: raw text, step.Output.Shape
  Process:
    - ShapeText: return as-is
    - ShapeStructured: parse as JSON (suggestions list)
    - ShapeFileOps: parse as JSON []FileOp, strip code fences,
      handle truncation, validate each op
  Output: TransformResult { Raw, Ops, IsText }
```

---

## Current Implementation Gaps

### 1. Output parsing not in the adapter

`LLMBackend.Execute` returns raw text with `IsText: true` always.
It doesn't know the step's output shape, so it can't parse ops.

Fix: either pass output shape to the backend adapter, or parse
at the Executor level after `Execute` returns:

```go
// In Executor.executeStep, after backend.Execute:
if step.Output.Shape == ShapeFileOps {
    ops, err := parseOps(result.Raw)
    if err == nil {
        result.Ops = ops
        result.IsText = false
    }
}
```

The `parseOps` function already exists in `apply.ParseOps`.

### 2. PathSpec resolution stubbed

`context.go` resolves Requires (target, parent, children, siblings)
but PathSpec resolution is `nil` placeholder. Functions that declare
custom Context paths (bridge, synthesize) won't get their context.

Fix: wire `graphops.Compose` into PathSpec resolution:

```go
for _, ps := range step.Paths {
    if ps.As == "" { continue }
    subject := kb.NodeSubject(root, target)
    path := graphops.ParsePath(ps.Path)
    terminals, err := k.Graph().Compose(ctx, subject, path)
    // resolve terminal subjects to titles
    rc.Paths[ps.As] = resolveTitles(terminals)
}
```

### 3. Missing prompt variables

Variables not yet substituted by `RenderPrompt`:
- `{{instruction}}` -- the ad-hoc instruction flag
- `{{timestamp}}` -- current time
- Block-specific: `{{block.path}}`, `{{block.kind}}`, etc.
- Context files: `{{context}}` -- loaded external files
- History: `{{history}}` -- formatted log entries
- Cross-walk: `{{cross-walk}}` -- output from another function

The old `apply.RenderWithContext` handles all of these. The new
`function.RenderPrompt` handles only the basics.

### 4. Revision prompt construction

`Executor.Revise` manually appends revision history XML to the prompt.
This should use the same template rendering path with a revision-aware
template variable (e.g., `{{revision_history}}`), not string
concatenation.

---

## Context Policies (shorthand)

Functions can declare a context policy instead of explicit Requires:

| Policy | What it resolves |
|---|---|
| `minimal` | target only |
| `neighborhood` | target + parent + siblings + children |
| `full` | target + parent + siblings + children + cross-refs + history |

These are sugar. In the new architecture, they should expand to the
equivalent set of Requires at function load time, not at resolution
time. This eliminates the dual rendering path (simple vs. full-context)
that exists in the old code.

---

## Relationship to Concepts

This dataflow crosses two concept boundaries:
- **Function** owns stages 1, 2, and 6 (context, template, parsing)
- **Backend** owns stages 3, 4, and 5 (system prompt, packaging, transport)

The boundary is the `TransformBackend.Execute(RenderedPrompt)` call.
Everything before it is Function's responsibility. Everything inside
it is the backend's.

Stage 6 (output parsing) is awkward: it's Function's concern (the
function defines the output shape) but currently happens at the CLI
boundary. It should happen in the Executor, after `Execute` returns.
