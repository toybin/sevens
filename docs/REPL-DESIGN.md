# Sevens -- REPL Design

**Date**: 2026-04-08

## Overview

An interactive REPL that wraps the existing sevens CLI internals with shorter syntax, persistent state (focus, model, backend, includes), conversational interaction modes, and navigation shortcuts. The REPL doesn't replace the CLI -- it's an alternative entry point that reuses the same pipeline runner, context resolver, and graph operations. The CLI remains for scripting, CI, and one-off commands.

### Why

The current CLI workflow for iterative exploration:

```
sevens apply notice "The Commons"
sevens apply challenge "The Commons"
sevens apply discuss "The Commons"
sevens walk "Discussion: The Commons"
# open file, type response, save
sevens apply discuss "The Commons"
sevens accept "The Commons" --with "make titles shorter"
```

That's a lot of ceremony for what is fundamentally a tight loop: focus on a node, think about it from different angles, navigate to related nodes, repeat. The REPL compresses this to:

```
The Commons> notice
The Commons> challenge
The Commons> discuss
[you]> Yeah, but what about the insurance model?
[you]> .end
The Commons> accept --with "make titles shorter"
```

### Prior Art

Influenced by sigoden/aichat's REPL patterns: dot commands for meta-operations, tab completion on everything, macros as YAML with variables, multi-line input toggles. Adapted for sevens' graph-centric workflow rather than chat-centric.

## Entering the REPL

```
$ sevens repl
sevens>

$ sevens repl "The Commons"
The Commons>
```

Optional node title argument starts the REPL with that node focused.

## Command Grammar

The REPL interprets bare input based on what the first token matches, checked in this order:

1. **Dot command** (`.model`, `.info`, `.backend`) -- meta-operations
2. **Mode input** -- if in discussion/revision/note mode, all non-dot input is mode content
3. **Navigation** (`..`, `up`, `child 3`, `c 3`, `sibling 2`, `s 2`, `root`) -- changes focus
4. **Numeric selection** (`2`, `5`) -- focuses the Nth item from the last list (search results, children, siblings)
5. **Node title** -- if the input matches a node title, focuses that node
6. **Function name** (`notice`, `elaborate`, `decompose`) -- applies function to focused node
7. **Command** (`walk`, `tree`, `overview`, `search`, `pending`, `log`, `accept`, `reject`, `query`) -- runs the command
8. **Unknown** -- error message with suggestions

Flags pass through: `decompose --model capable`, `accept --with "feedback"`, `search governance`.

### Ambiguity resolution

If a token matches both a function name and a node title, function takes precedence. Use `focus <title>` or `f <title>` for explicit navigation. In practice this rarely collides -- function names are verbs (notice, challenge, elaborate) and node titles are noun phrases.

If a token matches both a function and a command, function takes precedence. The built-in commands (`walk`, `tree`, `overview`, `search`, `pending`, `log`, `accept`, `reject`, `query`, `sync`, `status`, `revert`) are reserved and cannot collide with user-defined function names.

## The Prompt

The prompt shows the focused node. It is the primary affordance for "where am I":

```
sevens>                              # no focus
The Commons>                         # focused on The Commons
Governance Evolution Pathways>       # focused on a deeper node
[you]>                               # discussion mode
[revision]>                          # revision mode
[note]>                              # note mode
```

Long titles are truncated with ellipsis in the prompt. The full title is always available via `.info`.

## Navigation

### Focus by title

Typing a node title focuses it. Tab completion on node titles makes this fast.

```
sevens> The Commons
The Commons>
```

Explicit focus command (for when the title collides with a function/command name):

```
sevens> focus The Commons
sevens> f The Commons
```

### Relative navigation

```
The Commons> child 3                 # focus 3rd child
Cultural and Religious Organizations>
Cultural and Religious Organizations> ..      # parent
Existing Community Infrastructure>
Existing Community Infrastructure> up         # same as ..
The Commons>
The Commons> sibling 2               # focus 2nd sibling (if any)
The Commons> root                    # unfocus entirely
sevens>
```

