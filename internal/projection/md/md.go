package md

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"olympos.io/encoding/edn"
	"sevens/internal/kb"
	"sevens/internal/projection"
	"sevens/internal/triple"
	"sevens/internal/types"
)

// MarkdownProjection implements projection.Projection for markdown files.
type MarkdownProjection struct {
	kb *kb.KB
}

// New creates a markdown projection.
func New(k *kb.KB) *MarkdownProjection {
	return &MarkdownProjection{kb: k}
}

// Sync reads all markdown files in root, parses them, and writes
// the resulting triples to the knowledge base.
func (m *MarkdownProjection) Sync(ctx context.Context, root string) (*projection.SyncResult, error) {
	files, err := ScanFiles(root)
	if err != nil {
		return nil, fmt.Errorf("md: scan: %w", err)
	}

	nodes, errs := ParseFiles(files)

	// Clear existing triples for this root and rewrite.
	// This is the current sync model: full clear + reinsert.
	if err := m.kb.ClearRoot(ctx, root); err != nil {
		return nil, fmt.Errorf("md: clear root: %w", err)
	}

	// Build signifier→predicate reverse map from type definitions so that
	// orthography slots like "@julian" emit meta/assignee instead of meta/@.
	var sigMap map[string]string
	if allTypes, err := types.LoadAllTypeDefs(); err == nil {
		sigMap = types.BuildSignifierMap(allTypes)
	}

	tripleCount := 0
	for _, node := range nodes {
		triples := nodeToTriples(node, root, sigMap)
		if err := m.kb.Graph().Store().AssertBatch(ctx, triples); err != nil {
			return nil, fmt.Errorf("md: assert triples for %q: %w", node.Title, err)
		}
		tripleCount += len(triples)
	}

	return &projection.SyncResult{
		NodesScanned:   len(nodes),
		TriplesWritten: tripleCount,
		Errors:         errs,
	}, nil
}

// Write renders a single node from graph state to a markdown file.
func (m *MarkdownProjection) Write(ctx context.Context, root, nodeTitle string) error {
	w, err := m.kb.Walk(ctx, root, nodeTitle, kb.GatherNeighborhood)
	if err != nil {
		return err
	}

	fm := Frontmatter{Title: w.Target.Title}
	if w.Parent != nil {
		fm.Parent = w.Parent.Title
	}
	if w.Target.Role != "" {
		fm.SiblingRole = w.Target.Role
	}

	// Read meta/* predicates into Extra frontmatter fields.
	subject := kb.NodeSubject(root, nodeTitle)
	allTriples, _ := m.kb.Graph().Store().BySubject(ctx, subject)
	for _, t := range allTriples {
		if strings.HasPrefix(t.Predicate, "meta/") {
			key := strings.TrimPrefix(t.Predicate, "meta/")
			if fm.Extra == nil {
				fm.Extra = make(map[string]string)
			}
			fm.Extra[key] = t.Object
		}
	}

	content := RenderNode(fm, w.Target.Content)
	filePath := filepath.Join(root, SanitizeFilename(w.Target.Title))
	return os.WriteFile(filePath, []byte(content), 0644)
}

// WriteAll renders all nodes in a root.
func (m *MarkdownProjection) WriteAll(ctx context.Context, root string) error {
	nodes, err := m.kb.Overview(ctx, root)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := m.Write(ctx, root, n.Title); err != nil {
			return err
		}
	}
	return nil
}

// ApplyOps executes file operations against the filesystem.
// It pre-validates all edit ops before applying anything, to avoid
// partial state on failure.
func (m *MarkdownProjection) ApplyOps(ctx context.Context, root string, ops []projection.FileOp) (*projection.ApplyResult, error) {
	// Pre-validate all edit ops before applying anything.
	for _, op := range ops {
		if op.Action == "edit" {
			if err := validateEditOp(root, op, m.kb); err != nil {
				return nil, fmt.Errorf("pre-validate op (edit %q): %w", op.File, err)
			}
		}
	}

	// All validated — apply.
	result := &projection.ApplyResult{}
	for _, op := range ops {
		switch op.Action {
		case "create":
			path, err := createFile(root, op)
			if err != nil {
				return result, err
			}
			result.FilesCreated = append(result.FilesCreated, path)
		case "edit":
			path, err := editFile(root, op, m.kb)
			if err != nil {
				return result, err
			}
			result.FilesEdited = append(result.FilesEdited, path)
		}
	}
	return result, nil
}

// Commit creates a git commit.
func (m *MarkdownProjection) Commit(ctx context.Context, root, message string) (string, error) {
	if !IsGitRepo(root) {
		return "", nil // no-op for non-git roots
	}
	return CommitAll(root, message)
}

// Revert undoes a git commit.
func (m *MarkdownProjection) Revert(ctx context.Context, root, commitRef string) error {
	return RevertCommit(root, commitRef)
}

