package repl

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"sevens/internal/ui"
)

// dispatch is the REPL's command grammar.  Order of checks follows the design doc:
//
//  1. Dot command (.info, .model, …)
//  2. Navigation keyword (.., up, child N, sibling N, root, focus)
//  3. Numeric selection (bare number referencing the last printed node/block list)
//  4. Node title (matches a node in the current root)
//  5. Function name (applies function to focused node)
//  6. Named command (walk, children, siblings, search, pending, log, accept, reject)
//  7. Unknown → error with suggestions
func (r *REPL) dispatch(line string) error {
	tokens := tokenize(line)
	if len(tokens) == 0 {
		return nil
	}

	// Auto-sync: pick up file changes (e.g. from Obsidian) before every command.
	// Silence the "[sync] Wrote N triples" output for auto-syncs.
	if shouldAutoSync(tokens) {
		if err := r.resyncQuiet(); err != nil {
			fmt.Fprintf(os.Stderr, "%s sync: %v\n", ui.Warning.Render("[warn]"), err)
		}
	}

	head := tokens[0]

	// 1. Dot commands (but not ".." which is navigation)
	if strings.HasPrefix(head, ".") && head != ".." {
		return r.handleDot(tokens)
	}

	// 2. Navigation
	switch head {
	case "..", "up":
		return r.handleNavUp()
	case "root":
		r.setFocus("")
		r.printSystem("unfocused")
		return nil
	case "focus", "f":
		if len(tokens) < 2 {
			return fmt.Errorf("usage: focus <title>")
		}
		return r.handleFocusExplicit(strings.Join(tokens[1:], " "))
	case "child", "c":
		if len(tokens) < 2 {
			return fmt.Errorf("usage: child <n>")
		}
		return r.handleRelativeNav("child", tokens[1])
	case "sibling", "s":
		if len(tokens) < 2 {
			return fmt.Errorf("usage: sibling <n>")
		}
		return r.handleRelativeNav("sibling", tokens[1])
	}

	// 3. Numeric selection
	if n, ok := parsePositiveInt(head); ok {
		return r.handleNumericSelect(n)
	}

	// 4–6 all depend on what the token matches.  Function names take priority
	// over node titles for bare words, per the design doc — EXCEPT for
	// commands that shadow function names (discuss, note).

	// Named commands (checked before functions so "discuss" routes to the
	// interactive mode, not to handleApply).
	switch head {
	case "walk":
		return r.handleWalk(tokens)
	case "templates":
		return r.handleTemplates()
	case "capture":
		return r.handleCapture(tokens)
	case "instantiate":
		return r.handleInstantiate(tokens)
	case "inbox":
		return r.handleInbox(tokens)
	case "blocks":
		return r.handleBlocks(tokens)
	case "diff-blocks":
		return r.handleBlockDiff(tokens)
	case "extract-block":
		return r.handleExtractBlock(tokens)
	case "children":
		return r.handleChildren()
	case "siblings":
		return r.handleSiblings()
	case "search":
		if len(tokens) < 2 {
			return fmt.Errorf("usage: search <query>")
		}
		return r.handleSearch(strings.Join(tokens[1:], " "))
	case "pending":
		return r.handlePending()
	case "log":
		return r.handleLog(tokens)
	case "accept":
		return r.handleAccept(tokens)
	case "reject":
		return r.handleReject()
	case "revert":
		focus, err := r.requireFocus()
		if err != nil {
			return err
		}
		return r.doRevert(focus)
	case "overview":
		return r.handleOverview()
	case "sync":
		return r.handleSync()
	case "note":
		return r.enterNote()
	case "discuss":
		flags := parseInlineFlags(tokens[1:])
		return r.enterDiscussion(flags.has("noninteractive"))
	case "new":
		if len(tokens) < 2 {
			return fmt.Errorf("usage: new <title>")
		}
		return r.handleNew(strings.Join(tokens[1:], " "))
	}

	// 5. Function name (after named commands so discuss/note route to modes)
	if isFunctionName(head) {
		return r.handleApply(tokens)
	}

	// 6. Node title
	title := strings.Join(tokens, " ")
	if r.isNodeTitle(title) {
		return r.handleFocusExplicit(title)
	}
	// Also try just the head (single word title).
	if len(tokens) > 1 && r.isNodeTitle(head) {
		return r.handleFocusExplicit(head)
	}

	// 7. Unknown
	return fmt.Errorf("unknown command or node %q — .help for commands", head)
}

