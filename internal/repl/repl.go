// Package repl implements an interactive REPL for sevens.
// It uses injected interfaces for graph queries, pipeline execution,
// apply operations, and template operations — no direct imports of the
// old apply, engine, graph, or store packages.
package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"olympos.io/encoding/edn"
	"sevens/internal/backend"
	"sevens/internal/config"
	"sevens/internal/function"
	"sevens/internal/kb"
	projmd "sevens/internal/projection/md"
	"sevens/internal/ui"
)

// Mode represents the REPL's current input mode.
type Mode int

const (
	ModeNormal     Mode = iota
	ModeNote            // collecting note text
	ModeDiscussion      // multi-turn conversation
)

// REPL holds state for an interactive sevens session.
type REPL struct {
	root               string
	focus              string // focused node title; "" = no focus
	focusBlock         *BlockListEntry
	includes           []string // extra context nodes for apply calls
	dryRun             bool
	modelFlag          string   // model override; "" = use globalCfg default
	backendName        string   // backend override; "" = use globalCfg default
	lastList           []string // last numbered list printed (for numeric nav)
	lastBlocks         []BlockListEntry
	globalCfg          config.GlobalConfig
	mode               Mode
	noteLines          []string // buffer for note mode
	discussState       *DiscussionState // active discussion state (nil when not in discussion)
	kbInstance         *kb.KB   // new architecture KB

	// Injected interfaces (implementations provided by CLI layer).
	graphQ     GraphQuerier
	pipelineR  PipelineRunner
	applyR     ApplyRunner
	templateR  TemplateRunner
	discussR   DiscussionRunner

	rl *readline.Instance
}

// Option configures optional REPL dependencies.
type Option func(*REPL)

// WithKB injects a KB instance for commands that have been migrated to the new architecture.
func WithKB(k *kb.KB) Option {
	return func(r *REPL) { r.kbInstance = k }
}

// WithGraphQuerier injects the graph query implementation.
func WithGraphQuerier(q GraphQuerier) Option {
	return func(r *REPL) { r.graphQ = q }
}

// WithPipelineRunner injects the pipeline/suspension implementation.
func WithPipelineRunner(p PipelineRunner) Option {
	return func(r *REPL) { r.pipelineR = p }
}

// WithApplyRunner injects the apply operations implementation.
func WithApplyRunner(a ApplyRunner) Option {
	return func(r *REPL) { r.applyR = a }
}

// WithTemplateRunner injects the template operations implementation.
func WithTemplateRunner(t TemplateRunner) Option {
	return func(r *REPL) { r.templateR = t }
}

// WithDiscussionRunner injects the discussion workflow implementation.
func WithDiscussionRunner(d DiscussionRunner) Option {
	return func(r *REPL) { r.discussR = d }
}

// New creates a REPL and initialises readline. focusNode may be "".
func New(root string, focusNode string, globalCfg config.GlobalConfig, opts ...Option) (*REPL, error) {
	// Apply theme from config if set.
	if globalCfg.Theme != "" {
		ui.SetTheme(globalCfg.Theme)
	}

	r := &REPL{
		root:      root,
		focus:     focusNode,
		globalCfg: globalCfg,
	}
	for _, o := range opts {
		o(r)
	}

	// Resolve initial focus to canonical case.
	if focusNode != "" && r.graphQ != nil {
		if canonical := r.graphQ.ResolveTitle(focusNode, root); canonical != "" {
			r.focus = canonical
		}
	}

	histPath, _ := historyFile() // non-fatal if unavailable

	rl, err := readline.NewEx(&readline.Config{
		Prompt:            r.prompt(),
		HistoryFile:       histPath,
		AutoComplete:      newCompleter(r),
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		HistorySearchFold: true,
	})
	if err != nil {
		return nil, fmt.Errorf("initializing readline: %w", err)
	}
	r.rl = rl
	return r, nil
}

