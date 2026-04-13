// Package workflow encodes concept synchronization rules as functions.
//
// Each function corresponds to one sync rule from the concept design:
// it composes actions across Graph, KB, Function, and Projection in the
// correct order. Both CLI and REPL call these instead of reimplementing
// orchestration inline.
//
// The workflow layer owns NO state. It coordinates concept actions and
// returns results for the caller to display.
package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sevens/internal/function"
	"sevens/internal/kb"
	"sevens/internal/projection"
	projmd "sevens/internal/projection/md"
)

// Deps bundles the concept implementations a workflow needs.
// Constructed once per CLI/REPL invocation, passed to every workflow.
type Deps struct {
	KB       *kb.KB
	Proj     *projmd.MarkdownProjection
	Store    *function.PipelineStore
	Backend  function.TransformBackend // nil for non-LLM workflows
}

// Executor returns a function.Executor from the deps.
func (d *Deps) Executor() *function.Executor {
	return function.NewExecutor(d.KB, d.Backend, d.Store)
}

// --- Results ---

// ApplyResult is the outcome of applying a function.
type ApplyResult struct {
	// Pipeline state after execution.
	Suspended    bool
	PipelineID   string
	FunctionName string
	StepName     string
	Target       string
	BackendName  string

	// The step output.
	Output      string
	Ops         []function.FileOp
	Suggestions []function.Suggestion
	IsText      bool

	// If ops were applied (non-suspended, ungated):
	FilesCreated []string
	FilesEdited  []string
	CommitHash   string
}

// AcceptResult is the outcome of accepting or revising a pipeline.
type AcceptResult struct {
	// Same shape as ApplyResult for the post-accept state.
	Suspended    bool
	PipelineID   string
	FunctionName string
	StepName     string
	Target       string

	Output      string
	Ops         []function.FileOp
	Suggestions []function.Suggestion
	IsText      bool

	FilesCreated []string
	FilesEdited  []string
	CommitHash   string

	Completed bool
}

// --- Sync: ApplyFunction ---

// ApplyFunction loads a function, executes it against a target node,
// and materializes any resulting file operations.
//
// Sync rule: Function.apply → [Projection.applyOps → Projection.commit
// → Projection.sync → KB.appendLog] (if ops and ungated)
func ApplyFunction(ctx context.Context, d *Deps, root, fnName, nodeTitle string) (*ApplyResult, error) {
	// Auto-sync before executing to ensure the KB reflects disk state.
	ensureSynced(ctx, d, root)

	fn, _, err := function.LoadFunction(fnName)
	if err != nil {
		return nil, fmt.Errorf("loading function: %w", err)
	}

	exec := d.Executor()
	result, err := exec.Apply(ctx, root, fn, nodeTitle)
	if err != nil {
		return nil, fmt.Errorf("applying function: %w", err)
	}

	ar := &ApplyResult{
		Suspended:    result.Suspended,
		PipelineID:   result.Pipeline.ID,
		FunctionName: fn.Name,
		Target:       nodeTitle,
		BackendName:  result.Pipeline.BackendName,
	}

	if result.Pipeline.CurrentStep < len(fn.EffectiveSteps()) {
		ar.StepName = fn.EffectiveSteps()[result.Pipeline.CurrentStep].Name
	}

	if result.Result != nil {
		ar.Output = result.Result.Raw
		ar.Ops = result.Result.Ops
		ar.Suggestions = result.Result.Suggestions
		ar.IsText = result.Result.IsText
	}

	// If completed with ops, materialize them.
	if !result.Suspended && result.Result != nil && len(result.Result.Ops) > 0 {
		created, edited, hash, err := materializeOps(ctx, d, root, result.Result.Ops,
			fmt.Sprintf("sevens: apply %s to %q", fnName, nodeTitle))
		if err != nil {
			return nil, err
		}
		ar.FilesCreated = created
		ar.FilesEdited = edited
		ar.CommitHash = hash

		_ = d.KB.AppendLog(ctx, kb.LogEntry{
			Event:        "applied",
			Root:         root,
			Function:     fnName,
			Node:         nodeTitle,
			Timestamp:    now(),
			Commit:       hash,
			FilesCreated: created,
			FilesEdited:  edited,
		})
	}

	return ar, nil
}

