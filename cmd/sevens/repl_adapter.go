package main

// repl_adapter.go provides the GraphQuerier adapter that bridges
// the new KB/projection packages to the REPL's interface contract.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"sevens/internal/backend"
	"sevens/internal/config"
	"sevens/internal/function"
	"sevens/internal/kb"
	"sevens/internal/projection"
	projmd "sevens/internal/projection/md"
	"sevens/internal/repl"
	"sevens/internal/workflow"
)

// kbGraphQuerier adapts kb.KB + MarkdownProjection to the REPL's
// GraphQuerier interface.
type kbGraphQuerier struct {
	k    *kb.KB
	proj *projmd.MarkdownProjection
}

func newGraphQuerier(k *kb.KB, proj *projmd.MarkdownProjection) *kbGraphQuerier {
	return &kbGraphQuerier{k: k, proj: proj}
}

func (q *kbGraphQuerier) ctx() context.Context { return context.Background() }

func (q *kbGraphQuerier) ResolveTitle(title, root string) string {
	subj := q.k.Resolve(q.ctx(), root, title)
	if subj == "" {
		return ""
	}
	return title
}

func (q *kbGraphQuerier) ResolveNode(title, root string) (string, string) {
	subj := kb.NodeSubject(root, title)
	filePath, _, _ := q.k.Graph().Lookup(q.ctx(), subj, kb.PredNodeFile)
	return subj, filePath
}

func (q *kbGraphQuerier) GetObject(subject, predicate string) (string, error) {
	val, _, err := q.k.Graph().Lookup(q.ctx(), subject, predicate)
	return val, err
}

func (q *kbGraphQuerier) NodeTitle(subject string) (string, error) {
	val, _, err := q.k.Graph().Lookup(q.ctx(), subject, kb.PredNodeTitle)
	return val, err
}

func (q *kbGraphQuerier) ListNodeTitles(root string) ([]string, error) {
	return q.k.ListNodeTitles(q.ctx(), root)
}

func (q *kbGraphQuerier) SearchTitles(query, root string) ([]string, error) {
	subjects, err := q.k.Graph().Store().Search(q.ctx(), kb.PredNodeTitle, query)
	if err != nil {
		return nil, err
	}
	var titles []string
	for _, subj := range subjects {
		if t, _, _ := q.k.Graph().Lookup(q.ctx(), subj, kb.PredNodeTitle); t != "" {
			// Filter to this root by checking node/root
			if r, _, _ := q.k.Graph().Lookup(q.ctx(), subj, kb.PredNodeRoot); r == root {
				titles = append(titles, t)
			}
		}
	}
	return titles, nil
}

func (q *kbGraphQuerier) SearchContent(query, root string) ([]string, error) {
	subjects, err := q.k.Graph().Store().Search(q.ctx(), kb.PredNodeContent, query)
	if err != nil {
		return nil, err
	}
	var titles []string
	for _, subj := range subjects {
		if r, _, _ := q.k.Graph().Lookup(q.ctx(), subj, kb.PredNodeRoot); r == root {
			if t, _, _ := q.k.Graph().Lookup(q.ctx(), subj, kb.PredNodeTitle); t != "" {
				titles = append(titles, t)
			}
		}
	}
	return titles, nil
}

func (q *kbGraphQuerier) BuildWalk(root, title, shape string) (*repl.WalkResult, error) {
	gather := ResolveGatherSpec(shape)
	return q.k.Walk(q.ctx(), root, title, gather)
}

func (q *kbGraphQuerier) BuildOverview(root string) ([]repl.OverviewNode, error) {
	return q.k.Overview(q.ctx(), root)
}