// ─── Dot commands ─────────────────────────────────────────────────────────────

func (r *REPL) handleDot(tokens []string) error {
	cmd := tokens[0]
	args := tokens[1:]

	switch cmd {
	case ".quit", ".exit", ".q":
		fmt.Fprintln(os.Stderr, systemStyle.Render("goodbye"))
		os.Exit(0)

	case ".clear":
		// Clear visible screen only; scrollback is preserved so the user can scroll up.
		fmt.Print("\033[H\033[2J")

	case ".help", ".h":
		r.printHelp()

	case ".info":
		r.printInfo()

	case ".functions", ".fns":
		return r.printFunctions()

	case ".model":
		if len(args) == 0 {
			r.printSystem("model: %s", r.modelFlag)
			return nil
		}
		r.modelFlag = args[0]
		r.printSystem("model → %s", r.modelFlag)

	case ".backend":
		if len(args) == 0 {
			r.printSystem("backend: %s", orDefault(r.backendName, "(default)"))
			return nil
		}
		r.backendName = args[0]
		r.printSystem("backend → %s", r.backendName)

	case ".theme":
		if len(args) == 0 {
			r.printSystem("theme: %s", ui.Theme())
			return nil
		}
		t := args[0]
		if t != "light" && t != "dark" {
			return fmt.Errorf("usage: .theme light|dark")
		}
		ui.SetTheme(t)
		r.printSystem("theme → %s", t)

	case ".dry":
		r.dryRun = !r.dryRun
		if r.dryRun {
			r.printSystem("dry-run ON")
		} else {
			r.printSystem("dry-run OFF")
		}

	case ".include":
		if len(args) == 0 {
			if len(r.includes) == 0 {
				r.printSystem("includes: (none)")
			} else {
				r.printSystem("includes: %s", strings.Join(r.includes, ", "))
			}
			return nil
		}
		if args[0] == "clear" {
			r.includes = nil
			r.printSystem("includes cleared")
			return nil
		}
		seen := make(map[string]bool, len(r.includes))
		for _, include := range r.includes {
			seen[include] = true
		}
		var resolved []string
		for _, arg := range args {
			// Group include: .include @GroupName
			if strings.HasPrefix(arg, "@") {
				if err := r.includeGroup(arg[1:]); err != nil {
					return err
				}
				continue
			}

			canonical := r.resolveTitle(arg)
			if canonical == "" {
				return fmt.Errorf("node not found: %q", arg)
			}
			resolved = append(resolved, canonical)
		}
		var added []string
		for _, canonical := range resolved {
			if seen[canonical] {
				continue
			}
			r.includes = append(r.includes, canonical)
			seen[canonical] = true
			added = append(added, canonical)
		}
		if len(added) == 0 {
			r.printSystem("no new includes added")
			return nil
		}
		r.printSystem("include added: %s", strings.Join(added, ", "))

	case ".exclude":
		if len(args) == 0 {
			return fmt.Errorf("usage: .exclude <title>")
		}
		title := strings.Join(args, " ")
		canonical := r.resolveTitle(title)
		if canonical != "" {
			title = canonical
		}
		r.includes = removeString(r.includes, title)
		r.printSystem("include removed: %s", title)

	default:
		return fmt.Errorf("unknown dot command %q — .help for commands", cmd)
	}
	return nil
}

// ─── Info / help ──────────────────────────────────────────────────────────────

