package workflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sevens/internal/kb"
	projmd "sevens/internal/projection/md"
)

// DiscussionState tracks the state of a multi-turn discussion.
type DiscussionState struct {
	DiscussTitle  string // "Discussion - <FocusTitle>"
	FilePath      string // absolute path to discussion .md file
	InitialCommit string // commit hash from the initial draft commit
	FileCreated   bool   // true if the file was newly created (vs. continuing)
	FocusTitle    string // the original node being discussed
}

// StartDiscussion begins a discussion on a node. It runs the "discuss" function,
// auto-accepts the ops to create/update the discussion file, and commits a draft.
// Returns the discussion state and the agent's initial output text.
func StartDiscussion(ctx context.Context, d *Deps, root, nodeTitle string) (*DiscussionState, string, error) {
	discussTitle := "Discussion - " + nodeTitle

	// Check if discussion file already exists before we create it.
	existingPath := resolveFilePath(ctx, d, root, discussTitle)
	fileExistedBefore := existingPath != ""

	// Check if discussion is threaded (multiple # headings).
	threaded := existingPath != "" && isThreaded(existingPath)

	// Run the discuss function.
	result, err := ApplyFunction(ctx, d, root, "discuss", nodeTitle)
	if err != nil {
		return nil, "", fmt.Errorf("starting discussion: %w", err)
	}

	// If suspended with ops, auto-accept to materialize the discussion file.
	if result.Suspended && len(result.Ops) > 0 {
		_, err := AcceptPipeline(ctx, d, root, result.PipelineID, "")
		if err != nil {
			return nil, "", fmt.Errorf("accepting discussion ops: %w", err)
		}
	}

	// Resync to pick up the new file.
	_, _ = d.Proj.Sync(ctx, root)

	// Resolve file path (may have just been created).
	filePath := resolveFilePath(ctx, d, root, discussTitle)

	// Extract agent output.
	agentOutput := extractLastAgentBlock(filePath)

	state := &DiscussionState{
		DiscussTitle: discussTitle,
		FilePath:     filePath,
		FileCreated:  !fileExistedBefore,
		FocusTitle:   nodeTitle,
	}

	// Commit the initial draft for revert support, unless threaded.
	if !threaded && projmd.IsGitRepo(root) && filePath != "" {
		h, err := projmd.CommitFiles(root,
			fmt.Sprintf("sevens: discussion on %q (draft)", nodeTitle), []string{filePath})
		if err == nil {
			state.InitialCommit = h
		}
	}

	return state, agentOutput, nil
}

// ContinueDiscussion appends a user turn and runs the discuss function again.
// Returns the agent's response text.
func ContinueDiscussion(ctx context.Context, d *Deps, root string, state *DiscussionState, userInput string) (string, error) {
	if state.FilePath == "" {
		return "", fmt.Errorf("no discussion file path")
	}

	// Append user turn with timestamp.
	data, err := os.ReadFile(state.FilePath)
	if err != nil {
		return "", fmt.Errorf("reading discussion file: %w", err)
	}
	timestamp := time.Now().Format("2006-01-02 15:04")
	content := strings.TrimRight(string(data), "\n")
	content += fmt.Sprintf("\n\n**[user %s]** %s\n", timestamp, userInput)
	if err := os.WriteFile(state.FilePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing discussion file: %w", err)
	}

	// Resync to pick up the user's addition.
	if _, err := d.Proj.Sync(ctx, root); err != nil {
		return "", fmt.Errorf("re-sync: %w", err)
	}

	// Run discuss again.
	result, err := ApplyFunction(ctx, d, root, "discuss", state.FocusTitle)
	if err != nil {
		return "", fmt.Errorf("continuing discussion: %w", err)
	}

	// Auto-accept ops.
	if result.Suspended && len(result.Ops) > 0 {
		_, err := AcceptPipeline(ctx, d, root, result.PipelineID, "")
		if err != nil {
			return "", fmt.Errorf("accepting discussion ops: %w", err)
		}
	}

	// Resync.
	_, _ = d.Proj.Sync(ctx, root)

	return extractLastAgentBlock(state.FilePath), nil
}

// EndDiscussion commits the final discussion state.
func EndDiscussion(ctx context.Context, d *Deps, root string, state *DiscussionState) (string, error) {
	var commitHash string
	if projmd.IsGitRepo(root) && state.FilePath != "" {
		h, err := projmd.CommitFiles(root,
			fmt.Sprintf("sevens: discussion on %q", state.FocusTitle), []string{state.FilePath})
		if err == nil {
			commitHash = h
		}
	}

	if _, err := d.Proj.Sync(ctx, root); err != nil {
		return "", fmt.Errorf("re-sync: %w", err)
	}

	return commitHash, nil
}

// CancelDiscussion reverts the discussion to its pre-discussion state.
func CancelDiscussion(ctx context.Context, d *Deps, root string, state *DiscussionState) error {
	if state.InitialCommit != "" && projmd.IsGitRepo(root) {
		if err := projmd.RevertCommit(root, state.InitialCommit); err != nil {
			return fmt.Errorf("reverting draft commit: %w", err)
		}
	} else if state.FilePath != "" {
		if state.FileCreated {
			// Newly created: delete the file.
			_ = os.Remove(state.FilePath)
			if projmd.IsGitRepo(root) {
				relPath, err := filepath.Rel(root, state.FilePath)
				if err == nil {
					exec.Command("git", "-C", root, "rm", "--cached", "--force", relPath).Run()
				}
			}
		} else {
			// Pre-existing: restore from git.
			if projmd.IsGitRepo(root) {
				relPath, err := filepath.Rel(root, state.FilePath)
				if err == nil {
					exec.Command("git", "-C", root, "checkout", "--", relPath).Run()
				}
			}
		}
	}

	_, _ = d.Proj.Sync(ctx, root)
	return nil
}

// IsThreaded returns true if a discussion file has multiple top-level headings.
func IsThreaded(filePath string) bool {
	return isThreaded(filePath)
}

// --- Internal helpers ---

func resolveFilePath(ctx context.Context, d *Deps, root, title string) string {
	subject := d.KB.Resolve(ctx, root, title)
	if subject == "" {
		return ""
	}
	path, ok, _ := d.KB.Graph().Lookup(ctx, subject, kb.PredNodeFile)
	if !ok {
		return ""
	}
	return path
}

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

func extractLastAgentBlock(filePath string) string {
	if filePath == "" {
		return ""
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
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
		return strings.Join(lastAgentBlock, "\n")
	}
	return ""
}