func (q *kbGraphQuerier) BuildBlockList(root, nodeTitle string) (repl.BlockListOutput, error) {
	entries, err := q.k.ListBlocks(q.ctx(), root, nodeTitle)
	if err != nil {
		return repl.BlockListOutput{}, err
	}
	subj := kb.NodeSubject(root, nodeTitle)
	filePath, _, _ := q.k.Graph().Lookup(q.ctx(), subj, kb.PredNodeFile)

	var blocks []repl.BlockListEntry
	for _, e := range entries {
		blocks = append(blocks, repl.BlockListEntry{
			Path:  e.Path,
			Kind:  e.Kind,
			Text:  e.Content,
			Level: e.Level,
			Scope: splitScope(e.Scope),
		})
	}
	return repl.BlockListOutput{
		NodeTitle: nodeTitle,
		FilePath:  filePath,
		Blocks:    blocks,
	}, nil
}

func (q *kbGraphQuerier) BuildBlockDiff(root, nodeTitle string) (repl.BlockDiffOutput, error) {
	diff, err := q.proj.DiffBlocks(q.ctx(), root, nodeTitle)
	if err != nil {
		return repl.BlockDiffOutput{}, err
	}
	out := repl.BlockDiffOutput{
		NodeTitle: diff.NodeTitle,
		FilePath:  diff.FilePath,
	}
	for _, e := range diff.Unchanged {
		out.Unchanged = append(out.Unchanged, convertBlockDiffEntry(e))
	}
	for _, e := range diff.Edited {
		out.Edited = append(out.Edited, convertBlockDiffEntry(e))
	}
	for _, e := range diff.Inserted {
		out.Inserted = append(out.Inserted, convertBlockDiffEntry(e))
	}
	for _, e := range diff.Deleted {
		out.Deleted = append(out.Deleted, convertBlockDiffEntry(e))
	}
	return out, nil
}

func convertBlockDiffEntry(e projmd.BlockChange) repl.BlockDiffEntry {
	return repl.BlockDiffEntry{
		OldPath:  e.OldPath,
		NewPath:  e.NewPath,
		OldText:  e.OldText,
		NewText:  e.NewText,
		OldScope: splitScope(e.OldScope),
		NewScope: splitScope(e.NewScope),
	}
}

// splitScope converts a scope string like "Heading > SubHeading" to []string.
func splitScope(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, " > ")
	return parts
}

func (q *kbGraphQuerier) ChildrenSummary(root, nodeTitle string) ([]repl.ChildSummary, error) {
	summaries, err := q.k.ChildrenSummary(q.ctx(), root, nodeTitle)
	if err != nil {
		return nil, err
	}
	var out []repl.ChildSummary
	for _, s := range summaries {
		out = append(out, repl.ChildSummary{
			Title:     s.Title,
			CharCount: s.CharCount,
			Empty:     s.Empty,
		})
	}
	return out, nil
}

func (q *kbGraphQuerier) PrepareBlockExtraction(root, sourceTitle, blockPath, newTitle, parentTitle string) (repl.ExtractedNode, error) {
	subj := kb.NodeSubject(root, sourceTitle)
	content, _, _ := q.k.Graph().Lookup(q.ctx(), subj, kb.PredNodeContent)
	blocks := projmd.ExtractBlocks(content)
	target, idx, err := projmd.FindBlockByPath(blocks, blockPath)
	if err != nil {
		return repl.ExtractedNode{}, err
	}
	selected := projmd.SelectExtractedBlocks(blocks, idx)
	extractedContent := projmd.RenderExtractedContent(sourceTitle, target, selected)

	if newTitle == "" {
		newTitle = target.Text
	}
	if parentTitle == "" {
		parentTitle = sourceTitle
	}
	return repl.ExtractedNode{
		Title:       newTitle,
		ParentTitle: parentTitle,
		SourceTitle: sourceTitle,
		SourcePath:  blockPath,
		SourceKind:  target.Kind,
		SourceScope: target.HeadingChain,
		Content:     extractedContent,
	}, nil
}