func (r *REPL) printInfo() {
	kv := func(k, v string) {
		fmt.Printf("  %s  %s\n", keyStyle.Render(k+":"), valStyle.Render(v))
	}
	fmt.Println()
	if r.focus != "" {
		kv("focus  ", r.focus)
	} else {
		kv("focus  ", "(none)")
	}
	if r.focusBlock != nil {
		kv("block  ", fmt.Sprintf("%s (%s)", r.focusBlock.Path, r.focusBlock.Kind))
	}
	kv("root   ", r.root)
	kv("model  ", orDefault(r.modelFlag, r.globalCfg.LLM.Model+" (default)"))
	kv("backend", orDefault(r.backendName, orDefault(r.globalCfg.Backend, "anthropic")+" (default)"))
	kv("theme  ", ui.Theme())
	if r.dryRun {
		kv("dry-run", "on")
	}
	if len(r.includes) > 0 {
		kv("include", strings.Join(r.includes, ", "))
	}
	fmt.Println()
}

func (r *REPL) printHelp() {
	type entry struct{ cmd, desc string }
	sections := []struct {
		heading string
		items   []entry
	}{
		{"Navigation", []entry{
			{"<title>", "focus node by title"},
			{"focus <title>  /  f <title>", "explicit focus (use when title matches a command)"},
			{"..", "move to parent"},
			{"root", "clear focus"},
			{"child <n>  /  c <n>", "focus Nth child"},
			{"sibling <n>  /  s <n>", "focus Nth sibling"},
			{"<n>", "focus Nth item from last list"},
		}},
		{"Viewing", []entry{
			{"walk", "walk the focused node"},
			{"templates", "list available manual templates"},
			{"capture [title]", "quick-capture to inbox using the inbox-capture template"},
			{"instantiate <template> [args...]", "run a manual template"},
			{"instantiate <template> --dry-run ...", "preview a manual template without writing"},
			{"inbox", "list inbox child notes with capture summaries"},
			{"inbox <title>", "summarize another container node"},
			{"blocks", "list current blocks for the focused node"},
			{"blocks <title>", "list current blocks for another node"},
			{"diff-blocks", "show block-level changes since last sync"},
			{"diff-blocks --unchanged", "include unchanged blocks"},
			{"children", "list children with numbers"},
			{"siblings", "list siblings with numbers"},
			{"overview", "print full tree"},
			{"search <query>", "search titles and content"},
			{"pending", "list pending suggestions"},
			{"log", "show operation log for focused node"},
			{"sync", "sync filesystem changes into the graph"},
			{"new <title>", "create child of focused node"},
		}},
		{"Functions", []entry{
			{"<function>", "apply function to focused node or selected block"},
			{"<function> --model <tier>", "apply with model override"},
			{"<function> --dry-run", "preview prompt only"},
			{"extract-block [path] [title]", "create a new node from a selected or explicit block"},
			{"extract-block <path> --parent <title>", "extract to another parent node"},
			{"instantiate <template> --at .", "append or insert into the focused node"},
			{"accept", "accept pending (y/n/r prompt)"},
			{"reject", "reject pending suggestion"},
			{"revert", "revert last applied commit for focused node"},
			{"discuss", "interactive discussion (or non-interactive if threaded)"},
			{"discuss -n", "non-interactive: run discuss function once"},
			{"note", "quick annotation appended to node"},
		}},
		{"Session", []entry{
			{".info", "show current state"},
			{".model <tier>", "switch model (fast / capable / powerful / or raw ID)"},
			{".backend <name>", "switch backend"},
			{".theme light|dark", "switch glamour rendering theme"},
			{".dry", "toggle dry-run"},
			{".include <title>...", "add one or more nodes to context"},
			{".include clear", "clear all includes"},
			{".exclude <title>", "remove node from context"},
			{".functions", "list available functions"},
			{".help", "this help"},
			{".quit", "exit"},
		}},
	}

	fmt.Println()
	for _, sec := range sections {
		fmt.Println(ui.Label.Render(sec.heading))
		for _, e := range sec.items {
			cmdStyle := helpCmdStyle
			if strings.HasPrefix(e.cmd, ".") {
				cmdStyle = dotCmdStyle
			}
			fmt.Printf("  %s  %s\n",
				cmdStyle.Render(fmt.Sprintf("%-38s", e.cmd)),
				helpDescStyle.Render(e.desc),
			)
		}
		fmt.Println()
	}
}