Short forms: `c 3`, `s 2`.

### List-then-select

After any command that prints a numbered list, bare numbers navigate:

```
The Commons> children

  1. Commons Governance Models
  2. Commons Revenue and Sustainability
  3. Commons-in-a-Box Replication Model
  4. Discussion: The Commons
  5. Evidence and Precedents
  6. Existing Community Infrastructure
  7. Lending Infrastructure Design
  8. Multi-Use Space Design
  9. Serendipitous Encounter Design
 10. Staffing and Volunteer Management

The Commons> 6
Existing Community Infrastructure>
```

Works after `children`, `siblings`, `search`, `pending`.

### Children and siblings as commands

```
The Commons> children                # list children with numbers
The Commons> siblings                # list siblings with numbers
```

These are REPL-only commands (the CLI uses `walk` which shows everything).

## Function Application

Bare function names apply to the focused node:

```
The Commons> notice                  # sevens apply notice "The Commons"
The Commons> challenge               # sevens apply challenge "The Commons"
The Commons> elaborate               # sevens apply elaborate "The Commons"
```

Flags pass through:

```
The Commons> decompose --model capable
The Commons> notice --dry-run
The Commons> relate --backend codex
```

### Batch application

The `each` keyword applies a function to all children:

```
The Commons> elaborate each

Elaborating 10 children (parallel, 4 at a time)...

  [1/10] Commons Governance Models ✓
  [2/10] Commons Revenue and Sustainability ✓
  ...
  [9/10] Serendipitous Encounter Design ✓
 [10/10] Staffing and Volunteer Management ✓

[applied] elaborate × 10 children (commit e4a8b12)
```

This runs the function on each child of the focused node, using parallel execution when the backend supports it (CLI backends). A confirmation prompt shows estimated scope before running.

## Interactive Modes

### Discussion Mode

Entered by running `discuss` on a focused node. The REPL switches to a conversational interface where bare text is sent as user messages to the agent.

```
The Commons> discuss

[agent] You mention "lending infrastructure" as if it's one system,
but lending books and lending table saws have completely different
liability profiles. Have you thought about how the insurance model
works across these domains?

[you]> Yeah actually the Portland tool library uses per-item waivers.
       But I think the real issue is less about insurance and more
       about the social contract.

[agent] That's a much more interesting framing. The insurance question
has known solutions — the social contract question is where most
lending libraries actually fail...

[you]> Say more about enforcement fatigue

[agent] The pattern they describe: initially the community self-polices.
Then a few bad actors test boundaries. The org responds with formal
rules, which feel punitive to the majority...

[you]> .end

[saved] Discussion: The Commons (6 turns, commit f2a1b3c)

The Commons>
```

**Under the hood**:

1. `discuss` loads the existing discussion node (if any) as conversation history
2. The agent's first response streams to the terminal
3. The prompt changes to `[you]>` -- bare text is user input, not commands
4. Each user message triggers another agent turn with full conversation history
5. The agent always sees the parent node's content (grounding context) plus any `.include`'d nodes
6. `.end` exits discussion mode, writes the full conversation to the discussion node file, commits

**Controls within discussion mode**:

- `.end` / `.done` -- exit and save
- `.cancel` -- exit without saving (discards turns since entering discuss mode)
- `.model <tier>` -- switch model mid-conversation
- `.info` -- show current context (parent, includes, model)
- Ctrl-C -- interrupts current agent response, stays in discussion mode
- Ctrl-D -- same as `.end`
- `:::` -- toggle multi-line input
- Ctrl-O -- open external editor for longer responses

**Persistence**: The discussion node file (`Discussion: <Node Title>`) accumulates the full conversation with `[agent]` and `[user]` markers. If you exit the REPL and come back later, `discuss` picks up from the persisted file. The REPL is the interaction surface; the file is the artifact.

### Revision Mode

Entered when reviewing pending suggestions. Instead of `accept --with "feedback"`, the REPL offers an inline revision flow:

```
The Commons> pending

  1. [suggested] decompose → "The Commons"
     suggest 7 children: Commons Governance Models, Commons Revenue...

The Commons> accept

Apply these suggestions? [y/n/r]> r

[revision]> Make the titles shorter and drop the replication node,
            it's premature

[re-running suggest with feedback...]

  Revised suggestions (6 children):
  1. Governance
  2. Revenue
  3. Space Design
  ...

Apply? [y/n/r]> y

[generating...]
[applied] decompose → "The Commons" (commit 7c3d1a8)

The Commons>
```

The `[y/n/r]` prompt is simple: yes applies, no cancels (same as `reject`), revise enters revision mode where bare text is feedback. The revision cycle can repeat until the user accepts or cancels.

### Note Mode

Quick annotation without opening a file:

```
Lending Infrastructure Design> note

[note]> The per-item waiver thing from Portland is documented in their
        2024 annual report. Should track that down.

[appended] note to "Lending Infrastructure Design" (commit 8d1e2f3)

Lending Infrastructure Design>
```

Appends to the node's content (under a `## Notes` section, or at the end). Single submission -- enter sends, multi-line via `:::` or ctrl-o. `.cancel` aborts.

## Dot Commands

Meta-operations that control REPL state. Always available, in any mode.

### Information

| Command | Effect |
|---|---|
| `.info` | Show focus, backend, model, includes, root |
| `.help` | Show command reference |
| `.functions` | List available functions |

### Configuration

| Command | Effect |
|---|---|
| `.model <tier-or-id>` | Switch model (`fast`, `capable`, `powerful`, or raw ID) |
| `.backend <name>` | Switch backend (`codex`, `claude`, `anthropic`) |
| `.dry` | Toggle dry-run mode |

### Context

| Command | Effect |
|---|---|
| `.include <node>` | Add node to focus context |
| `.include clear` | Clear all includes |
| `.exclude <node>` | Remove node from focus context |

### Mode exits

| Command | Context | Effect |
|---|---|---|
| `.end` / `.done` | Discussion | Save and exit |
| `.cancel` | Discussion, revision, note | Discard and exit |

## Macros

Predefined workflows stored as YAML in `~/.config/sevens/macros/`:

```yaml
# ~/.config/sevens/macros/deep-review.yaml
variables:
  - name: node
    default: "."
steps:
  - focus {{node}}
  - notice
  - challenge
  - discuss
```

Invoked with `!` prefix:

```
The Commons> !deep-review

[macro: deep-review]
  step 1/3: notice...
  ...
  step 2/3: challenge...
  ...
  step 3/3: discuss...
  [agent] I notice the challenge raised a strong point...

[you]> ...
[you]> .end

[macro complete: 3 steps]

The Commons>
```

### Macro semantics

- Steps execute sequentially
- Variables are substituted before execution (`{{node}}` → the argument or default)
- If a step enters a mode (discuss), the macro pauses until the mode exits
- If a step hits a gate (decompose suggest), the macro pauses for user input
- `.` in macro steps refers to the focused node at time of execution
- Macros can be interrupted with ctrl-c (stops at current step, preserves completed work)

### Built-in macros (suggested)

```yaml
# deep-review: notice → challenge → discuss
# deepen: decompose → accept → elaborate each
# survey: notice each child
# audit: notice → challenge → synthesize
```

## Tab Completion

Everything completes on tab:

| Context | Completes |
|---|---|
| Empty prompt | Node titles, function names, commands |
| After `.` | Dot commands |
| After `.model` | Model tiers and IDs |
| After `.backend` | Backend names |
| After `.include` / `.exclude` | Node titles |
| After `child` / `c` | Child indices |
| After `sibling` / `s` | Sibling indices |
| After `search` | Recent search terms |
| After `!` | Macro names |

Completion sources: DB queries for node titles (same as existing Cobra completions), function directory scan for function names, config for model/backend names.

## Input Handling

### History

Readline-style, persisted to `~/.config/sevens/repl-history`. Up-arrow traverses, ctrl-r searches. History persists across REPL sessions.

### Multi-line input

