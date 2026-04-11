package apply

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// IsGitRepo reports whether root contains a .git entry (directory or file for worktrees).
func IsGitRepo(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil
}

// HasChanges reports whether the working tree has uncommitted changes.
// Returns (false, nil) if git is unavailable or root is not a repository.
func HasChanges(root string) (bool, error) {
	out, err := runGit(root, "status", "--porcelain")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) != "", nil
}

// CommitAll stages all changes and creates a commit with message, returning the short hash.
func CommitAll(root, message string) (string, error) {
	if _, err := runGit(root, "add", "-A"); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	if _, err := runGit(root, "commit", "-m", message); err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	hash, err := runGit(root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(hash), nil
}

// ChangedFiles returns dirty repo paths filtered to sevens-managed note/config files.
func ChangedFiles(root string) ([]string, error) {
	out, err := runGit(root, "status", "--porcelain", "--untracked-files=all", "--")
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}

	seen := make(map[string]bool)
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" || len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}
		if unquoted, err := strconv.Unquote(path); err == nil {
			path = unquoted
		}
		if path == ".sevens.edn" || strings.HasSuffix(path, ".md") {
			if !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
		}
	}
	return files, nil
}

// CommitFiles stages only the specified files and creates a commit with message,
// returning the short hash. Returns an error if files is empty.
func CommitFiles(root, message string, files []string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("no files to commit")
	}
	args := append([]string{"add", "--"}, files...)
	if _, err := runGit(root, args...); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	if _, err := runGit(root, "commit", "-m", message); err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	out, err := runGit(root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// RevertCommit runs git revert on a commit hash and returns the new commit's short hash.
// If there are uncommitted changes, they are stashed and re-applied after the revert.
func RevertCommit(root, hash string) (string, error) {
	dirty, _ := HasChanges(root)
	if dirty {
		if _, err := runGit(root, "stash", "push", "-m", "sevens-revert-autostash"); err != nil {
			return "", fmt.Errorf("git stash: %w", err)
		}
	}
	_, revertErr := runGit(root, "revert", "--no-edit", hash)
	if dirty {
		runGit(root, "stash", "pop") // best-effort restore
	}
	if revertErr != nil {
		return "", fmt.Errorf("git revert: %w", revertErr)
	}
	newHash, err := runGit(root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(newHash), nil
}

func runGit(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