// Run starts the interactive loop.
func (r *REPL) Run() error {
	defer r.rl.Close()

	r.printSystem("sevens repl  —  .help for commands, .quit to exit")
	if r.focus != "" {
		r.printSystem("focused: %s", r.focus)
	}
	fmt.Println()

	for {
		line, err := r.rl.Readline()
		if err == readline.ErrInterrupt {
			if strings.TrimSpace(line) == "" {
				return nil
			}
			continue
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("readline: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			if r.mode == ModeNote {
				// Empty line in note mode submits the note.
				if err := r.endNote(); err != nil {
					r.printError(err.Error())
				}
			}
			continue
		}

		// Mode-specific input handling.
		switch r.mode {
		case ModeNote:
			if err := r.handleNoteInput(line); err != nil {
				r.printError(err.Error())
			}
			continue
		case ModeDiscussion:
			if err := r.handleDiscussionInput(line); err != nil {
				r.printError(err.Error())
			}
			continue
		}

		if err := r.dispatch(line); err != nil {
			r.printError(err.Error())
		}
	}
}

// ─── Prompt ───────────────────────────────────────────────────────────────────

func (r *REPL) prompt() string {
	title := "sevens"
	if r.focus != "" {
		title = truncateTitle(r.focus, 42)
	}
	if r.focusBlock != nil {
		title = truncateTitle(fmt.Sprintf("%s#%s", r.focus, r.focusBlock.Path), 42)
	}
	return promptStyle.Render(title+">") + " "
}

func (r *REPL) updatePrompt() {
	if r.rl == nil {
		return
	}
	r.rl.SetPrompt(r.prompt())
}

func (r *REPL) setFocus(title string) {
	r.focus = title
	r.focusBlock = nil
	r.lastList = nil
	r.lastBlocks = nil
	r.updatePrompt()
}

func (r *REPL) setFocusBlock(block BlockListEntry) {
	r.focusBlock = &block
	r.updatePrompt()
}

func (r *REPL) clearFocusBlock() {
	r.focusBlock = nil
	r.updatePrompt()
}

func truncateTitle(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "…"
}

// ─── Output helpers ───────────────────────────────────────────────────────────

func (r *REPL) printSystem(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, systemStyle.Render(msg))
}

func (r *REPL) printError(msg string) {
	fmt.Fprintln(os.Stderr, ui.Error.Render("[error]")+" "+msg)
}

// printList prints a numbered list and stores titles for subsequent numeric nav.
func (r *REPL) printList(titles []string) {
	r.lastList = titles
	r.lastBlocks = nil
	if len(titles) == 0 {
		r.printSystem("(none)")
		return
	}
	width := len(fmt.Sprintf("%d", len(titles)))
	for i, t := range titles {
		num := fmt.Sprintf("%*d.", width, i+1)
		fmt.Printf("  %s %s\n",
			listNumStyle.Render(num),
			listItemStyle.Render(t),
		)
	}
}

// ─── State helpers ────────────────────────────────────────────────────────────

func (r *REPL) requireFocus() (string, error) {
	if r.focus == "" {
		return "", fmt.Errorf("no node focused — type a node title or 'focus <title>'")
	}
	return r.focus, nil
}

func (r *REPL) nodeTitles() []string {
	if r.graphQ == nil {
		return nil
	}
	titles, _ := r.graphQ.ListNodeTitles(r.root)
	return titles
}

func (r *REPL) validateRootFlag(explicit string) error {
	if explicit == "" {
		return nil
	}
	want, err := filepath.Abs(explicit)
	if err != nil {
		return fmt.Errorf("resolving --root: %w", err)
	}
	have, err := filepath.Abs(r.root)
	if err != nil {
		return fmt.Errorf("resolving current root: %w", err)
	}
	if want != have {
		return fmt.Errorf("REPL session is bound to root %q; --root %q is not supported here", have, want)
	}
	return nil
}

func printEDN(v any) error {
	enc := edn.NewEncoder(os.Stdout)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encoding EDN: %w", err)
	}
	return nil
}

func functionNames() []string {
	names, err := function.ListFunctions()
	if err != nil {
		return nil
	}
	return names
}