// HasChanges checks for uncommitted git changes.
func (m *MarkdownProjection) HasChanges(ctx context.Context, root string) (bool, error) {
	if !IsGitRepo(root) {
		return false, nil
	}
	return HasChanges(root)
}

// --- File operations ---

func createFile(root string, op projection.FileOp) (string, error) {
	fm := Frontmatter{Title: op.Title, Parent: op.Parent, Extra: op.Extra}
	body := op.Content
	// Strip any LLM-generated frontmatter from body
	if strings.HasPrefix(strings.TrimSpace(body), "---") {
		_, body = ParseFrontmatter(body)
	}
	content := RenderNode(fm, body)
	path := filepath.Join(root, SanitizeFilename(op.Title))
	return path, os.WriteFile(path, []byte(content), 0644)
}

// validateEditOp checks that the target file exists and contains the expected
// old text, without modifying any state. Used for pre-validation.
func validateEditOp(root string, op projection.FileOp, k *kb.KB) error {
	subject := kb.NodeSubject(root, op.File)
	filePath, ok, err := k.Graph().Lookup(context.Background(), subject, kb.PredNodeFile)
	if err != nil {
		return err
	}
	if !ok {
		filePath = filepath.Join(root, SanitizeFilename(op.File))
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("md: read %q: %w", filePath, err)
	}
	if !strings.Contains(string(data), op.OldText) {
		return fmt.Errorf("md: exact match not found in %q\n  expected: %s",
			filePath, formatOldText(op.OldText))
	}
	return nil
}

// formatOldText truncates a long old_text for error messages. Shows
// the first and last 60 characters with an ellipsis in between, so
// the user can see what the LLM thought the anchor was without
// dumping an entire block to the terminal.
func formatOldText(s string) string {
	const maxChars = 140
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= maxChars {
		return fmt.Sprintf("%q", s)
	}
	head := s[:60]
	tail := s[len(s)-60:]
	return fmt.Sprintf("%q ... %q", head, tail)
}

func editFile(root string, op projection.FileOp, k *kb.KB) (string, error) {
	// Resolve file path from graph
	subject := kb.NodeSubject(root, op.File)
	filePath, ok, err := k.Graph().Lookup(context.Background(), subject, kb.PredNodeFile)
	if err != nil {
		return "", err
	}
	if !ok {
		// Fall back to filename from title
		filePath = filepath.Join(root, SanitizeFilename(op.File))
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("md: read %q: %w", filePath, err)
	}

	content := string(data)
	if !strings.Contains(content, op.OldText) {
		return "", fmt.Errorf("md: exact match not found in %q\n  expected: %s",
			filePath, formatOldText(op.OldText))
	}

	content = strings.Replace(content, op.OldText, op.NewText, 1)
	return filePath, os.WriteFile(filePath, []byte(content), 0644)
}

// --- Root discovery ---

// FindRoot walks up from dir to find the nearest .sevens.edn file.
func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving path %s: %w", dir, err)
	}
	current := abs
	for {
		candidate := filepath.Join(current, ".sevens.edn")
		if _, err := os.Stat(candidate); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no .sevens.edn found in %s or any parent directory", abs)
		}
		current = parent
	}
}

// LoadConfig reads and parses the .sevens.edn config file in root.
func LoadConfig(root string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(root, ".sevens.edn"))
	if err != nil {
		return Config{}, fmt.Errorf("reading .sevens.edn in %s: %w", root, err)
	}
	var cfg Config
	if err := edn.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing .sevens.edn in %s: %w", root, err)
	}
	cfg.Path = expandTilde(cfg.Path)
	return cfg, nil
}

// Config holds the parsed .sevens.edn configuration.
type Config struct {
	Path        string           `edn:"path"`
	Alias       string           `edn:"alias"`
	MaxChars    *int             `edn:"max-chars"`
	MaxChildren *int             `edn:"max-children"`
	Groups      map[string]Group `edn:"groups"`
}

// Group defines a subgraph that can be included as context.
type Group struct {
	Root    string   `edn:"root"`
	Exclude []string `edn:"exclude"`
	Nodes   []string `edn:"nodes"`
}

func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// --- Scanning and parsing ---

// ScanFiles returns all .md file paths under root, skipping .git.
func ScanFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// ParseFiles parses all files into ParsedNodes. Returns nodes and
// any error messages (malformed files are skipped, not fatal).
func ParseFiles(files []string) ([]ParsedNode, []string) {
	var nodes []ParsedNode
	var errs []string
	seen := make(map[string]struct{})

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			errs = append(errs, fmt.Sprintf("read %s: %v", f, err))
			continue
		}

		fm, body := ParseFrontmatter(string(data))
		if fm.Title == "" {
			continue // skip files without title
		}
		if _, ok := seen[fm.Title]; ok {
			errs = append(errs, fmt.Sprintf("duplicate title %q in %s", fm.Title, f))
			continue
		}
		seen[fm.Title] = struct{}{}

		node := ParsedNode{
			Title:        fm.Title,
			FilePath:     f,
			Content:      body,
			CrossRefs:    ExtractWikiLinks(body),
			SiblingRole:  fm.SiblingRole,
			IncludeGroup: fm.IncludeGroup,
			Blocks:       ExtractBlocks(body),
			Extra:        fm.Extra,
		}
		if fm.Parent != "" {
			node.Parent = &fm.Parent
		}
		nodes = append(nodes, node)
	}
	return nodes, errs
}

