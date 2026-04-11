package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"olympos.io/encoding/edn"
	"sevens/internal/apply"
	"sevens/internal/backend"
	"sevens/internal/engine"
	"sevens/internal/function"
	"sevens/internal/graph"
	"sevens/internal/kb"
	projmd "sevens/internal/projection/md"
	"sevens/internal/store"
	"sevens/internal/ui"
)

// openDB opens the central sevens database and ensures the schema exists.
func openDB() (*sql.DB, error) {
	db, err := store.OpenDB()
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if err := store.InitTriplesSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialising schema: %w", err)
	}
	return db, nil
}

// resolveRoot determines the root directory. If explicit is provided, use it.
// Otherwise walk up from cwd looking for .sevens.edn. If that fails, try the
// active focus session.
func resolveRoot(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	root, err := graph.FindRoot(cwd)
	if err == nil {
		return root, nil
	}
	// No .sevens.edn found — try the active focus session
	session, sErr := apply.LoadSession()
	if sErr == nil && session != nil {
		return session.Root, nil
	}
	return "", err // return the original FindRoot error
}

func resolveNodeTitle(title string) (string, error) {
	if title != "." {
		return title, nil
	}
	session, err := apply.LoadSession()
	if err != nil {
		return "", fmt.Errorf("loading session: %w", err)
	}
	if session == nil {
		return "", fmt.Errorf("no active focus session — use 'sevens focus <node>' first")
	}
	return session.NodeTitle, nil
}

func syncRoot(rootDir string) error {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("resolving root path: %w", err)
	}

	// Pre-sync git commit (using new projection/md package)
	if projmd.IsGitRepo(root) {
		files, err := projmd.ChangedFiles(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s could not check git status: %v\n", ui.Warning.Render("[sync]"), err)
		}
		if len(files) > 0 {
			hash, err := projmd.CommitFiles(root, "sevens: sync", files)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s git commit failed: %v\n", ui.Success.Render("[sync]"), err)
			} else {
				fmt.Fprintf(os.Stderr, "%s Committed changes: %s\n", ui.Success.Render("[sync]"), hash)
			}
		}
	}

	// Open the new KB stack
	stack, err := openKB()
	if err != nil {
		return err
	}
	defer stack.Close()

	// Register root (still using old store for roots.edn)
	if err := store.AddRoot(root); err != nil {
		return fmt.Errorf("updating roots registry: %w", err)
	}

	// Sync via new projection
	proj := openProjection(stack)
	result, err := proj.Sync(context.Background(), root)
	if err != nil {
		return fmt.Errorf("syncing: %w", err)
	}

	// Validate via new KB
	violations, err := stack.KB.Validate(context.Background(), root, 9, 0)
	if err != nil {
		return fmt.Errorf("validating: %w", err)
	}

	// Print results
	fmt.Fprintf(os.Stderr, "%s scanned %d files, %d triples\n",
		ui.Success.Render("[sync]"), result.NodesScanned, result.TriplesWritten)
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "%s %s\n", ui.Warning.Render("[sync]"), e)
	}
	for _, v := range violations {
		fmt.Fprintf(os.Stderr, "%s %s: %s — %s\n",
			ui.Warning.Render("[validate]"), v.Kind, v.Title, v.Detail)
	}

	return nil
}

func syncAllRoots() error {
	roots, err := store.LoadRoots()
	if err != nil {
		return fmt.Errorf("loading roots: %w", err)
	}
	if len(roots) == 0 {
		return fmt.Errorf("no roots registered and no .sevens.edn found; run `sevens sync` from a directory with .sevens.edn")
	}
	fmt.Fprintf(os.Stderr, "%s Syncing %d roots\n", ui.Success.Render("[sync]"), len(roots))
	for _, root := range roots {
		fmt.Fprintf(os.Stderr, "%s --- %s ---\n", ui.Success.Render("[sync]"), root)
		if err := syncRoot(root); err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %v\n", ui.Error.Render("[error]"), root, err)
		}
	}
	return nil
}

func printEDN(v any) error {
	bs, err := edn.MarshalPPrint(v, nil)
	if err != nil {
		return fmt.Errorf("marshalling EDN: %w", err)
	}
	if _, err := os.Stdout.Write(bs); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	fmt.Println()
	return nil
}

func initCmd() *cobra.Command {
	var alias string
	var maxChars int

	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize a new sevens root",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			abs, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}

			if err := os.MkdirAll(abs, 0755); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}

			configPath := filepath.Join(abs, ".sevens.edn")
			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf(".sevens.edn already exists in %s", abs)
			}

			home, _ := os.UserHomeDir()
			portablePath := abs
			if home != "" && strings.HasPrefix(abs, home) {
				portablePath = "~" + abs[len(home):]
			}

			if alias == "" {
				alias = filepath.Base(abs)
			}

			var config strings.Builder
			config.WriteString(fmt.Sprintf("{:path %q\n", portablePath))
			config.WriteString(fmt.Sprintf(" :alias %q", alias))
			if maxChars > 0 {
				config.WriteString(fmt.Sprintf("\n :max-chars %d", maxChars))
			}
			config.WriteString("}\n")

			if err := os.WriteFile(configPath, []byte(config.String()), 0644); err != nil {
				return fmt.Errorf("writing .sevens.edn: %w", err)
			}
			fmt.Fprintf(os.Stderr, "%s Created %s\n", ui.Success.Render("[init]"), configPath)

			if !apply.IsGitRepo(abs) {
				out, err := runGitInit(abs)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s git init failed: %v\n", ui.Success.Render("[init]"), err)
				} else {
					fmt.Fprintf(os.Stderr, "%s %s", ui.Success.Render("[init]"), out)
				}
			}

			return syncRoot(abs)
		},
	}

	cmd.Flags().StringVar(&alias, "alias", "", "Short name for this root (defaults to directory name)")
	cmd.Flags().IntVar(&maxChars, "max-chars", 0, "Default character limit for nodes")
	return cmd
}

func runGitInit(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "init")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git init: %s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// printTree renders an OverviewOutput as a human-readable ASCII tree.
func formatCharCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}

func printTree(output *graph.OverviewOutput) {
	// Legacy printTree for EDN compat paths.
	printTreeFromNodes(overviewNodesToKB(output.Nodes), nil)

	v := output.Validation
	if len(v.Orphans) > 0 || len(v.MissingParents) > 0 {
		fmt.Println()
	}
	for _, o := range v.Orphans {
		fmt.Println(o + " " + ui.Warning.Render("(orphan)"))
	}
	for _, mp := range v.MissingParents {
		fmt.Println(mp + " " + ui.Warning.Render("(missing parent)"))
	}
}

// overviewNodesToKB converts old graph.OverviewNode to kb.OverviewNode.
func overviewNodesToKB(nodes []graph.OverviewNode) []kb.OverviewNode {
	result := make([]kb.OverviewNode, len(nodes))
	for i, n := range nodes {
		result[i] = kb.OverviewNode{
			Title:      n.Title,
			Parent:     n.Parent,
			Children:   n.Children,
			ChildCount: n.ChildCount,
			CrossRefs:  n.CrossRefs,
			CharCount:  n.CharCount,
		}
	}
	return result
}

// printTreeFromNodes prints a tree from kb.OverviewNode slice.
// violations is optional -- printed after the tree.
func printTreeFromNodes(nodes []kb.OverviewNode, violations []kb.Violation) {
	childMap := make(map[string][]string)
	nodeMap := make(map[string]kb.OverviewNode)

	rootNodes := []string{}
	for _, n := range nodes {
		nodeMap[n.Title] = n
		if n.Parent == nil {
			rootNodes = append(rootNodes, n.Title)
		} else {
			childMap[*n.Parent] = append(childMap[*n.Parent], n.Title)
		}
	}

	var sb strings.Builder

	nodeAnnotation := func(title string) string {
		n, ok := nodeMap[title]
		if !ok {
			return ""
		}
		if n.CharCount > 0 {
			return " " + ui.Dim.Render("("+formatCharCount(n.CharCount)+")")
		}
		return ""
	}

	var printNode func(title, prefix string, isLast bool)
	printNode = func(title, prefix string, isLast bool) {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		sb.WriteString(prefix + ui.Dim.Render(connector) + ui.NodeTitle.Render(title) + nodeAnnotation(title) + "\n")

		childPrefix := prefix + "│   "
		if isLast {
			childPrefix = prefix + "    "
		}

		children := childMap[title]
		for i, child := range children {
			printNode(child, childPrefix, i == len(children)-1)
		}
	}

	for i, root := range rootNodes {
		sb.WriteString(ui.NodeTitle.Render(root) + nodeAnnotation(root) + "\n")
		children := childMap[root]
		for j, child := range children {
			printNode(child, "", j == len(children)-1)
		}
		if i < len(rootNodes)-1 {
			sb.WriteString("\n")
		}
	}

	fmt.Print(sb.String())

	if len(violations) > 0 {
		fmt.Println()
		for _, v := range violations {
			fmt.Printf("%s %s: %s — %s\n",
				ui.Warning.Render("["+v.Kind+"]"), v.Title, v.Detail, v.Kind)
		}
	}
}

// opName returns a display name for a FileOp.
func opName(op apply.FileOp) string {
	if op.Title != "" {
		return op.Title
	}
	if op.File != "" {
		return op.File
	}
	return "unknown"
}

// printSuggestion prints a human-readable summary of a suggested LogEntry.
func printSuggestion(entry apply.LogEntry) {
	fmt.Fprintln(os.Stderr, ui.FormatStep(entry.Function, "suggest", entry.Target))
	for _, op := range entry.Ops {
		fmt.Fprintln(os.Stderr, ui.FormatOp(op.Action, opName(op)))
	}
	fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to apply.\n", entry.Target)
}

// buildRevisionHistory reads the log for a node and constructs the full
// suggestion/revision conversation thread for the given step index.