func isFunctionName(name string) bool {
	names, err := function.ListFunctions()
	if err != nil {
		return false
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func (r *REPL) isNodeTitle(s string) bool {
	if r.graphQ == nil {
		return false
	}
	return r.graphQ.ResolveTitle(s, r.root) != ""
}

// resolveTitle returns the canonical (as-stored) title for a case-insensitive input.
func (r *REPL) resolveTitle(s string) string {
	if r.graphQ == nil {
		return ""
	}
	return r.graphQ.ResolveTitle(s, r.root)
}

func opName(op FileOp) string {
	if op.Title != "" {
		return op.Title
	}
	if op.File != "" {
		return op.File
	}
	return "unknown"
}

func historyFile() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "repl-history"), nil
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (r *REPL) resolveSuspensionBlock(root, nodeTitle string, sus *Suspension) *BlockTarget {
	if sus == nil || r.graphQ == nil {
		return nil
	}
	if sus.BlockID != "" {
		if block, err := r.graphQ.ResolveBlockTargetBySubject(sus.BlockID); err == nil {
			return block
		}
	}
	if sus.BlockPath != "" {
		if block, err := r.graphQ.ResolveBlockTarget(root, nodeTitle, sus.BlockPath); err == nil {
			return block
		}
	}
	return nil
}

// ─── Effective config ─────────────────────────────────────────────────────────

func (r *REPL) effectiveCfg() config.GlobalConfig {
	cfg := r.globalCfg
	if r.modelFlag != "" {
		resolved := cfg.ResolveModel(r.modelFlag)
		if resolved.Model != cfg.LLM.Model || r.modelFlag == resolved.Model {
			cfg.LLM = resolved
		} else {
			cfg.LLM.Model = r.modelFlag
		}
	}
	return cfg
}

// ─── Pipeline runner ──────────────────────────────────────────────────────────

// pipelineOpts controls optional behavior of runPipeline.
type pipelineOpts struct {
	instruction    string // ad-hoc instruction for {{instruction}}
	skipAutoAccept bool   // if true, don't auto-enter y/n/r after suspension
	suppressHint   bool   // if true, don't print the manual accept hint when skipAutoAccept is set
	blockPath      string
	blockID        string
	includes       []string
}

func (r *REPL) runPipeline(nodeTitle string, fnDef *FunctionDef, startStep int, prev string, dryRun bool, opts ...pipelineOpts) error {
	if r.pipelineR == nil {
		return fmt.Errorf("pipeline runner not available")
	}

	globalCfg := r.effectiveCfg()

	var opt pipelineOpts
	if len(opts) > 0 {
		opt = opts[0]
	}

	// Build context string from context files.
	var ctxFiles []string
	ctxFiles = append(ctxFiles, globalCfg.ContextFiles...)
	ctxFiles = append(ctxFiles, fnDef.ContextFiles...)

	// Note: context files from frontmatter are no longer carried on WalkNode.
	// They are resolved at the adapter layer if needed.

	contextStr := ""
	if r.applyR != nil {
		contextStr = r.applyR.LoadContextFiles(r.root, ctxFiles)
	}

	// Auto-include group if node has include-group: true in frontmatter.
	var autoIncludes []string
	if r.graphQ != nil {
		autoIncludes, _ = r.graphQ.AutoGroupIncludes(r.root, nodeTitle)
	}
	allIncludes := append([]string(nil), r.includes...)
	allIncludes = append(allIncludes, opt.includes...)
	allIncludes = append(allIncludes, autoIncludes...)

	for _, inc := range allIncludes {
		if r.graphQ == nil {
			continue
		}
		incWalk, err := r.graphQ.BuildWalk(r.root, inc, "minimal")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s included node %q: %v\n",
				ui.Warning.Render("[warn]"), inc, err)
			continue
		}
		contextStr += fmt.Sprintf("<included-node title=%q>\n%s\n</included-node>\n\n",
			inc, incWalk.Target.Content)
	}

	var be backend.Backend
	var err error
	if !dryRun {
		be, err = backend.FromConfig(globalCfg, r.backendName)
		if err != nil {
			return fmt.Errorf("initializing backend: %w", err)
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", systemStyle.Render("[backend]"), be.Name())
	}

	cfg := PipelineConfig{
		Root:          r.root,
		NodeTitle:     nodeTitle,
		FunctionName:  fnDef.Name,
		StartStep:     startStep,
		PrevOutput:    prev,
		DryRun:        dryRun,
		ModelOverride: r.modelFlag,
		BackendName:   r.backendName,
		Instruction:   opt.instruction,
		ContextStr:    contextStr,
		StreamText:    true,
		Includes:      allIncludes,
	}

	if opt.blockID != "" && r.graphQ != nil {
		block, err := r.graphQ.ResolveBlockTargetBySubject(opt.blockID)
		if err != nil {
			return fmt.Errorf("resolving block target: %w", err)
		}
		cfg.TargetBlock = block
	} else if opt.blockPath != "" && r.graphQ != nil {
		block, err := r.graphQ.ResolveBlockTarget(r.root, nodeTitle, opt.blockPath)
		if err != nil {
			return fmt.Errorf("resolving block target: %w", err)
		}
		cfg.TargetBlock = block
	}

	targetLabel := nodeTitle
	if cfg.TargetBlock != nil {
		targetLabel = cfg.TargetBlock.Label()
	}

	result, err := r.pipelineR.RunPipeline(cfg)
	if err != nil {
		return err
	}

	// In dry-run mode the engine already printed the prompt to stdout — don't repeat it.
	if dryRun {
		return nil
	}

	if result.Suspended && result.Suspension != nil {
		sus := result.Suspension
		fmt.Fprintln(os.Stderr, ui.FormatStep(sus.Function, sus.StepName, orDefault(sus.TargetLabel, sus.Target)))
		if sus.OutputType == "ops" && len(sus.Ops) > 0 {
			for _, op := range sus.Ops {
				fmt.Fprintln(os.Stderr, ui.FormatOp(op.Action, opName(op)))
			}
		} else if sus.Output != "" {
			fmt.Println(ui.RenderMarkdownOrPlain(sus.Output))
		}
		if opt.skipAutoAccept {
			if !opt.suppressHint {
				fmt.Fprintf(os.Stderr, "\n%s\n", systemStyle.Render("→ type 'accept' to apply"))
			}
			return nil
		}
		return r.handleAccept([]string{"accept"})
	} else if result.Result != nil {
		res := result.Result
		if res.OutputType == "ops" && len(res.Ops) == 0 {
			fmt.Fprintln(os.Stderr, ui.FormatStep(fnDef.Name, res.StepName, targetLabel))
			r.printSystem("No changes proposed.")
		} else if res.Output != "" && res.OutputType == "text" && be == nil {
			// Text was already streamed via API streaming — just show the label.
			fmt.Fprintln(os.Stderr, ui.FormatStep(fnDef.Name, res.StepName, targetLabel))
		} else if res.Output != "" {
			fmt.Fprintln(os.Stderr, ui.FormatStep(fnDef.Name, res.StepName, targetLabel))
			fmt.Println(ui.RenderMarkdownOrPlain(res.Output))
		}
	}

	return nil
}

