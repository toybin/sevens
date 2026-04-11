# Sevens — Agent Mode Design

**Date**: 2026-04-07

## The Two Modes

**Standalone mode**: Sevens calls the LLM directly (Anthropic API). Controls the full prompt. Cost gates, token counting, streaming. Good for personal use with an API key.

**Agent mode**: An external agent harness (Claude Code, ChatGPT, etc.) provides the intelligence. Sevens compiles tasks, the agent executes them. No inference cost to sevens. Good for enterprise access where the agent is already running.

Both modes use the same graph, same functions, same triples. The difference is who calls the LLM.

## `sevens prepare` — Task Compiler

`sevens prepare <function> <node-title>` reads the graph, resolves context paths, and emits a structured task for the agent to execute. No content is included — just structural information and instructions.

### Output format

```
[task] notice → "Container Strategy"
[model] text output — plain text, not JSON

[read] target
  $ sevens walk "Container Strategy"

[read] parent
  $ sevens walk "EHE Modernization" --depth 0

[read] siblings (9 nodes)
  $ sevens walk "Terraform Cleanup" --depth 0
  $ sevens walk "Cortex Platform" --depth 0
  $ sevens walk "Lending Infrastructure Design" --depth 0
  $ sevens walk "Staffing and Volunteer Management" --depth 0
  ... (5 more)

[read] context-files
  $ cat ~/Documents/sevens-context/author-bio.md

[instruction]
  <full instruction from notice.md with template vars filled>

[submit]
  $ sevens submit "Container Strategy" --function notice --output text --response-file /tmp/response.txt
  OR: $ sevens submit "Container Strategy" --function notice --output text --response "inline text here"
```

### For multi-step pipelines

```
[task] audit → "Lending Infrastructure Design"
[pipeline] 3 steps: observe → stress-test → fix

[step 1: observe]
  [delegates to: notice]
  [read] ... (same as notice prepare)
  [instruction] ... (notice instruction)
  [submit] $ sevens submit "Lending Infrastructure Design" --function audit --step observe --output text --response-file /tmp/step1.txt
  [gate] approve — review output, then:
    $ sevens accept "Lending Infrastructure Design"
    OR: $ sevens reject "Lending Infrastructure Design"

[step 2: stress-test] (only if step 1 approved)
  [delegates to: challenge]
  [read] target + history (includes step 1 output via {{prev}})
  [instruction] ... (challenge instruction, with {{prev}} = step 1 output)
  [submit] $ sevens submit "Lending Infrastructure Design" --function audit --step stress-test --output text --response-file /tmp/step2.txt
  [gate] approve

[step 3: fix] (only if step 2 approved)
  [read] target + prev (step 1 + step 2 output)
  [instruction] ... (fix instruction)
  [submit] $ sevens submit "Lending Infrastructure Design" --function audit --step fix --output ops --response-file /tmp/step3.json
```

### For composed functions with map-over

```
[task] deepen → "Some Node"
[pipeline] 2 steps: split (decompose) → enrich (elaborate × children)

[step 1: split]
  [delegates to: decompose]
  [sub-pipeline] suggest → approve → generate → approve
  ... (decompose's own multi-step prepare output)

[step 2: enrich] (after step 1 ops applied)
  [map-over] children of "Some Node" (run elaborate on each)
  [for each child]:
    [read] $ sevens walk "<child-title>"
    [instruction] ... (elaborate instruction)
    [submit] $ sevens submit "<child-title>" --function elaborate --output ops --response-file /tmp/elaborate-<n>.json
```

## `sevens submit` — Response Ingestion

`sevens submit <node-title> --function <fn> --step <step> --output <type> --response <text>` or `--response-file <path>`

Takes the agent's response and processes it exactly like the standalone mode would after an LLM call:
- Parses ops if output type is "ops"
- Logs the entry (as triples)
- Creates a suspension if there's a gate
- Displays the result
- For text output, logs as "completed"

This is the other half of `apply --dry-run`. Together they form the agent round-trip:
```
sevens prepare → agent reads instructions → agent does the work → sevens submit
```

## Node Templates

A template is a structural pattern for creating one or more nodes. Defined as EDN in `~/.config/sevens/templates/`.

### Single-node template

```clojure
;; ~/.config/sevens/templates/daily-note.edn
{:name "daily-note"
 :title-pattern "{{date}}"
 :frontmatter {:type "journal"}
 :content "# {{date}}\n\n## Morning\n\n## Tasks\n\n## Notes\n\n## Reflection\n"}
```

`sevens new --template daily-note` creates a node with today's date as title, the frontmatter, and the scaffolding. No LLM needed.

### Subtree template

