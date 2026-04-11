# Code Survey: `internal/backend`

Package path: `sevens/internal/backend`

This package abstracts LLM inference behind a single interface. It supports three
concrete backends (Anthropic API, Claude Code CLI, OpenAI Codex CLI), provides
prompt-building utilities shared across all backends, handles capability/MCP server
definitions, and exposes a factory function that constructs the right backend from
global config.

---

## Exported Types

### `Backend` (interface, `backend.go`)

The core abstraction. All three concrete implementations satisfy this.

```go
type Backend interface {
    Complete(ctx context.Context, req InferenceRequest) (string, error)
    Name() string
}
```

- `Complete` sends a prompt and returns the raw text response.
- `Name` returns a string identifier for the backend (e.g. `"anthropic"`, `"claude"`, `"codex"`).

---

### `InferenceRequest` (struct, `backend.go`)

Everything a backend needs to make a single inference call.

| Field | Type | Notes |
|---|---|---|
| `SystemPrompt` | `string` | Baked into user prompt for CLI backends; sent as a separate system message for the API backend. |
| `Prompt` | `string` | Rendered user prompt. |
| `Model` | `string` | Model ID or tier name (passed through to the backend as-is). |
| `Exploration` | `string` | `"closed"` (default) or `"scoped"`. Controls what tools/filesystem access the agent is allowed. |
| `ReadOnly` | `bool` | If true, CLI backends run in read-only sandbox mode. |
| `AllowFileReads` | `bool` | If true, the prompt preamble and CLI tool allowlists permit reading explicitly-referenced files. |
| `Capabilities` | `[]string` | MCP server names needed for this call (e.g. `["github", "jira"]`). |
| `StreamTo` | `io.Writer` | If non-nil, streaming output is written here. If nil, the Anthropic backend prints dots to stderr; CLI backends forward their stderr. |

---

### `BackendConfig` (struct, `backend.go`)

Per-backend configuration section, typically nested inside `GlobalConfig.Backends`.

| Field | EDN tag | Notes |
|---|---|---|
| `Type` | `type` | `"anthropic"`, `"codex"`, or `"claude"`. |
| `Command` | `command` | CLI binary path (for codex/claude). |
| `GeneratedConfDir` | `generated-conf-dir` | Directory where generated MCP config files live. |

Note: `BackendConfig` in this package is a mirror/alias of `apply.BackendConfig`. The
`backend.BackendConfig` struct exists but the factory (`FromConfig`) actually accepts
`apply.GlobalConfig` and uses `apply.BackendConfig` for per-backend settings.

---

### `BackendSettings` (struct, `backend.go`)

Defined in this package but appears to be a duplicate/legacy of `apply.BackendConfig`.
Same three fields (`Type`, `Command`, `GeneratedConfDir`). Used inside the
`BackendConfig` struct defined in this file.

---

### `AnthropicBackend` (struct, `anthropic.go`)

Calls the Anthropic Messages API directly using `anthropic-sdk-go`.

| Field | Type | Notes |
|---|---|---|
| `APIKey` | `string` | Exported; set by constructor. |

Constructor:
```go
func NewAnthropicBackend(apiKey, apiKeyEnv string) (*AnthropicBackend, error)
```
Falls back to the env var named by `apiKeyEnv` (default `ANTHROPIC_API_KEY`) if `apiKey`
is empty. Returns an error if no key is found.

Uses streaming internally (`Messages.NewStreaming`). Always sends with `MaxTokens: 16384`.
Sends `SystemPrompt` as a proper system message (not concatenated into the user turn).

---

### `ClaudeBackend` (struct, `claude.go`)

Invokes the Claude Code CLI (`claude -p`) as a subprocess.

| Field | Type | Notes |
|---|---|---|
| `Command` | `string` | Path to `claude` binary; defaults to `"claude"`. |
| `GeneratedConfDir` | `string` | Directory containing a generated `mcp.json` for MCP server definitions. |