// ─── Accept / reject ─────────────────────────────────────────────────────────

// doAccept accepts the pending suspension for nodeTitle.
// If susSubjectOverride is non-empty it selects that specific suspension by ID,
// bypassing the ambiguity check (used after the caller has already resolved it).
func (r *REPL) doAccept(nodeTitle, withFeedback string, susSubjectOverride ...string) error {
	if r.pipelineR == nil {
		return fmt.Errorf("pipeline runner not available")
	}

	var sus *Suspension
	var susSubject string
	var err error

	if len(susSubjectOverride) > 0 && susSubjectOverride[0] != "" {
		sus, err = r.pipelineR.FindSuspensionBySubject(r.root, susSubjectOverride[0])
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suspension with id %q", susSubjectOverride[0])
		}
		susSubject = susSubjectOverride[0]
	} else {
		sus, susSubject, err = r.pipelineR.FindSuspension(r.root, nodeTitle)
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suggestions for %q", nodeTitle)
		}
	}

	fnDef, err := r.loadFunctionDef(sus.Function)
	if err != nil {
		return fmt.Errorf("loading function: %w", err)
	}

	if withFeedback != "" {
		revResult, err := r.pipelineR.ReviseStep(ReviseConfig{
			Root:        r.root,
			NodeTitle:   nodeTitle,
			FuncName:    sus.Function,
			SusSubject:  susSubject,
			Feedback:    withFeedback,
			BackendName: r.backendName,
			ModelFlag:   r.modelFlag,
		})
		if err != nil {
			return err
		}
		if revResult == nil || revResult.NewEntry == nil {
			r.printSystem("cancelled")
			return nil
		}

		if revResult.IsLast && revResult.OutputType == "ops" && revResult.NewEntry != nil {
			for _, op := range revResult.NewEntry.Ops {
				fmt.Fprintln(os.Stderr, ui.FormatOp(op.Action, opName(op)))
			}
		} else {
			fmt.Fprintln(os.Stderr, ui.FormatStep(sus.Function, revResult.StepName, orDefault(sus.TargetLabel, nodeTitle)))
			if revResult.LLMOutput != "" {
				fmt.Println(ui.RenderMarkdownOrPlain(revResult.LLMOutput))
			}
		}
		// WriteSuspension and ResolveSuspension handled by the ReviseStep implementation.
		return nil
	}

	// No feedback: continue pipeline or execute ops.
	steps := fnDef // we need step count from the function
	_ = steps

	// Check if there's a next step by checking sus.StepIndex against the function.
	// The pipeline runner handles the actual step counting internally.
	// For now, delegate fully to the pipeline runner for continuation.

	// If the suspension output type is not "ops", or there are more steps,
	// log and resolve, then continue.

	if sus.OutputType != "ops" || sus.StepIndex == 0 {
		// Log acceptance.
		if r.applyR != nil {
			_ = r.applyR.AppendLog(LogEntry{
				Event:     "accepted",
				Root:      r.root,
				Function:  sus.Function,
				Target:    nodeTitle,
				Timestamp: nowISO(),
			})
		}

		// Try to continue the pipeline from the next step.
		r.pipelineR.ResolveSuspension(susSubject, "accepted")
		return r.runPipeline(nodeTitle, fnDef, sus.StepIndex+1, sus.Output, false, pipelineOpts{
			blockPath: sus.BlockPath,
			blockID:   sus.BlockID,
		})
	}

	// Execute ops.
	if r.applyR == nil {
		return fmt.Errorf("apply runner not available")
	}

	created, edited, err := r.applyR.ExecuteOps(sus.Ops, r.root)
	if err != nil {
		return fmt.Errorf("executing ops: %w", err)
	}

	_ = r.applyR.AppendLog(LogEntry{
		Event:     "accepted",
		Root:      r.root,
		Function:  sus.Function,
		Target:    nodeTitle,
		Timestamp: nowISO(),
	})
	r.pipelineR.ResolveSuspension(susSubject, "accepted")

	commitHash := ""
	if projmd.IsGitRepo(r.root) {
		allFiles := append(created, edited...)
		if len(allFiles) > 0 {
			h, cerr := projmd.CommitFiles(r.root,
				fmt.Sprintf("sevens: apply %s to %q", sus.Function, nodeTitle), allFiles)
			if cerr != nil {
				fmt.Fprintf(os.Stderr, "%s git commit failed: %v\n", ui.Warning.Render("[warn]"), cerr)
			} else {
				commitHash = h
			}
		}
	}

	_ = r.applyR.AppendLog(LogEntry{
		Event:        "applied",
		Root:         r.root,
		Function:     sus.Function,
		Target:       nodeTitle,
		Timestamp:    nowISO(),
		Commit:       commitHash,
		FilesCreated: created,
		FilesEdited:  edited,
	})

	label := ui.Success.Render("[accept]")
	if len(created) > 0 {
		fmt.Fprintf(os.Stderr, "%s created: %s\n", label, strings.Join(created, ", "))
	}
	if len(edited) > 0 {
		fmt.Fprintf(os.Stderr, "%s edited: %s\n", label, strings.Join(edited, ", "))
	}
	if commitHash != "" {
		fmt.Fprintf(os.Stderr, "%s %s\n", label, systemStyle.Render("commit "+commitHash))
	}

	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}
	return nil
}