func (q *kbGraphQuerier) ResolveBlockTarget(root, nodeTitle, blockPath string) (*repl.BlockTarget, error) {
	entry, err := q.k.ResolveBlock(q.ctx(), root, nodeTitle, blockPath)
	if err != nil {
		return nil, err
	}
	subj := kb.BlockSubject(root, nodeTitle, blockPath)
	return &repl.BlockTarget{
		Subject:   subj,
		NodeTitle: nodeTitle,
		Path:      entry.Path,
		Kind:      entry.Kind,
		Text:      entry.Content,
		Level:     entry.Level,
		Scope:     splitScope(entry.Scope),
	}, nil
}

func (q *kbGraphQuerier) ResolveBlockTargetBySubject(subject string) (*repl.BlockTarget, error) {
	parts := strings.SplitN(subject, ":", 4)
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid block subject: %q", subject)
	}
	return nil, fmt.Errorf("ResolveBlockTargetBySubject not fully implemented")
}

func (q *kbGraphQuerier) LoadConfig(root string) (repl.GraphConfig, error) {
	cfg, err := projmd.LoadConfig(root)
	if err != nil {
		return repl.GraphConfig{}, err
	}
	groups := make(map[string]repl.GraphGroup)
	for name, g := range cfg.Groups {
		groups[name] = repl.GraphGroup{
			Root:    g.Root,
			Exclude: g.Exclude,
			Nodes:   g.Nodes,
		}
	}
	return repl.GraphConfig{Groups: groups}, nil
}

func (q *kbGraphQuerier) AutoGroupIncludes(root, nodeTitle string) ([]string, error) {
	return nil, nil
}

func (q *kbGraphQuerier) ResolveGroup(root string, group repl.GraphGroup) ([]string, error) {
	return group.Nodes, nil
}

func (q *kbGraphQuerier) Resync(root string) error {
	_, err := q.proj.Sync(q.ctx(), root)
	return err
}

func (q *kbGraphQuerier) ScopeString(scope []string) string {
	return projmd.ScopeString(scope)
}

func (q *kbGraphQuerier) RenderBlockMarkdown(block repl.BlockListEntry) string {
	return projmd.RenderBlockMarkdown(projmd.ParsedBlock{
		Path: block.Path, Kind: block.Kind, Text: block.Text,
		HeadingChain: block.Scope,
	}, 0)
}

// ---------------------------------------------------------------------------
// ApplyRunner adapter
// ---------------------------------------------------------------------------

// kbApplyRunner adapts kb.KB + projection to the REPL's ApplyRunner interface.
type kbApplyRunner struct {
	k    *kb.KB
	proj *projmd.MarkdownProjection
}

func newApplyRunner(k *kb.KB, proj *projmd.MarkdownProjection) *kbApplyRunner {
	return &kbApplyRunner{k: k, proj: proj}
}

func (a *kbApplyRunner) LoadFunction(name string) (*repl.FunctionDef, error) {
	fn, _, err := function.LoadFunction(name)
	if err != nil {
		return nil, err
	}
	return &repl.FunctionDef{
		Name:        fn.Name,
		Description: fn.Description,
	}, nil
}

func (a *kbApplyRunner) ListFunctions() ([]repl.FunctionDef, error) {
	fns, err := function.ListFunctionDefs()
	if err != nil {
		return nil, err
	}
	var out []repl.FunctionDef
	for _, fn := range fns {
		out = append(out, repl.FunctionDef{
			Name:        fn.Name,
			Description: fn.Description,
		})
	}
	return out, nil
}

func (a *kbApplyRunner) LoadContextFiles(root string, paths []string) string {
	var parts []string
	for _, p := range paths {
		expanded := p
		if strings.HasPrefix(expanded, "~/") {
			home, _ := os.UserHomeDir()
			expanded = home + expanded[1:]
		}
		data, err := os.ReadFile(expanded)
		if err == nil {
			parts = append(parts, string(data))
		}
	}
	return strings.Join(parts, "\n\n")
}

