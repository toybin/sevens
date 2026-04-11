# Sevens — Markdown Orthography + Open Semantic Slots

**Date**: 2026-04-08

## Summary

This document specifies a Markdown-only orthographic layer for sevens.

The goal is not to replace Markdown with a richer markup language. The goal is
to keep Markdown as the host notation and add a small amount of configurable,
block-local orthography that can witness semantic structure and derivational
state.

The key idea is:

- Markdown provides the structural host.
- Property lists provide a local orthographic witness attached to a block.
- Inline signifier-symbol atoms provide lightweight semantic marking in prose.
- Triples define what those orthographic forms mean.
- "Types" are not closed parser concepts. They emerge from derivations,
  semantic definitions, and attached predicates.

This is intentionally small. The syntax should remain readable in raw Markdown,
easy to type, and easy to extend by configuration.

## Motivation

Sevens already has a graph model and a growing notion of function composition:
nodes and groups are shaped by the morphisms that produced them. What is still
missing is a local visible orthography that can express some of that semantic
structure directly in the text.

YAML frontmatter is too coarse. It attaches to whole documents, not to local
blocks. It is also visually separated from the thing it describes.

The desired system should instead:

- attach semantics to headings and list items directly
- remain visually compact
- work in plain Markdown
- support both explicit named keys and compact signifier forms
- keep semantic categories open-ended rather than hardcoding a closed set of
  property types

## Design Principles

1. Markdown stays primary.
   Headings, list items, paragraphs, links, code fences, and ordinary Markdown
   remain the base language. The orthographic layer only adds block-attached
   property lists and inline signifier-symbol atoms.

2. Orthography is a witness, not the whole type system.
   The parser recognizes a compact surface form. The actual meaning of that
   form is defined in the graph as triples about keys, signifiers, value
   parsers, enums, state machines, references, and emitted predicates.

3. Semantic forms are open, not closed.
   "Enum", "date", "reference", "state machine", "estimate", and similar
   notions are not hardcoded slot types. They are semantic definitions that the
   orthographic layer resolves through configured graph objects.

4. Locality matters.
   A property witness should be adjacent to the block it refines. This keeps
   semantic state legible and avoids hiding meaning in document-global metadata.

5. One syntax, multiple layouts.
   A property list should mean the same thing whether written inline on one line
   or vertically across multiple lines.

6. Signifiers are configurable tokens.
   A signifier is not limited to a single character. Multi-character signifiers
   are allowed. Parsing uses registered orthographic tokens, not hardcoded
   character classes.

## Scope

This design is deliberately limited to Markdown.

Initial attachment targets:

- ATX headings
- unordered list items
- ordered list items

Out of scope for the first pass:

- non-Markdown host formats
- document-global schema blocks
- code block parsing
- full span-level AST identity
- full construction capture / instantiation machinery

Those may build on this layer later, but this document only specifies the
Markdown orthography itself and its semantic resolution model.

## Core Orthographic Forms

There are two primary orthographic constructs.

### 1. Attached Property Lists

A property list is a parenthesized slot list attached to a heading or list
item.

Examples:

```md
# (!) Heading

# Heading
(status done | assignee julian)

# Heading
(@ julian | #research)

- (x) list item

- (@ julian) list item

- list item
  (status done | ! urgent)
```

Property lists may also span multiple lines:

```md
# Heading
(
| status done
| @ julian
| #research
)
```

The multiline and single-line forms are semantically identical.

### 2. Inline Signifier-Symbol Atoms

A signifier-symbol atom is a registered signifier token immediately followed by
a symbol payload.

Examples:

```md
# Heading about #research

- Ask @julian about #sevens

This paragraph mentions ::blocked, @julian, and ~2h.
```

These atoms may appear:

- in ordinary document body text
- inside property lists

Examples:

```md
# Heading
(@julian | #research | status done)
```

## Attachment Rules

Property lists are only recognized in attachment context.

That means:

- on a heading line, a parenthesized form immediately after the heading marker
  or heading text can be parsed as an attached property list
- on the immediately following line after a heading, a standalone property list
  line can attach to that heading
- on a list item line, a parenthesized form immediately after the list marker
  or after the item text can be parsed as an attached property list
- on a continuation line under a list item, an indented property list can attach
  to that list item

Outside of attachment context, ordinary parenthesized text remains ordinary
Markdown text.