- `:::` on its own line toggles multi-line mode (input submitted on second `:::`)
- Ctrl-O opens `$VISUAL` or `$EDITOR` for composing longer text
- Ctrl-J / Shift-Enter / Alt-Enter insert a newline without submitting

### Keybindings

Emacs-style by default (readline standard). The line editing library handles this.

## Streaming

Agent responses stream to the terminal in real-time. In discussion mode this is natural -- you see the agent "typing." For function application (notice, challenge, etc.), text-type output streams while ops-type output shows a progress indicator then the result summary.

## Implementation

### Framework: Bubbletea (Elm Architecture)

The REPL is a bubbletea v2 program (`charm.land/bubbletea/v2`). Bubbletea provides an event loop where all state lives in a Model, events arrive as messages, Update processes them and returns side-effect commands, and View renders the current state to a string. The framework handles terminal raw mode, redrawing, and event dispatch.

Why bubbletea over readline:
- **Persistent layout** -- a tree panel, content viewport, and input area can coexist on screen simultaneously. Readline can only show a prompt and scroll output.
- **Async operations** -- LLM calls run as `tea.Cmd` goroutines. The UI stays responsive (spinner, streaming chunks) while the call executes. No manual goroutine management.
- **In-place updates** -- batch progress, streaming text, and tree mutations render without scrolling. Readline can only append lines.
- **Composition** -- bubbles components (textinput, viewport, list, spinner) plug directly into the model as embedded fields.

### Charmbracelet Stack

| Library | Import | Role in REPL |
|---|---|---|
| bubbletea v2 | `charm.land/bubbletea/v2` | Core framework: event loop, Model/Update/View |
| bubbles | `charm.land/bubbles/v2` | Components: textinput (command prompt), viewport (content), spinner (loading), help (keybinding bar) |
| lipgloss v2 | `charm.land/lipgloss/v2` | Styling + layout composition (JoinHorizontal/Vertical for panels). Already used in `ui/render.go`. |
| glamour v2 | `charm.land/glamour/v2` | Markdown rendering for node content. Already used in `ui/render.go`. |
| huh v2 | `charm.land/huh/v2` | Structured prompts: the `[y/n/r]` accept cycle, model selection, confirmation dialogs. Embeds as a tea.Model. |
| fang v2 | `charm.land/fang/v2` | CLI entry point: wraps cobra with styled help, errors, manpages, completions. Replaces manual SilenceUsage/error formatting. |

### Two Entry Points, Shared Internals

```
sevens <cmd> [args]   -->  fang.Execute(ctx, cobraRoot)   -->  internal packages
sevens repl [node]    -->  tea.NewProgram(tui.New(...))    -->  internal packages
```

Fang wraps the existing cobra command tree for the CLI path. It provides styled help pages, styled error output, automatic version/manpage generation, and signal handling. The cobra commands themselves are unchanged -- fang is additive.

The REPL path launches a bubbletea program. Both paths call the same internal functions (`engine.RunPipeline`, `graph.BuildWalk`, `apply.LoadFunction`, etc.). No business logic duplication.

### Layout Modes

The REPL supports two layout modes, toggled with `.layout` or adapting to terminal width:

**Simple mode** (narrow terminal or user preference):

```
+----------------------------------+
|  viewport (scrollable output)    |
|  - walk content, function output |
|  - search results, log entries   |
|                                  |
+----------------------------------+
| The Commons> _                   |
+----------------------------------+
| ctrl+t tree  ctrl+l layout  ?   |
+----------------------------------+
```

Vertical stack: viewport above, textinput below, help bar at bottom. Behaves like a traditional REPL -- commands produce output that fills the viewport. Scrollable. This is the fallback for terminals under ~100 columns or when the user prefers simplicity.

**Full mode** (wide terminal, default):

```
+------------------+-------------------+
| The Commons      | # The Commons     |
| ├ Governance     |                   |
| ├ Revenue ←      | A privacy-focused |
| ├ Infrastructure | smart home...     |
| │ ├ Asset Map    |                   |
| │ ├ Informal     | ## Components     |
| │ └ Integration  |                   |
| ├ Lending        | **Lighting**...   |
| ├ Space Design   |                   |
| └ Staffing       |                   |
+------------------+-------------------+
| The Commons> _                       |
+--------------------------------------+
| ↑↓ navigate  enter focus  ? help    |
+--------------------------------------+
```