func (a *kbApplyRunner) ExecuteOps(ops []repl.FileOp, root string) ([]string, []string, error) {
	projOps := make([]projection.FileOp, len(ops))
	for i, op := range ops {
		projOps[i] = projection.FileOp(op)
	}
	result, err := a.proj.ApplyOps(context.Background(), root, projOps)
	if err != nil {
		return nil, nil, err
	}
	return result.FilesCreated, result.FilesEdited, nil
}

func (a *kbApplyRunner) AppendLog(entry repl.LogEntry) error {
	return a.k.AppendLog(context.Background(), kb.LogEntry{
		Event:        entry.Event,
		Root:         entry.Root,
		Function:     entry.Function,
		Node:         entry.Target,
		Step:         entry.Step,
		Timestamp:    entry.Timestamp,
		Commit:       entry.Commit,
		Note:         entry.Note,
		FilesCreated: entry.FilesCreated,
		FilesEdited:  entry.FilesEdited,
	})
}

func (a *kbApplyRunner) ReadLog(root, nodeTitle string) ([]repl.LogEntry, error) {
	entries, err := a.k.ReadLog(context.Background(), root, nodeTitle)
	if err != nil {
		return nil, err
	}
	var out []repl.LogEntry
	for _, e := range entries {
		out = append(out, repl.LogEntry{
			Event:        e.Event,
			Root:         e.Root,
			Function:     e.Function,
			Target:       e.Node,
			Step:         e.Step,
			Timestamp:    e.Timestamp,
			Commit:       e.Commit,
			Note:         e.Note,
			FilesCreated: e.FilesCreated,
			FilesEdited:  e.FilesEdited,
		})
	}
	return out, nil
}

func (a *kbApplyRunner) RevertCommit(root, hash string) (string, error) {
	err := projmd.RevertCommit(root, hash)
	if err != nil {
		return "", err
	}
	newHash, err := projmd.CommitAll(root, fmt.Sprintf("sevens: revert %s", hash))
	return newHash, err
}

func (a *kbApplyRunner) SanitizeFilename(title string) string {
	return projmd.SanitizeFilename(title)
}

// ---------------------------------------------------------------------------
// TemplateRunner adapter
// ---------------------------------------------------------------------------

// kbTemplateRunner adapts function.LoadFunction + deterministic backend
// to the REPL's TemplateRunner interface.
type kbTemplateRunner struct {
	k    *kb.KB
	proj *projmd.MarkdownProjection
}

func newTemplateRunner(k *kb.KB, proj *projmd.MarkdownProjection) *kbTemplateRunner {
	return &kbTemplateRunner{k: k, proj: proj}
}

func (tr *kbTemplateRunner) LoadTemplate(name string) (*repl.NodeTemplate, error) {
	fn, _, err := function.LoadFunction(name)
	if err != nil {
		return nil, err
	}
	if !function.IsDeterministic(fn) {
		return nil, fmt.Errorf("%q is not a deterministic function", name)
	}
	var paramNames []string
	for _, p := range fn.Params {
		paramNames = append(paramNames, p.Name)
	}
	return &repl.NodeTemplate{
		Name:        fn.Name,
		Description: fn.Description,
		ParamNames:  paramNames,
	}, nil
}

func (tr *kbTemplateRunner) ListTemplates() ([]string, error) {
	fns, err := function.ListDeterministicFunctions()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, fn := range fns {
		names = append(names, fn.Name)
	}
	return names, nil
}

func (tr *kbTemplateRunner) BindTemplateArgs(tmpl *repl.NodeTemplate, args []string, vars map[string]string) map[string]string {
	fn, _, err := function.LoadFunction(tmpl.Name)
	if err != nil {
		return vars
	}
	return function.BindArgs(fn, args, vars)
}