// --- Sync: AcceptPipeline ---

// AcceptPipeline accepts a pending pipeline, optionally with revision feedback.
// If feedback is non-empty, it revises instead of accepting.
//
// Sync rule: Function.accept → [Projection.applyOps → Projection.commit
// → Projection.sync → KB.appendLog] (if completed with ops)
func AcceptPipeline(ctx context.Context, d *Deps, root, pipelineID, feedback string) (*AcceptResult, error) {
	pipeline, err := d.Store.Load(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("loading pipeline: %w", err)
	}

	fn, _, err := function.LoadFunction(pipeline.FunctionName)
	if err != nil {
		return nil, fmt.Errorf("loading function: %w", err)
	}

	exec := d.Executor()

	if feedback != "" {
		// Revision
		result, err := exec.Revise(ctx, root, fn, pipelineID, feedback)
		if err != nil {
			return nil, fmt.Errorf("revising: %w", err)
		}
		ar := &AcceptResult{
			Suspended:    result.Suspended,
			PipelineID:   result.Pipeline.ID,
			FunctionName: fn.Name,
			Target:       pipeline.Target,
		}
		if result.Pipeline.CurrentStep < len(fn.EffectiveSteps()) {
			ar.StepName = fn.EffectiveSteps()[result.Pipeline.CurrentStep].Name
		}
		if result.Result != nil {
			ar.Output = result.Result.Raw
			ar.Ops = result.Result.Ops
			ar.IsText = result.Result.IsText
		}
		return ar, nil
	}

	// Accept and advance
	result, err := exec.Accept(ctx, root, fn, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("accepting: %w", err)
	}

	ar := &AcceptResult{
		Suspended:    result.Suspended,
		PipelineID:   result.Pipeline.ID,
		FunctionName: fn.Name,
		Target:       pipeline.Target,
		Completed:    result.Pipeline.Phase == function.PhaseCompleted,
	}
	if result.Pipeline.CurrentStep < len(fn.EffectiveSteps()) {
		ar.StepName = fn.EffectiveSteps()[result.Pipeline.CurrentStep].Name
	}
	if result.Result != nil {
		ar.Output = result.Result.Raw
		ar.Ops = result.Result.Ops
		ar.Suggestions = result.Result.Suggestions
		ar.IsText = result.Result.IsText
	}

	// If completed with ops, materialize
	if !result.Suspended && result.Result != nil && len(result.Result.Ops) > 0 {
		created, edited, hash, err := materializeOps(ctx, d, root, result.Result.Ops,
			fmt.Sprintf("sevens: apply %s to %q", fn.Name, pipeline.Target))
		if err != nil {
			return nil, err
		}
		ar.FilesCreated = created
		ar.FilesEdited = edited
		ar.CommitHash = hash

		_ = d.KB.AppendLog(ctx, kb.LogEntry{
			Event:        "applied",
			Root:         root,
			Function:     fn.Name,
			Node:         pipeline.Target,
			Timestamp:    now(),
			Commit:       hash,
			FilesCreated: created,
			FilesEdited:  edited,
		})
	}

	return ar, nil
}

// --- Sync: RejectPipeline ---

