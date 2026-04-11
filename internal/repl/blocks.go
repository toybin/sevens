package repl

import (
	"fmt"
	"os"
	"strings"

	projmd "sevens/internal/projection/md"
	"sevens/internal/ui"
)

func (r *REPL) handleSync() error {
	if err := r.resync(); err != nil {
		return err
	}
	r.printSystem("synced")
	return nil
}

func (r *REPL) handleBlocks(tokens []string) error {
	root, ednOutput, nodeTitle, err := blockListArgs(tokens[1:])
	if err != nil {
		return err
	}
	if err := r.validateRootFlag(root); err != nil {
		return err
	}
	if nodeTitle == "" {
		nodeTitle, err = r.requireFocus()
		if err != nil {
			return err
		}
	}
	if canonical := r.resolveTitle(nodeTitle); canonical != "" {
		nodeTitle = canonical
	}

	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	output, err := r.graphQ.BuildBlockList(r.root, nodeTitle)
	if err != nil {
		return err
	}
	if ednOutput {
		r.lastList = nil
		r.lastBlocks = nil
		return printEDN(output)
	}

	fmt.Println()
	fmt.Println(ui.NodeTitle.Render(output.NodeTitle))
	fmt.Println(ui.Dim.Render(output.FilePath))
	fmt.Println(ui.Separator.Render(strings.Repeat("─", 60)))
	if len(output.Blocks) == 0 {
		r.lastList = nil
		r.lastBlocks = nil
		r.printSystem("(no blocks)")
		return nil
	}
	width := len(fmt.Sprintf("%d", len(output.Blocks)))
	r.lastList = nil
	r.lastBlocks = append([]BlockListEntry(nil), output.Blocks...)
	for i, block := range output.Blocks {
		num := fmt.Sprintf("%*d.", width, i+1)
		label := block.Kind
		if block.Kind == "heading" {
			label = fmt.Sprintf("h%d", block.Level)
		}
		if block.Kind == "task" && block.Signifier != "" {
			label = fmt.Sprintf("task[%s]", block.Signifier)
		}
		fmt.Printf("  %s %s  %s\n",
			listNumStyle.Render(num),
			ui.Label.Render(fmt.Sprintf("%-10s", label)),
			ui.Dim.Render(block.Path)+" "+summarizeBlockText(block.Text, 88),
		)
		if len(block.Scope) > 0 {
			fmt.Printf("      %s %s\n", ui.Dim.Render("scope:"), ui.Dim.Render(scopeString(block.Scope)))
		}
	}
	fmt.Println()
	return nil
}

func (r *REPL) handleBlockDiff(tokens []string) error {
	root, ednOutput, showUnchanged, nodeTitle, err := blockDiffArgs(tokens[1:])
	if err != nil {
		return err
	}
	if err := r.validateRootFlag(root); err != nil {
		return err
	}
	if nodeTitle == "" {
		nodeTitle, err = r.requireFocus()
		if err != nil {
			return err
		}
	} else {
		canonical := r.resolveTitle(nodeTitle)
		if canonical == "" {
			return fmt.Errorf("node not found: %q", nodeTitle)
		}
		nodeTitle = canonical
	}

	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	output, err := r.graphQ.BuildBlockDiff(r.root, nodeTitle)
	if err != nil {
		return err
	}
	r.lastList = nil
	r.lastBlocks = nil
	if ednOutput {
		return printEDN(output)
	}
	printREPLBlockDiff(output, showUnchanged)
	return nil
}

func (r *REPL) handleInbox(tokens []string) error {
	root, _, nodeTitle, err := inboxArgs(tokens[1:])
	if err != nil {
		return err
	}
	if err := r.validateRootFlag(root); err != nil {
		return err
	}
	if nodeTitle == "" {
		nodeTitle, _ = r.requireFocus()
		if nodeTitle == "" {
			return fmt.Errorf("no node specified and no focus active")
		}
	}
	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	items, err := r.graphQ.ChildrenSummary(r.root, nodeTitle)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println(ui.NodeTitle.Render(nodeTitle))
	if len(items) == 0 {
		r.printSystem("(none)")
		return nil
	}

	titles := make([]string, 0, len(items))
	width := len(fmt.Sprintf("%d", len(items)))
	r.lastBlocks = nil
	for i, item := range items {
		titles = append(titles, item.Title)
		num := fmt.Sprintf("%*d.", width, i+1)
		detail := fmt.Sprintf("%d chars", item.CharCount)
		if item.Empty {
			detail = "empty"
		}
		fmt.Printf("  %s %s  %s\n",
			listNumStyle.Render(num),
			ui.NodeTitle.Render(item.Title),
			ui.Dim.Render("("+detail+")"),
		)
	}
	r.lastList = titles
	fmt.Println()
	return nil
}

