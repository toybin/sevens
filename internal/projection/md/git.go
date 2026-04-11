package md

import (
	"os/exec"
	"strings"
)

// IsGitRepo checks if root is a git repository.
func IsGitRepo(root string) bool {
	err := runGit(root, "rev-parse", "--git-dir")
	return err == nil
}

// HasChanges returns true if there are uncommitted changes.
func HasChanges(root string) (bool, error) {
	out, err := runGitOutput(root, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// CommitAll stages and commits all changes.
func CommitAll(root, message string) (string, error) {
	if err := runGit(root, "add", "-A"); err != nil {
		return "", err
	}
	return commitAndHash(root, message)
}

// CommitFiles stages and commits specific files.
func CommitFiles(root, message string, files []string) (string, error) {
	args := append([]string{"add", "--"}, files...)
	if err := runGit(root, args...); err != nil {
		return "", err
	}
	return commitAndHash(root, message)
}

// RevertCommit reverts a specific commit.
func RevertCommit(root, hash string) error {
	// Stash any dirty state
	_ = runGit(root, "stash")
	err := runGit(root, "revert", "--no-edit", hash)
	// Pop stash regardless
	_ = runGit(root, "stash", "pop")
	return err
}

// ChangedFiles returns files changed since last commit,
// filtered to .md and .sevens.edn.
func ChangedFiles(root string) ([]string, error) {
	out, err := runGitOutput(root, "diff", "--name-only", "HEAD")
	if err != nil {
		// May fail if no commits yet; try status instead
		out, err = runGitOutput(root, "status", "--porcelain", "--short")
		if err != nil {
			return nil, err
		}
	}
	var result []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// status --porcelain prefixes with status chars
		if len(line) > 3 && (line[0] == '?' || line[0] == 'M' || line[0] == 'A') {
			line = strings.TrimSpace(line[2:])
		}
		if strings.HasSuffix(line, ".md") || strings.HasSuffix(line, ".sevens.edn") {
			result = append(result, line)
		}
	}
	return result, nil
}

func commitAndHash(root, message string) (string, error) {
	if err := runGit(root, "commit", "-m", message); err != nil {
		return "", err
	}
	hash, err := runGitOutput(root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}

func runGit(root string, args ...string) error {
	_, err := runGitOutput(root, args...)
	return err
}

func runGitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