// doReject rejects the pending suspension for nodeTitle.
// If susSubjectOverride is non-empty it selects that specific suspension by ID.
func (r *REPL) doReject(nodeTitle string, susSubjectOverride ...string) error {
	if r.pipelineR == nil {
		return fmt.Errorf("pipeline runner not available")
	}

	var sus *Suspension
	var susSubject string
	var err error

	if len(susSubjectOverride) > 0 && susSubjectOverride[0] != "" {
		sus, err = r.pipelineR.FindSuspensionBySubject(r.root, susSubjectOverride[0])
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suspension with id %q", susSubjectOverride[0])
		}
		susSubject = susSubjectOverride[0]
	} else {
		sus, susSubject, err = r.pipelineR.FindSuspension(r.root, nodeTitle)
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suggestions for %q", nodeTitle)
		}
	}
	_ = sus

	if r.applyR != nil {
		_ = r.applyR.AppendLog(LogEntry{
			Event:     "rejected",
			Root:      r.root,
			Target:    nodeTitle,
			Timestamp: nowISO(),
		})
	}
	r.pipelineR.ResolveSuspension(susSubject, "rejected")
	fmt.Fprintf(os.Stderr, "%s %s\n", ui.Warning.Render("rejected"), ui.NodeTitle.Render(nodeTitle))
	return nil
}

