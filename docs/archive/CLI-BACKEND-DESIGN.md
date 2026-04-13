# Sevens -- CLI Backend Design

**Date**: 2026-04-08

## Overview

Invert the inference architecture: instead of calling the Anthropic API directly, outsource inference to CLI coding agents (OpenAI Codex, Claude Code) invoked programmatically. Sevens retains full ownership of context resolution, prompt rendering, output parsing, pipeline management, and graph operations. The CLI is a text-in/text-out inference pipe that brings its own model access, authentication, and (for scoped exploration) external tool integrations.

### Why

- **No API key required** -- users bring their existing subscription (ChatGPT Enterprise, Claude Max, Copilot Enterprise)
- **No per-token billing** -- covered by flat-rate plans or employer seats
- **Model flexibility** -- Codex offers o3/o4-mini/gpt-5/gpt-5.4, Claude offers Opus/Sonnet/Haiku. Mix per model tier.
- **Enterprise compliance** -- inference stays within the org's approved channel
- **Parallel execution** -- independent CLI processes, no shared rate limits

### Architectural Fit

The current `CallLLM` function is already the thinnest layer in the stack:

```go
func CallLLM(ctx context.Context, config LLMConfig, systemPrompt, prompt string, streamTo io.Writer) (string, error)
```

It takes rendered text, returns rendered text. Everything upstream (path evaluation, context resolution, template rendering) and downstream (ops parsing, suspension management, git integration) is untouched. The CLI backend is a direct substitution at this seam.

## Backend Interface

```go
type Backend interface {
    Complete(ctx context.Context, req InferenceRequest) (string, error)
}

type InferenceRequest struct {
    SystemPrompt   string
    Prompt         string
    Model          string
    Exploration    string   // "closed", "scoped"
    ReadOnly       bool
    AllowFileReads bool
    Capabilities   []string // MCP server names needed for this call
    StreamTo       io.Writer
}
```

The existing Anthropic API becomes one backend implementation. CLI invocations become others. The engine and pipeline code interact only with this interface.

## Backend Selection

Global default in `config.edn`, overridable per call:

```clojure
{:backend "codex"

 :backends
 {:codex {:command "codex" :subcommand "exec"}
  :claude {:command "claude"}
  :anthropic {:provider "anthropic"
              :api-key-env "ANTHROPIC_API_KEY"}}

 :models
 {:fast {:backend "codex" :model "o4-mini"}
  :capable {:backend "codex" :model "gpt-5"}
  :powerful {:backend "claude" :model "opus"}}}
```

Per-call override:

```
sevens apply --backend claude --model sonnet notice "The Commons"
```

The function's `AgentConfig.Model` resolves through `GlobalConfig.ResolveModel()` as before. The resolved model profile now includes which backend to use. A single pipeline (e.g., `deepen`) can use different backends for different steps if the model tiers map to different backends.

## Prompt Strategy

### System prompt injection

