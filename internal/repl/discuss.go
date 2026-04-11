package repl

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sevens/internal/apply"
	"sevens/internal/store"
	"sevens/internal/ui"
)

// isThreaded checks if a discussion file has multiple # headings,
// indicating it has branched into threads.
func isThreaded(filePath string) bool {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}
	headingCount := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "# ") {
			headingCount++
		}
	}
	return headingCount > 1
}

func resolveDiscussionFilePath(db *sql.DB, root, discussTitle string) (string, error) {
	subject, _ := store.ResolveNode(db, discussTitle, root)
	if subject == "" {
		return "", nil
	}
	return store.GetObject(db, subject, "node/file-path")
}

// enterDiscussion starts or continues a discussion.
// If nonInteractive is true, or the discussion is threaded, it runs the discuss
// function once and returns (no interactive [you]> loop).
func (r *REPL) enterDiscussion(nonInteractive bool) error {
	focus, err := r.requireFocus()
	if err != nil {
		return err
	}

	fn, err := apply.LoadFunction("discuss")
	if err != nil {
		return fmt.Errorf("loading discuss function: %w", err)
	}

	discussTitle := "Discussion - " + focus

	// Check if the discussion file already exists before we create/modify it.
	existingPath, _ := resolveDiscussionFilePath(r.db, r.root, discussTitle)
	fileExistedBefore := existingPath != ""

	// Check if the discussion is threaded — if so, force non-interactive.
	if existingPath != "" && isThreaded(existingPath) {
		if !nonInteractive {
			r.printSystem("discussion is threaded — running non-interactively")
			r.printSystem("edit the discussion file directly to add [user] messages to specific threads")
		}
		nonInteractive = true
	}

	// Run the discuss function.
	r.printSystem("starting discussion...")
	if err := r.runPipeline(focus, fn, 0, "", false, pipelineOpts{
		skipAutoAccept: true,
		suppressHint:   true,
	}); err != nil {
		return err
	}

	// Accept the pending ops directly.
	if err := r.doAccept(focus, ""); err != nil {
		r.printSystem("note: %v", err)
	}

	r.resyncQuiet()

	// Resolve the file path now (it may have just been created by the pipeline).
	resolvedPath, _ := resolveDiscussionFilePath(r.db, r.root, discussTitle)

	// Show the latest agent turns.
	r.showDiscussionTurns(discussTitle)

	if nonInteractive {
		return nil
	}

	// Enter interactive discussion mode.
	// Commit the initial discussion file so we have a clean base to revert to.
	var initialCommit string
	if apply.IsGitRepo(r.root) && resolvedPath != "" {
		h, cerr := apply.CommitFiles(r.root,
			fmt.Sprintf("sevens: discussion on %q (draft)", focus), []string{resolvedPath})
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "%s git commit: %v\n", ui.Warning.Render("[warn]"), cerr)
		} else {
			initialCommit = h
		}
	}

	r.mode = ModeDiscussion
	r.discussNode = discussTitle
	r.discussFileCreated = !fileExistedBefore
	r.discussFilePath = resolvedPath
	r.discussCommit = initialCommit
	r.rl.SetPrompt(modeStyle.Render("[you]>") + " ")
	r.printSystem(".end to save, .cancel to discard")
	return nil
}

// handleDiscussionInput processes a line of input while in discussion mode.
func (r *REPL) handleDiscussionInput(line string) error {
	switch line {
	case ".end", ".done":
		return r.endDiscussion()
	case ".cancel":
		return r.cancelDiscussion()
	case ".info":
		r.printInfo()
		return nil
	}

	// All dot commands pass through to normal dispatch.
	if strings.HasPrefix(line, ".") {
		tokens := tokenize(line)
		return r.handleDot(tokens)
	}

	// Named commands that aren't user text pass through to dispatch.
	firstWord := line
	if idx := strings.IndexByte(line, ' '); idx > 0 {
		firstWord = line[:idx]
	}
	for _, cmd := range namedCommands {
		if firstWord == cmd {
			return r.dispatch(line)
		}
	}
	// Function names also pass through.
	if isFunctionName(firstWord) {
		return r.dispatch(line)
	}

	// User message — append to discussion file with timestamp, re-run discuss.
	focus := r.focus
	discussTitle := r.discussNode

	filePath, err := resolveDiscussionFilePath(r.db, r.root, discussTitle)
	if err != nil || filePath == "" {
		return fmt.Errorf("could not find discussion file for %q", discussTitle)
	}

	// Append user turn with local timestamp.
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading discussion file: %w", err)
	}
	timestamp := time.Now().Format("2006-01-02 15:04")
	content := strings.TrimRight(string(data), "\n")
	content += fmt.Sprintf("\n\n**[user %s]** %s\n", timestamp, line)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing discussion file: %w", err)
	}

	if err := r.resync(); err != nil {
		return fmt.Errorf("re-sync: %w", err)
	}

	// Run discuss again.
	fn, err := apply.LoadFunction("discuss")
	if err != nil {
		return fmt.Errorf("loading discuss: %w", err)
	}

	r.printSystem("thinking...")
	if err := r.runPipeline(focus, fn, 0, "", false, pipelineOpts{skipAutoAccept: true}); err != nil {
		return err
	}

	if err := r.doAccept(focus, ""); err != nil {
		return fmt.Errorf("applying agent response: %w", err)
	}

	r.resyncQuiet()
	r.showDiscussionTurns(discussTitle)
	return nil
}

