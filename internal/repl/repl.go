// Package repl implements an interactive REPL for sevens.
// It wraps the same internal pipeline, graph, and store packages as the CLI,
// but with persistent focus state, shorter syntax, and inline navigation.
package repl

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"olympos.io/encoding/edn"
	"sevens/internal/apply"
	"sevens/internal/backend"
	"sevens/internal/config"
	"sevens/internal/engine"
	"sevens/internal/function"
	"sevens/internal/graph"
	"sevens/internal/kb"
	projmd "sevens/internal/projection/md"
	"sevens/internal/store"
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
	db                 *sql.DB
	root               string
	focus              string // focused node title; "" = no focus
	focusBlock         *graph.BlockListEntry
	includes           []string // extra context nodes for apply calls
	dryRun             bool
	modelFlag          string   // model override; "" = use globalCfg default
	backendName        string   // backend override; "" = use globalCfg default
	lastList           []string // last numbered list printed (for numeric nav)
	lastBlocks         []graph.BlockListEntry
	globalCfg          config.GlobalConfig
	mode               Mode
	noteLines          []string // buffer for note mode
	discussNode        string   // "Discussion - <Title>" when in discussion mode
	discussFileCreated bool     // true if the discussion file was created during this session
	discussFilePath    string   // absolute path to the discussion file
	discussCommit      string   // short commit hash if a commit was made during enterDiscussion
	kbInstance         *kb.KB   // new architecture KB; nil if not provided

	rl *readline.Instance
}

// Option configures optional REPL dependencies.
type Option func(*REPL)

// WithKB injects a KB instance for commands that have been migrated to the new architecture.
func WithKB(k *kb.KB) Option {
	return func(r *REPL) { r.kbInstance = k }
}