func (tr *kbTemplateRunner) PreviewTemplate(root string, tmpl *repl.NodeTemplate, parent, targetNode string, vars map[string]string) (*repl.TemplatePreview, error) {
	fn, _, err := function.LoadFunction(tmpl.Name)
	if err != nil {
		return nil, err
	}
	allVars := function.BuiltinVars()
	for k, v := range vars {
		allVars[k] = v
	}
	if parent != "" {
		allVars["parent"] = parent
	}
	content := function.RenderPrompt(fn.Steps[0].Backend.PromptTemplate,
		&function.ResolvedContext{Target: &kb.WalkResult{Target: kb.WalkNode{Title: targetNode}}})
	content = function.ResolveTemplateVars(content, allVars)

	return &repl.TemplatePreview{
		TemplateName: tmpl.Name,
		Title:        allVars["title"],
		Parent:       allVars["parent"],
		Content:      content,
	}, nil
}

func (tr *kbTemplateRunner) ExecuteTemplate(root string, tmpl *repl.NodeTemplate, parent, targetNode string, vars map[string]string) (*repl.TemplateResult, error) {
	fn, _, err := function.LoadFunction(tmpl.Name)
	if err != nil {
		return nil, err
	}
	allVars := function.BuiltinVars()
	for k, v := range vars {
		allVars[k] = v
	}
	if parent != "" {
		allVars["parent"] = parent
	}

	// Execute via the deterministic backend through the executor
	ctx := context.Background()
	ps := function.NewPipelineStore(tr.k.Graph().Store())
	exec := function.NewExecutor(tr.k, &function.DeterministicBackend{}, ps)
	result, err := exec.Apply(ctx, root, fn, targetNode)
	if err != nil {
		return nil, err
	}

	// Apply ops if any
	if result.Result != nil && len(result.Result.Ops) > 0 {
		projOps := make([]projection.FileOp, len(result.Result.Ops))
		for i, op := range result.Result.Ops {
			projOps[i] = projection.FileOp(op)
		}
		applyResult, err := tr.proj.ApplyOps(ctx, root, projOps)
		if err != nil {
			return nil, err
		}
		return &repl.TemplateResult{
			PrimaryTitle:  allVars["title"],
			Created:       applyResult.FilesCreated,
			Edited:        applyResult.FilesEdited,
			CommitMessage: fmt.Sprintf("sevens: new from template %s", tmpl.Name),
		}, nil
	}

	return &repl.TemplateResult{
		PrimaryTitle: allVars["title"],
	}, nil
}

// ---------------------------------------------------------------------------
// PipelineRunner adapter
// ---------------------------------------------------------------------------

// kbPipelineRunner adapts function.Executor + PipelineStore to the REPL's
// PipelineRunner interface.
type kbPipelineRunner struct {
	k         *kb.KB
	proj      *projmd.MarkdownProjection
	store     *function.PipelineStore
	globalCfg config.GlobalConfig
}

func newPipelineRunner(k *kb.KB, proj *projmd.MarkdownProjection, store *function.PipelineStore, cfg config.GlobalConfig) *kbPipelineRunner {
	return &kbPipelineRunner{k: k, proj: proj, store: store, globalCfg: cfg}
}

func (pr *kbPipelineRunner) ctx() context.Context { return context.Background() }

func (pr *kbPipelineRunner) makeBackend(name string) function.TransformBackend {
	be, err := backend.FromConfig(pr.globalCfg, name)
	if err != nil {
		return nil
	}
	return function.NewLLMBackend(be)
}

func (pr *kbPipelineRunner) buildDeps(backendName string) *workflow.Deps {
	return &workflow.Deps{
		KB:      pr.k,
		Proj:    pr.proj,
		Store:   pr.store,
		Backend: pr.makeBackend(backendName),
	}
}