func (r *REPL) handleExtractBlock(tokens []string) error {
	defaultSource, err := r.requireFocus()
	if err != nil {
		return err
	}
	defaultPath := ""
	if r.focusBlock != nil {
		defaultPath = r.focusBlock.Path
	}
	root, sourceTitle, blockPath, title, parent, err := extractBlockArgs(tokens[1:], defaultSource, defaultPath, r.resolveTitle)
	if err != nil {
		return err
	}
	if err := r.validateRootFlag(root); err != nil {
		return err
	}

	if r.graphQ == nil {
		return fmt.Errorf("graph querier not available")
	}
	extracted, err := r.graphQ.PrepareBlockExtraction(r.root, sourceTitle, blockPath, title, parent)
	if err != nil {
		return err
	}

	if r.applyR == nil {
		return fmt.Errorf("apply runner not available")
	}
	ops := []FileOp{{
		Action:  "create",
		Title:   extracted.Title,
		Parent:  extracted.ParentTitle,
		Content: extracted.Content,
	}}
	created, _, err := r.applyR.ExecuteOps(ops, r.root)
	if err != nil {
		return fmt.Errorf("creating node: %w", err)
	}
	if projmd.IsGitRepo(r.root) && len(created) > 0 {
		h, cerr := projmd.CommitFiles(r.root, fmt.Sprintf("sevens: extract block %s from %q", extracted.SourcePath, extracted.SourceTitle), created)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "%s git commit: %v\n", ui.Warning.Render("[warn]"), cerr)
		} else {
			r.printSystem("extracted %s (%s)", extracted.Title, h)
		}
	} else {
		r.printSystem("extracted %s", extracted.Title)
	}
	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}
	r.setFocus(extracted.Title)
	return nil
}

func blockListArgs(tokens []string) (root string, ednOutput bool, nodeTitle string, err error) {
	var args []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if !strings.HasPrefix(t, "-") {
			args = append(args, t)
			continue
		}
		if t == "-e" {
			ednOutput = true
			continue
		}
		key, val, hasEq := strings.Cut(t[2:], "=")
		switch key {
		case "edn":
			if hasEq {
				return "", false, "", fmt.Errorf("--edn does not take a value")
			}
			ednOutput = true
		case "root":
			if !hasEq {
				if i+1 >= len(tokens) || strings.HasPrefix(tokens[i+1], "--") {
					return "", false, "", fmt.Errorf("flag --%s requires a value", key)
				}
				val = tokens[i+1]
				i++
			}
			root = val
		default:
			return "", false, "", fmt.Errorf("unknown flag %q", t)
		}
	}
	return root, ednOutput, strings.Join(args, " "), nil
}

func blockDiffArgs(tokens []string) (root string, ednOutput bool, showUnchanged bool, nodeTitle string, err error) {
	var args []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if !strings.HasPrefix(t, "-") {
			args = append(args, t)
			continue
		}
		if t == "-u" {
			showUnchanged = true
			continue
		}
		if t == "-e" {
			ednOutput = true
			continue
		}
		key, val, hasEq := strings.Cut(t[2:], "=")
		switch key {
		case "unchanged":
			if hasEq {
				return "", false, false, "", fmt.Errorf("--unchanged does not take a value")
			}
			showUnchanged = true
		case "edn":
			if hasEq {
				return "", false, false, "", fmt.Errorf("--edn does not take a value")
			}
			ednOutput = true
		case "root":
			if !hasEq {
				if i+1 >= len(tokens) || strings.HasPrefix(tokens[i+1], "--") {
					return "", false, false, "", fmt.Errorf("flag --%s requires a value", key)
				}
				val = tokens[i+1]
				i++
			}
			root = val
		default:
			return "", false, false, "", fmt.Errorf("unknown flag %q", t)
		}
	}
	return root, ednOutput, showUnchanged, strings.Join(args, " "), nil
}

func inboxArgs(tokens []string) (root string, ednOutput bool, nodeTitle string, err error) {
	var args []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if !strings.HasPrefix(t, "-") {
			args = append(args, t)
			continue
		}
		if t == "-e" {
			ednOutput = true
			continue
		}
		key, val, hasEq := strings.Cut(t[2:], "=")
		switch key {
		case "edn":
			if hasEq {
				return "", false, "", fmt.Errorf("--edn does not take a value")
			}
			ednOutput = true
		case "root":
			if !hasEq {
				if i+1 >= len(tokens) || strings.HasPrefix(tokens[i+1], "--") {
					return "", false, "", fmt.Errorf("flag --%s requires a value", key)
				}
				val = tokens[i+1]
				i++
			}
			root = val
		default:
			return "", false, "", fmt.Errorf("unknown flag %q", t)
		}
	}
	return root, ednOutput, strings.Join(args, " "), nil
}

