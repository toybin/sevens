package md

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"sevens/internal/kb"
)

// BlockChange classifies how a block changed between stored graph
// state and the current file on disk.
type BlockChange struct {
	OldPath string
	NewPath string
	Kind    string
	Status  string // "unchanged", "edited", "inserted", "deleted"
	OldText string
	NewText string
	OldScope string
	NewScope string
}

// BlockDiffOutput holds the full diff result for a node's blocks.
type BlockDiffOutput struct {
	NodeTitle string
	FilePath  string
	Unchanged []BlockChange
	Edited    []BlockChange
	Inserted  []BlockChange
	Deleted   []BlockChange
}

// DiffBlocks compares blocks stored in the graph against the current
// file on disk. Uses exact path matching: blocks at the same path are
// compared; unmatched paths are insertions or deletions.
func (m *MarkdownProjection) DiffBlocks(ctx context.Context, root, nodeTitle string) (*BlockDiffOutput, error) {
	// 1. Resolve the node and its file path from the graph.
	nodeSubj := kb.NodeSubject(root, nodeTitle)
	filePath, ok, err := m.kb.Graph().Lookup(ctx, nodeSubj, kb.PredNodeFile)
	if err != nil {
		return nil, fmt.Errorf("looking up file path: %w", err)
	}
	if !ok || filePath == "" {
		return nil, fmt.Errorf("node %q has no file path", nodeTitle)
	}

	// Resolve canonical title
	title, _, _ := m.kb.Graph().Lookup(ctx, nodeSubj, kb.PredNodeTitle)
	if title == "" {
		title = nodeTitle
	}

	// 2. Read stored blocks from KB.
	storedBlocks, err := m.kb.ListBlocks(ctx, root, nodeTitle)
	if err != nil {
		return nil, fmt.Errorf("loading stored blocks: %w", err)
	}

	// 3. Parse current file from disk.
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading current file: %w", err)
	}
	_, body := ParseFrontmatter(string(data))
	currentBlocks := ExtractBlocks(body)

	// 4. Compare by exact path matching.
	oldByPath := make(map[string]kb.BlockEntry, len(storedBlocks))
	for _, b := range storedBlocks {
		oldByPath[b.Path] = b
	}
	newByPath := make(map[string]ParsedBlock, len(currentBlocks))
	for _, b := range currentBlocks {
		newByPath[b.Path] = b
	}

	output := &BlockDiffOutput{
		NodeTitle: title,
		FilePath:  filePath,
	}

	// Check old blocks: matched or deleted
	matchedOld := make(map[string]bool)
	for _, newBlock := range currentBlocks {
		oldBlock, exists := oldByPath[newBlock.Path]
		if !exists {
			scope := ScopeString(VisibleBlockScope(newBlock.Kind, newBlock.Text, newBlock.HeadingChain))
			output.Inserted = append(output.Inserted, BlockChange{
				NewPath:  newBlock.Path,
				Kind:     newBlock.Kind,
				Status:   "inserted",
				NewText:  newBlock.Text,
				NewScope: scope,
			})
			continue
		}
		matchedOld[newBlock.Path] = true

		normalizedOld := strings.ToLower(strings.Join(strings.Fields(oldBlock.Content), " "))
		normalizedNew := strings.ToLower(strings.Join(strings.Fields(newBlock.Text), " "))
		newScope := ScopeString(VisibleBlockScope(newBlock.Kind, newBlock.Text, newBlock.HeadingChain))

		if normalizedOld == normalizedNew {
			output.Unchanged = append(output.Unchanged, BlockChange{
				OldPath:  oldBlock.Path,
				NewPath:  newBlock.Path,
				Kind:     newBlock.Kind,
				Status:   "unchanged",
				OldText:  oldBlock.Content,
				NewText:  newBlock.Text,
				OldScope: oldBlock.Scope,
				NewScope: newScope,
			})
		} else {
			output.Edited = append(output.Edited, BlockChange{
				OldPath:  oldBlock.Path,
				NewPath:  newBlock.Path,
				Kind:     newBlock.Kind,
				Status:   "edited",
				OldText:  oldBlock.Content,
				NewText:  newBlock.Text,
				OldScope: oldBlock.Scope,
				NewScope: newScope,
			})
		}
	}

	// Remaining old blocks not matched = deleted
	for _, oldBlock := range storedBlocks {
		if matchedOld[oldBlock.Path] {
			continue
		}
		output.Deleted = append(output.Deleted, BlockChange{
			OldPath:  oldBlock.Path,
			Kind:     oldBlock.Kind,
			Status:   "deleted",
			OldText:  oldBlock.Content,
			OldScope: oldBlock.Scope,
		})
	}

	// Sort for deterministic output
	sortChanges := func(changes []BlockChange) {
		sort.Slice(changes, func(i, j int) bool {
			pi := changes[i].NewPath
			if pi == "" {
				pi = changes[i].OldPath
			}
			pj := changes[j].NewPath
			if pj == "" {
				pj = changes[j].OldPath
			}
			return pi < pj
		})
	}
	sortChanges(output.Unchanged)
	sortChanges(output.Edited)
	sortChanges(output.Inserted)
	sortChanges(output.Deleted)

	return output, nil
}