func shouldAutoSync(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	switch tokens[0] {
	case "blocks", "diff-blocks", "extract-block", "sync":
		return false
	default:
		return true
	}
}

func (r *REPL) printFunctions() error {
	if r.applyR == nil {
		return fmt.Errorf("apply runner not available")
	}
	fns, err := r.applyR.ListFunctions()
	if err != nil {
		return fmt.Errorf("listing functions: %w", err)
	}
	if len(fns) == 0 {
		r.printSystem("no functions defined")
		return nil
	}
	maxLen := 0
	for _, f := range fns {
		if len(f.Name) > maxLen {
			maxLen = len(f.Name)
		}
	}
	fmt.Println()
	for _, f := range fns {
		pad := strings.Repeat(" ", maxLen-len(f.Name))
		fmt.Printf("  %s%s  %s\n",
			ui.Label.Render(f.Name), pad,
			ui.Dim.Render(f.Description))
	}
	fmt.Println()
	return nil
}

// ─── Navigation ───────────────────────────────────────────────────────────────

func (r *REPL) handleNavUp() error {
	if r.focus == "" {
		r.printSystem("already at root")
		return nil
	}
	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	subject, _ := r.graphQ.ResolveNode(r.focus, r.root)
	if subject == "" {
		return fmt.Errorf("could not resolve focused node %q", r.focus)
	}
	parentSubject, err := r.graphQ.GetObject(subject, "node/parent")
	if err != nil || parentSubject == "" {
		r.setFocus("")
		r.printSystem("at root — unfocused")
		return nil
	}
	parentTitle, err := r.graphQ.NodeTitle(parentSubject)
	if err != nil {
		return fmt.Errorf("resolving parent title: %w", err)
	}
	if parentTitle == "" {
		r.setFocus("")
		r.printSystem("at root — unfocused")
		return nil
	}
	r.setFocus(parentTitle)
	return r.showFocusSummary()
}

func (r *REPL) handleFocusExplicit(title string) error {
	canonical := r.resolveTitle(title)
	if canonical == "" {
		return fmt.Errorf("node not found: %q", title)
	}
	r.setFocus(canonical)
	return r.showFocusSummary()
}

func (r *REPL) handleRelativeNav(rel, indexStr string) error {
	focus, err := r.requireFocus()
	if err != nil {
		return err
	}

	n, ok := parsePositiveInt(indexStr)
	if !ok || n < 1 {
		return fmt.Errorf("expected a positive number, got %q", indexStr)
	}

	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	var titles []string
	walk, werr := r.graphQ.BuildWalk(r.root, focus, 1)
	if werr != nil {
		return werr
	}
	switch rel {
	case "child":
		titles = walk.Node.Children
	case "sibling":
		if walk.Node.Parent == nil {
			return fmt.Errorf("%q has no parent (no siblings)", focus)
		}
		titles = walk.Node.Siblings
	}
	if n > len(titles) {
		return fmt.Errorf("index %d out of range (1–%d)", n, len(titles))
	}
	r.setFocus(titles[n-1])
	return r.showFocusSummary()
}

func (r *REPL) handleNumericSelect(n int) error {
	if len(r.lastBlocks) > 0 {
		if n < 1 || n > len(r.lastBlocks) {
			return fmt.Errorf("index %d out of range (1–%d)", n, len(r.lastBlocks))
		}
		r.setFocusBlock(r.lastBlocks[n-1])
		return r.showFocusedBlock()
	}
	if len(r.lastList) == 0 {
		return fmt.Errorf("no list to select from — run children, siblings, search, or pending first")
	}
	if n < 1 || n > len(r.lastList) {
		return fmt.Errorf("index %d out of range (1–%d)", n, len(r.lastList))
	}
	title := r.lastList[n-1]
	r.setFocus(title)
	return r.showFocusSummary()
}