func printREPLBlockDiff(output BlockDiffOutput, showUnchanged bool) {
	fmt.Println()
	fmt.Println(ui.NodeTitle.Render(output.NodeTitle))
	fmt.Println(ui.Dim.Render(output.FilePath))
	fmt.Println(ui.Separator.Render(strings.Repeat("─", 60)))

	printGroup := func(label string, entries []BlockDiffEntry) {
		if len(entries) == 0 {
			return
		}
		fmt.Println(ui.Label.Render(label))
		for _, entry := range entries {
			text := entry.NewText
			if text == "" {
				text = entry.OldText
			}
			text = summarizeBlockText(text, 88)
			fmt.Printf("  %s %s\n", ui.Dim.Render(orDefault(entry.NewPath, entry.OldPath)), text)
			if entry.OldPath != "" && entry.NewPath != "" && entry.OldPath != entry.NewPath {
				fmt.Printf("    %s %s -> %s\n", ui.Dim.Render("path:"), entry.OldPath, entry.NewPath)
			}
			oldScope := scopeString(entry.OldScope)
			newScope := scopeString(entry.NewScope)
			if oldScope != "" || newScope != "" {
				if oldScope == newScope || newScope == "" {
					fmt.Printf("    %s %s\n", ui.Dim.Render("scope:"), orDefault(oldScope, newScope))
				} else {
					fmt.Printf("    %s %s -> %s\n", ui.Dim.Render("scope:"), orDefault(oldScope, "(none)"), orDefault(newScope, "(none)"))
				}
			}
		}
		fmt.Println()
	}

	if showUnchanged {
		printGroup("Unchanged", output.Unchanged)
	}
	printGroup("Edited", output.Edited)
	printGroup("Scope Changed", output.ScopeChanged)
	printGroup("Reordered", output.Reordered)
	printGroup("Inserted", output.Inserted)
	printGroup("Deleted", output.Deleted)

	if !showUnchanged &&
		len(output.Edited) == 0 &&
		len(output.ScopeChanged) == 0 &&
		len(output.Reordered) == 0 &&
		len(output.Inserted) == 0 &&
		len(output.Deleted) == 0 {
		fmt.Println(ui.Dim.Render("No block changes"))
		fmt.Println()
	}
}

func scopeString(scope []string) string {
	return strings.Join(scope, " > ")
}

func summarizeBlockText(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}

func extractBlockArgs(tokens []string, defaultSource string, defaultPath string, resolveTitle func(string) string) (root string, sourceTitle string, blockPath string, title string, parent string, err error) {
	var args []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if !strings.HasPrefix(t, "-") {
			args = append(args, t)
			continue
		}
		flagText := t
		if t == "-p" {
			flagText = "--parent"
		} else if t == "-r" {
			flagText = "--root"
		}
		key, val, hasEq := strings.Cut(flagText[2:], "=")
		switch key {
		case "root":
			// handled below
		case "parent":
			// handled below
		default:
			return "", "", "", "", "", fmt.Errorf("unknown flag %q", "--"+key)
		}
		if !hasEq {
			if i+1 >= len(tokens) || strings.HasPrefix(tokens[i+1], "--") {
				return "", "", "", "", "", fmt.Errorf("flag --%s requires a value", key)
			}
			val = tokens[i+1]
			i++
		}
		switch key {
		case "root":
			root = val
		case "parent":
			parent = val
		}
	}
	if len(args) == 0 {
		if defaultPath == "" {
			return "", "", "", "", "", fmt.Errorf("usage: extract-block [<source-node>] <path> [title] [--parent <title>]")
		}
		return root, defaultSource, defaultPath, "", parent, nil
	}
	if len(args) >= 2 {
		if canonical := resolveTitle(args[0]); canonical != "" && blockPathLike(args[1]) {
			sourceTitle = canonical
			blockPath = args[1]
			if len(args) > 2 {
				title = strings.Join(args[2:], " ")
			}
			return root, sourceTitle, blockPath, title, parent, nil
		}
	}
	pathCandidate := args[0]
	if defaultPath != "" && !blockPathLike(pathCandidate) {
		return root, defaultSource, defaultPath, strings.Join(args, " "), parent, nil
	}
	sourceTitle = defaultSource
	blockPath = pathCandidate
	if len(args) > 1 {
		title = strings.Join(args[1:], " ")
	}
	return root, sourceTitle, blockPath, title, parent, nil
}

func blockPathLike(s string) bool {
	if s == "root" {
		return true
	}
	for i, ch := range s {
		if (ch < '0' || ch > '9') && ch != '.' {
			return false
		}
		if ch == '.' && (i == 0 || i == len(s)-1) {
			return false
		}
	}
	return s != ""
}