func (pr *kbPipelineRunner) RunPipeline(cfg repl.PipelineConfig) (*repl.PipelineResult, error) {
	if cfg.DryRun {
		fn, _, err := function.LoadFunction(cfg.FunctionName)
		if err != nil {
			return nil, fmt.Errorf("loading function: %w", err)
		}
		steps := fn.EffectiveSteps()
		if len(steps) == 0 {
			return nil, fmt.Errorf("function %q has no steps", cfg.FunctionName)
		}
		step := steps[cfg.StartStep]
		rc, err := function.ResolveContext(pr.ctx(), pr.k, cfg.Root, cfg.NodeTitle, step, cfg.PrevOutput)
		if err != nil {
			return nil, err
		}
		prompt := function.RenderPrompt(step.Backend.PromptTemplate, rc)
		fmt.Println(prompt)
		return &repl.PipelineResult{}, nil
	}

	deps := pr.buildDeps(cfg.BackendName)

	if cfg.StartStep > 0 {
		// Continuing a pipeline -- find and accept the pending pipeline
		pipeline, err := workflow.FindPendingPipeline(pr.ctx(), deps, cfg.Root, cfg.NodeTitle)
		if err != nil {
			return nil, err
		}
		ar, err := workflow.AcceptPipeline(pr.ctx(), deps, cfg.Root, pipeline.ID, "")
		if err != nil {
			return nil, err
		}
		return pr.convertAcceptResult(ar), nil
	}

	result, err := workflow.ApplyFunction(pr.ctx(), deps, cfg.Root, cfg.FunctionName, cfg.NodeTitle)
	if err != nil {
		return nil, err
	}
	return pr.convertApplyResult(result), nil
}

func (pr *kbPipelineRunner) convertApplyResult(r *workflow.ApplyResult) *repl.PipelineResult {
	outputType := "text"
	if !r.IsText && len(r.Ops) > 0 {
		outputType = "ops"
	}

	if r.Suspended {
		return &repl.PipelineResult{
			Suspended: true,
			Suspension: &repl.Suspension{
				Subject:    r.PipelineID,
				Root:       "",
				Function:   r.FunctionName,
				Target:     r.Target,
				StepName:   r.StepName,
				Backend:    r.BackendName,
				Output:     r.Output,
				OutputType: outputType,
				Ops:        r.Ops,
			},
		}
	}

	return &repl.PipelineResult{
		Result: &repl.StepResult{
			StepName:   r.StepName,
			OutputType: outputType,
			Output:     r.Output,
			Ops:        r.Ops,
		},
	}
}

func (pr *kbPipelineRunner) convertAcceptResult(r *workflow.AcceptResult) *repl.PipelineResult {
	outputType := "text"
	if !r.IsText && len(r.Ops) > 0 {
		outputType = "ops"
	}

	if r.Suspended {
		return &repl.PipelineResult{
			Suspended: true,
			Suspension: &repl.Suspension{
				Subject:    r.PipelineID,
				Function:   r.FunctionName,
				Target:     r.Target,
				StepName:   r.StepName,
				Output:     r.Output,
				OutputType: outputType,
				Ops:        r.Ops,
			},
		}
	}

	return &repl.PipelineResult{
		Result: &repl.StepResult{
			StepName:   r.StepName,
			OutputType: outputType,
			Output:     r.Output,
			Ops:        r.Ops,
		},
	}
}

func (pr *kbPipelineRunner) FindSuspension(root, nodeTitle string) (*repl.Suspension, string, error) {
	pending, err := pr.store.FindPending(pr.ctx(), root)
	if err != nil {
		return nil, "", err
	}
	for _, p := range pending {
		if p.Target == nodeTitle {
			sus := pr.pipelineToSuspension(p)
			return sus, p.ID, nil
		}
	}
	return nil, "", fmt.Errorf("no pending pipeline for %q", nodeTitle)
}

func (pr *kbPipelineRunner) FindSuspensionBySubject(root, subject string) (*repl.Suspension, error) {
	p, err := pr.store.Load(pr.ctx(), subject)
	if err != nil {
		return nil, err
	}
	return pr.pipelineToSuspension(p), nil
}