Constructor:
```go
func NewClaudeBackend(command, generatedConfDir string) *ClaudeBackend
```

Behavior by exploration tier:
- `"closed"` / empty: passes `--tools ""` (disables all tools).
- `"scoped"`: passes `--mcp-config <path>/mcp.json` (if capabilities exist) and
  `--allowedTools <list>` built from `AllowFileReads` and `Capabilities`.

Prompt is piped via stdin. Subprocess stderr is forwarded to `StreamTo` or `os.Stderr`.

---

### `CodexBackend` (struct, `codex.go`)

Invokes the OpenAI Codex CLI (`codex exec`) as a subprocess.

| Field | Type | Notes |
|---|---|---|
| `Command` | `string` | Path to `codex` binary; defaults to `"codex"`. |
| `GeneratedConfDir` | `string` | Directory where generated MCP configs live (used by `sevens config generate codex` to write `~/.codex/config.toml`, not read at call time). |

Constructor:
```go
func NewCodexBackend(command, generatedConfDir string) *CodexBackend
```

Always uses `--full-auto`. Adds `--ephemeral` for closed exploration (no session
persistence). For scoped exploration with `ReadOnly`, adds `-c sandbox_mode="read-only"`.

MCP servers for Codex are registered globally in `~/.codex/config.toml` (prefixed
`sevens-*`) by a separate config-generation step — not injected per-call.

---

### `MCPServerDef` (struct, `capabilities.go`)

A single MCP server definition parsed from `capabilities.edn`.

| Field | Type | EDN tag |
|---|---|---|
| `Description` | `string` | `description` |
| `Command` | `string` | `command` |
| `Args` | `[]string` | `args` |
| `Env` | `map[edn.Keyword]string` | `env` |

---

### `Capabilities` (struct, `capabilities.go`)

Top-level structure of `capabilities.edn`.

| Field | Type | EDN tag |
|---|---|---|
| `MCPServers` | `map[edn.Keyword]MCPServerDef` | `mcp-servers` |

---

## Exported Functions

### Prompt Construction (`backend.go`)

```go
func BuildPrompt(req InferenceRequest) string
```
Concatenates `SystemPrompt` and `Prompt` with a `\n\n---\n\n` separator. Returns `Prompt`
alone if `SystemPrompt` is empty. Used by CLI backends that lack a dedicated system
prompt flag.

```go
func BuildPreamble(exploration string, capabilities []string, allowFileReads bool) string
```
Generates a `CONTEXT POLICY` block prepended to the prompt to constrain agent behavior:
- `"closed"` / empty: single-line block forbidding all file reads, filesystem search,
  and tool use.
- `"scoped"`: multi-section block with an `EXTERNAL TOOLS` section (only if
  `capabilities` is non-empty) and a `FILE ACCESS` section (permissive or restrictive
  based on `allowFileReads`).

```go
func PreparePrompt(req InferenceRequest) string
```
Composes the full prompt for CLI backends: `BuildPreamble(...)` + `BuildPrompt(req)`.
Called by both `ClaudeBackend.Complete` and `CodexBackend.Complete`.

---

### Factory (`factory.go`)

```go
func FromConfig(globalConfig apply.GlobalConfig, backendName string) (Backend, error)
```
Primary entry point for callers. Resolution order:
1. Use `backendName` if non-empty.
2. Fall back to `globalConfig.Backend`.
3. Fall back to `"anthropic"`.

Then either looks up an explicit entry in `globalConfig.Backends` or infers the type
from the name string. Recognized names without explicit config: `"anthropic"` / `"api"`,
`"codex"`, `"claude"`. Returns `ErrBackendNotFound`-adjacent errors for unknown names.

---

### Capabilities (`capabilities.go`)

```go
func LoadCapabilities() (*Capabilities, error)
```
Reads `~/.config/sevens/capabilities.edn` via `store.ConfigDir()`. Returns an empty
`Capabilities{}` (not an error) if the file does not exist.