// New creates a REPL and initialises readline. focusNode may be "".
func New(db *sql.DB, root string, focusNode string, globalCfg config.GlobalConfig, opts ...Option) (*REPL, error) {
	// Apply theme from config if set.
	if globalCfg.Theme != "" {
		ui.SetTheme(globalCfg.Theme)
	}

	// Resolve initial focus to canonical case.
	if focusNode != "" {
		if canonical := store.ResolveTitle(db, focusNode, root); canonical != "" {
			focusNode = canonical
		}
	}

	r := &REPL{
		db:        db,
		root:      root,
		focus:     focusNode,
		globalCfg: globalCfg,
	}
	for _, o := range opts {
		o(r)
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

func (r *REPL) setFocusBlock(block graph.BlockListEntry) {
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
	titles, _ := store.ListNodeTitles(r.db, r.root)
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
	return store.ResolveTitle(r.db, s, r.root) != ""
}

// resolveTitle returns the canonical (as-stored) title for a case-insensitive input.
func (r *REPL) resolveTitle(s string) string {
	return store.ResolveTitle(r.db, s, r.root)
}

func opName(op apply.FileOp) string {
	if op.Title != "" {
		return op.Title
	}
	if op.File != "" {
		return op.File
	}
	return "unknown"
}

func historyFile() (string, error) {
	dir, err := store.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "repl-history"), nil
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func resolveSuspensionBlock(db *sql.DB, root, nodeTitle string, sus *engine.Suspension) *graph.BlockTarget {
	if sus == nil {
		return nil
	}
	if sus.BlockID != "" {
		if block, err := graph.ResolveBlockTargetBySubject(db, sus.BlockID); err == nil {
			return block
		}
	}
	if sus.BlockPath != "" {
		if block, err := graph.ResolveBlockTarget(db, root, nodeTitle, sus.BlockPath); err == nil {
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

func (r *REPL) runPipeline(nodeTitle string, fn *apply.Function, startStep int, prev string, dryRun bool, opts ...pipelineOpts) error {
	walk, err := graph.BuildWalk(r.db, r.root, nodeTitle, 1)
	if err != nil {
		return fmt.Errorf("building walk: %w", err)
	}

	globalCfg := r.effectiveCfg()

	var ctxFiles []string
	ctxFiles = append(ctxFiles, globalCfg.ContextFiles...)
	ctxFiles = append(ctxFiles, fn.ContextFiles...)
	ctxFiles = append(ctxFiles, walk.Node.ContextFiles...)
	contextStr := apply.LoadContextFiles(r.root, ctxFiles)

	var opt pipelineOpts
	if len(opts) > 0 {
		opt = opts[0]
	}

	// Auto-include group if node has include-group: true in frontmatter.
	config, _ := graph.LoadConfig(r.root)
	autoIncludes, _ := graph.AutoGroupIncludes(r.db, r.root, nodeTitle, config)
	allIncludes := append([]string(nil), r.includes...)
	allIncludes = append(allIncludes, opt.includes...)
	allIncludes = append(allIncludes, autoIncludes...)

	for _, inc := range allIncludes {
		incWalk, err := graph.BuildWalk(r.db, r.root, inc, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s included node %q: %v\n",
				ui.Warning.Render("[warn]"), inc, err)
			continue
		}
		contextStr += fmt.Sprintf("<included-node title=%q>\n%s\n</included-node>\n\n",
			inc, incWalk.Node.Content)
	}

	var be backend.Backend
	if !dryRun {
		be, err = backend.FromConfig(globalCfg, r.backendName)
		if err != nil {
			return fmt.Errorf("initializing backend: %w", err)
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", systemStyle.Render("[backend]"), be.Name())
	}

	var targetBlock *graph.BlockTarget
	if opt.blockID != "" {
		targetBlock, err = graph.ResolveBlockTargetBySubject(r.db, opt.blockID)
		if err != nil {
			return fmt.Errorf("resolving block target: %w", err)
		}
	} else if opt.blockPath != "" {
		targetBlock, err = graph.ResolveBlockTarget(r.db, r.root, nodeTitle, opt.blockPath)
		if err != nil {
			return fmt.Errorf("resolving block target: %w", err)
		}
	}
	targetLabel := nodeTitle
	if targetBlock != nil {
		targetLabel = targetBlock.Label()
	}
	cfg := engine.PipelineConfig{
		DB:            r.db,
		Root:          r.root,
		NodeTitle:     nodeTitle,
		TargetBlock:   targetBlock,
		Function:      fn,
		GlobalConfig:  globalCfg,
		Walk:          walk,
		ContextStr:    contextStr,
		DryRun:        dryRun,
		Confirm:       false, // REPL: readline owns stdin, can't prompt; skip cost confirmation
		StreamText:    true,
		Backend:       be,
		ModelOverride: r.modelFlag,
		Instruction:   opt.instruction,
	}

	result := engine.RunPipeline(context.Background(), cfg, startStep, prev)

	// In dry-run mode the engine already printed the prompt to stdout — don't repeat it.
	if dryRun {
		return nil
	}

	if result.IsLeft() {
		sus := result.MustLeft()
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
	} else {
		res := result.MustRight()
		if res.OutputType == "ops" && len(res.Ops) == 0 {
			fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, res.StepName, targetLabel))
			r.printSystem("No changes proposed.")
		} else if res.Output != "" && res.OutputType == "text" && be == nil {
			// Text was already streamed via API streaming — just show the label.
			fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, res.StepName, targetLabel))
		} else if res.Output != "" {
			fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, res.StepName, targetLabel))
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
	var sus *engine.Suspension
	var susSubject string
	var err error

	if len(susSubjectOverride) > 0 && susSubjectOverride[0] != "" {
		sus, err = engine.FindSuspensionBySubject(r.db, r.root, susSubjectOverride[0])
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suspension with id %q", susSubjectOverride[0])
		}
		susSubject = susSubjectOverride[0]
	} else {
		sus, susSubject, err = engine.FindSuspension(r.db, r.root, nodeTitle)
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suggestions for %q", nodeTitle)
		}
	}

	fn, err := apply.LoadFunction(sus.Function)
	if err != nil {
		return fmt.Errorf("loading function: %w", err)
	}

	if withFeedback != "" {
		globalCfg := r.effectiveCfg()
		be, _ := backend.FromConfig(globalCfg, r.backendName)

		steps := fn.EffectiveSteps()
		stepIdx := sus.StepIndex
		if stepIdx >= len(steps) {
			stepIdx = len(steps) - 1
		}
		step := steps[stepIdx]

		var streamTo *os.File
		if step.Output == "text" {
			streamTo = os.Stderr
		}

		newEntry, llmOutput, err := engine.ReviseStep(engine.ReviseConfig{
			DB:         r.db,
			Root:       r.root,
			NodeTitle:  nodeTitle,
			Function:   fn,
			Suspension: sus,
			Feedback:   withFeedback,
			Confirm:    false, // REPL: readline owns stdin
			StreamText: streamTo,
			Backend:    be,
		})
		if err != nil {
			return err
		}
		if newEntry == nil {
			r.printSystem("cancelled")
			return nil
		}

		isLast := stepIdx == len(steps)-1
		outputType := step.Output
		if isLast && outputType == "ops" {
			for _, op := range newEntry.Ops {
				fmt.Fprintln(os.Stderr, ui.FormatOp(op.Action, opName(op)))
			}
		} else {
			fmt.Fprintln(os.Stderr, ui.FormatStep(sus.Function, step.Name, orDefault(sus.TargetLabel, nodeTitle)))
			if llmOutput != "" {
				fmt.Println(ui.RenderMarkdownOrPlain(llmOutput))
			}
		}
		// Always create a new suspension so the [y/n/r] loop can act on it.
		engine.WriteSuspension(r.db, r.root, nodeTitle, orDefault(sus.TargetLabel, nodeTitle), resolveSuspensionBlock(r.db, r.root, nodeTitle, sus), sus.Function, step.Name, sus.GateType, outputType, newEntry.RawOutput, stepIdx, newEntry.Summary, newEntry.Ops, r.backendName)
		engine.ResolveSuspension(r.db, susSubject, "revised")
		return nil
	}

	// No feedback: continue pipeline or execute ops.
	steps := fn.EffectiveSteps()
	nextStep := sus.StepIndex + 1

	if nextStep < len(steps) {
		entry := apply.LogEntry{Event: "accepted", Root: r.root, Function: sus.Function, Target: nodeTitle, Timestamp: nowISO()}
		if err := apply.AppendLogDB(r.db, entry); err != nil {
			return fmt.Errorf("appending log: %w", err)
		}
		engine.ResolveSuspension(r.db, susSubject, "accepted")
		return r.runPipeline(nodeTitle, fn, nextStep, sus.Output, false, pipelineOpts{
			blockPath: sus.BlockPath,
			blockID:   sus.BlockID,
		})
	}

	lastStep := steps[sus.StepIndex]
	if lastStep.Output != "ops" {
		entry := apply.LogEntry{Event: "accepted", Root: r.root, Function: sus.Function, Target: nodeTitle, Timestamp: nowISO()}
		if err := apply.AppendLogDB(r.db, entry); err != nil {
			return fmt.Errorf("appending log: %w", err)
		}
		engine.ResolveSuspension(r.db, susSubject, "accepted")
		fmt.Fprintf(os.Stderr, "%s acknowledged %s output for %s\n",
			ui.Success.Render("[accept]"), lastStep.Output, ui.NodeTitle.Render(nodeTitle))
		return nil
	}

	// Execute ops.
	created, edited, err := apply.ExecuteOps(sus.Ops, r.root, r.db)
	if err != nil {
		return fmt.Errorf("executing ops: %w", err)
	}

	entry := apply.LogEntry{Event: "accepted", Root: r.root, Function: sus.Function, Target: nodeTitle, Timestamp: nowISO()}
	if err := apply.AppendLogDB(r.db, entry); err != nil {
		return fmt.Errorf("appending log: %w", err)
	}
	engine.ResolveSuspension(r.db, susSubject, "accepted")

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

	appliedEntry := apply.LogEntry{
		Event:        "applied",
		Root:         r.root,
		Function:     sus.Function,
		Target:       nodeTitle,
		Timestamp:    nowISO(),
		Commit:       commitHash,
		FilesCreated: created,
		FilesEdited:  edited,
	}
	if err := apply.AppendLogDB(r.db, appliedEntry); err != nil {
		return fmt.Errorf("appending log: %w", err)
	}

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
	var sus *engine.Suspension
	var susSubject string
	var err error

	if len(susSubjectOverride) > 0 && susSubjectOverride[0] != "" {
		sus, err = engine.FindSuspensionBySubject(r.db, r.root, susSubjectOverride[0])
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suspension with id %q", susSubjectOverride[0])
		}
		susSubject = susSubjectOverride[0]
	} else {
		sus, susSubject, err = engine.FindSuspension(r.db, r.root, nodeTitle)
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suggestions for %q", nodeTitle)
		}
	}
	_ = sus // used for its Target field by the caller; susSubject is what we need here
	entry := apply.LogEntry{Event: "rejected", Root: r.root, Target: nodeTitle, Timestamp: nowISO()}
	if err := apply.AppendLogDB(r.db, entry); err != nil {
		return fmt.Errorf("appending log: %w", err)
	}
	engine.ResolveSuspension(r.db, susSubject, "rejected")
	fmt.Fprintf(os.Stderr, "%s %s\n", ui.Warning.Render("rejected"), ui.NodeTitle.Render(nodeTitle))
	return nil
}