This is important for not breaking normal prose.

### Inline Heading Attachment

Examples:

```md
# (!) Heading
# Heading (@ julian | status done)
```

### Following-Line Heading Attachment

Examples:

```md
# Heading
(status done | @ julian)

# Heading
(
| status done
| @ julian
)
```

### Inline List-Item Attachment

Examples:

```md
- (x) list item
- (@ julian) list item
- list item (status done | #research)
```

### Following-Line List-Item Attachment

Examples:

```md
- list item
  (x | @ julian)

- list item
  (
  | x
  | @ julian
  )
```

## Property List Layout

A property list is a delimited slot sequence.

- `(` opens the list
- `)` closes the list
- `|` separates slots
- in multiline layout, leading `|` starts the next slot within the still-open
  property list

Examples of equivalent layouts:

```md
# Heading
(x | @ julian | #research)
```

```md
# Heading
(
| x
| @ julian
| #research
)
```

```md
# Heading
(x
| @ julian
| #research)
```

## Surface Grammar

The parser should keep the surface grammar small and leave semantics open.

Pseudo-grammar:

```text
attached-property-list :=
  "(" slot-list? ")"

slot-list :=
  slot (slot-sep slot)*

slot-sep :=
  "|" | newline-leading-pipe

newline-leading-pipe :=
  newline optional-indent "|"

slot :=
  token-and-payload
  | token-symbol

token-and-payload :=
  token ws payload

token-symbol :=
  signifier symbol

token :=
  word-key
  | registered-signifier
  | registered-orthographic-token

word-key :=
  [a-z][a-z0-9-]*

signifier :=
  registered orthographic token

payload :=
  remaining slot text after the first separating whitespace,
  trimmed but otherwise uninterpreted by the surface parser
```

This grammar intentionally does not hardcode "enum", "date", "reference",
"state", or similar semantic classes.

## Signifiers

Signifiers are configurable orthographic tokens.

Important constraints:

- a signifier may be single-character or multi-character
- signifiers are not limited to punctuation, though punctuation-like tokens will
  usually be the most useful
- longest-match wins when multiple signifiers share a prefix
- signifiers can be allowed in one or more contexts:
  - property-list token head
  - inline signifier-symbol atom
  - both

Examples:

- `#`
- `@`
- `!`
- `~`
- `::`
- `=>`

The parser must not assume signifiers are one character wide.

## Slot Forms

From the orthographic point of view, a slot is just a token head plus an
optional payload. There are, however, some common surface shapes worth naming.

### 1. Enum-Like Bare Token

Examples:

```md
(x)
(!)
(::blocked)
```

These should not be interpreted by the parser as a closed "enum slot" type.
They are simply token heads that resolve through semantic definitions. A token
may resolve to a state, an alias, a transition request, or some other semantic
object.

### 2. Named Key + Payload

Examples:

```md
(status done)
(due 2026-04-08)
(estimate 2h)
```

The key is explicit text; the semantic definition of the key decides how the
payload is parsed and normalized.

### 3. Signifier + Payload

Examples:

```md
(@ julian)
(! urgent)
(~ 2h)
(:: state-name)
```

This is a compact orthographic alias for a semantic key or transition object.

### 4. Signifier-Symbol Atom

Examples:

```md
(@julian)
(#research)
(::blocked)
```

These can appear either as property-list slots or inline in prose.

## Open Semantic Resolution

The surface parser should stop at a small number of orthographic facts.

For each slot or inline atom, it should produce something like:

- containing block
- source range
- token head
- token spelling
- raw payload text, if any
- context of occurrence

Only after that should semantic resolution happen.

This resolution step looks up triples describing what the token means.

### Why the Semantic Layer Must Stay Open

The system should not encode a closed slot taxonomy like:

- enum
- date
- reference
- number
- duration

Those may be useful semantic definitions, but they are not parser-level
categories.

Instead:

- an enum is a semantic object with members, aliases, and possibly transition
  structure
- a state machine is a semantic object with states, transitions, triggers, and
  constraints
- a date field is a semantic object whose payload parser normalizes date-like
  strings
- a person reference is a semantic object that resolves a symbol to a person or
  actor node

All of these should be definable via triples.

## Triples for Semantic Definitions

The exact predicate names may change, but the semantic model should include
objects like the following.

### Orthographic Tokens