Three regions: tree panel (left), content viewport (right), textinput + help bar (bottom spanning full width). The tree panel shows the graph overview with the focused node highlighted. Arrow keys navigate the tree; Enter focuses the highlighted node and loads its content into the viewport. Commands typed in the textinput operate on the focused node.

Layout is built with `lipgloss.JoinHorizontal` for tree + content, then `lipgloss.JoinVertical` for top panels + input + help. Widths are calculated from `tea.WindowSizeMsg` minus borders/padding.

**Switching**: `.layout simple` / `.layout full` / `.layout` (toggle). Auto-detect: if terminal < 100 columns, start in simple mode.

### Model

```go
type Model struct {
    // Layout
    layout     Layout        // Simple or Full
    width      int           // terminal width (from WindowSizeMsg)
    height     int           // terminal height

    // Components
    input      textinput.Model
    viewport   viewport.Model
    tree       TreeModel          // custom: navigable graph tree
    spinner    spinner.Model
    help       help.Model
    keys       KeyMap

    // State (same as original REPLState, adapted for bubbletea)
    focusNode  string
    root       string
    backend    string
    model      string
    includes   []string
    dryRun     bool
    mode       Mode               // Normal, Discussion, Revision, Note
    lastList   []string           // for numeric selection
    loading    bool               // spinner active

    // Resources
    db         *sql.DB
    globalCfg  apply.GlobalConfig

    // Discussion mode
    discuss    *DiscussModel      // nil when not in discussion mode

    // History
    history    []string
    historyIdx int
}

type Layout int
const (
    LayoutSimple Layout = iota
    LayoutFull
)
```

### Update / Event Flow

