# Concept: Configuration

Status: draft v1

---

## Name and Type Parameters

```
concept Configuration [Root]
```

Parameterized by Root -- each sevens root can override global settings.

---

## Purpose

To let users customize tool behavior, function definitions, template
definitions, and backend settings without modifying code.

---

## Operational Principle

If you place a function definition file in `~/.config/sevens/functions/`,
it overrides the bundled default with the same name. If you create a new
file there, it becomes available as a new function. If you edit
`~/.config/sevens/config.edn`, the tool uses your settings on the next
invocation. Per-root `.sevens.edn` overrides global settings for that
root.

---

## State

```
state:
  a GlobalConfig with
    a defaultModel LLMConfig
    a namedModels map of (String -> LLMConfig)
    a defaultBackend String
    a backends map of (String -> BackendConfig)
    a systemPrompt String
    a contextFiles seq String
    a costThreshold Float
    a theme one of (light, dark)

  a set of RootConfigs with
    a path String (unique)
    a alias lone String
    a maxChars lone Int
    a groups map of (String -> Group)

  a set of FunctionDefs loaded from
    user dir (~/.config/sevens/functions/) with
    bundled fallback (defaults/functions/)

  a set of TemplateDefs loaded from
    user dir (~/.config/sevens/templates/) with
    bundled fallback (defaults/templates/)

  a set of RegisteredRoots (seq String)
    -- paths registered via `sevens init` or `sevens sync`
```

---

## Actions

```
actions:
  loadGlobalConfig (): (config: GlobalConfig)
    requires: --
    effects: reads ~/.config/sevens/config.edn, applies defaults.

  loadRootConfig (root: String): (config: RootConfig)
    requires: root directory exists with .sevens.edn
    effects: reads and parses .sevens.edn.

  loadFunction (name: String): (fn: FunctionDef)
    requires: --
    effects: searches user dir, falls back to bundled. Loads EDN
    + markdown sidecar prompt.

  loadTemplate (name: String): (tmpl: TemplateDef)
    requires: --
    effects: searches user dir, falls back to bundled.

  listFunctions (): (names: seq String)
  listTemplates (): (names: seq String)

  registerRoot (path: String): ()
    requires: path is a valid directory.
    effects: adds to RegisteredRoots (idempotent).

  resolveModel (name: String): (config: LLMConfig)
    requires: --
    effects: looks up namedModels, inherits from defaultModel.
```

---

## What This Concept Does NOT Do

- Execute functions or templates (that's Function concept)
- Store graph state (that's Graph/KB)
- Manage files (that's Projection)

---

## Discussion

This concept exists because the current code has config loading
scattered across `apply.LoadGlobalConfig`, `apply.LoadFunction`,
`apply.LoadTemplate`, `graph.LoadConfig`, `store.LoadRoots`, and
`backend.FromConfig`. These are all "read config and construct something"
operations that should have one home.

The question is whether this is really a concept (user-facing, purposive)
or just a utility package. The user DOES interact with it: they edit
config files, they create function definitions, they register roots.
But the purpose ("customize the tool") is generic. It might be better
as infrastructure that each concept uses, rather than a concept itself.

For now, documenting it as a concept catches the scattered config
loading and gives it a single spec to evaluate against.
