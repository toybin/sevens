package graph

import (
	"sort"
	"strconv"
	"strings"
)

// SearchASTNode is a toy AST node for prototyping bounded rematching across
// snapshots. StableID is required on the old tree; the new tree may omit it.
type SearchASTNode struct {
	StableID string
	Kind     string
	Label    string
	Children []*SearchASTNode
}

type indexedSearchNode struct {
	Node   *SearchASTNode
	Parent *indexedSearchNode
	Path   string
	Index  int
}

// SearchMatchResult maps old stable IDs to paths in the new tree.
type SearchMatchResult struct {
	Matches map[string]string
}

func indexSearchTree(root *SearchASTNode) []*indexedSearchNode {
	if root == nil {
		return nil
	}
	var indexed []*indexedSearchNode
	var walk func(node *SearchASTNode, parent *indexedSearchNode, path string)
	walk = func(node *SearchASTNode, parent *indexedSearchNode, path string) {
		current := &indexedSearchNode{
			Node:   node,
			Parent: parent,
			Path:   path,
			Index:  len(indexed),
		}
		indexed = append(indexed, current)
		for i, child := range node.Children {
			childPath := path
			if childPath != "" {
				childPath += "."
			}
			childPath += strconv.Itoa(i)
			walk(child, current, childPath)
		}
	}
	walk(root, nil, "")
	return indexed
}

func similarityScore(oldNode, newNode *SearchASTNode) int {
	if oldNode == nil || newNode == nil || oldNode.Kind != newNode.Kind {
		return -1
	}
	score := 10
	if oldNode.Label == newNode.Label {
		score += 100
	} else {
		oldTokens := strings.Fields(strings.ToLower(oldNode.Label))
		newTokens := strings.Fields(strings.ToLower(newNode.Label))
		tokenSet := make(map[string]bool, len(oldTokens))
		for _, token := range oldTokens {
			tokenSet[token] = true
		}
		overlap := 0
		for _, token := range newTokens {
			if tokenSet[token] {
				score += 10
				overlap++
			}
		}
		if overlap == 0 {
			return -1
		}
	}
	return score
}

func findIndexedByPath(nodes []*indexedSearchNode, path string) *indexedSearchNode {
	for _, node := range nodes {
		if node.Path == path {
			return node
		}
	}
	return nil
}

func childrenOf(nodes []*indexedSearchNode, parent *indexedSearchNode) []*indexedSearchNode {
	var children []*indexedSearchNode
	for _, node := range nodes {
		if node.Parent != nil && parent != nil && node.Parent.Path == parent.Path {
			children = append(children, node)
		}
	}
	return children
}

func candidateParents(oldParent *SearchASTNode, matchedParent *indexedSearchNode, newNodes []*indexedSearchNode) []*indexedSearchNode {
	if oldParent == nil {
		return nil
	}
	seen := make(map[string]bool)
	var candidates []*indexedSearchNode
	appendCandidate := func(node *indexedSearchNode) {
		if node == nil || seen[node.Path] {
			return
		}
		seen[node.Path] = true
		candidates = append(candidates, node)
	}

	appendCandidate(matchedParent)
	if matchedParent != nil && matchedParent.Parent != nil {
		siblings := childrenOf(newNodes, matchedParent.Parent)
		sort.Slice(siblings, func(i, j int) bool {
			di := absInt(siblings[i].Index - matchedParent.Index)
			dj := absInt(siblings[j].Index - matchedParent.Index)
			if di != dj {
				return di < dj
			}
			return siblings[i].Path < siblings[j].Path
		})
		for _, sibling := range siblings {
			if sibling.Path == matchedParent.Path {
				continue
			}
			appendCandidate(sibling)
		}
	}

	type scoredParent struct {
		Node  *indexedSearchNode
		Score int
	}
	var similar []scoredParent
	for _, node := range newNodes {
		score := similarityScore(oldParent, node.Node)
		if score <= 0 {
			continue
		}
		distance := 0
		if matchedParent != nil {
			distance = absInt(node.Index - matchedParent.Index)
		}
		similar = append(similar, scoredParent{
			Node:  node,
			Score: score*10 - distance,
		})
	}
	sort.Slice(similar, func(i, j int) bool {
		if similar[i].Score != similar[j].Score {
			return similar[i].Score > similar[j].Score
		}
		return similar[i].Node.Path < similar[j].Node.Path
	})
	for _, candidate := range similar {
		appendCandidate(candidate.Node)
	}
	return candidates
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func bestChildMatch(oldNode *SearchASTNode, parent *indexedSearchNode, newNodes []*indexedSearchNode, used map[string]bool) *indexedSearchNode {
	var best *indexedSearchNode
	bestScore := -1
	for _, child := range childrenOf(newNodes, parent) {
		if used[child.Path] {
			continue
		}
		score := similarityScore(oldNode, child.Node)
		if score > bestScore {
			bestScore = score
			best = child
		}
	}
	if bestScore <= 0 {
		return nil
	}
	return best
}

func bestGlobalMatch(oldNode *SearchASTNode, near *indexedSearchNode, newNodes []*indexedSearchNode, used map[string]bool) *indexedSearchNode {
	var best *indexedSearchNode
	bestScore := -1
	for _, node := range newNodes {
		if used[node.Path] {
			continue
		}
		score := similarityScore(oldNode, node.Node)
		if score <= 0 {
			continue
		}
		if near != nil {
			score = score*10 - absInt(node.Index-near.Index)
		}
		if score > bestScore {
			bestScore = score
			best = node
		}
	}
	return best
}

func rematchRecursive(oldNode *SearchASTNode, oldIndexed map[string]*indexedSearchNode, newNodes []*indexedSearchNode, matched map[string]string, used map[string]bool) {
	var matchedCurrent *indexedSearchNode
	if currentPath, ok := matched[oldNode.StableID]; ok {
		matchedCurrent = findIndexedByPath(newNodes, currentPath)
	}

	for _, oldChild := range oldNode.Children {
		if oldChild.StableID == "" {
			continue
		}
		var found *indexedSearchNode
		for _, candidateParent := range candidateParents(oldNode, matchedCurrent, newNodes) {
			found = bestChildMatch(oldChild, candidateParent, newNodes, used)
			if found != nil {
				break
			}
		}
		if found == nil {
			found = bestGlobalMatch(oldChild, matchedCurrent, newNodes, used)
		}
		if found == nil {
			continue
		}
		matched[oldChild.StableID] = found.Path
		used[found.Path] = true
		rematchRecursive(oldChild, oldIndexed, newNodes, matched, used)
	}
}

// RematchStableIDs tries to recover stable IDs from an old tree onto a new tree
// using bounded search: first the matched parent, then sibling parents, then
// structurally similar parents ordered by distance.
func RematchStableIDs(oldRoot, newRoot *SearchASTNode) SearchMatchResult {
	result := SearchMatchResult{Matches: map[string]string{}}
	if oldRoot == nil || newRoot == nil || oldRoot.StableID == "" {
		return result
	}

	oldIndexedList := indexSearchTree(oldRoot)
	newIndexedList := indexSearchTree(newRoot)
	oldIndexed := make(map[string]*indexedSearchNode, len(oldIndexedList))
	for _, node := range oldIndexedList {
		if node.Node.StableID != "" {
			oldIndexed[node.Node.StableID] = node
		}
	}

	result.Matches[oldRoot.StableID] = ""
	used := map[string]bool{"": true}
	rematchRecursive(oldRoot, oldIndexed, newIndexedList, result.Matches, used)
	return result
}
