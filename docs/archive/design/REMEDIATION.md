# Walkthrough Bug Remediation

Bugs found by running every command in WALKTHROUGH.md against a fresh root.

## BUG-1: False cycle detection (High) -- FIXED

**Symptom:** Every child node triggers "node is its own ancestor via node/parent."
**Location:** `internal/kb/query.go` Validate (line ~239)
**Root cause:** Used `Reachable` which includes start node in results AND deduplicates, so (a) every node with a parent appeared as its own ancestor, and (b) real cycles couldn't be detected since the BFS visited-set prevented revisiting start.
**Fix:** Replaced `Reachable`-based check with a direct parent-chain walk that stops when it finds the start node again (real cycle) or reaches a root/already-visited node (no cycle).
**Test:** `TestValidateNoCycleInSimpleTree`, `TestValidateDetectsRealCycle`

## BUG-2: Graph context template variables unresolved (Critical) -- FIXED

**Symptom:** `{{parent-content}}`, `{{siblings-content}}`, `{{children-content}}` appear as literals in prompts sent to LLM.
**Location:** `internal/function/context.go`
**Root cause:** Path specs declared `:with ["node/content"]` but `ResolveContext` only resolved terminals to titles, ignoring the `With` field. `RenderPrompt` only handled `{{key}}` (titles), not `{{key-content}}` (content).
**Fix:** Added `PathNodes` map to `ResolvedContext` with `ResolvedNode{Title, Content}`. `ResolveContext` now fetches `node/content` when `With` includes it. `RenderPrompt` substitutes `{{key-content}}` with formatted content blocks. Cleans up `{{context}}` placeholder.
**Test:** `TestPathSpecResolvesContent`

## BUG-3: `challenge` unknown require role "history" (Medium) -- FIXED

**Symptom:** `Error: unknown require role "history"`
**Location:** `internal/function/context.go` -- `ResolveContext` role switch
**Root cause:** "history" role not handled in switch; fell through to default error.
**Fix:** Added "history" case calling `k.ReadLog`, formats entries into `rc.History`.
**Test:** `TestHistoryRoleResolution`

## BUG-4: Structure-modifying functions don't apply ops (High) -- FIXED

**Symptom:** `sharpen` returns JSON ops as displayed text; file unchanged, no pending state.
**Location:** Three places:
1. `internal/sevtypes/fileop.go` -- missing JSON tags (root cause)
2. `cmd/sevens/pipeline_cmds.go` -- `applyCmd2` missing ops application
3. `internal/workflow/workflow.go` -- new workflow layer centralizes ops application

**Root cause (deeper than initially diagnosed):** `FileOp` struct had no JSON tags. LLM returns snake_case (`old_text`, `new_text`); Go's `json.Unmarshal` expected PascalCase (`OldText`, `NewText`). `ParseOps` parsed the JSON without error but all edit fields were empty strings. The edit ops "succeeded" (found empty string in file, replaced with empty string) but changed nothing. Additionally, `applyCmd2` had no ops-application path for non-suspended results.
**Fix:**
1. Added `json:"snake_case"` tags to all `FileOp` fields
2. Added ops-application block to `applyCmd2`
3. Created `internal/workflow` package with `materializeOps` as the shared ops-application path
**Test:** `TestApplyFunction_OpsApplied` in workflow -- verifies mock sharpen ops are parsed, applied to disk, and file content changes. `TestApplyOpsEdit` in projection/md -- verifies edit ops work at the projection level.

## BUG-5: Pipeline doesn't persist backend choice (Medium) -- FIXED

**Symptom:** `accept --with` defaults to anthropic even when original `apply` used claude.
**Location:** `internal/function/pipeline.go`, `internal/function/store.go`, `cmd/sevens/pipeline_cmds.go`
**Root cause:** `Pipeline` struct had no backend field; CLI always used global config default.
**Fix:** Added `BackendName` field to Pipeline. `executor.Apply` sets it from `Backend.Name()`. Store persists/loads it via `pipeline/backend` predicate. `acceptCmd2` uses stored backend name when `--backend` flag is empty.
**Test:** `TestPipelineBackendPersistence`, `TestApplySetsBackendName`

## BUG-6: `audit` step 2 fails (Medium) -- EXPECTED FIX VIA BUG-2

**Symptom:** `Error: claude failed (exit 1)` on stress-test step.
**Root cause:** Downstream of BUG-2 (degenerate step 1 output due to missing context). With context now populated, step 1 should produce meaningful output, making step 2 viable.
**Status:** Needs re-test after BUG-2 fix.

## BUG-7: REPL "graph querier not available" (Critical) -- FIXED

**Symptom:** Every graph command in REPL fails.
**Location:** `cmd/sevens/repl.go`, `cmd/sevens/repl_adapter.go` (new file)
**Root cause:** `replCmd()` passed `WithKB` but not `WithGraphQuerier`. The REPL checked `r.graphQ == nil` on every graph command.
**Fix:** Created `kbGraphQuerier` adapter in `repl_adapter.go` implementing all 20+ `GraphQuerier` methods by delegating to `kb.KB` and `md.MarkdownProjection`. Wired into `replCmd()` via `WithGraphQuerier(gq)`.
**Test:** E2E piped REPL commands all produce correct output.

## BUG-8: extract-block doesn't remove content from source (Medium) -- FIXED

**Symptom:** New node created but source file retains extracted section.
**Location:** `cmd/sevens/main.go` -- `extractBlockCmd`
**Root cause:** Only generated a "create" FileOp for the new node. No "edit" FileOp to remove the extracted section from the source.
**Fix:** After creating the new node's op, render the selected blocks back to markdown and add an "edit" op that replaces that text with empty string. Also updated git commit to include edited files.
**Test:** Requires E2E; verified build compiles.

## BUG-9: Block path format differs from walkthrough (Low) -- DOC FIX

**Symptom:** Paths are `0`, `1`, `4.0` not `p.0`, `h.1`.
**Fix:** Update walkthrough documentation to match actual output format.

## BUG-10: `discuss` fails with claude backend (Medium) -- OPEN

**Symptom:** `Error: claude failed (exit 1)`
**Location:** `internal/backend/claude.go`
**Status:** Needs investigation of claude CLI subprocess error output. May be prompt format issue specific to the discussion function's loop structure.

## BUG-11: `config generate` fails (Low) -- FIXED

**Symptom:** `Error: no MCP servers defined in capabilities.edn`
**Location:** `cmd/sevens/main.go` -- config generate command
**Root cause:** Hard error on empty MCP servers. `LoadCapabilities` already returns empty struct when file missing, but the CLI command refused to proceed.
**Fix:** Changed hard error to a warning; allows generating empty config files.