// ─── Revert ──────────────────────────────────────────────────────────────────

func (r *REPL) doRevert(nodeTitle string) error {
	if !projmd.IsGitRepo(r.root) {
		return fmt.Errorf("not a git repo — cannot revert")
	}
	if r.applyR == nil {
		return fmt.Errorf("apply runner not available")
	}

	// Find the last "applied" log entry with a commit hash for this node.
	entries, err := r.applyR.ReadLog(r.root, nodeTitle)
	if err != nil {
		return fmt.Errorf("reading log: %w", err)
	}

	var target *LogEntry
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Event == "applied" && entries[i].Commit != "" {
			e := entries[i]
			target = &e
			break
		}
	}
	if target == nil {
		return fmt.Errorf("no applied commit found in log for %q", nodeTitle)
	}

	fmt.Fprintf(os.Stderr, "%s reverting %s (%s, commit %s)\n",
		systemStyle.Render("[revert]"),
		ui.NodeTitle.Render(nodeTitle),
		target.Function, target.Commit)

	newHash, err := r.applyR.RevertCommit(r.root, target.Commit)
	if err != nil {
		return err
	}

	// Log the revert.
	_ = r.applyR.AppendLog(LogEntry{
		Event:     "reverted",
		Root:      r.root,
		Function:  target.Function,
		Target:    nodeTitle,
		Commit:    newHash,
		Note:      fmt.Sprintf("reverted commit %s", target.Commit),
		Timestamp: nowISO(),
	})

	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}

	fmt.Fprintf(os.Stderr, "%s reverted to %s\n", ui.Success.Render("[revert]"), newHash)
	return nil
}

// ─── Note mode ───────────────────────────────────────────────────────────────

func (r *REPL) enterNote() error {
	if _, err := r.requireFocus(); err != nil {
		return err
	}
	r.mode = ModeNote
	r.noteLines = nil
	r.rl.SetPrompt(modeStyle.Render("[note]>") + " ")
	r.printSystem(".end to save, .cancel to discard")
	return nil
}

func (r *REPL) handleNoteInput(line string) error {
	switch line {
	case ".end", ".done":
		return r.endNote()
	case ".cancel":
		r.noteLines = nil
		r.mode = ModeNormal
		r.updatePrompt()
		r.printSystem("note discarded")
		return nil
	default:
		r.noteLines = append(r.noteLines, line)
		return nil
	}
}

func (r *REPL) endNote() error {
	if len(r.noteLines) == 0 {
		r.mode = ModeNormal
		r.updatePrompt()
		r.printSystem("empty note, nothing saved")
		return nil
	}

	focus := r.focus
	noteText := strings.Join(r.noteLines, "\n")

	// Find the node's file.
	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	subject, _ := r.graphQ.ResolveNode(focus, r.root)
	if subject == "" {
		return fmt.Errorf("could not resolve node %q", focus)
	}
	filePath, err := r.graphQ.GetObject(subject, "node/file-path")
	if err != nil || filePath == "" {
		// Try the new predicate name.
		filePath, err = r.graphQ.GetObject(subject, "node/file")
		if err != nil || filePath == "" {
			return fmt.Errorf("could not find file for %q", focus)
		}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filePath, err)
	}

	content := string(data)

	// Append under existing ## Notes section, or create one.
	if strings.Contains(content, "## Notes") {
		content = strings.TrimRight(content, "\n") + "\n\n" + noteText + "\n"
	} else {
		content = strings.TrimRight(content, "\n") + "\n\n## Notes\n\n" + noteText + "\n"
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", filePath, err)
	}

	// Commit.
	commitHash := ""
	if projmd.IsGitRepo(r.root) {
		h, cerr := projmd.CommitFiles(r.root, fmt.Sprintf("sevens: note on %q", focus), []string{filePath})
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "%s git commit: %v\n", ui.Warning.Render("[warn]"), cerr)
		} else {
			commitHash = h
		}
	}

	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}

	msg := fmt.Sprintf("appended note to %q", focus)
	if commitHash != "" {
		msg += " (" + commitHash + ")"
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", ui.Success.Render("[note]"), msg)

	r.noteLines = nil
	r.mode = ModeNormal
	r.updatePrompt()
	return nil
}