// RejectPipeline rejects a pending pipeline. Terminal.
func RejectPipeline(ctx context.Context, d *Deps, root, pipelineID string) error {
	pipeline, err := d.Store.Load(ctx, pipelineID)
	if err != nil {
		return fmt.Errorf("loading pipeline: %w", err)
	}

	exec := d.Executor()
	_, err = exec.Reject(ctx, pipelineID)
	if err != nil {
		return fmt.Errorf("rejecting: %w", err)
	}

	_ = d.KB.AppendLog(ctx, kb.LogEntry{
		Event:     "rejected",
		Root:      root,
		Function:  pipeline.FunctionName,
		Node:      pipeline.Target,
		Timestamp: now(),
	})
	return nil
}

// --- Sync: SyncFiles ---

// SyncFiles commits any uncommitted changes, syncs files to the graph,
// and runs validation.
func SyncFiles(ctx context.Context, d *Deps, root string, maxChildren int) (*projection.SyncResult, []kb.Violation, error) {
	// Pre-commit uncommitted changes
	if projmd.IsGitRepo(root) {
		hasChanges, _ := projmd.HasChanges(root)
		if hasChanges {
			_, _ = projmd.CommitAll(root, "sevens: sync")
		}
	}

	result, err := d.Proj.Sync(ctx, root)
	if err != nil {
		return nil, nil, fmt.Errorf("sync: %w", err)
	}

	violations, err := d.KB.Validate(ctx, root, maxChildren, 0)
	if err != nil {
		return result, nil, fmt.Errorf("validate: %w", err)
	}

	return result, violations, nil
}

// --- Sync: CreateFromTemplate ---

// TemplateResult is the outcome of template execution.
type TemplateResult struct {
	PrimaryTitle string
	FilesCreated []string
	FilesEdited  []string
	CommitHash   string
}

// CreateFromTemplate executes a deterministic function (template) and
// materializes the result.
func CreateFromTemplate(ctx context.Context, d *Deps, root string, fn *function.Function, target string, vars map[string]string) (*TemplateResult, error) {
	ensureSynced(ctx, d, root)
	exec := d.Executor()
	result, err := exec.Apply(ctx, root, fn, target)
	if err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	tr := &TemplateResult{}
	if vars["title"] != "" {
		tr.PrimaryTitle = vars["title"]
	}

	if result.Result != nil && len(result.Result.Ops) > 0 {
		created, edited, hash, err := materializeOps(ctx, d, root, result.Result.Ops,
			fmt.Sprintf("sevens: new from template %s", fn.Name))
		if err != nil {
			return nil, err
		}
		tr.FilesCreated = created
		tr.FilesEdited = edited
		tr.CommitHash = hash
	}

	return tr, nil
}

// --- Sync: RevertOperation ---

// RevertOperation reverts the last applied operation on a node.
func RevertOperation(ctx context.Context, d *Deps, root, nodeTitle string) (string, error) {
	entries, err := d.KB.ReadLog(ctx, root, nodeTitle)
	if err != nil {
		return "", fmt.Errorf("reading log: %w", err)
	}

	// Find last entry with a commit hash
	var commitHash string
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Commit != "" && entries[i].Event == "applied" {
			commitHash = entries[i].Commit
			break
		}
	}
	if commitHash == "" {
		return "", fmt.Errorf("no applied operations with git commits found for %q", nodeTitle)
	}

	if err := projmd.RevertCommit(root, commitHash); err != nil {
		return "", fmt.Errorf("reverting commit: %w", err)
	}

	newHash, err := projmd.CommitAll(root, fmt.Sprintf("sevens: revert %s on %q", commitHash, nodeTitle))
	if err != nil {
		return "", fmt.Errorf("post-revert commit: %w", err)
	}

	// Resync
	if _, err := d.Proj.Sync(ctx, root); err != nil {
		return "", fmt.Errorf("post-revert sync: %w", err)
	}

	_ = d.KB.AppendLog(ctx, kb.LogEntry{
		Event:     "reverted",
		Root:      root,
		Function:  "",
		Node:      nodeTitle,
		Timestamp: now(),
		Commit:    newHash,
	})

	return newHash, nil
}

// --- Sync: SubmitExternalResult ---