func (pr *kbPipelineRunner) FindSuspensions(root, nodeTitle string) ([]repl.Suspension, error) {
	pending, err := pr.store.FindPending(pr.ctx(), root)
	if err != nil {
		return nil, err
	}
	var result []repl.Suspension
	for _, p := range pending {
		if p.Target == nodeTitle {
			result = append(result, *pr.pipelineToSuspension(p))
		}
	}
	return result, nil
}

func (pr *kbPipelineRunner) ListSuspensions(root string) ([]repl.Suspension, error) {
	pending, err := pr.store.FindPending(pr.ctx(), root)
	if err != nil {
		return nil, err
	}
	var result []repl.Suspension
	for _, p := range pending {
		result = append(result, *pr.pipelineToSuspension(p))
	}
	return result, nil
}

func (pr *kbPipelineRunner) WriteSuspension(root, nodeTitle, targetLabel string, block *repl.BlockTarget,
	fnName, step, gate, outputType, rawOutput string,
	stepIndex int, summary string, ops []repl.FileOp, backendName string) {
	// Agent mode submit: create a pipeline in Pending state
	p := function.NewPipeline(root, fnName, nodeTitle)
	p.BackendName = backendName
	p.CurrentStep = stepIndex
	result := function.TransformResult{Raw: rawOutput, IsText: outputType == "text", Ops: ops}
	p.CurrentResult = &result
	// Force to pending phase
	p.Phase = function.PhasePending
	_ = pr.store.Save(pr.ctx(), p)
}

func (pr *kbPipelineRunner) ResolveSuspension(subject, status string) error {
	// Pure state transition — the REPL manages continuation separately.
	p, err := pr.store.Load(pr.ctx(), subject)
	if err != nil {
		return err
	}
	switch status {
	case "accepted":
		_ = p.Accept()
	case "rejected":
		_ = p.Reject()
	}
	return pr.store.Save(pr.ctx(), p)
}

func (pr *kbPipelineRunner) ReviseStep(cfg repl.ReviseConfig) (*repl.ReviseResult, error) {
	deps := pr.buildDeps(cfg.BackendName)

	ar, err := workflow.AcceptPipeline(pr.ctx(), deps, cfg.Root, cfg.SusSubject, cfg.Feedback)
	if err != nil {
		return nil, err
	}

	fn, _, _ := function.LoadFunction(cfg.FuncName)
	isLast := false
	if fn != nil {
		steps := fn.EffectiveSteps()
		// Find step index from step name
		for i, s := range steps {
			if s.Name == ar.StepName && i >= len(steps)-1 {
				isLast = true
			}
		}
	}

	outputType := "text"
	if !ar.IsText && len(ar.Ops) > 0 {
		outputType = "ops"
	}

	return &repl.ReviseResult{
		StepName:   ar.StepName,
		OutputType: outputType,
		IsLast:     isLast,
		LLMOutput:  ar.Output,
	}, nil
}

func (pr *kbPipelineRunner) pipelineToSuspension(p *function.Pipeline) *repl.Suspension {
	fn, _, _ := function.LoadFunction(p.FunctionName)
	stepName := ""
	gateType := ""
	if fn != nil {
		steps := fn.EffectiveSteps()
		if p.CurrentStep < len(steps) {
			stepName = steps[p.CurrentStep].Name
			if steps[p.CurrentStep].Gate != nil {
				gateType = "approve"
			}
		}
	}

	outputType := "text"
	var output string
	var ops []repl.FileOp
	if p.CurrentResult != nil {
		output = p.CurrentResult.Raw
		ops = p.CurrentResult.Ops
		if !p.CurrentResult.IsText {
			outputType = "ops"
		}
	}

	return &repl.Suspension{
		Subject:    p.ID,
		Root:       p.Root,
		Function:   p.FunctionName,
		Target:     p.Target,
		StepName:   stepName,
		StepIndex:  p.CurrentStep,
		GateType:   gateType,
		Output:     output,
		OutputType: outputType,
		Ops:        ops,
		Backend:    p.BackendName,
	}
}