// ─── New node ────────────────────────────────────────────────────────────────

func (r *REPL) handleNew(title string) error {
	parent, err := r.requireFocus()
	if err != nil {
		return err
	}

	filename := projmd.SanitizeFilename(title)
	path := filepath.Join(r.root, filename)

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("file already exists: %s", filename)
	}

	// Write minimal scaffolding.
	var content strings.Builder
	content.WriteString("---\n")
	content.WriteString("title: " + title + "\n")
	content.WriteString("parent: \"[[" + parent + "]]\"\n")
	content.WriteString("---\n\n")

	if err := os.WriteFile(path, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", filename, err)
	}

	// Commit.
	if projmd.IsGitRepo(r.root) {
		h, cerr := projmd.CommitFiles(r.root, fmt.Sprintf("sevens: new %q under %q", title, parent), []string{path})
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "%s git commit: %v\n", ui.Warning.Render("[warn]"), cerr)
		} else {
			fmt.Fprintf(os.Stderr, "%s %s → %s (%s)\n",
				ui.Success.Render("[new]"), title, filename, h)
		}
	} else {
		fmt.Fprintf(os.Stderr, "%s %s → %s\n",
			ui.Success.Render("[new]"), title, filename)
	}

	// Resync and focus the new node.
	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}
	r.setFocus(title)
	return nil
}

// ─── Group include ───────────────────────────────────────────────────────────

func (r *REPL) includeGroup(name string) error {
	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	cfg, err := r.graphQ.LoadConfig(r.root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	group, ok := cfg.Groups[name]
	if !ok {
		// List available groups for the error.
		var names []string
		for k := range cfg.Groups {
			names = append(names, k)
		}
		if len(names) == 0 {
			return fmt.Errorf("no groups defined in .sevens.edn")
		}
		return fmt.Errorf("group %q not found (available: %s)", name, strings.Join(names, ", "))
	}

	titles, err := r.graphQ.ResolveGroup(r.root, group)
	if err != nil {
		return fmt.Errorf("resolving group %q: %w", name, err)
	}

	// Add all resolved titles to includes (deduplicated).
	seen := make(map[string]bool, len(r.includes))
	for _, t := range r.includes {
		seen[t] = true
	}
	added := 0
	for _, t := range titles {
		if !seen[t] {
			r.includes = append(r.includes, t)
			seen[t] = true
			added++
		}
	}
	r.printSystem("included group %q: %d nodes", name, added)
	return nil
}

func (r *REPL) resync() error {
	if r.graphQ != nil {
		return r.graphQ.Resync(r.root)
	}
	return nil
}

// resyncQuiet runs resync but suppresses stderr output.
func (r *REPL) resyncQuiet() error {
	old := os.Stderr
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return r.resync() // fallback to noisy
	}
	os.Stderr = devNull
	syncErr := r.resync()
	os.Stderr = old
	devNull.Close()
	return syncErr
}

// loadFunctionDef loads a function definition through the apply runner,
// or falls back to function.LoadFunction if no apply runner.
func (r *REPL) loadFunctionDef(name string) (*FunctionDef, error) {
	if r.applyR != nil {
		return r.applyR.LoadFunction(name)
	}
	// Fallback: use function package directly (no old-package import needed).
	fn, edn, err := function.LoadFunction(name)
	if err != nil {
		return nil, err
	}
	var ctxFiles []string
	if edn != nil {
		ctxFiles = edn.ContextFiles
	}
	return &FunctionDef{
		Name:         fn.Name,
		Description:  fn.Description,
		ContextFiles: ctxFiles,
	}, nil
}

// ─── Unused context suppression ─────────────────────────────────────────────

var _ = context.Background // ensure context import for future use