func (r *REPL) showFocusedBlock() error {
	if r.focusBlock == nil {
		return fmt.Errorf("no block selected")
	}
	block := *r.focusBlock
	fmt.Println()
	fmt.Printf("%s  %s\n", ui.Label.Render(block.Path), ui.Dim.Render(block.Kind))
	if len(block.Scope) > 0 {
		scopeStr := strings.Join(block.Scope, " > ")
		fmt.Printf("  %s %s\n", ui.Dim.Render("scope:"), ui.Dim.Render(scopeStr))
	}
	if block.Signifier != "" {
		fmt.Printf("  %s %s\n", ui.Dim.Render("signifier:"), ui.Dim.Render(block.Signifier))
	}
	fmt.Println(ui.Separator.Render(strings.Repeat("─", 60)))
	blockMd := block.Text
	if r.graphQ != nil {
		blockMd = r.graphQ.RenderBlockMarkdown(block)
	}
	fmt.Println(ui.RenderMarkdownOrPlain(blockMd))
	return nil
}

// showFocusSummary prints a one-line summary when focus changes.
func (r *REPL) showFocusSummary() error {
	if r.graphQ == nil {
		return nil
	}
	walk, err := r.graphQ.BuildWalk(r.root, r.focus, 1)
	if err != nil {
		return fmt.Errorf("building walk: %w", err)
	}
	n := walk.Node
	var parts []string
	if n.Parent != nil {
		parts = append(parts, ui.Dim.Render("↑ "+*n.Parent))
	}
	if len(n.Children) > 0 {
		parts = append(parts, ui.Dim.Render(fmt.Sprintf("%d children", len(n.Children))))
	}
	if len(n.Siblings) > 0 {
		parts = append(parts, ui.Dim.Render(fmt.Sprintf("%d siblings", len(n.Siblings))))
	}
	if len(parts) > 0 {
		fmt.Fprintf(os.Stderr, "  %s\n", strings.Join(parts, "  "))
	}
	return nil
}

// ─── Viewing commands ─────────────────────────────────────────────────────────

func (r *REPL) handleWalk(tokens []string) error {
	var nodeTitle string
	if len(tokens) > 1 {
		nodeTitle = strings.Join(tokens[1:], " ")
		r.clearFocusBlock()
	} else {
		if r.focusBlock != nil {
			return r.showFocusedBlock()
		}
		var err error
		nodeTitle, err = r.requireFocus()
		if err != nil {
			return err
		}
	}

	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	walk, err := r.graphQ.BuildWalk(r.root, nodeTitle, 1)
	if err != nil {
		return fmt.Errorf("building walk: %w", err)
	}
	n := walk.Node
	fmt.Print(ui.FormatNodeHeader(
		n.Title, n.Parent, n.Role,
		n.Children, n.Siblings,
		n.ChildRoles, n.SiblingRoles,
		n.CrossRefs,
	))
	fmt.Println(ui.RenderMarkdownOrPlain(n.Content))
	return nil
}

func (r *REPL) handleChildren() error {
	focus, err := r.requireFocus()
	if err != nil {
		return err
	}
	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	walk, err := r.graphQ.BuildWalk(r.root, focus, 1)
	if err != nil {
		return fmt.Errorf("building walk: %w", err)
	}
	fmt.Println()
	r.printList(walk.Node.Children)
	fmt.Println()
	return nil
}

func (r *REPL) handleSiblings() error {
	focus, err := r.requireFocus()
	if err != nil {
		return err
	}
	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	walk, err := r.graphQ.BuildWalk(r.root, focus, 1)
	if err != nil {
		return fmt.Errorf("building walk: %w", err)
	}
	if walk.Node.Parent == nil {
		r.printSystem("(no parent — no siblings)")
		return nil
	}
	fmt.Println()
	r.printList(walk.Node.Siblings)
	fmt.Println()
	return nil
}

