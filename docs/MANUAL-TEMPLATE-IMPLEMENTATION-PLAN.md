# Manual Template Implementation Plan

Templates are the manual, deterministic sibling of functions.

- Functions are transformational morphisms.
- Templates are first-order dependent type constructors.

They construct initial artifacts from parameters plus placement context without invoking any AI path.

## Goals

- Fast manual capture into the graph.
- EDN + Markdown pairing parallel to functions.
- Deterministic placement and insertion behavior.
- Good behavior both with full arguments and with missing arguments.
- Node-first UX by default, with later structural placement support where it adds clear value.

## Conceptual Model

Templates should mirror functions structurally, but not operationally.

- `template.edn` defines behavior, params, target, placement, and draft behavior.
- `template.md` defines the scaffold or inserted content.
- Instantiation is deterministic.
- Missing params can produce draft scaffolds instead of errors.

Templates are the constructive base layer of the type system.

- A template instantiates an initial typed witness.
- Later functions can transform or elaborate it.
- Repeated function chains over template instances can later become reusable constructions.

## Recommended Schema

Base fields:

- `:name`
- `:description`
- `:mode`
- `:title-pattern`
- `:draft-title-pattern`
- `:target`
- `:placement`
- `:params`
- `:draft`
- `:frontmatter`
- `:commit-message`

Recommended shape:

```edn
{:name "meeting-capture"
 :description "Create a meeting capture note under inbox"
 :mode "create-node"
 :title-pattern "{{person}} - {{topic}} - {{date}}"
 :draft-title-pattern "Capture {{date}} {{time}}"
 :target {:root "current"
          :parent "inbox"}
 :placement {:kind "child-last"}
 :params [{:name "person" :required true}
          {:name "topic" :required true}
          {:name "date" :default "{{today}}"}]
 :draft {:when-missing-params true
         :open true}
 :commit-message "sevens: instantiate template {{name}}"}
```

## Modes

Initial implementation target:

- `create-node`

Planned next:

- `append-node`
- `insert-block`
- `open-or-create`

## Placement

Initial implementation target:

- explicit parent node
- focused node
- configured parent like `inbox`

Planned next:

- `child-last`
- `child-first`
- `append-node`
- `prepend-node`
- `after-last-block`
- `under-heading`
- `after-matching-block`

## Params

Template params should support:

- required vs optional
- defaults
- positional convenience from CLI arguments
- builtins like `today`, `time`, `timestamp`

If required params are missing and draft mode is enabled:

- create a draft artifact anyway
- preserve placeholders in the content scaffold
- use `draft-title-pattern` or a generated fallback title
- optionally open the result in `$EDITOR`

## Markdown Sidecars

The Markdown template should support:

- `{{param}}`
- `{{today}}`
- `{{time}}`
- `{{timestamp}}`
- `{{focused-node}}`
- `{{target-node}}`

For block-relative placement later:

- `{{block-path}}`
- `{{block-scope}}`

## CLI

Initial implementation target:

- `sevens templates`
- `sevens new --template <name> [title] --set key=value`
- `sevens capture [title]`

Planned next:

- `sevens template <name>`
- `sevens instantiate <template> [args...]`

## REPL

Initial implementation target:

- `.templates`
- `capture [title]`

Planned next:

- `template <name>`
- `instantiate <name> ...`

## Logging

Template instantiation should be logged as deterministic constructor events.

Planned event names:

- `template-instantiated`
- later, block-level placement events when insertion modes exist

## Implementation Phases

### Phase 1

- bundled + user template loading
- EDN + Markdown pairing
- param definitions and builtins
- draft scaffold behavior
- `create-node` mode only
- inbox capture template
- CLI template listing

### Phase 2

- append-to-node templates
- `$EDITOR` open behavior
- REPL template commands
- better preview / dry-run output

### Phase 3

- block-relative placement
- section-aware insertion
- provenance hooks for template-created nodes and blocks

## Current Implementation Decision

Do not over-unify execution with functions.

- Reuse loader/config patterns where helpful.
- Keep template runtime deterministic and separate from the LLM pipeline.

## Immediate MVP Templates

- `inbox-capture`
- `daily-note`
- `meeting-capture`
- `discussion-thread`

The first implementation slice should make `sevens capture` and `sevens new --template inbox-capture` genuinely useful before broader placement machinery is added.