// blockMarkdownLine reconstructs a markdown-formatted line for a block,
// suitable for orthography parsing (headings get ## prefix, list items get - prefix).
func blockMarkdownLine(block ParsedBlock) string {
	switch block.Kind {
	case "heading":
		return strings.Repeat("#", block.Level) + " " + block.Text
	case "list-item", "task":
		return "- " + block.Text
	default:
		return block.Text
	}
}

// nodeToTriples converts a ParsedNode into triples for the store.
func nodeToTriples(node ParsedNode, root string, sigMap map[string]string) []triple.Triple {
	subj := kb.NodeSubject(root, node.Title)
	triples := []triple.Triple{
		{Subject: subj, Predicate: kb.PredNodeTitle, Object: node.Title},
		{Subject: subj, Predicate: kb.PredNodeRoot, Object: root},
		{Subject: subj, Predicate: kb.PredNodeContent, Object: node.Content},
		{Subject: subj, Predicate: kb.PredNodeFile, Object: node.FilePath},
		{Subject: subj, Predicate: kb.PredNodeCharCount, Object: fmt.Sprintf("%d", len(node.Content))},
	}

	if node.Parent != nil {
		parentSubj := kb.NodeSubject(root, *node.Parent)
		triples = append(triples, triple.Triple{Subject: subj, Predicate: kb.PredNodeParent, Object: parentSubj})
	}

	for _, ref := range node.CrossRefs {
		refSubj := kb.NodeSubject(root, ref)
		triples = append(triples, triple.Triple{Subject: subj, Predicate: kb.PredNodeLink, Object: refSubj})
	}

	if node.SiblingRole != "" {
		triples = append(triples, triple.Triple{Subject: subj, Predicate: kb.PredNodeRole, Object: node.SiblingRole})
	}

	// Extra frontmatter fields → meta/* predicates
	for k, v := range node.Extra {
		triples = append(triples, triple.Triple{Subject: subj, Predicate: "meta/" + k, Object: v})
	}

	// Block triples: blocks are nodes in the graph with block/* predicates.
	// Create the orthography registry once for all blocks.
	reg := DefaultRegistry()

	for _, block := range node.Blocks {
		blockSubj := kb.BlockSubject(root, node.Title, block.Path)
		triples = append(triples,
			triple.Triple{Subject: blockSubj, Predicate: kb.PredBlockNode, Object: subj},
			triple.Triple{Subject: blockSubj, Predicate: kb.PredBlockRoot, Object: root},
			triple.Triple{Subject: blockSubj, Predicate: kb.PredBlockPath, Object: block.Path},
			triple.Triple{Subject: blockSubj, Predicate: kb.PredBlockKind, Object: block.Kind},
			triple.Triple{Subject: blockSubj, Predicate: kb.PredBlockContent, Object: block.Text},
		)
		if len(block.HeadingChain) > 0 {
			scope := ScopeString(VisibleBlockScope(block.Kind, block.Text, block.HeadingChain))
			triples = append(triples, triple.Triple{Subject: blockSubj, Predicate: kb.PredBlockScope, Object: scope})
		}

		// Orthography: parse property lists from the block's markdown line.
		mdLine := blockMarkdownLine(block)
		pls := FindPropertyLists(mdLine, nil, reg)
		for _, pl := range pls {
			for _, slot := range pl.Slots {
				pred := "meta/" + slot.Token
				// Resolve signifier to semantic predicate name if a binding exists.
				if sigMap != nil {
					if semantic, ok := sigMap[slot.Token]; ok {
						pred = "meta/" + semantic
					}
				}
				switch slot.Arity {
				case ArityTwo:
					triples = append(triples, triple.Triple{Subject: blockSubj, Predicate: pred, Object: slot.Payload})
				case ArityOne:
					triples = append(triples, triple.Triple{Subject: blockSubj, Predicate: pred, Object: slot.Symbol})
				case ArityZero:
					triples = append(triples, triple.Triple{Subject: blockSubj, Predicate: pred, Object: "true"})
				}
			}
		}

		// Orthography: parse inline atoms from the block text.
		atoms := FindInlineAtoms(block.Text, reg)
		for _, atom := range atoms {
			triples = append(triples, triple.Triple{
				Subject:   blockSubj,
				Predicate: "ref/" + atom.Signifier,
				Object:    atom.Symbol,
			})
		}
	}

	return triples
}