func (r *REPL) handleSearch(query string) error {
	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	titles, err := r.graphQ.SearchTitles(query, r.root)
	if err != nil {
		return fmt.Errorf("searching titles: %w", err)
	}
	content, err := r.graphQ.SearchContent(query, r.root)
	if err != nil {
		return fmt.Errorf("searching content: %w", err)
	}

	// Deduplicate: title matches first, then content-only matches.
	seen := make(map[string]bool)
	var results []string
	for _, t := range titles {
		seen[t] = true
		results = append(results, t)
	}
	for _, t := range content {
		if !seen[t] {
			results = append(results, t)
		}
	}

	if len(results) == 0 {
		r.printSystem("no matches for %q", query)
		return nil
	}

	fmt.Println()
	if len(titles) > 0 {
		fmt.Println(ui.Label.Render("title matches"))
	}
	r.printList(results)
	if len(content) > 0 && len(titles) < len(results) {
		// Annotate the content-only section
		fmt.Printf("  %s\n", ui.Dim.Render(fmt.Sprintf("↳ %d content match(es)", len(content)-countIn(titles, results[:len(titles)]))))
	}
	fmt.Println()
	return nil
}

// countIn counts how many items in sub appear in set.
func countIn(sub, set []string) int {
	m := make(map[string]bool, len(set))
	for _, s := range set {
		m[s] = true
	}
	count := 0
	for _, s := range sub {
		if m[s] {
			count++
		}
	}
	return count
}

func (r *REPL) handlePending() error {
	if r.pipelineR == nil {
		return fmt.Errorf("pipeline runner not available")
	}
	suspensions, err := r.pipelineR.ListSuspensions(r.root)
	if err != nil {
		return fmt.Errorf("listing pending: %w", err)
	}
	if len(suspensions) == 0 {
		r.printSystem("no pending suggestions")
		return nil
	}
	fmt.Println()
	var titles []string
	for _, sus := range suspensions {
		fmt.Println(ui.FormatPending(orDefault(sus.TargetLabel, sus.Target), sus.Function, sus.StepName, sus.Summary, sus.Subject))
		titles = append(titles, sus.Target)
	}
	r.lastBlocks = nil
	r.lastList = titles
	fmt.Println()
	return nil
}

func (r *REPL) handleLog(tokens []string) error {
	var nodeTitle string
	if len(tokens) > 1 {
		nodeTitle = strings.Join(tokens[1:], " ")
	} else {
		var err error
		nodeTitle, err = r.requireFocus()
		if err != nil {
			return err
		}
	}

	if r.applyR == nil {
		return fmt.Errorf("apply runner not available")
	}
	entries, err := r.applyR.ReadLog(r.root, nodeTitle)
	if err != nil {
		return fmt.Errorf("reading log: %w", err)
	}
	if len(entries) == 0 {
		r.printSystem("no log entries for %q", nodeTitle)
		return nil
	}
	fmt.Println()
	for _, e := range entries {
		fmt.Println(ui.FormatLogEntry(e.Timestamp, e.Event, e.Function, e.Step, e.Commit, e.Note))
		for _, op := range e.Ops {
			fmt.Println(ui.FormatOp(op.Action, opName(op)))
		}
		if len(e.FilesCreated) > 0 {
			fmt.Printf("    created: %s\n", ui.Dim.Render(strings.Join(e.FilesCreated, ", ")))
		}
		if len(e.FilesEdited) > 0 {
			fmt.Printf("    edited:  %s\n", ui.Dim.Render(strings.Join(e.FilesEdited, ", ")))
		}
	}
	fmt.Println()
	return nil
}

func (r *REPL) handleOverview() error {
	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	output, err := r.graphQ.BuildOverview(r.root)
	if err != nil {
		return fmt.Errorf("building overview: %w", err)
	}
	// Re-use the tree printer logic inline.
	printOverviewTree(output, r.focus)
	return nil
}

// ─── Function application ─────────────────────────────────────────────────────

func (r *REPL) handleApply(tokens []string) error {
	focus, err := r.requireFocus()
	if err != nil {
		return err
	}

	fnName := tokens[0]
	flags := parseInlineFlags(tokens[1:])

	fn, err := r.loadFunctionDef(fnName)
	if err != nil {
		return fmt.Errorf("loading function %q: %w", fnName, err)
	}
	if err := r.validateRootFlag(flags.root); err != nil {
		return err
	}

	dryRun := r.dryRun || flags.has("dry-run")
	if flags.model != "" {
		r.modelFlag = flags.model
	}
	if flags.backend != "" {
		r.backendName = flags.backend
	}
	opts := pipelineOpts{instruction: flags.with, includes: flags.includes}
	if flags.block != "" {
		opts.blockPath = flags.block
	} else if r.focusBlock != nil {
		opts.blockPath = r.focusBlock.Path
	}
	return r.runPipeline(focus, fn, 0, "", dryRun, opts)
}