// printIntermediateOutput tries to format LLM output as a readable list.
// Falls back to rendering as markdown if it can't parse as structured data.
func printIntermediateOutput(raw string) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		start := 1
		end := len(lines) - 1
		for end > start && strings.TrimSpace(lines[end]) != "```" {
			end--
		}
		s = strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	}

	var items []struct {
		Title     string `json:"title"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(s), &items); err == nil && len(items) > 0 {
		for i, item := range items {
			fmt.Fprintf(os.Stderr, "  %d. %s\n     %s\n", i+1, item.Title, item.Rationale)
		}
		return
	}

	// Fallback: render as markdown
	fmt.Print(ui.RenderMarkdownOrPlain(raw))
}

// completeNodeTitles provides dynamic completion for node title arguments.
func completeNodeTitles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	root, _ := cmd.Flags().GetString("root")
	resolved, err := resolveRoot(root)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	db, err := openDB()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer db.Close()

	titles, err := store.ListNodeTitles(db, resolved)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	sort.Strings(titles)
	return titles, cobra.ShellCompDirectiveNoFileComp
}

// completeFunctionNames provides dynamic completion for function name arguments.
func completeFunctionNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	fns, err := apply.ListFunctions()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, fn := range fns {
		names = append(names, fn.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func syncCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Scan markdown files and rebuild the database",
		RunE: func(cmd *cobra.Command, args []string) error {
			// If no root specified, try to find one from cwd. If that fails too,
			// sync all roots from roots.edn.
			if root == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
				r, err := graph.FindRoot(cwd)
				if err != nil {
					// No .sevens.edn found walking up — sync all registered roots
					return syncAllRoots()
				}
				root = r
			}
			return syncRoot(root)
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory to scan")
	return cmd
}

func overviewCmd() *cobra.Command {
	var root string
	var ednOutput bool

	cmd := &cobra.Command{
		Use:   "overview",
		Short: "Print full tree overview",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			// EDN output uses legacy path for backward compat
			if ednOutput {
				config, err := graph.LoadConfig(resolved)
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				db, err := openDB()
				if err != nil {
					return err
				}
				defer db.Close()
				output, err := graph.BuildOverview(db, resolved, config)
				if err != nil {
					return fmt.Errorf("building overview: %w", err)
				}
				return printEDN(output)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			nodes, err := stack.KB.Overview(context.Background(), resolved)
			if err != nil {
				return fmt.Errorf("building overview: %w", err)
			}

			violations, err := stack.KB.Validate(context.Background(), resolved, 9, 0)
			if err != nil {
				return fmt.Errorf("validating: %w", err)
			}

			printTreeFromNodes(nodes, violations)
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().BoolVar(&ednOutput, "edn", false, "Output in EDN format")
	return cmd
}

func walkCmd() *cobra.Command {
	var root string
	var depth int
	var ednOutput bool

	cmd := &cobra.Command{
		Use:               "walk <node-title>",
		Short:             "Walk a node and its neighborhood",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			// EDN output still uses legacy path for backward compat
			if ednOutput {
				db, err := openDB()
				if err != nil {
					return err
				}
				defer db.Close()
				output, err := graph.BuildWalk(db, resolved, nodeTitle, depth)
				if err != nil {
					return fmt.Errorf("building walk: %w", err)
				}
				return printEDN(output)
			}

			// Non-EDN path uses new KB
			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			w, err := stack.KB.Walk(context.Background(), resolved, nodeTitle)
			if err != nil {
				return fmt.Errorf("walking node: %w", err)
			}

			fmt.Print(ui.FormatNodeHeader(
				w.Title, w.Parent, w.Role,
				w.Children, w.Siblings,
				w.ChildRoles, w.SiblingRoles,
				w.CrossRefs,
			))
			fmt.Println(ui.RenderMarkdownOrPlain(w.Content))

			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().IntVar(&depth, "depth", 1, "Depth of walk")
	cmd.Flags().BoolVar(&ednOutput, "edn", false, "Output in EDN format")
	return cmd
}

func treeCmd() *cobra.Command {
	var root string
	var ednOutput bool

	cmd := &cobra.Command{
		Use:               "tree <node-title>",
		Short:             "Show the subtree rooted at a node",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			// EDN output uses legacy path
			if ednOutput {
				config, err := graph.LoadConfig(resolved)
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				db, err := openDB()
				if err != nil {
					return err
				}
				defer db.Close()
				output, err := graph.BuildOverview(db, resolved, config)
				if err != nil {
					return fmt.Errorf("building overview: %w", err)
				}
				return printEDN(output)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			if subj := stack.KB.Resolve(context.Background(), resolved, nodeTitle); subj == "" {
				return fmt.Errorf("node not found: %s", nodeTitle)
			}

			// Get full overview and filter to subtree for display
			nodes, err := stack.KB.Overview(context.Background(), resolved)
			if err != nil {
				return fmt.Errorf("building overview: %w", err)
			}

			childMap := make(map[string][]string)
			for _, n := range nodes {
				if n.Parent != nil {
					childMap[*n.Parent] = append(childMap[*n.Parent], n.Title)
				}
			}

			var printNode func(title, prefix string, isLast bool)
			printNode = func(title, prefix string, isLast bool) {
				connector := "├── "
				if isLast {
					connector = "└── "
				}
				fmt.Print(prefix + ui.Dim.Render(connector) + ui.NodeTitle.Render(title) + "\n")
				childPrefix := prefix + "│   "
				if isLast {
					childPrefix = prefix + "    "
				}
				children := childMap[title]
				for i, child := range children {
					printNode(child, childPrefix, i == len(children)-1)
				}
			}

			fmt.Println(ui.NodeTitle.Render(nodeTitle))
			children := childMap[nodeTitle]
			for i, child := range children {
				printNode(child, "", i == len(children)-1)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().BoolVar(&ednOutput, "edn", false, "Output in EDN format")
	return cmd
}

func diffBlocksCmd() *cobra.Command {
	var root string
	var ednOutput bool
	var showUnchanged bool

	cmd := &cobra.Command{
		Use:               "diff-blocks <node-title>",
		Short:             "Show block-level changes for a node since last sync",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			output, err := graph.BuildBlockDiff(stack.Store.DB(), resolved, nodeTitle)
			if err != nil {
				return err
			}

			if ednOutput {
				return printEDN(output)
			}

			fmt.Println(ui.NodeTitle.Render(output.NodeTitle))
			fmt.Println(ui.Dim.Render(output.FilePath))
			fmt.Println(ui.Separator.Render(strings.Repeat("─", 60)))

			printGroup := func(label string, entries []graph.BlockDiffEntry) {
				if len(entries) == 0 {
					return
				}
				fmt.Println(ui.Label.Render(label + ":"))
				for _, entry := range entries {
					text := entry.NewText
					if text == "" {
						text = entry.OldText
					}
					if len(text) > 88 {
						text = text[:85] + "..."
					}
					fmt.Printf("  %s %s\n", ui.Dim.Render(orDefault(entry.NewPath, entry.OldPath)), text)
					if entry.OldPath != "" && entry.NewPath != "" && entry.OldPath != entry.NewPath {
						fmt.Printf("    %s %s -> %s\n", ui.Dim.Render("path:"), entry.OldPath, entry.NewPath)
					}
					oldScope := graph.ScopeString(entry.OldScope)
					newScope := graph.ScopeString(entry.NewScope)
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
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().BoolVar(&ednOutput, "edn", false, "Output in EDN format")
	cmd.Flags().BoolVar(&showUnchanged, "unchanged", false, "Include unchanged blocks")
	return cmd
}

func blocksCmd() *cobra.Command {
	var root string
	var ednOutput bool

	cmd := &cobra.Command{
		Use:               "blocks <node-title>",
		Short:             "List the current block structure of a node",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			output, err := graph.BuildBlockList(stack.Store.DB(), resolved, nodeTitle)
			if err != nil {
				return err
			}
			if ednOutput {
				return printEDN(output)
			}

			fmt.Println(ui.NodeTitle.Render(output.NodeTitle))
			fmt.Println(ui.Dim.Render(output.FilePath))
			fmt.Println(ui.Separator.Render(strings.Repeat("─", 60)))
			for _, block := range output.Blocks {
				text := summarizeInline(block.Text, 88)
				label := block.Kind
				if block.Kind == "heading" {
					label = fmt.Sprintf("h%d", block.Level)
				}
				if block.Kind == "task" && block.Signifier != "" {
					label = fmt.Sprintf("task[%s]", block.Signifier)
				}
				fmt.Printf("  %s  %s  %s\n",
					ui.Dim.Render(fmt.Sprintf("%-6s", block.Path)),
					ui.Label.Render(fmt.Sprintf("%-10s", label)),
					text,
				)
				if len(block.Scope) > 0 {
					fmt.Printf("          %s %s\n", ui.Dim.Render("scope:"), ui.Dim.Render(graph.ScopeString(block.Scope)))
				}
			}
			if len(output.Blocks) == 0 {
				fmt.Println(ui.Dim.Render("No blocks"))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().BoolVar(&ednOutput, "edn", false, "Output in EDN format")
	return cmd
}

func inboxCmd() *cobra.Command {
	var root string
	var ednOutput bool

	cmd := &cobra.Command{
		Use:               "inbox [node-title]",
		Short:             "Show inbox-style child summaries for a container node",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle := "inbox"
			if len(args) > 0 {
				nodeTitle = args[0]
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			output, err := graph.BuildInboxOverview(stack.Store.DB(), resolved, nodeTitle)
			if err != nil {
				return err
			}

			if ednOutput {
				return printEDN(output)
			}

			fmt.Println(ui.NodeTitle.Render(output.NodeTitle))
			fmt.Println(ui.Separator.Render(strings.Repeat("─", 60)))
			if len(output.Items) == 0 {
				fmt.Println(ui.Dim.Render("No child notes"))
				return nil
			}
			for _, item := range output.Items {
				fmt.Printf("  %s  %s\n", ui.Label.Render(fmt.Sprintf("%-12s", item.Kind)), ui.NodeTitle.Render(item.Title))
				var parts []string
				if item.Empty {
					parts = append(parts, "empty")
				} else {
					parts = append(parts, fmt.Sprintf("%d chars", item.CharCount))
				}
				if item.BulletCount > 0 {
					parts = append(parts, fmt.Sprintf("%d bullets", item.BulletCount))
				}
				if item.HeadingCount > 0 {
					parts = append(parts, fmt.Sprintf("%d headings", item.HeadingCount))
				}
				if item.Error != "" {
					parts = append(parts, "error: "+item.Error)
				}
				fmt.Printf("    %s\n", ui.Dim.Render(strings.Join(parts, " · ")))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().BoolVar(&ednOutput, "edn", false, "Output in EDN format")
	return cmd
}

func extractBlockCmd() *cobra.Command {
	var root string
	var parent string

	cmd := &cobra.Command{
		Use:               "extract-block <source-node> <block-path> [new-title]",
		Short:             "Create a new node from a block or heading section",
		Args:              cobra.RangeArgs(2, 3),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceTitle := args[0]
			blockPath := args[1]
			newTitle := ""
			if len(args) == 3 {
				newTitle = args[2]
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			db := stack.Store.DB()
			extracted, err := graph.PrepareBlockExtraction(db, resolved, sourceTitle, blockPath, newTitle, parent)
			if err != nil {
				return err
			}

			ops := []apply.FileOp{{
				Action:  "create",
				Title:   extracted.Title,
				Parent:  extracted.ParentTitle,
				Content: extracted.Content,
			}}
			created, _, err := apply.ExecuteOps(ops, resolved, db)
			if err != nil {
				return fmt.Errorf("creating node: %w", err)
			}
			for _, f := range created {
				fmt.Fprintf(os.Stderr, "%s Created: %s\n", ui.Success.Render("[extract]"), f)
			}
			if projmd.IsGitRepo(resolved) && len(created) > 0 {
				hash, err := projmd.CommitFiles(resolved, fmt.Sprintf("sevens: extract block %s from %q", blockPath, extracted.SourceTitle), created)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s git commit failed: %v\n", ui.Success.Render("[extract]"), err)
				} else {
					fmt.Fprintf(os.Stderr, "%s Committed: %s\n", ui.Success.Render("[extract]"), hash)
				}
			}
			return syncRoot(resolved)
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringVarP(&parent, "parent", "p", "", "Parent node title (defaults to the source node)")
	return cmd
}

func rootsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "roots",
		Short: "List all registered sevens roots",
		RunE: func(cmd *cobra.Command, args []string) error {
			roots, err := store.LoadRoots()
			if err != nil {
				return fmt.Errorf("loading roots: %w", err)
			}
			if len(roots) == 0 {
				fmt.Fprintln(os.Stderr, "No roots registered. Run `sevens sync` from a directory with .sevens.edn")
				return nil
			}
			activeRoot, _ := resolveRoot("")
			for _, r := range roots {
				marker := ""
				if r == activeRoot {
					marker = " " + ui.Dim.Render("(active)")
				}
				fmt.Println(ui.NodeTitle.Render(r) + marker)
			}
			return nil
		},
	}
}

// summarizeOutput generates a brief summary string from LLM output based on the output type.
func summarizeOutput(outputType, llmOutput string, ops []apply.FileOp) string {
	if outputType == "ops" && len(ops) > 0 {
		var parts []string
		creates, edits := 0, 0
		for _, op := range ops {
			switch op.Action {
			case "create":
				creates++
			case "edit":
				edits++
			}
		}
		if creates > 0 {
			parts = append(parts, fmt.Sprintf("create %d nodes", creates))
		}
		if edits > 0 {
			parts = append(parts, fmt.Sprintf("edit %d nodes", edits))
		}
		return strings.Join(parts, ", ")
	}
	if outputType == "suggestions" {
		var suggestions []struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal([]byte(llmOutput), &suggestions); err == nil && len(suggestions) > 0 {
			titles := make([]string, len(suggestions))
			for i, s := range suggestions {
				titles[i] = s.Title
			}
			if len(titles) <= 3 {
				return strings.Join(titles, ", ")
			}
			return fmt.Sprintf("%s, ... (%d total)", strings.Join(titles[:2], ", "), len(titles))
		}
	}
	// Default: first 80 chars of output
	summary := strings.TrimSpace(llmOutput)
	if len(summary) > 80 {
		summary = summary[:80] + "..."
	}
	return summary
}

func resolveSuspensionBlockForCLI(db *sql.DB, root, nodeTitle string, sus *engine.Suspension) *graph.BlockTarget {
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

// runPipeline runs function steps starting from startStep using the engine.
// Returns when a gate is hit (suspension) or all steps complete.
// allowedSteps, if non-nil, restricts execution to only the named steps.
func runPipeline(root, nodeTitle string, fn *apply.Function, startStep int, prev string, dryRun bool, confirm bool, includes []string, model string, allowedSteps map[string]bool, backendName string, blockPath string, blockID string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	walk, err := graph.BuildWalk(db, root, nodeTitle, 1)
	if err != nil {
		return fmt.Errorf("building walk: %w", err)
	}
	var targetBlock *graph.BlockTarget
	if blockID != "" {
		targetBlock, err = graph.ResolveBlockTargetBySubject(db, blockID)
		if err != nil {
			return fmt.Errorf("resolving block target: %w", err)
		}
	} else if blockPath != "" {
		targetBlock, err = graph.ResolveBlockTarget(db, root, nodeTitle, blockPath)
		if err != nil {
			return fmt.Errorf("resolving block target: %w", err)
		}
	}
	targetLabel := nodeTitle
	if targetBlock != nil {
		targetLabel = targetBlock.Label()
	}

	globalConfig, err := apply.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("loading global config: %w", err)
	}

	if model != "" {
		resolved := globalConfig.ResolveModel(model)
		if resolved.Model != globalConfig.LLM.Model || model == resolved.Model {
			globalConfig.LLM = resolved
		} else {
			globalConfig.LLM.Model = model
		}
	}

	// Build context from global config + function-level + node-level context files.
	var allContextFiles []string
	allContextFiles = append(allContextFiles, globalConfig.ContextFiles...)
	allContextFiles = append(allContextFiles, fn.ContextFiles...)
	allContextFiles = append(allContextFiles, walk.Node.ContextFiles...)
	contextStr := apply.LoadContextFiles(root, allContextFiles)

	session, _ := apply.LoadSession()
	if session != nil && len(session.Includes) > 0 {
		for _, inc := range session.Includes {
			incWalk, err := graph.BuildWalk(db, root, inc, 0)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s included node %q: %v\n", ui.Warning.Render("[warn]"), inc, err)
				continue
			}
			contextStr += fmt.Sprintf("<included-node title=%q>\n%s\n</included-node>\n\n", inc, incWalk.Node.Content)
		}
	}

	// Auto-include group if node has include-group: true in frontmatter.
	config, _ := graph.LoadConfig(root)
	autoIncludes, _ := graph.AutoGroupIncludes(db, root, nodeTitle, config)

	// One-shot includes from --include flag (supports @GroupName)
	resolvedIncludes := append([]string{}, autoIncludes...)
	for _, inc := range includes {
		if strings.HasPrefix(inc, "@") {
			groupName := inc[1:]
			config, cerr := graph.LoadConfig(root)
			if cerr != nil {
				fmt.Fprintf(os.Stderr, "%s loading config for group %q: %v\n", ui.Warning.Render("[warn]"), groupName, cerr)
				continue
			}
			grp, ok := config.Groups[groupName]
			if !ok {
				fmt.Fprintf(os.Stderr, "%s group %q not found\n", ui.Warning.Render("[warn]"), groupName)
				continue
			}
			titles, gerr := graph.ResolveGroup(db, root, grp)
			if gerr != nil {
				fmt.Fprintf(os.Stderr, "%s resolving group %q: %v\n", ui.Warning.Render("[warn]"), groupName, gerr)
				continue
			}
			resolvedIncludes = append(resolvedIncludes, titles...)
		} else {
			resolvedIncludes = append(resolvedIncludes, inc)
		}
	}
	for _, inc := range resolvedIncludes {
		incWalk, err := graph.BuildWalk(db, root, inc, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s included node %q: %v\n", ui.Warning.Render("[warn]"), inc, err)
			continue
		}
		contextStr += fmt.Sprintf("<included-node title=%q>\n%s\n</included-node>\n\n", inc, incWalk.Node.Content)
	}

	// Create inference backend
	var be backend.Backend
	if !dryRun {
		var beErr error
		be, beErr = backend.FromConfig(globalConfig, backendName)
		if beErr != nil {
			return fmt.Errorf("initializing backend: %w", beErr)
		}
		if model != "" {
			fmt.Fprintf(os.Stderr, "[backend] %s (model: %s)\n", be.Name(), model)
		} else {
			fmt.Fprintf(os.Stderr, "[backend] %s\n", be.Name())
		}
	}

	cfg := engine.PipelineConfig{
		DB:            db,
		Root:          root,
		NodeTitle:     nodeTitle,
		TargetBlock:   targetBlock,
		Function:      fn,
		GlobalConfig:  globalConfig,
		Walk:          walk,
		ContextStr:    contextStr,
		DryRun:        dryRun,
		Confirm:       confirm,
		StreamText:    true,
		AllowedSteps:  allowedSteps,
		Backend:       be,
		ModelOverride: model,
	}

	result := engine.RunPipeline(context.Background(), cfg, startStep, prev)

	if result.IsLeft() {
		// Suspension — display what was suggested
		sus := result.MustLeft()
		if sus.OutputType == "ops" && len(sus.Ops) > 0 {
			fmt.Fprintln(os.Stderr, ui.FormatStep(sus.Function, sus.StepName, orDefault(sus.TargetLabel, sus.Target)))
			for _, op := range sus.Ops {
				fmt.Fprintln(os.Stderr, ui.FormatOp(op.Action, opName(op)))
			}
			fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to apply.\n", sus.Target)
		} else {
			fmt.Fprintln(os.Stderr, ui.FormatStep(sus.Function, sus.StepName, orDefault(sus.TargetLabel, sus.Target)))
			fmt.Fprintln(os.Stderr)
			fmt.Print(ui.RenderMarkdownOrPlain(sus.Output))
			fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to approve and continue.\n", sus.Target)
		}
	} else {
		// Completed — display final output
		res := result.MustRight()
		if res.OutputType == "ops" && len(res.Ops) == 0 {
			fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, res.StepName, targetLabel))
			fmt.Fprintln(os.Stderr, "No changes proposed.")
		} else if res.Output != "" && res.OutputType == "text" && be == nil {
			// Text was already streamed via API streaming, just show the label
			fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, res.StepName, targetLabel))
		} else if res.Output != "" {
			fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, res.StepName, targetLabel))
			fmt.Println(ui.RenderMarkdownOrPlain(res.Output))
		}
	}

	return nil
}

func applyCmd() *cobra.Command {
	var root string
	var dryRun bool
	var yes bool
	var includes []string
	var model string
	var backendFlag string
	var blockPath string

	cmd := &cobra.Command{
		Use:   "apply <function> <node-title>",
		Short: "Apply a function to a node or a block within it",
		Args:  cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeFunctionNames(cmd, args, toComplete)
			}
			return completeNodeTitles(cmd, args, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			function := args[0]
			nodeTitle, err := resolveNodeTitle(args[1])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			fn, err := apply.LoadFunction(function)
			if err != nil {
				return fmt.Errorf("loading function: %w", err)
			}

			return runPipeline(resolved, nodeTitle, fn, 0, "", dryRun, !yes, includes, model, nil, backendFlag, blockPath, "")
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print rendered prompt without calling LLM")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip cost confirmation")
	cmd.Flags().StringSliceVar(&includes, "include", nil, "Additional node titles to include as context")
	cmd.Flags().StringVar(&model, "model", "", "Model name or profile to use (e.g., 'fast', 'powerful', or 'claude-opus-4-6')")
	cmd.Flags().StringVar(&backendFlag, "backend", "", "Inference backend (codex, claude, anthropic)")
	cmd.Flags().StringVar(&blockPath, "block", "", "Block path within the node to target")
	return cmd
}

func discussCmd() *cobra.Command {
	var root string
	var dryRun bool
	var yes bool
	var includes []string
	var model string
	var backendFlag string

	cmd := &cobra.Command{
		Use:               "discuss <node-title>",
		Short:             "Run the discuss function for a node",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			fn, err := apply.LoadFunction("discuss")
			if err != nil {
				return fmt.Errorf("loading function: %w", err)
			}

			return runPipeline(resolved, nodeTitle, fn, 0, "", dryRun, !yes, includes, model, nil, backendFlag, "", "")
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print rendered prompt without calling LLM")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip cost confirmation")
	cmd.Flags().StringSliceVar(&includes, "include", nil, "Additional node titles to include as context")
	cmd.Flags().StringVar(&model, "model", "", "Model name or profile to use (e.g., 'fast', 'powerful', or 'claude-opus-4-6')")
	cmd.Flags().StringVar(&backendFlag, "backend", "", "Inference backend (codex, claude, anthropic)")
	return cmd
}

func acceptCmd() *cobra.Command {
	var root string
	var with string
	var yes bool
	var stepsFlag string
	var backendFlag string

	cmd := &cobra.Command{
		Use:               "accept <node-title>",
		Short:             "Accept pending suggestions for a node",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := args[0]

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			var sus *engine.Suspension
			var susSubject string

			if strings.HasPrefix(arg, "suspension:") {
				// Direct subject ID lookup.
				sus, err = engine.FindSuspensionBySubject(db, resolved, arg)
				if err != nil {
					return fmt.Errorf("finding pending: %w", err)
				}
				if sus == nil {
					return fmt.Errorf("no pending suspension with id %s", arg)
				}
				susSubject = arg
			} else {
				// Node title: resolve and check for ambiguity.
				nodeTitle, rerr := resolveNodeTitle(arg)
				if rerr != nil {
					return rerr
				}
				all, ferr := engine.FindSuspensions(db, resolved, nodeTitle)
				if ferr != nil {
					return fmt.Errorf("finding pending: %w", ferr)
				}
				if len(all) == 0 {
					return fmt.Errorf("no pending suggestions for %s", nodeTitle)
				}
				if len(all) > 1 {
					fmt.Fprintf(os.Stderr, "%s multiple pending suspensions for %s — specify an id:\n",
						ui.Warning.Render("[ambiguous]"), ui.NodeTitle.Render(nodeTitle))
					for _, s := range all {
						fmt.Println(ui.FormatPending(orDefault(s.TargetLabel, s.Target), s.Function, s.StepName, s.Summary, s.Subject))
					}
					return fmt.Errorf("ambiguous: pass the suspension id (shown above) instead of the node title")
				}
				sus = &all[0]
				susSubject = sus.Subject
			}
			nodeTitle := sus.Target

			fn, err := apply.LoadFunction(sus.Function)
			if err != nil {
				return fmt.Errorf("loading function: %w", err)
			}

			if with != "" {
				steps := fn.EffectiveSteps()
				stepIndex := sus.StepIndex
				if stepIndex >= len(steps) {
					stepIndex = len(steps) - 1
				}
				step := steps[stepIndex]

				var streamTo *os.File
				if step.Output == "text" {
					streamTo = os.Stderr
				}

				globalConfig, _ := apply.LoadGlobalConfig()
				resolvedBackend := backendFlag
				if resolvedBackend == "" {
					resolvedBackend = sus.Backend
				}
				be, beErr := backend.FromConfig(globalConfig, resolvedBackend)
				if beErr != nil {
					fmt.Fprintf(os.Stderr, "[warn] backend init: %v, falling back to API\n", beErr)
				}

				newEntry, llmOutput, err := engine.ReviseStep(engine.ReviseConfig{
					DB:         db,
					Root:       resolved,
					NodeTitle:  nodeTitle,
					Function:   fn,
					Suspension: sus,
					Feedback:   with,
					Confirm:    !yes,
					StreamText: streamTo,
					Backend:    be,
				})
				if err != nil {
					return err
				}
				if newEntry == nil {
					fmt.Fprintf(os.Stderr, "%s Cancelled by user\n", ui.Warning.Render("[abort]"))
					return nil
				}

				// Display the result
				isLastStep := stepIndex == len(steps)-1
				if isLastStep && step.Output == "ops" {
					newEntry.Summary = summarizeOutput("ops", llmOutput, newEntry.Ops)
					printSuggestion(*newEntry)
				} else if isLastStep {
					fmt.Fprintln(os.Stderr, ui.FormatStep(sus.Function, step.Name, orDefault(sus.TargetLabel, nodeTitle)))
					fmt.Println(ui.RenderMarkdownOrPlain(llmOutput))
				} else {
					fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, step.Name, orDefault(sus.TargetLabel, nodeTitle)))
					printIntermediateOutput(llmOutput)
					fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to approve and continue.\n", nodeTitle)
				}
				// Write a new pending suspension so the user can act on the revised output.
				revBackendName := ""
				if be != nil {
					revBackendName = be.Name()
				}
				engine.WriteSuspension(db, resolved, nodeTitle, orDefault(sus.TargetLabel, nodeTitle), resolveSuspensionBlockForCLI(db, resolved, nodeTitle, sus), sus.Function, step.Name, sus.GateType, step.Output, newEntry.RawOutput, stepIndex, newEntry.Summary, newEntry.Ops, revBackendName)
				engine.ResolveSuspension(db, susSubject, "revised")
				return nil
			}

			// No --with: log acceptance and continue pipeline or execute ops.
			steps := fn.EffectiveSteps()

			nextStep := sus.StepIndex + 1
			if nextStep < len(steps) {
				// More pipeline steps — log acceptance and continue.
				acceptEntry := apply.LogEntry{
					Event:     "accepted",
					Root:      resolved,
					Function:  sus.Function,
					Target:    nodeTitle,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				}
				if err := apply.AppendLogDB(db, acceptEntry); err != nil {
					return fmt.Errorf("appending log: %w", err)
				}
				engine.ResolveSuspension(db, susSubject, "accepted")
				var allowedSteps map[string]bool
				if stepsFlag != "" {
					allowedSteps = make(map[string]bool)
					for _, s := range strings.Split(stepsFlag, ",") {
						allowedSteps[strings.TrimSpace(s)] = true
					}
				}
				pipelineBackend := backendFlag
				if pipelineBackend == "" {
					pipelineBackend = sus.Backend
				}
				return runPipeline(resolved, nodeTitle, fn, nextStep, sus.Output, false, !yes, nil, "", allowedSteps, pipelineBackend, sus.BlockPath, sus.BlockID)
			}

			// Last step was accepted.
			// If it's not ops output, just log acceptance and done.
			lastStep := steps[sus.StepIndex]
			if lastStep.Output != "ops" {
				acceptEntry := apply.LogEntry{
					Event:     "accepted",
					Root:      resolved,
					Function:  sus.Function,
					Target:    nodeTitle,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				}
				if err := apply.AppendLogDB(db, acceptEntry); err != nil {
					return fmt.Errorf("appending log: %w", err)
				}
				engine.ResolveSuspension(db, susSubject, "accepted")
				fmt.Fprintf(os.Stderr, "%s Acknowledged %s output for %s\n", ui.Success.Render("[accept]"), lastStep.Output, ui.NodeTitle.Render(nodeTitle))
				return nil
			}

			// Execute ops FIRST, then log acceptance only on success.
			created, edited, err := apply.ExecuteOps(sus.Ops, resolved, db)
			if err != nil {
				return fmt.Errorf("executing ops: %w", err)
			}

			// Ops succeeded — now log acceptance.
			acceptEntry := apply.LogEntry{
				Event:     "accepted",
				Root:      resolved,
				Function:  sus.Function,
				Target:    nodeTitle,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			if err := apply.AppendLogDB(db, acceptEntry); err != nil {
				return fmt.Errorf("appending log: %w", err)
			}
			engine.ResolveSuspension(db, susSubject, "accepted")

			commitHash := ""
			if apply.IsGitRepo(resolved) {
				allFiles := append(created, edited...)
				if len(allFiles) > 0 {
					hash, err := apply.CommitFiles(resolved, fmt.Sprintf("sevens: apply %s to %q", sus.Function, nodeTitle), allFiles)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s git commit failed: %v\n", ui.Success.Render("[accept]"), err)
					} else {
						commitHash = hash
					}
				}
			}

			appliedEntry := apply.LogEntry{
				Event:        "applied",
				Root:         resolved,
				Function:     sus.Function,
				Target:       nodeTitle,
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
				Commit:       commitHash,
				FilesCreated: created,
				FilesEdited:  edited,
			}

			if err := apply.AppendLogDB(db, appliedEntry); err != nil {
				return fmt.Errorf("appending log: %w", err)
			}

			if err := syncRoot(resolved); err != nil {
				fmt.Fprintf(os.Stderr, "%s re-sync failed: %v\n", ui.Success.Render("[accept]"), err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringVar(&with, "with", "", "Revision feedback to re-run with")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip cost confirmation")
	cmd.Flags().StringVar(&stepsFlag, "steps", "", "Comma-separated step names to advance (skips others)")
	cmd.Flags().StringVar(&backendFlag, "backend", "", "Inference backend override (codex, claude, anthropic)")
	return cmd
}

func rejectCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:               "reject <node-title>",
		Short:             "Reject pending suggestions for a node",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := args[0]

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}
			_ = resolved

			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			var sus *engine.Suspension
			var susSubject string

			if strings.HasPrefix(arg, "suspension:") {
				sus, err = engine.FindSuspensionBySubject(db, resolved, arg)
				if err != nil {
					return fmt.Errorf("finding pending: %w", err)
				}
				if sus == nil {
					return fmt.Errorf("no pending suspension with id %s", arg)
				}
				susSubject = arg
			} else {
				nodeTitle, rerr := resolveNodeTitle(arg)
				if rerr != nil {
					return rerr
				}
				all, ferr := engine.FindSuspensions(db, resolved, nodeTitle)
				if ferr != nil {
					return fmt.Errorf("finding pending: %w", ferr)
				}
				if len(all) == 0 {
					return fmt.Errorf("no pending suggestions for %s", nodeTitle)
				}
				if len(all) > 1 {
					fmt.Fprintf(os.Stderr, "%s multiple pending suspensions for %s — specify an id:\n",
						ui.Warning.Render("[ambiguous]"), ui.NodeTitle.Render(nodeTitle))
					for _, s := range all {
						fmt.Println(ui.FormatPending(orDefault(s.TargetLabel, s.Target), s.Function, s.StepName, s.Summary, s.Subject))
					}
					return fmt.Errorf("ambiguous: pass the suspension id (shown above) instead of the node title")
				}
				sus = &all[0]
				susSubject = sus.Subject
			}
			nodeTitle := sus.Target

			entry := apply.LogEntry{
				Event:     "rejected",
				Root:      resolved,
				Target:    nodeTitle,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}

			if err := apply.AppendLogDB(db, entry); err != nil {
				return fmt.Errorf("appending log: %w", err)
			}

			engine.ResolveSuspension(db, susSubject, "rejected")

			fmt.Fprintf(os.Stderr, "%s %s\n", ui.Warning.Render("Rejected suggestions for"), ui.NodeTitle.Render(nodeTitle))
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	return cmd
}

func pendingCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "pending",
		Short: "List nodes with pending suggestions",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			suspensions, err := engine.ListSuspensions(db, resolved)
			if err != nil {
				return fmt.Errorf("listing pending: %w", err)
			}

			if len(suspensions) == 0 {
				fmt.Fprintln(os.Stderr, "No pending suggestions")
				return nil
			}

			for _, sus := range suspensions {
				fmt.Println(ui.FormatPending(orDefault(sus.TargetLabel, sus.Target), sus.Function, sus.StepName, sus.Summary, sus.Subject))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	return cmd
}

func functionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "functions",
		Short: "List available functions",
		RunE: func(cmd *cobra.Command, args []string) error {
			functions, err := apply.ListFunctions()
			if err != nil {
				return fmt.Errorf("listing functions: %w", err)
			}

			if len(functions) == 0 {
				fmt.Fprintln(os.Stderr, "No functions defined")
				return nil
			}

			// Find the longest name for alignment.
			maxLen := 0
			for _, fn := range functions {
				if len(fn.Name) > maxLen {
					maxLen = len(fn.Name)
				}
			}

			for _, fn := range functions {
				padding := strings.Repeat(" ", maxLen-len(fn.Name))
				fmt.Fprintf(os.Stdout, "%s%s  %s\n", ui.Label.Render(fn.Name), padding, ui.Dim.Render(fn.Description))
			}
			return nil
		},
	}
}

func defineCmd() *cobra.Command {
	var description string
	var prompt string

	cmd := &cobra.Command{
		Use:   "define <name>",
		Short: "Define a new function",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			configDir, err := store.ConfigDir()
			if err != nil {
				return fmt.Errorf("getting config dir: %w", err)
			}

			fnDir := filepath.Join(configDir, "functions")
			if err := os.MkdirAll(fnDir, 0755); err != nil {
				return fmt.Errorf("creating functions dir: %w", err)
			}

			fn := apply.Function{
				Name:        name,
				Description: description,
				Input:       "node",
				Output:      "text",
			}

			// If inline prompt provided, store it; otherwise create a .md file
			if prompt != "" {
				fn.Prompt = prompt
			} else {
				mdPath := filepath.Join(fnDir, name+".md")
				mdContent := fmt.Sprintf(`<instruction>
%s

Examine the target node and provide your analysis.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
{{context}}
</graph-context>

<output-spec>
Return plain text — your analysis, not JSON.
</output-spec>
`, description)
				if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
					return fmt.Errorf("writing prompt file: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Created prompt template: %s\n", mdPath)
			}

			data, err := edn.MarshalPPrint(fn, nil)
			if err != nil {
				return fmt.Errorf("marshalling function: %w", err)
			}

			path := filepath.Join(fnDir, name+".edn")
			if err := os.WriteFile(path, data, 0644); err != nil {
				return fmt.Errorf("writing function file: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Defined function %q at %s\n", name, path)
			return nil
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "Short description")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt template (optional; creates .md file if omitted)")
	cmd.MarkFlagRequired("description")
	return cmd
}

func focusCmd() *cobra.Command {
	var root string
	var includes []string
	var excludes []string

	cmd := &cobra.Command{
		Use:               "focus <node-title>",
		Short:             "Pin a node as the active focus for subsequent commands",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle := args[0]
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			// Verify node exists via new KB
			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			w, err := stack.KB.Walk(context.Background(), resolved, nodeTitle)
			if err != nil {
				return fmt.Errorf("walking node: %w", err)
			}

			// Still persist to EDN file for REPL compat
			session := &apply.Session{
				Root:      resolved,
				NodeTitle: nodeTitle,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
				Includes:  includes,
				Excludes:  excludes,
			}
			if err := apply.SaveSession(session); err != nil {
				return fmt.Errorf("saving session: %w", err)
			}

			fmt.Fprintf(os.Stderr, "%s %s\n", ui.Success.Render("[focus]"), ui.NodeTitle.Render(w.Title))
			if w.Parent != nil {
				fmt.Fprintf(os.Stderr, "%s%s\n", ui.Dim.Render("  parent: "), *w.Parent)
			}
			if len(w.Children) > 0 {
				fmt.Fprintf(os.Stderr, "%s%s\n", ui.Dim.Render("  children: "), strings.Join(w.Children, ", "))
			}
			if len(w.Siblings) > 0 {
				fmt.Fprintf(os.Stderr, "%s%s\n", ui.Dim.Render("  siblings: "), strings.Join(w.Siblings, ", "))
			}
			if len(includes) > 0 {
				fmt.Fprintf(os.Stderr, "%s%s\n", ui.Dim.Render("  includes: "), strings.Join(includes, ", "))
			}
			fmt.Fprintf(os.Stderr, "\n%s\n", ui.Dim.Render("Use '.' as node title in other commands to reference this node."))
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringSliceVar(&includes, "include", nil, "Additional node titles to include in context")
	cmd.Flags().StringSliceVar(&excludes, "exclude", nil, "Node titles to exclude from context")
	return cmd
}

func unfocusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unfocus",
		Short: "Clear the active focus session",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, _ := apply.LoadSession()
			if session == nil {
				fmt.Fprintln(os.Stderr, "No active focus session")
				return nil
			}
			if err := apply.ClearSession(); err != nil {
				return fmt.Errorf("clearing session: %w", err)
			}
			fmt.Fprintf(os.Stderr, "%s Cleared focus on %s\n", ui.Success.Render("[unfocus]"), ui.NodeTitle.Render(session.NodeTitle))
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current focus session and pending state",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := apply.LoadSession()
			if err != nil {
				return fmt.Errorf("loading session: %w", err)
			}
			if session == nil {
				fmt.Fprintln(os.Stderr, "No active focus session")
				return nil
			}
			fmt.Printf("%s %s\n", ui.Label.Render("Focused:"), ui.NodeTitle.Render(session.NodeTitle))
			fmt.Printf("%s %s\n", ui.Dim.Render("Root:"), ui.Dim.Render(session.Root))
			fmt.Printf("%s %s\n", ui.Dim.Render("Since:"), ui.Dim.Render(session.CreatedAt))
			if len(session.Includes) > 0 {
				fmt.Printf("%s %s\n", ui.Dim.Render("Includes:"), strings.Join(session.Includes, ", "))
			}
			if len(session.Excludes) > 0 {
				fmt.Printf("%s %s\n", ui.Dim.Render("Excludes:"), strings.Join(session.Excludes, ", "))
			}

			globalCfg, gErr := apply.LoadGlobalConfig()
			if gErr == nil && len(globalCfg.ContextFiles) > 0 {
				fmt.Printf("%s %s\n", ui.Dim.Render("Global context:"), ui.Dim.Render(strings.Join(globalCfg.ContextFiles, ", ")))
			}

			return nil
		},
	}
}

func logCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:               "log <node-title>",
		Short:             "Show the operation log for a node",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			if subj := stack.KB.Resolve(context.Background(), resolved, nodeTitle); subj == "" {
				return fmt.Errorf("node not found: %s", nodeTitle)
			}

			entries, err := stack.KB.ReadLog(context.Background(), resolved, nodeTitle)
			if err != nil {
				return fmt.Errorf("reading log: %w", err)
			}

			if len(entries) == 0 {
				fmt.Fprintln(os.Stderr, "No log entries")
				return nil
			}

			for _, e := range entries {
				fmt.Println(ui.FormatLogEntry(e.Timestamp, e.Event, e.Function, e.Step, "", ""))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	return cmd
}

func queryCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "query <sql>",
		Short: "Run a SQL query against the triples store",
		Long:  "Execute a SQL query against the triples table. Template variables {{root}} and {{target}} (from focus session) are substituted.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			bindings := map[string]string{
				"root": resolved,
			}

			// Also bind focus session if active
			session, _ := apply.LoadSession()
			if session != nil {
				bindings["target"] = session.NodeTitle
				bindings["focused"] = session.NodeTitle
			}

			// Substitute template variables into the SQL query
			query := args[0]
			for k, v := range bindings {
				placeholder := "{{" + k + "}}"
				escaped := strings.ReplaceAll(v, "'", "''")
				query = strings.ReplaceAll(query, placeholder, "'"+escaped+"'")
			}

			results, err := stack.Store.RawQuery(context.Background(), query)
			if err != nil {
				return fmt.Errorf("running query: %w", err)
			}

			if len(results) <= 1 {
				fmt.Fprintln(os.Stderr, "No results")
				return nil
			}

			// Print as aligned table
			// First row is headers
			headers := results[0]
			widths := make([]int, len(headers))
			for i, h := range headers {
				widths[i] = len(h)
			}
			for _, row := range results[1:] {
				for i, cell := range row {
					if i < len(widths) {
						// Truncate long values for display
						display := cell
						if len(display) > 80 {
							display = display[:77] + "..."
						}
						if len(display) > widths[i] {
							widths[i] = len(display)
						}
					}
				}
			}

			// Print header
			for i, h := range headers {
				fmt.Printf("%-*s  ", widths[i], h)
			}
			fmt.Println()
			// Print separator
			for i := range headers {
				fmt.Print(strings.Repeat("─", widths[i]))
				fmt.Print("  ")
			}
			fmt.Println()
			// Print rows
			for _, row := range results[1:] {
				for i, cell := range row {
					display := cell
					if len(display) > 80 {
						display = display[:77] + "..."
					}
					if i < len(widths) {
						fmt.Printf("%-*s  ", widths[i], display)
					}
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	return cmd
}

func searchCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search node titles and content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			query := args[0]
			ctx := context.Background()

			// Search titles: find subjects whose node/title contains query,
			// then filter to this root.
			titleSubjects, err := stack.Store.Search(ctx, kb.PredNodeTitle, query)
			if err != nil {
				return fmt.Errorf("searching titles: %w", err)
			}
			var titles []string
			for _, subj := range titleSubjects {
				r, _, _ := stack.Graph.Lookup(ctx, subj, kb.PredNodeRoot)
				if r == resolved {
					t, _, _ := stack.Graph.Lookup(ctx, subj, kb.PredNodeTitle)
					if t != "" {
						titles = append(titles, t)
					}
				}
			}

			// Search content: find subjects whose node/content contains query,
			// then filter to this root and resolve to titles.
			contentSubjects, err := stack.Store.Search(ctx, kb.PredNodeContent, query)
			if err != nil {
				return fmt.Errorf("searching content: %w", err)
			}
			var contentMatches []string
			for _, subj := range contentSubjects {
				r, _, _ := stack.Graph.Lookup(ctx, subj, kb.PredNodeRoot)
				if r == resolved {
					t, _, _ := stack.Graph.Lookup(ctx, subj, kb.PredNodeTitle)
					if t != "" {
						contentMatches = append(contentMatches, t)
					}
				}
			}

			if len(titles) == 0 && len(contentMatches) == 0 {
				fmt.Fprintln(os.Stderr, "No matches")
				return nil
			}

			if len(titles) > 0 {
				fmt.Println(ui.Label.Render("Title matches:"))
				for _, t := range titles {
					fmt.Printf("  %s\n", ui.NodeTitle.Render(t))
				}
			}
			if len(contentMatches) > 0 {
				if len(titles) > 0 {
					fmt.Println()
				}
				fmt.Println(ui.Label.Render("Content matches:"))
				for _, t := range contentMatches {
					fmt.Printf("  %s\n", ui.NodeTitle.Render(t))
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	return cmd
}

func revertCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:               "revert <node-title>",
		Short:             "Undo the last applied operation on a node",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			// Find the last "applied" log entry with a commit hash
			// Note: kb.LogEntry doesn't carry Commit/FilesCreated/FilesEdited
			// yet -- those are richer fields from apply.LogEntry. For now,
			// fall back to legacy log for revert since it needs those fields.
			db := stack.Store.DB()
			entries, err := apply.ReadLogDB(db, resolved, nodeTitle)
			if err != nil {
				return fmt.Errorf("reading log: %w", err)
			}

			var lastApplied *apply.LogEntry
			for i := len(entries) - 1; i >= 0; i-- {
				if entries[i].Event == "applied" && entries[i].Commit != "" {
					lastApplied = &entries[i]
					break
				}
			}

			if lastApplied == nil {
				return fmt.Errorf("no applied operations with git commits found for %q", nodeTitle)
			}

			fmt.Fprintf(os.Stderr, "%s Last applied: %s %s (commit %s)\n",
				ui.Warning.Render("[revert]"), lastApplied.Function, lastApplied.Timestamp, lastApplied.Commit)
			fmt.Fprintf(os.Stderr, "%s\n", ui.Dim.Render("  Files created: "+strings.Join(lastApplied.FilesCreated, ", ")))
			fmt.Fprintf(os.Stderr, "%s\n", ui.Dim.Render("  Files edited: "+strings.Join(lastApplied.FilesEdited, ", ")))
			fmt.Fprintf(os.Stderr, "\nThis will revert to the commit before %s. Continue? [y/N] ", lastApplied.Commit)

			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))
			if input != "y" && input != "yes" {
				fmt.Fprintf(os.Stderr, "%s Cancelled\n", ui.Warning.Render("[revert]"))
				return nil
			}

			if !projmd.IsGitRepo(resolved) {
				return fmt.Errorf("root is not a git repository")
			}

			parentHashOut, err := exec.Command("git", "-C", resolved, "rev-parse", lastApplied.Commit+"~1").CombinedOutput()
			if err != nil {
				return fmt.Errorf("finding parent commit: %s", strings.TrimSpace(string(parentHashOut)))
			}
			parentHash := strings.TrimSpace(string(parentHashOut))

			allFiles := append(lastApplied.FilesCreated, lastApplied.FilesEdited...)
			for _, f := range allFiles {
				out, err := exec.Command("git", "-C", resolved, "checkout", parentHash, "--", f).CombinedOutput()
				if err != nil {
					fmt.Fprintf(os.Stderr, "[revert] warning: could not restore %s: %s\n", f, strings.TrimSpace(string(out)))
				}
			}

			for _, f := range lastApplied.FilesCreated {
				path := filepath.Join(resolved, f)
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr, "%s warning: could not remove %s: %v\n", ui.Warning.Render("[revert]"), f, err)
				} else {
					fmt.Fprintf(os.Stderr, "%s Removed: %s\n", ui.Warning.Render("[revert]"), f)
				}
			}

			hash, err := projmd.CommitFiles(resolved, fmt.Sprintf("sevens: revert %s on %q", lastApplied.Function, nodeTitle), allFiles)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s git commit failed: %v\n", ui.Warning.Render("[revert]"), err)
			} else {
				fmt.Fprintf(os.Stderr, "%s Committed revert: %s\n", ui.Warning.Render("[revert]"), hash)
			}

			// Log the revert via new KB
			stack.KB.AppendLog(context.Background(), kb.LogEntry{
				Event:     "reverted",
				Root:      resolved,
				Function:  lastApplied.Function,
				Node:      nodeTitle,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Result:    fmt.Sprintf("reverted commit %s", lastApplied.Commit),
			})

			return syncRoot(resolved)
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	return cmd
}

func prepareCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "prepare <function> <node-title>",
		Short: "Compile a function into an agent task checklist",
		Args:  cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeFunctionNames(cmd, args, toComplete)
			}
			return completeNodeTitles(cmd, args, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			fnName := args[0]
			nodeTitle, err := resolveNodeTitle(args[1])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			oldFn, err := apply.LoadFunction(fnName)
			if err != nil {
				return fmt.Errorf("loading function: %w", err)
			}
			fn := function.ConvertFunction(oldFn)

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			// Use new context resolution
			steps := fn.EffectiveSteps()
			prepSteps := make([]ui.PrepareStep, len(steps))
			for i, s := range steps {
				prompt := ""
				if s.Backend.PromptTemplate != "" {
					rc, rcErr := function.ResolveContext(context.Background(), stack.KB, resolved, nodeTitle, s, "")
					if rcErr == nil {
						prompt = function.RenderPrompt(s.Backend.PromptTemplate, rc)
					}
				}
				prepSteps[i] = ui.PrepareStep{
					Name: s.Name, Gate: oldFn.EffectiveSteps()[i].Gate, Fn: oldFn.EffectiveSteps()[i].Fn,
					MapOver: oldFn.EffectiveSteps()[i].MapOver, Output: oldFn.EffectiveSteps()[i].Output, Prompt: prompt,
				}
			}

			// Walk the target for context display
			walkCtx, walkErr := stack.KB.Walk(context.Background(), resolved, nodeTitle)
			if walkErr != nil {
				return fmt.Errorf("walking node: %w", walkErr)
			}

			// Determine what context the function needs
			needsParent, needsSiblings, needsChildren := false, false, false
			for _, ps := range oldFn.Context {
				switch ps.As {
				case "parent":
					needsParent = true
				case "siblings":
					needsSiblings = true
				case "children":
					needsChildren = true
				}
			}
			for _, r := range oldFn.Requires {
				switch r.Role {
				case "parent":
					needsParent = true
				case "siblings":
					needsSiblings = true
				case "children":
					needsChildren = true
				}
			}

			globalConfig, err := apply.LoadGlobalConfig()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			var allContextFiles []string
			allContextFiles = append(allContextFiles, globalConfig.ContextFiles...)
			allContextFiles = append(allContextFiles, oldFn.ContextFiles...)

			fmt.Print(ui.RenderPrepareChecklist(ui.PrepareData{
				FnName:       fnName,
				NodeTitle:    nodeTitle,
				Steps:        prepSteps,
				Parent:       walkCtx.Parent,
				Siblings:     walkCtx.Siblings,
				Children:     walkCtx.Children,
				NeedsParent:  needsParent,
				NeedsSibling: needsSiblings,
				NeedsChild:   needsChildren,
				CrossWalk:    oldFn.CrossWalk,
				ContextFiles: allContextFiles,
			}))

			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	return cmd
}

func submitCmd() *cobra.Command {
	var root string
	var fnName string
	var stepName string
	var outputType string
	var response string
	var responseFile string

	cmd := &cobra.Command{
		Use:               "submit <node-title>",
		Short:             "Submit an agent's response for a function step",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}
			_ = resolved

			// Get the response text
			responseText := response
			if responseFile != "" {
				data, err := os.ReadFile(responseFile)
				if err != nil {
					return fmt.Errorf("reading response file: %w", err)
				}
				responseText = string(data)
			}
			if responseText == "" {
				return fmt.Errorf("no response provided (use --response or --response-file)")
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()
			db := stack.Store.DB()

			// Determine step name and index
			if stepName == "" {
				stepName = "default"
			}
			stepIndex := 0
			if fn, err := apply.LoadFunction(fnName); err == nil {
				steps := fn.EffectiveSteps()
				for i, s := range steps {
					if s.Name == stepName {
						stepIndex = i
						break
					}
				}
			}

			// Create a pipeline in Pending state for the submitted result
			ps := function.NewPipelineStore(stack.Store)
			p := function.NewPipeline(resolved, fnName, nodeTitle)
			p.CurrentStep = stepIndex

			// Build the transform result from the submitted response
			var tfResult function.TransformResult
			tfResult.Raw = responseText

			switch outputType {
			case "ops":
				ops, parseErr := apply.ParseOps(responseText)
				if parseErr != nil {
					return fmt.Errorf("parsing ops response: %w", parseErr)
				}
				for _, op := range ops {
					tfResult.Ops = append(tfResult.Ops, function.FileOp{
						Action:  op.Action,
						Title:   op.Title,
						Parent:  op.Parent,
						File:    op.File,
						OldText: op.OldText,
						NewText: op.NewText,
						Content: op.Content,
					})
				}

				// Suspend in Pending phase
				p.CurrentResult = &tfResult
				p.Phase = function.PhasePending
				if err := ps.Save(context.Background(), p); err != nil {
					return fmt.Errorf("saving pipeline: %w", err)
				}

				// Also log to old system for backwards compat
				entry := apply.LogEntry{
					Root: resolved, Function: fnName, Target: nodeTitle,
					Step: stepName, StepIndex: stepIndex,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					RawOutput: responseText, Event: "suggested",
					Ops: ops, Summary: summarizeOutput("ops", responseText, ops),
				}
				_ = apply.AppendLogDB(db, entry)

				fmt.Fprintf(os.Stderr, "[submit] %s/%s → %q (pipeline %s)\n", fnName, stepName, nodeTitle, p.ID)
				for _, op := range ops {
					fmt.Fprintln(os.Stderr, ui.FormatOp(op.Action, opName(op)))
				}
				fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to apply.\n", nodeTitle)

			case "suggestions":
				tfResult.IsText = true
				p.CurrentResult = &tfResult
				p.Phase = function.PhasePending
				if err := ps.Save(context.Background(), p); err != nil {
					return fmt.Errorf("saving pipeline: %w", err)
				}

				summary := summarizeOutput("suggestions", responseText, nil)
				entry := apply.LogEntry{
					Root: resolved, Function: fnName, Target: nodeTitle,
					Step: stepName, StepIndex: stepIndex,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					RawOutput: responseText, Event: "suggested", Summary: summary,
				}
				_ = apply.AppendLogDB(db, entry)

				fmt.Fprintf(os.Stderr, "[submit] %s/%s → %q (%s, pipeline %s)\n", fnName, stepName, nodeTitle, summary, p.ID)
				fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to approve and continue.\n", nodeTitle)

			case "text":
				tfResult.IsText = true
				p.CurrentResult = &tfResult
				p.Phase = function.PhaseCompleted
				if err := ps.Save(context.Background(), p); err != nil {
					return fmt.Errorf("saving pipeline: %w", err)
				}

				entry := apply.LogEntry{
					Root: resolved, Function: fnName, Target: nodeTitle,
					Step: stepName, StepIndex: stepIndex,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					RawOutput: responseText, Event: "completed",
				}
				_ = apply.AppendLogDB(db, entry)

				fmt.Fprintf(os.Stderr, "[submit] %s/%s → %q (completed)\n", fnName, stepName, nodeTitle)
				fmt.Println(responseText)

			default:
				return fmt.Errorf("unknown output type: %q (use ops, suggestions, or text)", outputType)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringVar(&fnName, "function", "", "Function name")
	cmd.Flags().StringVar(&stepName, "step", "", "Step name (default: 'default')")
	cmd.Flags().StringVar(&outputType, "output", "", "Output type: ops, suggestions, or text")
	cmd.Flags().StringVar(&response, "response", "", "Response text (inline)")
	cmd.Flags().StringVar(&responseFile, "response-file", "", "Path to file containing response")
	cmd.MarkFlagRequired("function")
	cmd.MarkFlagRequired("output")
	return cmd
}

func instantiateTemplateNode(db *sql.DB, root string, tmpl *apply.NodeTemplate, cliParent string, cliTarget string, vars map[string]string) (*apply.TemplateExecutionResult, error) {
	result, err := apply.ExecuteTemplate(db, root, tmpl, apply.TemplateExecutionOptions{
		Parent:     cliParent,
		TargetNode: cliTarget,
		Vars:       vars,
	})
	if err != nil {
		return nil, err
	}

	files := append([]string(nil), result.Created...)
	files = append(files, result.Edited...)
	if projmd.IsGitRepo(root) && len(files) > 0 {
		if _, err := projmd.CommitFiles(root, result.CommitMessage, files); err != nil {
			fmt.Fprintf(os.Stderr, "%s git commit failed: %v\n", ui.Warning.Render("[warn]"), err)
		}
	}
	return result, nil
}

func previewTemplateNode(db *sql.DB, root string, tmpl *apply.NodeTemplate, cliParent string, cliTarget string, vars map[string]string) error {
	preview, err := apply.PreviewTemplate(db, root, tmpl, apply.TemplateExecutionOptions{
		Parent:     cliParent,
		TargetNode: cliTarget,
		Vars:       vars,
	})
	if err != nil {
		return err
	}

	fmt.Println(ui.Label.Render("Template Preview"))
	fmt.Printf("%s %s\n", ui.Dim.Render("template:"), tmpl.Name)
	fmt.Printf("%s %s\n", ui.Dim.Render("mode:"), preview.Mode)
	if preview.Draft {
		fmt.Printf("%s %s\n", ui.Dim.Render("draft:"), "yes")
	}
	if len(preview.Missing) > 0 {
		fmt.Printf("%s %s\n", ui.Dim.Render("missing:"), strings.Join(preview.Missing, ", "))
	}
	switch preview.Mode {
	case "append-node", "insert-block":
		fmt.Printf("%s %s\n", ui.Dim.Render("target:"), preview.TargetNode)
		if preview.Heading != "" {
			fmt.Printf("%s %s\n", ui.Dim.Render("heading:"), preview.Heading)
			if preview.CreateIfMissing {
				fmt.Printf("%s %s\n", ui.Dim.Render("create-heading:"), "yes")
			}
		}
	default:
		fmt.Printf("%s %s\n", ui.Dim.Render("title:"), preview.Title)
		if preview.Parent != "" {
			fmt.Printf("%s %s\n", ui.Dim.Render("parent:"), preview.Parent)
		}
		if preview.BootstrapParent != "" {
			fmt.Printf("%s %s\n", ui.Dim.Render("bootstrap:"), preview.BootstrapParent)
		}
	}
	fmt.Println(ui.Separator.Render(strings.Repeat("─", 60)))
	fmt.Println(ui.RenderMarkdownOrPlain(preview.Content))
	return nil
}

func addTemplateSemanticVars(varMap map[string]string, values map[string]string) map[string]string {
	if varMap == nil {
		varMap = make(map[string]string, len(values))
	}
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		varMap[key] = value
	}
	return varMap
}

func templatesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "templates",
		Short: "List available manual templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			templates, err := apply.ListTemplates()
			if err != nil {
				return fmt.Errorf("listing templates: %w", err)
			}
			if len(templates) == 0 {
				fmt.Fprintln(os.Stderr, "No templates defined")
				return nil
			}
			for _, name := range templates {
				tmpl, err := apply.LoadTemplate(name)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s %s: %v\n", ui.Warning.Render("[warn]"), name, err)
					continue
				}
				fmt.Fprintf(os.Stdout, "%s  %s\n", ui.Label.Render(name), ui.Dim.Render(tmpl.Description))
			}
			return nil
		},
	}
}

func captureCmd() *cobra.Command {
	var root string
	var parent string
	var vars []string
	var dryRun bool
	var titleVar string
	var summaryVar string

	cmd := &cobra.Command{
		Use:   "capture [title]",
		Short: "Quick-capture a note with the inbox-capture template",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}
			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()
			db := stack.Store.DB()

			varMap := make(map[string]string)
			for _, v := range vars {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					varMap[parts[0]] = parts[1]
				}
			}
			varMap = addTemplateSemanticVars(varMap, map[string]string{
				"title":   titleVar,
				"summary": summaryVar,
			})

			tmpl, err := apply.LoadTemplate("inbox-capture")
			if err != nil {
				return err
			}
			resolvedParent := parent
			if resolvedParent == "." {
				resolvedParent, err = resolveNodeTitle(".")
				if err != nil {
					return err
				}
			}
			varMap = apply.BindTemplateArgs(tmpl, args, varMap)
			if dryRun {
				return previewTemplateNode(db, resolved, tmpl, resolvedParent, "", varMap)
			}
			if _, err := instantiateTemplateNode(db, resolved, tmpl, resolvedParent, "", varMap); err != nil {
				return err
			}
			return syncRoot(resolved)
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringVarP(&parent, "parent", "p", "", "Parent node title (defaults to template target)")
	cmd.Flags().StringSliceVarP(&vars, "set", "s", nil, "Template variables as key=value")
	cmd.Flags().StringVar(&titleVar, "title", "", "Template title variable")
	cmd.Flags().StringVar(&summaryVar, "summary", "", "Template summary variable")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview template resolution without writing files")
	return cmd
}

func newCmd() *cobra.Command {
	var root string
	var templateName string
	var parent string
	var vars []string // key=value pairs
	var dryRun bool
	var titleVar string
	var summaryVar string
	var headingVar string
	var textVar string

	cmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Create a new node, optionally from a template",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()
			db := stack.Store.DB()

			// Parse template variables from --set flags
			varMap := make(map[string]string)
			for _, v := range vars {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					varMap[parts[0]] = parts[1]
				}
			}
			varMap = addTemplateSemanticVars(varMap, map[string]string{
				"title":   titleVar,
				"summary": summaryVar,
				"heading": headingVar,
				"text":    textVar,
			})

			if templateName != "" {
				// Template mode
				tmpl, err := apply.LoadTemplate(templateName)
				if err != nil {
					return err
				}
				resolvedParent := parent
				if resolvedParent == "." {
					resolvedParent, err = resolveNodeTitle(".")
					if err != nil {
						return err
					}
				}
				varMap = apply.BindTemplateArgs(tmpl, args, varMap)
				if dryRun {
					return previewTemplateNode(db, resolved, tmpl, resolvedParent, "", varMap)
				}
				if _, err := instantiateTemplateNode(db, resolved, tmpl, resolvedParent, "", varMap); err != nil {
					return err
				}
				return syncRoot(resolved)

			} else {
				// Simple mode — create a bare node
				if len(args) == 0 {
					return fmt.Errorf("provide a title or use --template")
				}
				title := args[0]
				content := "# " + title + "\n\n"
				ops := []apply.FileOp{{
					Action:  "create",
					Title:   title,
					Parent:  parent,
					Content: content,
				}}
				created, _, err := apply.ExecuteOps(ops, resolved, db)
				if err != nil {
					return fmt.Errorf("creating node: %w", err)
				}
				for _, f := range created {
					fmt.Fprintf(os.Stderr, "%s Created: %s\n", ui.Success.Render("[new]"), f)
				}

				if projmd.IsGitRepo(resolved) && len(created) > 0 {
					hash, err := projmd.CommitFiles(resolved, fmt.Sprintf("sevens: new node %q", title), created)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s git commit failed: %v\n", ui.Success.Render("[new]"), err)
					} else {
						fmt.Fprintf(os.Stderr, "%s Committed: %s\n", ui.Success.Render("[new]"), hash)
					}
				}

				return syncRoot(resolved)
			}
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringVarP(&templateName, "template", "t", "", "Template name")
	cmd.Flags().StringVarP(&parent, "parent", "p", "", "Parent node title")
	cmd.Flags().StringSliceVarP(&vars, "set", "s", nil, "Template variables as key=value")
	cmd.Flags().StringVar(&titleVar, "title", "", "Template title variable")
	cmd.Flags().StringVar(&summaryVar, "summary", "", "Template summary variable")
	cmd.Flags().StringVar(&headingVar, "heading", "", "Template heading variable")
	cmd.Flags().StringVar(&textVar, "text", "", "Template text variable")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview template resolution without writing files")
	return cmd
}

func instantiateCmd() *cobra.Command {
	var root string
	var parent string
	var targetNode string
	var vars []string
	var dryRun bool
	var titleVar string
	var summaryVar string
	var headingVar string
	var textVar string

	cmd := &cobra.Command{
		Use:   "instantiate <template> [args...]",
		Short: "Instantiate a manual template",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()
			db := stack.Store.DB()

			templateName := args[0]
			tmpl, err := apply.LoadTemplate(templateName)
			if err != nil {
				return err
			}

			varMap := make(map[string]string)
			for _, v := range vars {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					varMap[parts[0]] = parts[1]
				}
			}
			varMap = addTemplateSemanticVars(varMap, map[string]string{
				"title":   titleVar,
				"summary": summaryVar,
				"heading": headingVar,
				"text":    textVar,
			})
			varMap = apply.BindTemplateArgs(tmpl, args[1:], varMap)

			resolvedTarget := targetNode
			if resolvedTarget == "." {
				resolvedTarget, err = resolveNodeTitle(".")
				if err != nil {
					return err
				}
			}
			resolvedParent := parent
			if resolvedParent == "." {
				resolvedParent, err = resolveNodeTitle(".")
				if err != nil {
					return err
				}
			}
			if dryRun {
				return previewTemplateNode(db, resolved, tmpl, resolvedParent, resolvedTarget, varMap)
			}
			if _, err := instantiateTemplateNode(db, resolved, tmpl, resolvedParent, resolvedTarget, varMap); err != nil {
				return err
			}
			return syncRoot(resolved)
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringVarP(&parent, "parent", "p", "", "Parent node title for create-node templates")
	cmd.Flags().StringVarP(&targetNode, "at", "a", "", "Target node title for append-node and insert-block templates (use '.' for focused node)")
	cmd.Flags().StringSliceVarP(&vars, "set", "s", nil, "Template variables as key=value")
	cmd.Flags().StringVar(&titleVar, "title", "", "Template title variable")
	cmd.Flags().StringVar(&summaryVar, "summary", "", "Template summary variable")
	cmd.Flags().StringVar(&headingVar, "heading", "", "Template heading variable")
	cmd.Flags().StringVar(&textVar, "text", "", "Template text variable")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview template resolution without writing files")
	return cmd
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage sevens configuration",
	}

	generateCmd := &cobra.Command{
		Use:       "generate <backend>",
		Short:     "Generate MCP configs for a CLI backend from capabilities.edn",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"codex", "claude", "all"},
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			caps, err := backend.LoadCapabilities()
			if err != nil {
				return fmt.Errorf("loading capabilities: %w", err)
			}

			if len(caps.MCPServers) == 0 {
				return fmt.Errorf("no MCP servers defined in capabilities.edn")
			}

			configDir, err := store.ConfigDir()
			if err != nil {
				return fmt.Errorf("config dir: %w", err)
			}
			generatedBase := filepath.Join(configDir, "generated")

			switch target {
			case "codex":
				return backend.GenerateCodexConfig(caps, filepath.Join(generatedBase, "codex"))
			case "claude":
				return backend.GenerateClaudeConfig(caps, filepath.Join(generatedBase, "claude"))
			case "all":
				if err := backend.GenerateCodexConfig(caps, filepath.Join(generatedBase, "codex")); err != nil {
					return err
				}
				return backend.GenerateClaudeConfig(caps, filepath.Join(generatedBase, "claude"))
			default:
				return fmt.Errorf("unknown backend %q (use: codex, claude, all)", target)
			}
		},
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			globalConfig, err := apply.LoadGlobalConfig()
			if err != nil {
				return err
			}

			fmt.Printf("Backend: %s\n", orDefault(globalConfig.Backend, "anthropic"))
			fmt.Printf("Model:   %s\n", globalConfig.LLM.Model)

			caps, err := backend.LoadCapabilities()
			if err != nil {
				fmt.Printf("Capabilities: (error: %v)\n", err)
			} else if len(caps.MCPServers) > 0 {
				fmt.Printf("MCP Servers (%d):\n", len(caps.MCPServers))
				for kw, srv := range caps.MCPServers {
					name := string(kw)
					if len(name) > 0 && name[0] == ':' {
						name = name[1:]
					}
					fmt.Printf("  %s — %s\n", name, srv.Description)
				}
			}

			if len(globalConfig.Backends) > 0 {
				fmt.Printf("Backends:\n")
				for name, cfg := range globalConfig.Backends {
					fmt.Printf("  %s (type: %s)\n", name, orDefault(cfg.Type, name))
				}
			}

			return nil
		},
	}

	cmd.AddCommand(generateCmd, showCmd)
	return cmd
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func summarizeInline(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "sevens",
		Short: "Context server for AI agents over a tree-structured knowledge graph",
	}
	rootCmd.SilenceUsage = true

	rootCmd.AddCommand(
		initCmd(),
		syncCmd(),
		overviewCmd(),
		walkCmd(),
		treeCmd(),
		blocksCmd(),
		diffBlocksCmd(),
		inboxCmd(),
		extractBlockCmd(),
		rootsCmd(),
		applyCmd2(),
		discussCmd(),
		acceptCmd2(),
		rejectCmd2(),
		revertCmd(),
		pendingCmd2(),
		functionsCmd(),
		templatesCmd(),
		defineCmd(),
		focusCmd(),
		unfocusCmd(),
		statusCmd(),
		logCmd(),
		queryCmd(),
		searchCmd(),
		prepareCmd(),
		submitCmd(),
		newCmd(),
		instantiateCmd(),
		captureCmd(),
		configCmd(),
		replCmd(),
	)

	rootCmd.SetUsageFunc(func(cmd *cobra.Command) error {
		// Header
		fmt.Fprintf(os.Stderr, "%s %s\n\n", ui.Header.Render(cmd.Name()), ui.Dim.Render(cmd.Short))

		if cmd.Long != "" {
			fmt.Fprintf(os.Stderr, "%s\n\n", cmd.Long)
		}

		// Usage
		if cmd.HasAvailableSubCommands() {
			fmt.Fprintf(os.Stderr, "%s\n", ui.Label.Render("Usage:"))
			fmt.Fprintf(os.Stderr, "  %s [command]\n\n", cmd.CommandPath())
		} else if cmd.Use != "" {
			fmt.Fprintf(os.Stderr, "%s\n", ui.Label.Render("Usage:"))
			fmt.Fprintf(os.Stderr, "  %s\n\n", cmd.UseLine())
		}

		// Available commands (grouped)
		if cmd.HasAvailableSubCommands() {
			fmt.Fprintf(os.Stderr, "%s\n", ui.Label.Render("Commands:"))

			// Group commands by category
			graph := []string{"init", "sync", "overview", "walk", "tree", "blocks", "diff-blocks", "inbox", "extract-block", "roots", "search", "query"}
			functions := []string{"apply", "accept", "reject", "pending", "functions", "templates", "define", "prepare", "submit"}
			session := []string{"focus", "unfocus", "status", "log"}
			structure := []string{"new", "capture", "revert"}

			groups := []struct {
				name string
				cmds []string
			}{
				{"Graph", graph},
				{"Functions", functions},
				{"Session", session},
				{"Structure", structure},
			}

			allCmds := make(map[string]*cobra.Command)
			for _, sub := range cmd.Commands() {
				if !sub.IsAvailableCommand() {
					continue
				}
				allCmds[sub.Name()] = sub
			}

			for _, g := range groups {
				fmt.Fprintf(os.Stderr, "\n  %s\n", ui.Dim.Render(g.name))
				for _, name := range g.cmds {
					if sub, ok := allCmds[name]; ok {
						fmt.Fprintf(os.Stderr, "    %s  %s\n",
							ui.Label.Render(fmt.Sprintf("%-12s", sub.Name())),
							ui.Dim.Render(sub.Short))
						delete(allCmds, name)
					}
				}
			}

			// Any remaining commands not in a group
			if len(allCmds) > 0 {
				fmt.Fprintf(os.Stderr, "\n  %s\n", ui.Dim.Render("Other"))
				for _, sub := range cmd.Commands() {
					if _, ok := allCmds[sub.Name()]; ok {
						fmt.Fprintf(os.Stderr, "    %s  %s\n",
							ui.Label.Render(fmt.Sprintf("%-12s", sub.Name())),
							ui.Dim.Render(sub.Short))
					}
				}
			}
			fmt.Fprintln(os.Stderr)
		}

		// Flags
		if cmd.HasAvailableLocalFlags() {
			fmt.Fprintf(os.Stderr, "%s\n", ui.Label.Render("Flags:"))
			fmt.Fprintf(os.Stderr, "%s\n", cmd.LocalFlags().FlagUsages())
		}

		// Subcommand hint
		if cmd.HasAvailableSubCommands() {
			fmt.Fprintf(os.Stderr, "Use %s for more information about a command.\n",
				ui.Dim.Render(fmt.Sprintf("%s [command] --help", cmd.CommandPath())))
		}

		return nil
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