The bubbletea Update function is the dispatch loop. When the user presses Enter in the textinput:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyPressMsg:
        switch {
        case key.Matches(msg, m.keys.Submit):
            line := m.input.Value()
            m.input.Reset()
            m.history = append(m.history, line)
            return m.dispatch(line)

        case key.Matches(msg, m.keys.TreeToggle):
            m.layout = toggleLayout(m.layout)
            m.recalcSizes()
            return m, nil

        case key.Matches(msg, m.keys.Quit):
            return m, tea.Quit
        }

    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        m.recalcSizes()
        return m, nil

    // Async results
    case walkResultMsg:
        m.loading = false
        m.viewport.SetContent(renderWalk(msg))
        m.tree.SetFocus(msg.Node.Title)
        return m, nil

    case applyResultMsg:
        m.loading = false
        m.viewport.SetContent(renderApplyResult(msg))
        return m, m.refreshTree()   // tree may have changed

    case llmStreamChunkMsg:
        m.viewport.SetContent(m.viewport.Content + msg.Text)
        m.viewport.GotoBottom()
        return m, waitForChunk(msg.Ch)  // chain next read

    case errorMsg:
        m.loading = false
        m.viewport.SetContent(renderError(msg))
        return m, nil
    }

    // Forward to active component
    if m.mode == ModeDiscussion {
        return m.discuss.Update(msg)
    }
    // ... forward to input, tree, viewport as appropriate
}
```

### Dispatch

The `dispatch` method implements the same grammar from the Command Grammar section. It parses the input line and returns a `(Model, tea.Cmd)` pair -- the model may change state (focus, mode), and the command runs the operation asynchronously:

```go
func (m Model) dispatch(line string) (tea.Model, tea.Cmd) {
    tokens := tokenize(line)
    if len(tokens) == 0 {
        return m, nil
    }

    // Mode input (discussion, revision, note)
    if m.mode != ModeNormal {
        return m.handleModeInput(line)
    }

    switch {
    case isDotCommand(tokens[0]):
        return m.handleDotCommand(tokens)
    case isNavigation(tokens[0]):
        return m.handleNavigation(tokens)
    case isNumeric(tokens[0]) && len(m.lastList) > 0:
        return m.handleNumericSelect(tokens[0])
    case m.isNodeTitle(tokens):
        return m.handleFocusNode(tokens)
    case isFunction(tokens[0]):
        return m.handleApply(tokens)
    case isCommand(tokens[0]):
        return m.handleCommand(tokens)
    default:
        m.viewport.SetContent(renderError("unknown: " + tokens[0]))
        return m, nil
    }
}
```

Each handler returns a `tea.Cmd` for async work. For example, `handleApply` sets `m.loading = true` and returns a command that calls the engine:

```go
func (m Model) handleApply(tokens []string) (tea.Model, tea.Cmd) {
    fnName := tokens[0]
    m.loading = true
    return m, func() tea.Msg {
        // This runs in a goroutine -- UI stays responsive
        fn, err := apply.LoadFunction(fnName)
        if err != nil {
            return errorMsg{err}
        }
        // ... build walk, resolve context, call RunPipeline
        return applyResultMsg{...}
    }
}
```

### View

The View function composes the layout from components:

```go
func (m Model) View() string {
    if m.layout == LayoutFull {
        treeWidth := m.width / 4
        contentWidth := m.width - treeWidth - 3  // border

        treeView := treeStyle.Width(treeWidth).Render(m.tree.View())
        contentView := contentStyle.Width(contentWidth).Render(m.viewportOrSpinner())

        topPanels := lipgloss.JoinHorizontal(lipgloss.Top, treeView, contentView)
        inputView := m.input.View()
        helpView := m.help.View(m.keys)

        return lipgloss.JoinVertical(lipgloss.Left, topPanels, inputView, helpView)
    }

    // Simple layout
    return lipgloss.JoinVertical(lipgloss.Left,
        m.viewportOrSpinner(),
        m.input.View(),
        m.help.View(m.keys),
    )
}
```

### Tree Panel

The tree panel is a custom bubbletea component (not a standard bubble). It renders the graph overview as a navigable ASCII tree using the same rendering logic as `printTree` in the CLI, but with:

- A **cursor** that highlights the selected node
- **Arrow keys** move the cursor up/down through the flattened tree
- **Enter** focuses the highlighted node (loads content, updates prompt)
- **Left/Right** collapse/expand subtrees
- The **focused node** is styled differently from the cursor (focus = what commands operate on, cursor = what you're looking at)

The tree panel queries `graph.BuildOverview` for its data and caches it. It refreshes when the graph changes (after sync, accept, decompose).

```go
type TreeModel struct {
    nodes      []TreeEntry       // flattened tree with indent levels
    cursor     int               // highlighted row
    focused    string            // currently focused node title
    collapsed  map[string]bool   // manually collapsed nodes
    width      int
    height     int
}

type TreeEntry struct {
    Title    string
    Depth    int
    IsLast   bool               // last child at this depth
    CharCount int
    Pending  string
    HasChildren bool
}
```

### Async: LLM Streaming

For functions with text output (notice, challenge, thesis), the LLM response streams into the viewport in real-time. The pattern uses bubbletea's channel-based command chaining:

```go
type llmStreamChunkMsg struct {
    Text string
    Ch   <-chan string    // channel for next chunk
    Done bool
}

func startLLMStream(cfg StreamConfig) tea.Cmd {
    return func() tea.Msg {
        ch := make(chan string, 1)
        go func() {
            // Call LLM with streaming callback that sends to ch
            defer close(ch)
            apply.CallLLMStreaming(cfg, func(chunk string) {
                ch <- chunk
            })
        }()
        // Return first chunk
        text, ok := <-ch
        if !ok {
            return llmStreamChunkMsg{Done: true}
        }
        return llmStreamChunkMsg{Text: text, Ch: ch}
    }
}