// --- DiscussionRunner interface ---

type kbDiscussionRunner struct {
	k         *kb.KB
	proj      *projmd.MarkdownProjection
	store     *function.PipelineStore
	globalCfg config.GlobalConfig
}

func newDiscussionRunner(k *kb.KB, proj *projmd.MarkdownProjection, store *function.PipelineStore, cfg config.GlobalConfig) *kbDiscussionRunner {
	return &kbDiscussionRunner{k: k, proj: proj, store: store, globalCfg: cfg}
}

func (dr *kbDiscussionRunner) buildDeps() *workflow.Deps {
	be, _ := backend.FromConfig(dr.globalCfg, "")
	var tb function.TransformBackend
	if be != nil {
		tb = function.NewLLMBackend(be)
	}
	return &workflow.Deps{
		KB:      dr.k,
		Proj:    dr.proj,
		Store:   dr.store,
		Backend: tb,
	}
}

func (dr *kbDiscussionRunner) StartDiscussion(root, nodeTitle string) (*repl.DiscussionState, string, error) {
	deps := dr.buildDeps()
	state, output, err := workflow.StartDiscussion(context.Background(), deps, root, nodeTitle)
	if err != nil {
		return nil, "", err
	}
	return &repl.DiscussionState{
		DiscussTitle:  state.DiscussTitle,
		FilePath:      state.FilePath,
		InitialCommit: state.InitialCommit,
		FileCreated:   state.FileCreated,
		FocusTitle:    state.FocusTitle,
	}, output, nil
}

func (dr *kbDiscussionRunner) ContinueDiscussion(root string, state *repl.DiscussionState, userInput string) (string, error) {
	deps := dr.buildDeps()
	wState := &workflow.DiscussionState{
		DiscussTitle:  state.DiscussTitle,
		FilePath:      state.FilePath,
		InitialCommit: state.InitialCommit,
		FileCreated:   state.FileCreated,
		FocusTitle:    state.FocusTitle,
	}
	output, err := workflow.ContinueDiscussion(context.Background(), deps, root, wState, userInput)
	if err != nil {
		return "", err
	}
	// Update state in case file path changed.
	state.FilePath = wState.FilePath
	return output, nil
}

func (dr *kbDiscussionRunner) EndDiscussion(root string, state *repl.DiscussionState) (string, error) {
	deps := dr.buildDeps()
	wState := &workflow.DiscussionState{
		DiscussTitle:  state.DiscussTitle,
		FilePath:      state.FilePath,
		InitialCommit: state.InitialCommit,
		FileCreated:   state.FileCreated,
		FocusTitle:    state.FocusTitle,
	}
	return workflow.EndDiscussion(context.Background(), deps, root, wState)
}

func (dr *kbDiscussionRunner) CancelDiscussion(root string, state *repl.DiscussionState) error {
	deps := dr.buildDeps()
	wState := &workflow.DiscussionState{
		DiscussTitle:  state.DiscussTitle,
		FilePath:      state.FilePath,
		InitialCommit: state.InitialCommit,
		FileCreated:   state.FileCreated,
		FocusTitle:    state.FocusTitle,
	}
	return workflow.CancelDiscussion(context.Background(), deps, root, wState)
}

func (dr *kbDiscussionRunner) IsThreaded(filePath string) bool {
	return workflow.IsThreaded(filePath)
}

// ensure interfaces are satisfied at compile time
var (
	_ repl.GraphQuerier     = (*kbGraphQuerier)(nil)
	_ repl.ApplyRunner      = (*kbApplyRunner)(nil)
	_ repl.TemplateRunner   = (*kbTemplateRunner)(nil)
	_ repl.PipelineRunner   = (*kbPipelineRunner)(nil)
	_ repl.DiscussionRunner = (*kbDiscussionRunner)(nil)
)