// SubmitExternalResult injects an externally-produced result into a pending pipeline.
// The pipeline must be in PhasePending (e.g., from a `prepare` that used AgentBackend).
// After submission, the pipeline remains pending for `accept`.
func SubmitExternalResult(ctx context.Context, d *Deps, pipelineID string, result function.TransformResult) error {
	p, err := d.Store.Load(ctx, pipelineID)
	if err != nil {
		return fmt.Errorf("loading pipeline: %w", err)
	}
	if p.Phase != function.PhasePending {
		return fmt.Errorf("pipeline is not pending (phase: %s)", p.Phase)
	}
	p.CurrentResult = &result
	return d.Store.Save(ctx, p)
}

// --- Queries ---

// FindPendingPipeline finds the pending pipeline for a node title.
// Returns the pipeline and an error if ambiguous or not found.
func FindPendingPipeline(ctx context.Context, d *Deps, root, nodeTitle string) (*function.Pipeline, error) {
	pending, err := d.Store.FindPending(ctx, root)
	if err != nil {
		return nil, err
	}

	var matches []*function.Pipeline
	for _, p := range pending {
		if p.Target == nodeTitle {
			matches = append(matches, p)
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no pending suggestions for %q", nodeTitle)
	case 1:
		return matches[0], nil
	default:
		var ids []string
		for _, m := range matches {
			ids = append(ids, shortPipelineID(m.ID)+" ("+m.FunctionName+")")
		}
		return nil, fmt.Errorf("multiple pending pipelines for %q — pick one:\n  %s",
			nodeTitle, strings.Join(ids, "\n  "))
	}
}

// --- Internal helpers ---

// materializeOps applies file operations via projection, commits, and resyncs.
// This is the shared "write ops to disk" step used by every sync that
// produces file mutations.
func materializeOps(ctx context.Context, d *Deps, root string, ops []function.FileOp, commitMsg string) (created, edited []string, commitHash string, err error) {
	projOps := make([]projection.FileOp, len(ops))
	for i, op := range ops {
		projOps[i] = projection.FileOp(op)
	}

	result, err := d.Proj.ApplyOps(ctx, root, projOps)
	if err != nil {
		return nil, nil, "", fmt.Errorf("applying ops: %w", err)
	}
	created = result.FilesCreated
	edited = result.FilesEdited

	if projmd.IsGitRepo(root) {
		allFiles := append(created, edited...)
		if len(allFiles) > 0 {
			commitHash, err = projmd.CommitFiles(root, commitMsg, allFiles)
			if err != nil {
				// Non-fatal: log but continue
				commitHash = ""
			}
		}
	}

	// Resync graph with new file state.
	// NOTE: Sync reads files and writes triples. It does not modify files.
	_, _ = d.Proj.Sync(ctx, root)

	return created, edited, commitHash, nil
}

// ensureSynced commits any pending file changes and resyncs the graph
// so the KB reflects the current state of disk before a mutation.
func ensureSynced(ctx context.Context, d *Deps, root string) {
	if !projmd.IsGitRepo(root) {
		// Not a git repo — just sync the projection.
		_, _ = d.Proj.Sync(ctx, root)
		return
	}
	hasChanges, _ := projmd.HasChanges(root)
	if !hasChanges {
		return
	}
	_, _ = projmd.CommitAll(root, "sevens: auto-sync before apply")
	_, _ = d.Proj.Sync(ctx, root)
}

func now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// shortPipelineID extracts the timestamp:random suffix from a full pipeline ID.
// "pipeline:abcdef:20260412T175303:a175d086" → "20260412T175303:a175d086"
func shortPipelineID(id string) string {
	// Full format: pipeline:<hash>:<timestamp>:<random>
	// We want: <timestamp>:<random>
	parts := strings.SplitN(id, ":", 3)
	if len(parts) >= 3 {
		return parts[2]
	}
	return id
}