func (r *REPL) endDiscussion() error {
	if apply.IsGitRepo(r.root) && r.discussFilePath != "" {
		h, err := apply.CommitFiles(r.root,
			fmt.Sprintf("sevens: discussion on %q", r.focus), []string{r.discussFilePath})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s git commit: %v\n", ui.Warning.Render("[warn]"), err)
		} else {
			fmt.Fprintf(os.Stderr, "%s saved (%s)\n", ui.Success.Render("[discuss]"), h)
		}
	}

	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}

	r.mode = ModeNormal
	r.discussNode = ""
	r.updatePrompt()
	return nil
}

func (r *REPL) cancelDiscussion() error {
	// Revert the draft commit if one was made during enterDiscussion.
	if r.discussCommit != "" && apply.IsGitRepo(r.root) {
		if _, err := apply.RevertCommit(r.root, r.discussCommit); err != nil {
			fmt.Fprintf(os.Stderr, "%s could not revert draft commit %s: %v\n",
				ui.Warning.Render("[warn]"), r.discussCommit, err)
		}
	} else if r.discussFilePath != "" {
		// No commit was recorded — handle the file directly.
		if r.discussFileCreated {
			// File was newly created: just delete it.
			if err := os.Remove(r.discussFilePath); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "%s could not remove discussion file: %v\n",
					ui.Warning.Render("[warn]"), err)
			}
			// Remove from git index if in a git repo.
			if apply.IsGitRepo(r.root) {
				relPath, err := filepath.Rel(r.root, r.discussFilePath)
				if err == nil {
					exec.Command("git", "-C", r.root, "rm", "--cached", "--force", relPath).Run()
				}
			}
		} else {
			// File existed before: restore it from git if possible.
			if apply.IsGitRepo(r.root) {
				relPath, err := filepath.Rel(r.root, r.discussFilePath)
				if err == nil {
					if out, err := exec.Command("git", "-C", r.root, "checkout", "--", relPath).CombinedOutput(); err != nil {
						fmt.Fprintf(os.Stderr, "%s could not restore discussion file: %s\n",
							ui.Warning.Render("[warn]"), strings.TrimSpace(string(out)))
					}
				} else {
					fmt.Fprintf(os.Stderr, "%s could not compute relative path for restore: %v\n",
						ui.Warning.Render("[warn]"), err)
				}
			} else {
				fmt.Fprintf(os.Stderr, "%s not a git repo — cannot restore existing discussion file\n",
					ui.Warning.Render("[warn]"))
			}
		}
	}

	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}

	r.printSystem("discussion discarded")
	r.mode = ModeNormal
	r.discussNode = ""
	r.discussFileCreated = false
	r.discussFilePath = ""
	r.discussCommit = ""
	r.updatePrompt()
	return nil
}

// showDiscussionTurns reads the discussion file and prints the last agent turns.
func (r *REPL) showDiscussionTurns(discussTitle string) {
	filePath, err := resolveDiscussionFilePath(r.db, r.root, discussTitle)
	if err != nil || filePath == "" {
		return
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// Find the last contiguous block of [agent ...] turns (after the last [user ...] turn).
	var lastAgentBlock []string
	for _, l := range lines {
		if strings.HasPrefix(l, "**[agent") {
			lastAgentBlock = append(lastAgentBlock, l)
		} else if strings.HasPrefix(l, "**[user") {
			lastAgentBlock = nil
		} else if len(lastAgentBlock) > 0 {
			lastAgentBlock = append(lastAgentBlock, l)
		}
	}

	if len(lastAgentBlock) > 0 {
		fmt.Println()
		for _, l := range lastAgentBlock {
			fmt.Println(l)
		}
		fmt.Println()
	}
}