// ─── Revert ──────────────────────────────────────────────────────────────────

func (r *REPL) doRevert(nodeTitle string) error {
	if !projmd.IsGitRepo(r.root) {
		return fmt.Errorf("not a git repo — cannot revert")
	}

	// Find the last "applied" log entry with a commit hash for this node.
	entries, err := apply.ReadLogDB(r.db, r.root, nodeTitle)
	if err != nil {
		return fmt.Errorf("reading log: %w", err)
	}

	var target *apply.LogEntry
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Event == "applied" && entries[i].Commit != "" {
			target = &entries[i]
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

	newHash, err := apply.RevertCommit(r.root, target.Commit)
	if err != nil {
		return err
	}

	// Log the revert.
	revertEntry := apply.LogEntry{
		Event:     "reverted",
		Root:      r.root,
		Function:  target.Function,
		Target:    nodeTitle,
		Commit:    newHash,
		Note:      fmt.Sprintf("reverted commit %s", target.Commit),
		Timestamp: nowISO(),
	}
	if err := apply.AppendLogDB(r.db, revertEntry); err != nil {
		return fmt.Errorf("appending log: %w", err)
	}

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
	filePath, err := store.GetObject(r.db, focus, "node/file-path")
	if err != nil || filePath == "" {
		return fmt.Errorf("could not find file for %q", focus)
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

	filename := apply.SanitizeFilename(title)
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
	config, err := graph.LoadConfig(r.root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	group, ok := config.Groups[name]
	if !ok {
		// List available groups for the error.
		var names []string
		for k := range config.Groups {
			names = append(names, k)
		}
		if len(names) == 0 {
			return fmt.Errorf("no groups defined in .sevens.edn")
		}
		return fmt.Errorf("group %q not found (available: %s)", name, strings.Join(names, ", "))
	}

	titles, err := graph.ResolveGroup(r.db, r.root, group)
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
	config, err := graph.LoadConfig(r.root)
	if err != nil {
		return err
	}
	files, err := graph.ScanFiles(r.root)
	if err != nil {
		return err
	}
	nodes, _ := graph.ParseAllFiles(files)
	return graph.PopulateTriples(r.db, r.root, nodes, config)
}

// resyncQuiet runs resync but suppresses PopulateTriples' stderr output.
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
