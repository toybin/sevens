# Orthography Definition Layout

This directory is the intended home for versioned default definitions that back
the Markdown orthography layer described in
[MARKDOWN-ORTHOGRAPHY-DESIGN.md](/Users/dorseyj/tools/sevens/MARKDOWN-ORTHOGRAPHY-DESIGN.md).

The parser should stay small. Most meaning should live in data definitions that
the graph can version, inspect, and eventually override from user config.

Recommended split:

- `tokens.edn`
  Registered orthographic tokens and signifiers. Includes single-character and
  multi-character signifiers, allowed contexts, and matching precedence.

- `keys.edn`
  Semantic keys and their bindings. Maps tokens or word keys to emitted
  predicates and value models.

- `parsers.edn`
  Value parser definitions for payload normalization such as dates, durations,
  free symbols, or references.

- `state-machines.edn`
  Enum-like and transition-bearing semantic objects. State sets, aliases,
  transitions, and optional triggers belong here.

Function definitions should stay in `defaults/functions/`. They can later refer
to these semantic definitions indirectly by predicate names or by declaring
property requirements. The orthography schema itself should not be embedded into
individual function prompts.

User overrides should eventually live under:

- `~/.config/sevens/orthography/tokens.edn`
- `~/.config/sevens/orthography/keys.edn`
- `~/.config/sevens/orthography/parsers.edn`
- `~/.config/sevens/orthography/state-machines.edn`

That keeps the layering parallel with function overrides:

- bundled defaults live in-repo and ship with the binary
- local config can override or extend them
