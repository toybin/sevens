package graph

import (
	"database/sql"
	"fmt"
	"os"
	"sevens/internal/store"
	"sevens/internal/ui"
	"sort"
	"strconv"
	"strings"
)

const headingChainSeparator = "\n"

type storedBlock struct {
	Subject string
	Block   ParsedBlock
}

func loadStoredBlocks(db *sql.DB, root string, node ParsedNode) ([]storedBlock, error) {
	nodeSubject := store.NodeSubject(root, node.Title)
	subjects, err := store.GetSubjects(db, "block/node", nodeSubject)
	if err != nil {
		return nil, fmt.Errorf("loading block subjects for %q: %w", node.Title, err)
	}
	sort.Strings(subjects)

	var blocks []storedBlock
	for _, subject := range subjects {
		triples, err := store.GetSubjectTriples(db, subject)
		if err != nil {
			return nil, fmt.Errorf("loading triples for block %q: %w", subject, err)
		}

		var block ParsedBlock
		for _, triple := range triples {
			switch triple.Predicate {
			case "block/path":
				block.Path = triple.Object
			case "block/kind":
				block.Kind = triple.Object
			case "block/text":
				block.Text = triple.Object
			case "block/heading-level":
				level, _ := strconv.Atoi(triple.Object)
				block.Level = level
			case "block/signifier":
				block.Signifier = triple.Object
			case "block/tag":
				block.Tags = append(block.Tags, triple.Object)
			case "block/heading-chain":
				if triple.Object != "" {
					block.HeadingChain = strings.Split(triple.Object, headingChainSeparator)
				}
			case "block/anchor-hash":
				block.AnchorHashes = append(block.AnchorHashes, triple.Object)
			}
		}
		fillBlockIdentity(&block)
		blocks = append(blocks, storedBlock{Subject: subject, Block: block})
	}
	return blocks, nil
}

func normalizeNodeBlocks(node ParsedNode) ParsedNode {
	for i := range node.Blocks {
		fillBlockIdentity(&node.Blocks[i])
	}
	return node
}

func assignBlockSubjects(db *sql.DB, root string, node ParsedNode) (map[string]string, error) {
	node = normalizeNodeBlocks(node)
	storedBlocks, err := loadStoredBlocks(db, root, node)
	if err != nil {
		return nil, err
	}
	oldBlocks := make([]ParsedBlock, 0, len(storedBlocks))
	oldSubjects := make(map[string]string, len(storedBlocks))
	for _, stored := range storedBlocks {
		oldBlocks = append(oldBlocks, stored.Block)
		oldSubjects[stored.Block.Path] = stored.Subject
	}

	newToOld := resolveBlockMatches(oldBlocks, node.Blocks)
	subjects := make(map[string]string, len(node.Blocks))
	for _, block := range node.Blocks {
		if oldPath, ok := newToOld[block.Path]; ok {
			subjects[block.Path] = oldSubjects[oldPath]
			continue
		}
		subjects[block.Path] = store.BlockSubject(root, node.Title, block.Path)
	}
	return subjects, nil
}

func BlockToTriples(node ParsedNode, block ParsedBlock, root string, subject string) []store.Triple {
	nodeSubject := store.NodeSubject(root, node.Title)
	triples := []store.Triple{
		{Subject: subject, Predicate: "block/root", Object: root},
		{Subject: subject, Predicate: "block/id", Object: subject},
		{Subject: subject, Predicate: "block/node", Object: nodeSubject},
		{Subject: subject, Predicate: "block/path", Object: block.Path},
		{Subject: subject, Predicate: "block/kind", Object: block.Kind},
		{Subject: subject, Predicate: "block/text", Object: block.Text},
	}
	if block.Level > 0 {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "block/heading-level", Object: strconv.Itoa(block.Level)})
	}
	if block.Signifier != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "block/signifier", Object: block.Signifier})
	}
	if len(block.HeadingChain) > 0 {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "block/heading-chain", Object: strings.Join(block.HeadingChain, headingChainSeparator)})
	}
	for _, hash := range block.AnchorHashes {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "block/anchor-hash", Object: hash})
	}
	for _, tag := range block.Tags {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "block/tag", Object: tag})
	}
	return triples
}