```go
func GenerateCodexConfig(caps *Capabilities, _ string) error
```
Writes MCP server stanzas into `~/.codex/config.toml`. Strips any previously-generated
sevens section (delimited by a sentinel comment) before appending fresh stanzas. Prefixes
each server name with `sevens-`.

```go
func GenerateClaudeConfig(caps *Capabilities, outputDir string) error
```
Writes a `mcp.json` file to `outputDir` in the Claude Code format (`mcpServers` JSON
object). Called during `sevens config generate claude`.

```go
func CheckCapabilities(caps *Capabilities, requested []string) []string
```
Returns the subset of `requested` names that have no entry in `caps.MCPServers`.
Callers use this to warn before making a call that asks for unavailable capabilities.

---

## Sentinel Error

```go
var ErrBackendNotFound = fmt.Errorf("backend not found")
```
Defined in `backend.go`; intended for callers to test against when a named backend is
not configured.

---

## Unexported Helpers Worth Knowing

### `factory.go`

- `fromBackendConfig(name, cfg, globalConfig)` — resolves a backend from an explicit
  `BackendConfig` entry; handles the case where `Type` is omitted (falls back to `name`).
- `newAnthropicFromGlobal(globalConfig)` — extracts API key fields from `GlobalConfig.LLM`
  and delegates to `NewAnthropicBackend`.
- `generatedDir(backendName)` — computes the default generated-config directory path
  (`~/.config/sevens/generated/<backendName>`).
- `expandHome(path)` — expands a leading `~/` to the real home directory.

### `claude.go`

- `(b *ClaudeBackend) mcpConfigPath()` — returns `GeneratedConfDir + "/mcp.json"`.
- `(b *ClaudeBackend) buildAllowedTools(req)` — builds the `--allowedTools` comma-list
  from `AllowFileReads` (adds `Read`, `Grep`, `Glob`) and `Capabilities` (adds
  `mcp__<name>__*` wildcards).

### `capabilities.go`

- `kwStr(k edn.Keyword) string` — strips the leading `:` from an EDN keyword string.
  Used when converting EDN keyword map keys to plain strings for JSON or TOML output.
- `claudeMCPConfig` / `claudeMCPServer` — unexported structs for marshaling `mcp.json`
  in the Claude Code format.

---

## How Pieces Fit Together

```
caller
  |
  +--> FromConfig(globalConfig, backendName)  [factory.go]
  |       returns Backend
  |
  +--> backend.Complete(ctx, InferenceRequest)
          |
          +-- AnthropicBackend: calls Anthropic API directly, streams response
          |
          +-- ClaudeBackend:  PreparePrompt -> exec "claude -p ..." with stdin
          |
          +-- CodexBackend:   PreparePrompt -> exec "codex exec ..." with stdin
```

The three prompt helpers (`BuildPreamble`, `BuildPrompt`, `PreparePrompt`) are shared
utilities used only by CLI backends. `AnthropicBackend` handles system prompt and
user prompt separately through the SDK.

`Capabilities` is a parallel concern: it is loaded from `capabilities.edn` and used
to generate backend-specific config files (`GenerateCodexConfig`, `GenerateClaudeConfig`)
and to validate `InferenceRequest.Capabilities` before a call (`CheckCapabilities`).
At call time, capabilities affect the prompt preamble (via `BuildPreamble`) and the
CLI tool allowlist (via `ClaudeBackend.buildAllowedTools`); for Codex, the MCP servers
were already written to the global config at setup time.

A typical call sequence:
1. Load config: `apply.LoadGlobalConfig(...)` (outside this package).
2. Optionally load and check capabilities: `LoadCapabilities()`, `CheckCapabilities(...)`.
3. Construct backend: `FromConfig(globalConfig, "")`.
4. Build an `InferenceRequest` with prompt, model, exploration tier, capabilities.
5. Call `backend.Complete(ctx, req)` and handle the returned string.