// ─── Accept / reject dispatch ─────────────────────────────────────────────────

func (r *REPL) handleAccept(tokens []string) error {
	// If the first argument looks like a suspension subject ID, use it directly.
	// e.g. "accept suspension:MyNode:20260408T120000"
	var susSubjectArg string
	restTokens := tokens[1:]
	if len(restTokens) > 0 && strings.HasPrefix(restTokens[0], "suspension:") {
		susSubjectArg = restTokens[0]
		restTokens = restTokens[1:]
	}

	focus, err := r.requireFocus()
	if err != nil {
		return err
	}

	flags := parseInlineFlags(restTokens)

	var susSubject string
	if susSubjectArg != "" {
		if r.pipelineR == nil {
			return fmt.Errorf("pipeline runner not available")
		}
		// Validate the subject ID.
		sus, serr := r.pipelineR.FindSuspensionBySubject(r.root, susSubjectArg)
		if serr != nil {
			return fmt.Errorf("finding pending: %w", serr)
		}
		if sus == nil {
			return fmt.Errorf("no pending suspension with id %q", susSubjectArg)
		}
		susSubject = susSubjectArg
		focus = sus.Target
	} else {
		// Resolve which suspension to act on, detecting ambiguity.
		susSubject, err = r.resolveSuspensionSubject(focus)
		if err != nil {
			return err
		}
	}

	if flags.with != "" {
		// Explicit --with feedback, skip the interactive prompt.
		return r.doAccept(focus, flags.with, susSubject)
	}

	// Interactive y/n/r loop.
	for {
		savedPrompt := r.rl.Config.Prompt
		r.rl.SetPrompt(modeStyle.Render("[y/n/r]>") + " ")
		line, err := r.rl.Readline()
		r.rl.SetPrompt(savedPrompt)
		if err != nil {
			return nil // interrupted
		}
		line = strings.TrimSpace(strings.ToLower(line))

		switch line {
		case "y", "yes", "":
			return r.doAccept(focus, "", susSubject)
		case "n", "no":
			return r.doReject(focus, susSubject)
		case "r", "revise":
			r.rl.SetPrompt(modeStyle.Render("[revision]>") + " ")
			feedback, ferr := r.rl.Readline()
			r.rl.SetPrompt(savedPrompt)
			if ferr != nil {
				return nil
			}
			feedback = strings.TrimSpace(feedback)
			if feedback == "" {
				r.printSystem("empty feedback, try again")
				continue
			}
			if err := r.doAccept(focus, feedback, susSubject); err != nil {
				return err
			}
			// After revision, the old suspension was resolved and a new one written.
			// Re-resolve for the next iteration of the loop.
			susSubject, err = r.resolveSuspensionSubject(focus)
			if err != nil {
				return err
			}
			continue
		default:
			r.printSystem("y=accept, n=reject, r=revise")
			continue
		}
	}
}

func (r *REPL) handleReject() error {
	focus, err := r.requireFocus()
	if err != nil {
		return err
	}
	susSubject, err := r.resolveSuspensionSubject(focus)
	if err != nil {
		return err
	}
	return r.doReject(focus, susSubject)
}

// resolveSuspensionSubject returns the single pending suspension subject for the
// given node title, or errors with a list of choices when there is more than one.
func (r *REPL) resolveSuspensionSubject(nodeTitle string) (string, error) {
	if r.pipelineR == nil {
		return "", fmt.Errorf("pipeline runner not available")
	}
	all, err := r.pipelineR.FindSuspensions(r.root, nodeTitle)
	if err != nil {
		return "", fmt.Errorf("finding pending: %w", err)
	}
	if len(all) == 0 {
		return "", fmt.Errorf("no pending suggestions for %q", nodeTitle)
	}
	if len(all) == 1 {
		return all[0].Subject, nil
	}
	// Ambiguous — show all and ask the user to pick.
	fmt.Fprintf(os.Stderr, "%s multiple pending suspensions for %s — use 'accept <id>' with one of:\n",
		ui.Warning.Render("[ambiguous]"), ui.NodeTitle.Render(nodeTitle))
	for _, s := range all {
		fmt.Println(ui.FormatPending(orDefault(s.TargetLabel, s.Target), s.Function, s.StepName, s.Summary, s.Subject))
	}
	return "", fmt.Errorf("ambiguous: provide a suspension id")
}