```clojure
;; ~/.config/sevens/templates/pros-cons.edn
{:name "pros-cons"
 :title-pattern "{{topic}}: Analysis"
 :children
 [{:title-pattern "{{topic}}: Pros"
   :sibling-role "support"
   :content "# Pros\n\n- \n"}
  {:title-pattern "{{topic}}: Cons"
   :sibling-role "counterexample"
   :content "# Cons\n\n- \n"}
  {:title-pattern "{{topic}}: Synthesis"
   :sibling-role "continuation"
   :content "# Synthesis\n\n"}]}
```

`sevens new --template pros-cons --topic "Co-op Governance"` creates 4 nodes (root + 3 children) with typed sibling relationships.

### Journal entry template (the session-bound case)

```clojure
;; ~/.config/sevens/templates/journal-entry.edn
{:name "journal-entry"
 :parent-pattern "{{date}}"
 :parent-template "daily-note"
 :title-pattern "{{date}} — {{topic}}"
 :frontmatter {:type "entry"}
 :content "# {{topic}}\n\n"}
```

`sevens new --template journal-entry --topic "thoughts on governance"` checks if today's daily note exists. If not, creates it from the daily-note template. Then creates the entry as a child.

### Templates as LLM instruction

Functions can reference templates. Instead of "create 5-7 child nodes with scaffolding," the instruction says "use the pros-cons template for this decomposition." The LLM (or agent) stamps out the template structure and fills in the content.

In function EDN:
```clojure
{:name "analyze"
 :template "pros-cons"
 :description "Decompose a claim into supporting and opposing evidence using the pros-cons template"}
```

The prepare output would include:
```
[template] pros-cons
  Creates: root + 3 children (Pros, Cons, Synthesis)
  Sibling roles: support, counterexample, continuation
  The agent should fill in content for each child node.
```

## Typed Sibling Relationships

Siblings can have named relationships stored as triples:

```
("Pros"       :sibling/role       "support")
("Cons"       :sibling/role       "counterexample")
("Synthesis"  :sibling/role       "continuation")
("Evidence"   :sibling/role       "evidence")
("Rebuttal"   :sibling/role       "counterexample")
```

These are set by templates or by the user/agent when creating nodes. Functions can query by role:

```clojure
;; A path spec that gets only "support" siblings
{:path ["node/parent" "node/parent~"]
 :exclude-self true
 :filter {"sibling/role" "support"}
 :with ["node/content"]
 :as "supporting-nodes"}
```

The `sevens walk` output includes sibling roles when present:
```
siblings: Pros (support), Cons (counterexample), Synthesis (continuation)
```

### Predefined roles

Not a closed set — any string works. But some conventional ones:

| Role | Meaning |
|---|---|
| `continuation` | Main thread / next in sequence |
| `support` | Evidence or argument in favor |
| `counterexample` | Evidence or argument against |
| `evidence` | Empirical data or case study |
| `alternative` | A different approach to the same question |
| `meta` | Commentary about the parent, not content itself |
| `discussion` | Interactive thread (existing discuss function) |
| `summary` | Condensed version |

## Agent Skill Document

The skill doc (`.claude/skills/sevens/SKILL.md` or equivalent) teaches the agent:

1. **What sevens is** — knowledge graph over markdown, Miller's 7±2
2. **Core commands** — sync, walk, overview, tree, search, query
3. **The prepare/submit workflow** — how to read a prepared task and execute it
4. **Templates** — how to use `sevens new --template` and when to suggest templates
5. **When to decompose vs elaborate vs discuss** — heuristics
6. **Output formats** — ops JSON for mutations, plain text for analysis, suggestions for review
7. **Git integration** — sevens handles commits, the agent handles content

The skill doc does NOT contain the full content of every function's prompt — those live in the function .md files. The skill doc teaches the agent how to orchestrate, not how to think.

## What Changes in the Codebase

1. **`sevens prepare <fn> <node>`** — new command. Resolves context paths, emits structured task.
2. **`sevens submit <node> --function <fn> --output <type> --response <text>`** — new command. Ingests agent response, logs, suspends/applies.
3. **`sevens new --template <name> [--key value ...]`** — new command. Stamps out templates.
4. **Sibling role triples** — added during sync (from frontmatter `sibling-role` field) or by templates.
5. **`:filter` on path specs** — path evaluator checks predicate values on terminal nodes.
6. **Walk output enhanced** — shows sibling roles, template type.
7. **SKILL.md** — agent skill document.

## Open Questions

- **Template inheritance**: Can a template extend another? (journal-entry's parent-template already hints at this)
- **Template variables**: Just `{{date}}` and `{{topic}}`, or a richer variable system? Could templates reference the target node's properties?
- **Agent response validation**: When the agent submits a response, how strictly do we validate? The standalone mode parses ops strictly. Agent mode might need more lenient parsing with better error messages.
- **Multi-agent coordination**: If two agents (Claude Code + ChatGPT) both use sevens, do they see each other's suspensions? (Yes — triples are in the shared DB.)