func waitForChunk(ch <-chan string) tea.Cmd {
    return func() tea.Msg {
        text, ok := <-ch
        if !ok {
            return llmStreamChunkMsg{Done: true}
        }
        return llmStreamChunkMsg{Text: text, Ch: ch}
    }
}
```

In Update, each chunk appends to the viewport and chains the next read. The spinner runs concurrently for ops-type output (no streaming).

### Discussion Mode in Bubbletea

When `discuss` runs, the model switches to `ModeDiscussion`. The View changes to a conversation layout:

```
+--------------------------------------+
| Discussion: The Commons              |
|                                      |
| [agent] You mention "lending         |
| infrastructure" as if it's one       |
| system, but...                       |
|                                      |
| [you] Yeah, the Portland tool        |
| library uses per-item waivers.       |
|                                      |
| [agent] That's a much more           |
| interesting framing...               |
|                                      |
+--------------------------------------+
| [you]> _                             |
+--------------------------------------+
| .end save  .cancel discard  ctrl+o   |
+--------------------------------------+
```

The viewport shows the conversation history (scrollable). The textinput prompt changes to `[you]>`. Enter sends the message, triggers an LLM response that streams into the viewport. `.end` saves and exits back to normal mode.

The discussion model is a separate struct embedded in the root model:

```go
type DiscussModel struct {
    parentTitle  string
    parentContent string
    turns        []Turn          // accumulated conversation
    streaming    bool            // agent currently responding
    viewport     viewport.Model  // separate viewport for conversation
    input        textinput.Model // separate input for user messages
}
```

### Revision Cycle with Huh

The accept flow uses huh for the `[y/n/r]` prompt, embedded as a tea.Model:

```go
// When user runs "accept" and there's a pending suspension:
confirmForm := huh.NewConfirm().
    Title("Apply these suggestions?").
    Affirmative("Yes").
    Negative("No")

// Or the three-way choice:
selectForm := huh.NewSelect[string]().
    Title("Apply suggestions?").
    Options(
        huh.NewOption("Apply", "apply"),
        huh.NewOption("Revise", "revise"),
        huh.NewOption("Reject", "reject"),
    )
```

When the user selects "Revise", the model enters ModeRevision -- the textinput prompt becomes `[revision]>` and bare text is feedback. On submit, the engine's `ReviseStep` is called as a `tea.Cmd`, and the cycle repeats.

### Tab Completion

The bubbles textinput component has built-in suggestion support (`SetSuggestions`, `ShowSuggestions`). Suggestions are populated dynamically as the user types:

- Empty input: show all commands + function names
- After `.`: dot commands
- After `.model`: model tiers
- After `search `: recent search terms
- Partial word: fuzzy match against node titles + function names + commands

Suggestion sources are the same as the CLI's cobra completions (DB queries for node titles, directory scan for functions) but triggered on keypress rather than tab-complete.

### Keybindings

```go
type KeyMap struct {
    Submit      key.Binding   // Enter
    Quit        key.Binding   // ctrl+c, ctrl+d
    TreeToggle  key.Binding   // ctrl+t
    LayoutToggle key.Binding  // ctrl+l
    ScrollUp    key.Binding   // ctrl+u, page up
    ScrollDown  key.Binding   // ctrl+d, page down
    TreeUp      key.Binding   // up (when tree focused)
    TreeDown    key.Binding   // down (when tree focused)
    TreeExpand  key.Binding   // right (expand subtree)
    TreeCollapse key.Binding  // left (collapse subtree)
    TreeFocus   key.Binding   // enter (focus highlighted node)
    Help        key.Binding   // ?
    Editor      key.Binding   // ctrl+o (open editor)
}
```

The help bubble auto-generates the keybinding bar at the bottom from this KeyMap.

### Integration with Existing Code

The TUI calls the same internal functions as the CLI. The boundary is clean:

| REPL Action | Internal Call | Returns |
|---|---|---|
| Focus node | `graph.BuildWalk(db, root, title, 1)` | WalkOutput for viewport |
| Navigate tree | `graph.BuildOverview(db, root, cfg)` | OverviewOutput for tree panel |
| Apply function | `engine.RunPipeline(ctx, cfg, ...)` | EvalResult (Either[Suspension, StepResult]) |
| Accept | `engine.FindSuspension` + execute ops | Applied files list |
| Revise | `engine.ReviseStep(cfg)` | New LogEntry |
| Search | `store.SearchContent` + `SearchTitles` | Title lists for viewport |
| Walk | `graph.BuildWalk` | WalkOutput for viewport |
| Log | `apply.ReadLogDB` | Log entries for viewport |

No internal package imports UI or TUI types. Data flows one way: internals return structs, the TUI renders them.

## File Layout

```
sevens/
  cmd/sevens/
    main.go                 Existing CLI (Cobra), now launched via fang.Execute()
    repl.go                 `sevens repl` command: creates tea.Program, runs it

  internal/
    tui/
      model.go              Root Model, Init, top-level Update/View, layout logic
      dispatch.go           Command parsing and dispatch (grammar from §Command Grammar)
      tree.go               TreeModel: navigable graph tree panel component
      content.go            Content rendering: walk output, function results, errors
      discuss.go            DiscussModel: discussion mode conversation UI
      revision.go           Revision mode: huh-based accept/revise/reject cycle
      note.go               Note mode: quick annotation input
      keys.go               KeyMap definitions and help text
      messages.go           All custom tea.Msg types (walkResultMsg, applyResultMsg, etc.)
      complete.go           Tab completion: dynamic suggestion providers
      styles.go             Lipgloss styles specific to TUI layout (panel borders, etc.)

    repl/                   (removed -- replaced by tui/)

    ui/
      render.go             Shared lipgloss styles + glamour rendering (used by both CLI and TUI)