```text
token:#          orth/token-text           "#"
token:#          orth/context              "inline"
token:#          orth/context              "property-slot"
token:#          orth/kind                 "signifier"

token::status    orth/token-text           "status"
token::status    orth/context              "property-slot"
token::status    orth/kind                 "word-key"

token::blocked   orth/token-text           "::blocked"
token::blocked   orth/context              "inline"
token::blocked   orth/context              "property-slot"
token::blocked   orth/kind                 "enum-alias"
```

### Semantic Keys

```text
sem:status       sem/label                 "status"
sem:status       sem/emits-predicate       "meta/status"
sem:status       sem/value-model           "state-machine"

sem:due          sem/label                 "due"
sem:due          sem/emits-predicate       "meta/due"
sem:due          sem/value-model           "date"
```

### Token-to-Semantic Binding

```text
token:%          sem/binds                 sem:status
token:status     sem/binds                 sem:status
token:@          sem/binds                 sem:assignee
token:#          sem/binds                 sem:tag
```

### Payload Parsing

```text
sem:due          sem/value-parser          parser:date
sem:estimate     sem/value-parser          parser:duration
sem:assignee     sem/value-parser          parser:symbol-ref
```

### Enums and State Machines

```text
sm:todo-status   sm/state                  state:undone
sm:todo-status   sm/state                  state:in-progress
sm:todo-status   sm/state                  state:done

state:done       orth/alias                "x"
state:done       orth/alias                "::done"

sm:todo-status   sm/transition             trans:todo->in-progress
trans:todo->in-progress
                 sm/from                   state:todo
trans:todo->in-progress
                 sm/to                     state:in-progress

sem:status       sem/value-model           sm:todo-status
```

This is the important point: an enum is not a primitive parser type. It is a
graph object. A state machine is also a graph object. The orthographic layer
only supplies a token or payload that resolves into one of those objects.

## Suggested Parsing Pipeline

### Phase 1: Markdown Block Parse

Parse Markdown into block nodes, at least:

- headings
- list items
- paragraphs

For the first pass, property-list attachment only needs headings and list items.

### Phase 2: Attached Property List Detection

For each eligible block:

- inspect the block line for inline parenthesized property-list syntax
- inspect immediately following lines for attached property-list syntax
- for list items, also inspect valid continuation lines

At this stage, only identify delimiters, slot boundaries, token heads, and raw
payloads.

### Phase 3: Inline Atom Detection

Within body text and property-list slots:

- detect configured signifier-symbol atoms
- skip fenced code, code spans, and other shielded contexts
- prefer longest registered signifier token

### Phase 4: Semantic Resolution

Resolve token heads through the semantic definition graph.

Outcomes may include:

- emit one or more triples
- normalize payload strings
- resolve references to named graph objects
- validate a state transition
- reject an invalid payload

### Phase 5: Derived Type / Continuation Logic

The emitted triples become part of the block's semantic state and may contribute
to emergent typing and function-selection logic.

This is where the larger construction model enters:

- a block's current "type" is partly shaped by derivation history
- attached orthographic witnesses refine or disambiguate that state
- functions can use both derivation lineage and attached orthographic predicates
  to decide valid continuations

## Triple Emission

The parser should produce triples at block scope, not just document scope.

Examples:

```md
# Heading
(status done | @ julian | #research)
```

might emit:

```text
block:123        meta/status               "done"
block:123        meta/assignee             "julian"
block:123        meta/tag                  "research"
```

Likewise:

```md
This paragraph mentions #research and @julian.
```

may emit:

```text
block:456        ref/tag                   "research"
block:456        ref/person                "julian"
```

Whether inline atoms later become their own span-level nodes is a separate
question. The initial implementation can attach them to the containing block.

## Block Identity

This design assumes that headings and list items can be assigned stable block
identities.

For a first pass, stability can be "good enough" rather than perfect. A block ID
can be derived from:

- document identity
- structural path in the Markdown AST
- local anchor text such as heading text or list-item text
- optional content hash

Pure positional identity is not sufficient for long-term structural diffing, but
it is adequate as a starting point.

The important requirement here is that emitted triples attach to a block object,
not only to a whole note.

## Examples

### Example 1: Heading with Bare Token

```md
# (x) Draft review plan
```

Possible interpretation:

- `x` resolves through a configured state machine alias
- emits `meta/status = done`

### Example 2: Heading with Named Keys