// NodeToTriples converts a parsed node into triples.
// The subject is the node's title. The root is needed for the node/root predicate.
func NodeToTriples(node ParsedNode, root string, blockSubjects ...map[string]string) []store.Triple {
	node = normalizeNodeBlocks(node)
	subject := store.NodeSubject(root, node.Title)
	var triples []store.Triple
	blockSubjectMap := map[string]string(nil)
	if len(blockSubjects) > 0 {
		blockSubjectMap = blockSubjects[0]
	}

	// Core identity
	triples = append(triples, store.Triple{Subject: subject, Predicate: "node/title", Object: node.Title})
	triples = append(triples, store.Triple{Subject: subject, Predicate: "node/root", Object: root})
	triples = append(triples, store.Triple{Subject: subject, Predicate: "node/file-path", Object: node.FilePath})
	triples = append(triples, store.Triple{Subject: subject, Predicate: "node/content", Object: node.Content})

	// Tree structure
	if node.Parent != nil {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "node/parent", Object: store.NodeSubject(root, *node.Parent)})
	}

	// Max chars
	if node.MaxChars != nil {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "node/max-chars", Object: strconv.Itoa(*node.MaxChars)})
	}

	// Context files
	for _, cf := range node.ContextFiles {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "context/file", Object: cf})
	}

	// Cross-references (wiki links)
	for _, ref := range node.CrossRefs {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "ref/wiki-link", Object: ref})
	}

	// Sibling role (from frontmatter)
	if node.SiblingRole != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "sibling/role", Object: node.SiblingRole})
	}

	// Include group flag
	if node.IncludeGroup {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "node/include-group", Object: "true"})
	}

	// Computed content metrics
	charCount := len([]rune(node.Content))
	triples = append(triples, store.Triple{Subject: subject, Predicate: "content/char-count", Object: strconv.Itoa(charCount)})
	triples = append(triples, store.Triple{Subject: subject, Predicate: "content/token-estimate", Object: strconv.Itoa((charCount + 3) / 4)})

	// Count headings (lines starting with #)
	headingCount := 0
	for _, line := range strings.Split(node.Content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			headingCount++
		}
	}
	triples = append(triples, store.Triple{Subject: subject, Predicate: "content/heading-count", Object: strconv.Itoa(headingCount)})

	// Count wiki links
	triples = append(triples, store.Triple{Subject: subject, Predicate: "content/wiki-link-count", Object: strconv.Itoa(len(node.CrossRefs))})

	// Check for questions (lines containing ?)
	hasQuestions := false
	for _, line := range strings.Split(node.Content, "\n") {
		if strings.Contains(line, "?") {
			hasQuestions = true
			break
		}
	}
	triples = append(triples, store.Triple{Subject: subject, Predicate: "content/has-questions", Object: strconv.FormatBool(hasQuestions)})

	for _, block := range node.Blocks {
		blockSubject := ""
		if blockSubjectMap != nil {
			blockSubject = blockSubjectMap[block.Path]
		}
		if blockSubject == "" {
			blockSubject = store.BlockSubject(root, node.Title, block.Path)
		}
		triples = append(triples, BlockToTriples(node, block, root, blockSubject)...)
	}

	return triples
}

// RootConfigToTriples converts root config into triples.
func RootConfigToTriples(config Config, rootPath string) []store.Triple {
	var triples []store.Triple
	triples = append(triples, store.Triple{Subject: rootPath, Predicate: "root/path", Object: config.Path})
	if config.Alias != "" {
		triples = append(triples, store.Triple{Subject: rootPath, Predicate: "root/alias", Object: config.Alias})
	}
	if config.MaxChars != nil {
		triples = append(triples, store.Triple{Subject: rootPath, Predicate: "root/max-chars", Object: strconv.Itoa(*config.MaxChars)})
	}
	return triples
}

// PopulateTriples clears and repopulates all node triples for a root.
func PopulateTriples(db *sql.DB, root string, nodes []ParsedNode, config Config) error {
	nodeBlockSubjects := make(map[string]map[string]string, len(nodes))
	for _, node := range nodes {
		subjects, err := assignBlockSubjects(db, root, node)
		if err != nil {
			return fmt.Errorf("assigning block subjects for %q: %w", node.Title, err)
		}
		nodeBlockSubjects[node.Title] = subjects
	}

	// Clear existing node triples for this root
	if err := store.ClearRootTriples(db, root); err != nil {
		return fmt.Errorf("clearing root triples: %w", err)
	}

	// Collect all triples
	var allTriples []store.Triple

	// Root config triples
	allTriples = append(allTriples, RootConfigToTriples(config, root)...)

	// Node triples
	for _, node := range nodes {
		allTriples = append(allTriples, NodeToTriples(node, root, nodeBlockSubjects[node.Title])...)
	}

	// Insert all at once
	if err := store.InsertTriples(db, allTriples); err != nil {
		return fmt.Errorf("inserting triples: %w", err)
	}

	fmt.Fprintf(os.Stderr, "%s Wrote %d triples\n", ui.Success.Render("[sync]"), len(allTriples))
	return nil
}
