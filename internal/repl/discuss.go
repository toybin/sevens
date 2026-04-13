package repl

import (
	"fmt"
	"os"
	"strings"

	"sevens/internal/ui"
)

// enterDiscussion starts or continues a discussion.
// If nonInteractive is true, or the discussion is threaded, it runs once and returns.
func (r *REPL) enterDiscussion(nonInteractive bool) error {
	focus, err := r.requireFocus()
	if err != nil {
		return err
	}

	if r.discussR == nil {
		return fmt.Errorf("discussion runner not available")
	}

	// Check if threaded — force non-interactive.
	discussTitle := "Discussion - " + focus
	if r.graphQ != nil {
		if subj, _ := r.graphQ.ResolveNode(discussTitle, r.root); subj != "" {
			if fp, _ := r.graphQ.GetObject(subj, "node/file-path"); fp != "" {
				if r.discussR.IsThreaded(fp) {
					if !nonInteractive {
						r.printSystem("discussion is threaded — running non-interactively")
						r.printSystem("edit the discussion file directly to add [user] messages to specific threads")
					}
					nonInteractive = true
				}
			}
		}
	}

	r.printSystem("starting discussion...")
	state, agentOutput, err := r.discussR.StartDiscussion(r.root, focus)
	if err != nil {
		return err
	}

	// Display agent output.
	if agentOutput != "" {
		fmt.Println()
		fmt.Println(agentOutput)
		fmt.Println()
	}

	if nonInteractive {
		return nil
	}

	// Enter interactive discussion mode.
	r.mode = ModeDiscussion
	r.discussState = state
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

	// Dot commands pass through to normal dispatch.
	if strings.HasPrefix(line, ".") {
		tokens := tokenize(line)
		return r.handleDot(tokens)
	}

	// Named commands pass through to dispatch.
	firstWord := line
	if idx := strings.IndexByte(line, ' '); idx > 0 {
		firstWord = line[:idx]
	}
	for _, cmd := range namedCommands {
		if firstWord == cmd {
			return r.dispatch(line)
		}
	}
	if isFunctionName(firstWord) {
		return r.dispatch(line)
	}

	// User message — continue discussion.
	if r.discussR == nil || r.discussState == nil {
		return fmt.Errorf("discussion not active")
	}

	r.printSystem("thinking...")
	agentOutput, err := r.discussR.ContinueDiscussion(r.root, r.discussState, line)
	if err != nil {
		return err
	}

	if agentOutput != "" {
		fmt.Println()
		fmt.Println(agentOutput)
		fmt.Println()
	}
	return nil
}

func (r *REPL) endDiscussion() error {
	if r.discussR != nil && r.discussState != nil {
		hash, err := r.discussR.EndDiscussion(r.root, r.discussState)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %v\n", ui.Warning.Render("[warn]"), err)
		} else if hash != "" {
			fmt.Fprintf(os.Stderr, "%s saved (%s)\n", ui.Success.Render("[discuss]"), hash)
		}
	}

	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}

	r.mode = ModeNormal
	r.discussState = nil
	r.updatePrompt()
	return nil
}

func (r *REPL) cancelDiscussion() error {
	if r.discussR != nil && r.discussState != nil {
		if err := r.discussR.CancelDiscussion(r.root, r.discussState); err != nil {
			fmt.Fprintf(os.Stderr, "%s %v\n", ui.Warning.Render("[warn]"), err)
		}
	}

	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}

	r.printSystem("discussion discarded")
	r.mode = ModeNormal
	r.discussState = nil
	r.updatePrompt()
	return nil
}

// showDiscussionTurns reads the discussion file and prints the last agent turns.
func (r *REPL) showDiscussionTurns(discussTitle string) {
	if r.graphQ == nil {
		return
	}
	subj, _ := r.graphQ.ResolveNode(discussTitle, r.root)
	if subj == "" {
		return
	}
	filePath, _ := r.graphQ.GetObject(subj, "node/file-path")
	if filePath == "" {
		return
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	content := string(data)
	lines := strings.Split(content, "\n")

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