// ─── Inline flag parsing ──────────────────────────────────────────────────────

type inlineFlags struct {
	model          string
	backend        string
	with           string
	root           string
	block          string
	includes       []string
	dryRun         bool
	yes            bool
	nonInteractive bool
}

func (f inlineFlags) has(name string) bool {
	switch name {
	case "dry-run":
		return f.dryRun
	case "yes":
		return f.yes
	case "noninteractive":
		return f.nonInteractive
	}
	return false
}

// parseInlineFlags parses --flag value and --flag=value from a token slice.
func parseInlineFlags(tokens []string) inlineFlags {
	var f inlineFlags
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch t {
		case "-n":
			f.nonInteractive = true
			continue
		case "-y":
			f.yes = true
			continue
		}
		if !strings.HasPrefix(t, "--") {
			continue
		}
		key, val, hasEq := strings.Cut(t[2:], "=")
		if !hasEq && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "--") {
			val = tokens[i+1]
			i++
		}
		switch key {
		case "model":
			f.model = val
		case "backend":
			f.backend = val
		case "root":
			f.root = val
		case "block":
			f.block = val
		case "include":
			f.includes = append(f.includes, val)
		case "with":
			// --with may be multi-word; val already holds the next token (consumed above).
			// Grab any remaining tokens after that as part of the feedback string.
			if !hasEq && i+1 < len(tokens) {
				val = val + " " + strings.Join(tokens[i+1:], " ")
				i = len(tokens)
			}
			f.with = val
		case "dry-run":
			f.dryRun = true
		case "yes", "y":
			f.yes = true
		case "n", "non-interactive":
			f.nonInteractive = true
		}
	}
	return f
}

// ─── Overview tree printer ────────────────────────────────────────────────────

func printOverviewTree(output *OverviewOutput, highlightTitle string) {
	childMap := make(map[string][]string)
	rootNodes := []string{}

	for _, n := range output.Nodes {
		if n.Parent == nil {
			rootNodes = append(rootNodes, n.Title)
		} else {
			childMap[*n.Parent] = append(childMap[*n.Parent], n.Title)
		}
	}

	var printNode func(title, prefix string, isLast bool)
	printNode = func(title, prefix string, isLast bool) {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		label := ui.NodeTitle.Render(title)
		if title == highlightTitle {
			label = ui.Label.Render("→ " + title)
		}
		fmt.Print(prefix + ui.Dim.Render(connector) + label + "\n")

		childPrefix := prefix + "│   "
		if isLast {
			childPrefix = prefix + "    "
		}
		children := childMap[title]
		for i, child := range children {
			printNode(child, childPrefix, i == len(children)-1)
		}
	}

	fmt.Println()
	for i, root := range rootNodes {
		label := ui.NodeTitle.Render(root)
		if root == highlightTitle {
			label = ui.Label.Render("→ " + root)
		}
		fmt.Println(label)
		for j, child := range childMap[root] {
			printNode(child, "", j == len(childMap[root])-1)
		}
		if i < len(rootNodes)-1 {
			fmt.Println()
		}
	}
	fmt.Println()
}

// ─── Tokenizer ────────────────────────────────────────────────────────────────

// tokenize splits a line into tokens, respecting double-quoted strings.
func tokenize(line string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false

	for _, ch := range line {
		switch {
		case ch == '"':
			inQuote = !inQuote
		case unicode.IsSpace(ch) && !inQuote:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func parsePositiveInt(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

func removeString(slice []string, s string) []string {
	out := slice[:0]
	for _, v := range slice {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