```md
# Draft review plan
(status in-progress | assignee julian | due 2026-04-08)
```

Possible interpretation:

- `status` resolves to a state-machine-backed semantic key
- `assignee` resolves to a person reference key
- `due` resolves through a date parser

### Example 3: Compact Signifier Payloads

```md
# Draft review plan
(@ julian | ! urgent | ~ 2h)
```

Possible interpretation:

- `@` binds to assignee
- `!` binds to urgency
- `~` binds to estimate

### Example 4: Inline Signifier-Symbols

```md
This work relates to #research and needs input from @julian.
```

Possible interpretation:

- mention a tag-like semantic object
- mention a person-like semantic object

### Example 5: Multiline Property List

```md
- Review current construction chain
  (
  | status in-progress
  | @ julian
  | #sevens
  | due 2026-04-08
  )
```

Same semantic content as the single-line form, just with a layout optimized for
readability.

### Example 6: Multi-Character Signifier

```md
- (::blocked) Waiting on migration details
```

Possible interpretation:

- `::blocked` is a registered orthographic alias for a particular state or
  state transition in a state machine

## Precedence and Shielding

The parser should obey the following precedence rules.

1. Markdown structure wins first.
   Headings and list items must already be recognized as Markdown blocks before
   property-list attachment is considered.

2. Code is shielded.
   Fenced code blocks and inline code spans should not be parsed for property
   syntax or inline signifier-symbol atoms.

3. Attachment context gates property-list parsing.
   A parenthesized form is only a property list when attached to an eligible
   block.

4. Longest signifier token wins.
   If both `:` and `::` are registered, `::blocked` should parse as `::` plus
   `blocked`, not as `:` plus `:blocked`.

5. Semantic validation happens after syntax.
   Surface parsing should not fail just because a token resolves to an invalid
   semantic transition. The parse succeeds; semantic validation then reports the
   problem.

## Relationship to Emergent Types

This orthographic layer does not define the full type of a block or node.

Instead, a block's effective type emerges from at least three sources:

- derivation lineage: what functions and constructions produced it
- attached orthographic witnesses: what property lists and inline atoms assert
- local graph position: what subtree, parent, siblings, and semantic relations
  it participates in

This is why the semantic layer must stay open. A block may participate in a
state machine, carry references, denote an enum member, or witness some other
semantic relation without the parser knowing any of those categories in advance.

## Initial Defaults

The first implementation can ship with a conservative default registry, while
keeping everything configurable.

Possible defaults:

- `#` for tag/context atoms
- `@` for person/owner atoms
- `!` for urgency
- `~` for estimate
- `status` for a named state-machine-backed key
- `due` for a named date-like key
- `assignee` for a named reference key

Multi-character signifiers should be supported from the beginning even if the
default registry only includes single-character ones.

## Definition Location

Bundled default definitions should live in-repo so they can be reviewed,
versioned, and shipped with the binary.

Recommended layout:

- `defaults/functions/`
  Versioned default function EDN + prompt files.
- `defaults/orthography/tokens.edn`
  Registered signifier tokens and allowed contexts.
- `defaults/orthography/keys.edn`
  Semantic keys and emitted predicates.
- `defaults/orthography/parsers.edn`
  Value parser definitions.
- `defaults/orthography/state-machines.edn`
  Enum-like and transition-bearing semantic objects.

User overrides should live in parallel under `~/.config/sevens/`.

## Implementation Notes

This design does not require a full general-purpose markup system.

It does require:

- block-level Markdown parsing
- attachment-aware property-list detection
- a token registry with longest-match behavior
- shielding of code contexts
- a semantic-resolution pass driven by triples
- block-level triple emission

The surface parser should remain intentionally small.

## Future Directions

This layer is meant to support later work, not replace it.

Future work it should enable:

- block-level provenance and structural diffing
- derivation-aware function suggestions
- construction capture / construction instantiation
- richer semantic objects such as state machines, enums, and reference systems
- span-level identity if inline atoms later need first-class node status

Those are downstream consumers of this orthographic substrate.

## Bottom Line

The Markdown layer should stay minimal:

- attached property lists
- inline signifier-symbol atoms
- configurable tokens, including multi-character ones
- open semantic resolution through triples

That is enough to make orthography a local witness of emergent semantic and
derivational structure without replacing Markdown or hardcoding a closed type
system into the parser.