Both CLIs inject their own default system prompts (Claude Code's is extensive). Sevens needs clean control.

**Strategy**: Bake sevens' system prompt into the user prompt. Suppress the CLI's defaults.

- **Codex**: Set `developer_instructions = ""` in generated config.toml. No CLI flag for system prompt -- it goes in the prompt.
- **Claude Code**: Pass `--system-prompt " "` to blank the default. Sevens' system prompt is prepended to the user prompt.

```go
func buildPrompt(req InferenceRequest) string {
    if req.SystemPrompt != "" {
        return req.SystemPrompt + "\n\n---\n\n" + req.Prompt
    }
    return req.Prompt
}
```

### Context constraint preamble

Every CLI invocation gets a preamble prepended that tells the agent what context is available and what it should not do:

```
CONTEXT POLICY: All knowledge graph context for this task is provided
in full below. Do not search for or read additional files unless
explicitly instructed.
```

For tier 2 (scoped exploration), the preamble adds:

```
EXTERNAL TOOLS: You have access to external integrations (github, jira).
Use these to find related issues, tickets, or documentation in external
systems.

FILE ACCESS: Do not read local files or search the filesystem. All
necessary context is in this prompt.
```

Or, if the function allows file reads:

```
FILE ACCESS: You may read source files that are explicitly referenced
by path in the node content. Do not explore or search beyond those
references.
```

The preamble is generated from the function's `AgentConfig` -- the backend just sees a prompt string.

## Exploration Tiers

Functions declare how much autonomy the CLI gets:

### Tier 1: Closed Pipe

Prompt in, text out. No tools. The common case -- most functions (elaborate, decompose, summarize, challenge, discuss, distill, bridge, merge, thesis).

Sevens resolves all context via path specs. The CLI is a pure inference endpoint.

```clojure
{:name "elaborate"
 :agent {:exploration "closed"
         :model "fast"}}
```

**Codex invocation**:
```
codex exec "<prompt>" --model o4-mini --full-auto --ephemeral
```

**Claude invocation**:
```
claude -p "<prompt>" --model haiku --output-format text --system-prompt " " --tools ""
```

### Tier 2: Scoped Exploration

The CLI can use MCP tools to query external systems (GitHub, Jira, databases). Optionally can read explicitly-referenced source files. Cannot write, cannot freely explore the filesystem.

Used by functions that need external context sevens can't provide (relate, notice with open context, future LLM-driven context gathering).

```clojure
{:name "relate"
 :agent {:exploration "scoped"
         :model "capable"
         :capabilities ["github" "jira"]
         :allow-file-reads true
         :read-only true}}
```

**Codex invocation**:
```
codex exec "<prompt with preamble>" --model gpt-5 --approval-mode suggest
```
(With generated config.toml in `CODEX_HOME` containing the declared MCP servers.)

**Claude invocation**:
```
claude -p "<prompt with preamble>" --model sonnet --output-format text \
  --system-prompt " " \
  --mcp-config <generated-mcp.json> \
  --allowedTools "Read,mcp__github__*,mcp__jira__*"
```

Claude Code's `--allowedTools` provides precise tool scoping. Codex lacks this -- the constraint is enforced via the prompt preamble and sandbox mode.

### Constraint enforcement

Two layers, always:

1. **CLI flags** where supported (Claude's `--allowedTools`, `--tools ""`, Codex's sandbox/approval modes)
2. **Prompt preamble** always (explicit instructions about provided context, allowed tools, prohibited exploration)

The prompt layer is the universal constraint. CLI flags are defense in depth.

## Output Handling

### Key finding: both CLIs emit final message as plain text on stdout

- **Codex** `codex exec "prompt"` -- stdout is *only* the final agent message. All progress goes to stderr. This is enforced in Codex's source code.
- **Claude** `claude -p "prompt" --output-format text` -- stdout is plain text response.

This means sevens captures stdout and passes it to the existing parsers unchanged:

- **Text functions** (notice, challenge, thesis): stdout string used directly.
- **Ops functions** (elaborate, decompose, bridge): stdout string passed to `ParseOps()`, which already handles markdown fence stripping, truncated JSON recovery, and validation.
- **Suggestions functions** (synthesize, decompose suggest step): stdout string parsed as JSON suggestions.

No per-backend response parsing needed. The existing `ParseOps` and output handling code works for all backends.

### Schema-constrained output (optional optimization)

Both CLIs support schema-constrained structured output:

- **Codex**: `--output-schema schema.json -o result.json` -- writes schema-validated JSON to file
- **Claude**: `--json-schema '{...}' --output-format json` -- `.structured_output` field in JSON response

This could reduce parse failures for ops output by having the CLI enforce the FileOp schema. But it's not required for v1 -- the prompt-constrained approach works today and keeps both backends identical.

### Streaming

For text functions where sevens currently streams to terminal:

```go
cmd.Stderr = req.StreamTo // CLI progress output streams to terminal
```

Both CLIs stream progress to stderr. For tier 1 closed-pipe calls, stderr shows model output as it generates. For tier 2, stderr also shows tool call traces.

## MCP Configuration Generation

### Central definition: `capabilities.edn`

```clojure
{:mcp-servers
 {:github
  {:description "GitHub API access for issues, PRs, repos"
   :command "npx"
   :args ["-y" "@modelcontextprotocol/server-github"]
   :env {"GITHUB_PERSONAL_ACCESS_TOKEN" :env/GITHUB_TOKEN}
   :enabled-tools ["search_repositories" "get_issue" "list_issues"
                    "get_pull_request" "search_code"]
   :disabled-tools ["create_issue" "create_pull_request"]}

  :jira
  {:description "Jira project tracking"
   :command "npx"
   :args ["-y" "@modelcontextprotocol/server-atlassian"]
   :env {"ATLASSIAN_API_TOKEN" :env/ATLASSIAN_TOKEN
         "ATLASSIAN_URL" "https://myorg.atlassian.net"}}}}
```

The `:env/GITHUB_TOKEN` syntax means "read from environment variable `GITHUB_TOKEN` at generation time." Sevens doesn't store secrets.

### Generation: `sevens config generate <backend>`

Materializes the central definitions into per-backend config format:

**`sevens config generate codex`** writes `~/.config/sevens/generated/codex/config.toml`:

```toml
developer_instructions = ""

[mcp_servers.github]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env_vars = ["GITHUB_TOKEN"]
enabled_tools = ["search_repositories", "get_issue", "list_issues", "get_pull_request", "search_code"]
disabled_tools = ["create_issue", "create_pull_request"]

[mcp_servers.jira]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-atlassian"]
env_vars = ["ATLASSIAN_TOKEN"]
```

**`sevens config generate claude`** writes `~/.config/sevens/generated/claude/mcp.json`:

```json
{
  "mcpServers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}" }
    },
    "jira": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-atlassian"],
      "env": {
        "ATLASSIAN_API_TOKEN": "${ATLASSIAN_TOKEN}",
        "ATLASSIAN_URL": "https://myorg.atlassian.net"
      }
    }
  }
}
```

Same capabilities, different wire formats.

### Invocation-time config

When a function declares `:capabilities ["github"]`, sevens points the CLI at the generated config directory so it discovers the MCP servers:

```go
// Codex
cmd.Env = append(os.Environ(), "CODEX_HOME="+generatedCodexDir)

// Claude
args = append(args, "--mcp-config", generatedClaudeMCPPath)
```

Tier 1 functions don't pass config -- the CLI runs bare with no MCP servers.

### Capability verification

At invocation time, sevens checks that the generated config includes the declared capabilities:

```
$ sevens apply relate "Container Strategy"
error: function "relate" requires capabilities [github, jira]
       backend "codex" is missing: jira
       run: sevens config generate codex
       or: add :jira to ~/.config/sevens/capabilities.edn
```

## Parallel Execution

CLI invocations are independent processes. Map-over operations run concurrently:

```go
sem := make(chan struct{}, 4) // configurable concurrency limit
results := lop.Map(children, func(child string, _ int) EvalResult {
    sem <- struct{}{}
    defer func() { <-sem }()
    return invokeBackend(ctx, cfg, child, stepFn)
})
```

Each process has its own CLI instance, own MCP server connections. No shared state except the sevens DB (read-only during parallel execution, writes happen after collection).

The `deepen` composed function (decompose then map elaborate over children) runs all elaborate calls concurrently after decompose completes. What was "run elaborate 7 times sequentially" becomes "run 7 CLI processes in parallel."

## Cancellation

`exec.CommandContext` propagates context cancellation. For graceful shutdown (let the CLI clean up):

```go
cmd := exec.CommandContext(ctx, "codex", args...)
cmd.Cancel = func() error {
    return cmd.Process.Signal(os.Interrupt)
}
cmd.WaitDelay = 5 * time.Second // force kill after 5s if interrupt doesn't work
```

Ctrl-C in sevens cancels the context, which sends SIGINT to child CLI processes. If they don't exit within 5 seconds, SIGKILL.

## Function AgentConfig (Updated)

```go
type AgentConfig struct {
    Persona        string   `edn:"persona,omitempty"`
    SystemPrompt   string   `edn:"system-prompt,omitempty"`
    Model          string   `edn:"model,omitempty"`
    ContextPolicy  string   `edn:"context-policy,omitempty"`
    Exploration    string   `edn:"exploration,omitempty"`     // "closed" (default), "scoped"
    Capabilities   []string `edn:"capabilities,omitempty"`    // MCP server names
    AllowFileReads bool     `edn:"allow-file-reads,omitempty"`
    ReadOnly       bool     `edn:"read-only,omitempty"`
}
```

## Backend Implementations

### Codex

```go
func (b *CodexBackend) Complete(ctx context.Context, req InferenceRequest) (string, error) {
    prompt := buildPrompt(req)
    args := []string{"exec", prompt, "--model", req.Model}

    switch req.Exploration {
    case "closed":
        args = append(args, "--full-auto", "--ephemeral")
    case "scoped":
        if req.ReadOnly {
            args = append(args, "--approval-mode", "suggest")
        } else {
            args = append(args, "--full-auto")
        }
    default:
        args = append(args, "--full-auto", "--ephemeral")
    }

    cmd := exec.CommandContext(ctx, b.Command, args...)
    cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
    cmd.WaitDelay = 5 * time.Second

    if req.Exploration == "scoped" && len(req.Capabilities) > 0 {
        cmd.Env = append(os.Environ(), "CODEX_HOME="+b.GeneratedConfigDir)
    }
    cmd.Stderr = req.StreamTo

    out, err := cmd.Output()
    if err != nil {
        return "", fmt.Errorf("codex exec: %w", err)
    }
    return strings.TrimSpace(string(out)), nil
}
```

### Claude Code

```go
func (b *ClaudeBackend) Complete(ctx context.Context, req InferenceRequest) (string, error) {
    prompt := buildPrompt(req)
    args := []string{
        "-p", prompt,
        "--model", req.Model,
        "--output-format", "text",
        "--system-prompt", " ",
    }

    switch req.Exploration {
    case "closed":
        args = append(args, "--tools", "")
    case "scoped":
        if len(req.Capabilities) > 0 {
            args = append(args, "--mcp-config", b.mcpConfigPath())
        }
        allowed := b.buildAllowedTools(req)
        if allowed != "" {
            args = append(args, "--allowedTools", allowed)
        }
    }

    cmd := exec.CommandContext(ctx, b.Command, args...)
    cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
    cmd.WaitDelay = 5 * time.Second
    cmd.Stderr = req.StreamTo

    out, err := cmd.Output()
    if err != nil {
        return "", fmt.Errorf("claude: %w", err)
    }
    return strings.TrimSpace(string(out)), nil
}

func (b *ClaudeBackend) buildAllowedTools(req InferenceRequest) string {
    if req.Exploration != "scoped" {
        return ""
    }
    tools := []string{}
    if req.AllowFileReads {
        tools = append(tools, "Read", "Grep", "Glob")
    }
    for _, cap := range req.Capabilities {
        tools = append(tools, "mcp__"+cap+"__*")
    }
    return strings.Join(tools, ",")
}
```

### Anthropic API (Existing)

The current `CallLLM` function wraps into the Backend interface with minimal changes:

```go
func (b *AnthropicBackend) Complete(ctx context.Context, req InferenceRequest) (string, error) {
    return CallLLM(ctx, b.Config, req.SystemPrompt, req.Prompt, req.StreamTo)
}
```

## File Layout (New/Changed)

```
sevens/
  internal/
    backend/
      backend.go              Backend interface, InferenceRequest, buildPrompt, BuildPreamble
      codex.go                Codex CLI backend
      claude.go               Claude Code CLI backend
      anthropic.go            Existing API backend (wraps current CallLLM)
    apply/
      llm.go                  Retains LoadGlobalConfig, resolveAPIKey (used by anthropic backend)
      types.go                AgentConfig updated with Exploration, Capabilities, etc.

~/.config/sevens/
  config.edn                  Updated: :backend, :backends map
  capabilities.edn            Central MCP server definitions
  generated/
    codex/config.toml         Generated Codex config (MCP servers, blank developer_instructions)
    claude/mcp.json           Generated Claude MCP config
```

## Implementation Priority

1. Extract `Backend` interface, wrap existing `CallLLM` as `AnthropicBackend`
2. Implement `CodexBackend` (tier 1 closed pipe only)
3. Test all 13 functions through Codex
4. Implement `ClaudeBackend` (tier 1 closed pipe only)
5. Add `BuildPreamble` for context constraints
6. Add `capabilities.edn` and `sevens config generate`
7. Implement tier 2 scoped exploration for both backends
8. Add parallel map-over with semaphore
9. Add cancellation and timeout handling

## Caching

Not needed at the inference level. Context augmentation is equivalent to conversation length growth. The existing focus session (pins a node + its neighborhood) provides context reuse across calls within a session. No separate cache layer.

## Cost Gating

Dropped for CLI backends (flat-rate subscriptions). Retained for the Anthropic API backend where per-token billing applies.

## Agent Mode Relationship

The `prepare`/`submit` protocol from AGENT-DESIGN.md was designed for external agents driving sevens manually. With CLI backends, sevens drives the agent programmatically. The prepare/submit path remains available for the manual case (user copy-pastes into a chat window) but is no longer the primary agent mode path.
