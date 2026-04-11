package graph

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"sevens/internal/store"
)

type BlockDiffEntry struct {
	Subject  string   `edn:"subject,omitempty"`
	Kind     string   `edn:"kind"`
	OldPath  string   `edn:"old-path,omitempty"`
	NewPath  string   `edn:"new-path,omitempty"`
	OldText  string   `edn:"old-text,omitempty"`
	NewText  string   `edn:"new-text,omitempty"`
	OldScope []string `edn:"old-scope,omitempty"`
	NewScope []string `edn:"new-scope,omitempty"`
}

type BlockDiffOutput struct {
	NodeTitle    string           `edn:"node-title"`
	FilePath     string           `edn:"file-path"`
	Unchanged    []BlockDiffEntry `edn:"unchanged"`
	Edited       []BlockDiffEntry `edn:"edited"`
	ScopeChanged []BlockDiffEntry `edn:"scope-changed"`
	Reordered    []BlockDiffEntry `edn:"reordered"`
	Inserted     []BlockDiffEntry `edn:"inserted"`
	Deleted      []BlockDiffEntry `edn:"deleted"`
}

func buildBlockEntry(oldSubject string, oldBlock, newBlock *ParsedBlock, oldPath, newPath string) BlockDiffEntry {
	entry := BlockDiffEntry{
		Subject: oldSubject,
		OldPath: oldPath,
		NewPath: newPath,
	}
	if oldBlock != nil {
		entry.Kind = oldBlock.Kind
		entry.OldText = oldBlock.Text
		entry.OldScope = visibleBlockScope(oldBlock.Kind, oldBlock.Text, oldBlock.HeadingChain)
	}
	if newBlock != nil {
		if entry.Kind == "" {
			entry.Kind = newBlock.Kind
		}
		entry.NewText = newBlock.Text
		entry.NewScope = visibleBlockScope(newBlock.Kind, newBlock.Text, newBlock.HeadingChain)
	}
	return entry
}

func BuildBlockDiff(db *sql.DB, root, nodeTitle string) (BlockDiffOutput, error) {
	nodeSubject, canonical := store.ResolveNode(db, nodeTitle, root)
	if nodeSubject == "" {
		return BlockDiffOutput{}, fmt.Errorf("node not found: %s", nodeTitle)
	}

	filePath, err := store.GetObject(db, nodeSubject, "node/file-path")
	if err != nil {
		return BlockDiffOutput{}, fmt.Errorf("loading file path: %w", err)
	}
	if filePath == "" {
		return BlockDiffOutput{}, fmt.Errorf("node %q has no file path", canonical)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return BlockDiffOutput{}, fmt.Errorf("reading current file: %w", err)
	}
	currentNode, err := parseFile(filePath, data)
	if err != nil {
		return BlockDiffOutput{}, fmt.Errorf("parsing current file: %w", err)
	}
	if currentNode == nil {
		return BlockDiffOutput{}, fmt.Errorf("current file has no titled node: %s", filePath)
	}

	storedBlocks, err := loadStoredBlocks(db, root, ParsedNode{Title: canonical})
	if err != nil {
		return BlockDiffOutput{}, err
	}
	oldBlocks := make([]ParsedBlock, 0, len(storedBlocks))
	oldSubjects := make(map[string]string, len(storedBlocks))
	for _, stored := range storedBlocks {
		oldBlocks = append(oldBlocks, stored.Block)
		oldSubjects[stored.Block.Path] = stored.Subject
	}

	diff := DiffParsedBlocks(oldBlocks, currentNode.Blocks)
	oldByPath := make(map[string]ParsedBlock, len(oldBlocks))
	newByPath := make(map[string]ParsedBlock, len(currentNode.Blocks))
	for _, block := range oldBlocks {
		oldByPath[block.Path] = block
	}
	for _, block := range currentNode.Blocks {
		newByPath[block.Path] = block
	}

	output := BlockDiffOutput{
		NodeTitle: canonical,
		FilePath:  filePath,
	}

	for _, change := range diff.Unchanged {
		oldBlock := oldByPath[change.OldPath]
		newBlock := newByPath[change.NewPath]
		output.Unchanged = append(output.Unchanged, buildBlockEntry(oldSubjects[change.OldPath], &oldBlock, &newBlock, change.OldPath, change.NewPath))
	}
	for _, change := range diff.Edited {
		oldBlock := oldByPath[change.OldPath]
		newBlock := newByPath[change.NewPath]
		output.Edited = append(output.Edited, buildBlockEntry(oldSubjects[change.OldPath], &oldBlock, &newBlock, change.OldPath, change.NewPath))
	}
	for _, change := range diff.ScopeChanged {
		oldBlock := oldByPath[change.OldPath]
		newBlock := newByPath[change.NewPath]
		output.ScopeChanged = append(output.ScopeChanged, buildBlockEntry(oldSubjects[change.OldPath], &oldBlock, &newBlock, change.OldPath, change.NewPath))
	}
	for _, change := range diff.Reordered {
		oldBlock := oldByPath[change.OldPath]
		newBlock := newByPath[change.NewPath]
		output.Reordered = append(output.Reordered, buildBlockEntry(oldSubjects[change.OldPath], &oldBlock, &newBlock, change.OldPath, change.NewPath))
	}
	for _, path := range diff.Inserted {
		newBlock := newByPath[path]
		output.Inserted = append(output.Inserted, buildBlockEntry("", nil, &newBlock, "", path))
	}
	for _, path := range diff.Deleted {
		oldBlock := oldByPath[path]
		output.Deleted = append(output.Deleted, buildBlockEntry(oldSubjects[path], &oldBlock, nil, path, ""))
	}

	return output, nil
}

func ScopeString(scope []string) string {
	if len(scope) == 0 {
		return ""
	}
	return strings.Join(scope, " > ")
}