~/.config/sevens/
  macros/                   User-defined macros (YAML)
  repl-history              Input history (persisted across sessions)
```

## Relationship to CLI

The REPL and CLI coexist as two entry points to the same tool:

```
sevens <command> [args]     Pure CLI, styled by fang. One command, one result, exit.
sevens repl [node]          TUI session. Persistent state, interactive navigation, modes.
```

**fang** (`charm.land/fang/v2`) wraps the cobra command tree for the CLI path. It replaces the manual `SilenceUsage`, error formatting, and help rendering with Charm-themed equivalents. Adopting fang is a ~5 line change in `main()` -- the cobra commands are untouched.

**bubbletea** (`charm.land/bubbletea/v2`) powers the REPL. It owns the terminal during a REPL session. The `sevens repl` cobra command creates a `tea.NewProgram(tui.New(db, cfg))` and calls `.Run()`.

Both paths share the same internal packages. No business logic duplication.

Commands that make sense in both contexts (walk, overview, search, apply, accept) work identically -- the TUI just renders results differently (viewport vs. stdout). Commands that are REPL-only (navigation, dot commands, modes, macros, layout toggle) don't exist in the CLI. Commands that are CLI-only (init, sync, completion) don't exist in the REPL.

`sevens sync` is still run from the CLI. The REPL reads from the DB as-is. A future `.sync` dot command could trigger a sync within the session, re-querying the tree panel afterward.

## Open Questions

- **Tree panel: bubbles list vs. custom?** The bubbles list component has filtering and fuzzy search but renders as a flat list, not a tree. A custom tree component with indent rendering is probably needed. Could wrap a list internally and override View.
- **Focus vs. cursor:** In full layout, the tree has a cursor (highlighted row) and the system has a focus (what commands operate on). These are separate -- you can browse the tree without changing focus. Enter commits the cursor position as the new focus. Is this two-concept model confusing or natural?
- **Editor integration:** Ctrl+O opens `$EDITOR` for longer input. Bubbletea needs to suspend (`tea.ExecProcess`) while the editor runs. This works but needs careful state preservation.
- **Terminal size thresholds:** What's the minimum for full layout? 100 columns? 120? Should tree panel width be configurable or proportional?
- **Macro execution in TUI:** Macros run a sequence of commands. In bubbletea, each step returns a tea.Cmd. Sequencing them requires tea.Sequence or a step-by-step runner in Update. Gates (decompose suggest) need to pause the macro and switch to revision mode. This is the most complex interaction to get right.
