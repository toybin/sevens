package graph

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

type ParsedBlockChange struct {
	OldPath string
	NewPath string
}

type ParsedBlockDiff struct {
	Unchanged    []ParsedBlockChange
	Edited       []ParsedBlockChange
	ScopeChanged []ParsedBlockChange
	Reordered    []ParsedBlockChange
	Inserted     []string
	Deleted      []string
}

func fillBlockIdentity(block *ParsedBlock) {
	normalizedText := normalizeBlockText(block.Text)
	block.AnchorHashes = anchorHashesForText(block.Text)
	block.Anchors, block.AnchorCount = anchorValuesForText(block.Text)
	if normalizedText != "" {
		block.TextHash = shortHashUint64(normalizedText)
	} else {
		block.TextHash = 0
	}
	if len(block.HeadingChain) > 0 {
		block.ScopeHash = shortHashUint64(strings.Join(block.HeadingChain, headingChainSeparator))
	} else {
		block.ScopeHash = 0
	}
	if len(block.Tags) > 0 {
		block.TagHash = shortHashUint64(strings.Join(block.Tags, headingChainSeparator))
	} else {
		block.TagHash = 0
	}
}

func normalizeBlockText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func shortHash(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:6])
}

func shortHashUint64(text string) uint64 {
	sum := sha1.Sum([]byte(text))
	return binary.BigEndian.Uint64(sum[:8])
}

func anchorHashesForText(text string) []string {
	normalized := normalizeBlockText(text)
	if normalized == "" {
		return nil
	}
	tokens := strings.Fields(normalized)
	if len(tokens) == 0 {
		return nil
	}

	width := 3
	if len(tokens) < width {
		width = len(tokens)
	}

	seen := make(map[string]bool)
	var hashes []string
	for i := 0; i+width <= len(tokens); i++ {
		shingle := strings.Join(tokens[i:i+width], " ")
		hash := shortHash(shingle)
		if seen[hash] {
			continue
		}
		seen[hash] = true
		hashes = append(hashes, hash)
	}
	if len(hashes) == 0 {
		hashes = append(hashes, shortHash(normalized))
	}
	sort.Strings(hashes)
	if len(hashes) > 4 {
		hashes = hashes[:4]
	}
	return hashes
}

func anchorValuesForText(text string) ([4]uint64, uint8) {
	normalized := normalizeBlockText(text)
	var values [4]uint64
	if normalized == "" {
		return values, 0
	}
	tokens := strings.Fields(normalized)
	if len(tokens) == 0 {
		return values, 0
	}

	width := 3
	if len(tokens) < width {
		width = len(tokens)
	}

	seen := make(map[uint64]bool)
	var hashes []uint64
	for i := 0; i+width <= len(tokens); i++ {
		shingle := strings.Join(tokens[i:i+width], " ")
		hash := shortHashUint64(shingle)
		if seen[hash] {
			continue
		}
		seen[hash] = true
		hashes = append(hashes, hash)
	}
	if len(hashes) == 0 {
		hashes = append(hashes, shortHashUint64(normalized))
	}
	sort.Slice(hashes, func(i, j int) bool { return hashes[i] < hashes[j] })
	if len(hashes) > len(values) {
		hashes = hashes[:len(values)]
	}
	for i, hash := range hashes {
		values[i] = hash
	}
	return values, uint8(len(hashes))
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func overlapCount(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]bool, len(a))
	for _, value := range a {
		set[value] = true
	}
	count := 0
	for _, value := range b {
		if set[value] {
			count++
		}
	}
	return count
}

func overlapFixed(a [4]uint64, aCount uint8, b [4]uint64, bCount uint8) int {
	if aCount == 0 || bCount == 0 {
		return 0
	}
	count := 0
	i, j := 0, 0
	for i < int(aCount) && j < int(bCount) {
		if a[i] == b[j] {
			count++
			i++
			j++
			continue
		}
		if a[i] < b[j] {
			i++
			continue
		}
		j++
	}
	return count
}

func sharedPrefixLen(a, b []string) int {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	count := 0
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			break
		}
		count++
	}
	return count
}

func blockFamilyKey(block ParsedBlock) string {
	key := block.Kind
	if block.Kind == "heading" {
		return key + ":" + strconv.Itoa(block.Level)
	}
	if block.Kind == "task" {
		return key + ":" + block.Signifier
	}
	return key
}

func blockMatchScore(oldBlock, newBlock ParsedBlock) int {
	if oldBlock.Kind != newBlock.Kind {
		return -1
	}
	if oldBlock.Kind == "heading" && oldBlock.Level != newBlock.Level {
		return -1
	}
	if oldBlock.Kind == "task" && oldBlock.Signifier != newBlock.Signifier {
		return -1
	}

	anchors := overlapFixed(oldBlock.Anchors, oldBlock.AnchorCount, newBlock.Anchors, newBlock.AnchorCount)
	if oldBlock.TextHash != newBlock.TextHash && anchors == 0 {
		return -1
	}

	score := anchors * 100
	if oldBlock.TextHash == newBlock.TextHash {
		score += 200
	}
	if oldBlock.ScopeHash == newBlock.ScopeHash {
		score += 25
	} else {
		score += sharedPrefixLen(oldBlock.HeadingChain, newBlock.HeadingChain) * 15
	}
	if oldBlock.TagHash != 0 && oldBlock.TagHash == newBlock.TagHash {
		score += 10
	}
	return score
}

type blockCandidate struct {
	OldPath string
	NewPath string
	Score   int
}

func resolveBlockMatches(oldBlocks, newBlocks []ParsedBlock) map[string]string {
	oldByPath := make(map[string]ParsedBlock, len(oldBlocks))
	oldAnchors := make(map[string]map[uint64][]ParsedBlock)
	oldExact := make(map[string][]ParsedBlock)
	for _, block := range oldBlocks {
		oldByPath[block.Path] = block
		family := blockFamilyKey(block)
		if oldAnchors[family] == nil {
			oldAnchors[family] = make(map[uint64][]ParsedBlock)
		}
		for i := 0; i < int(block.AnchorCount); i++ {
			oldAnchors[family][block.Anchors[i]] = append(oldAnchors[family][block.Anchors[i]], block)
		}
		exactKey := family + "|" + strconv.FormatUint(block.TextHash, 16)
		oldExact[exactKey] = append(oldExact[exactKey], block)
	}

	var candidates []blockCandidate
	for _, newBlock := range newBlocks {
		family := blockFamilyKey(newBlock)
		seen := make(map[string]bool)
		var candidateOld []ParsedBlock
		exactKey := family + "|" + strconv.FormatUint(newBlock.TextHash, 16)
		candidateOld = append(candidateOld, oldExact[exactKey]...)
		for i := 0; i < int(newBlock.AnchorCount); i++ {
			candidateOld = append(candidateOld, oldAnchors[family][newBlock.Anchors[i]]...)
		}
		for _, oldBlock := range candidateOld {
			if seen[oldBlock.Path] {
				continue
			}
			seen[oldBlock.Path] = true
			score := blockMatchScore(oldBlock, newBlock)
			if score <= 0 {
				continue
			}
			candidates = append(candidates, blockCandidate{
				OldPath: oldBlock.Path,
				NewPath: newBlock.Path,
				Score:   score,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].OldPath != candidates[j].OldPath {
			return candidates[i].OldPath < candidates[j].OldPath
		}
		return candidates[i].NewPath < candidates[j].NewPath
	})

	matchedOld := make(map[string]bool, len(oldByPath))
	matchedNew := make(map[string]bool, len(newBlocks))
	newToOld := make(map[string]string)
	for _, candidate := range candidates {
		if matchedOld[candidate.OldPath] || matchedNew[candidate.NewPath] {
			continue
		}
		matchedOld[candidate.OldPath] = true
		matchedNew[candidate.NewPath] = true
		newToOld[candidate.NewPath] = candidate.OldPath
	}
	return newToOld
}

func previousMatchedPath(blocks []ParsedBlock, path string, matchedOld map[string]bool) string {
	prev := ""
	for _, block := range blocks {
		if block.Path == path {
			return prev
		}
		if matchedOld[block.Path] {
			prev = block.Path
		}
	}
	return prev
}

func previousResolvedOldPath(blocks []ParsedBlock, path string, newToOld map[string]string) string {
	prev := ""
	for _, block := range blocks {
		if block.Path == path {
			return prev
		}
		if resolved, ok := newToOld[block.Path]; ok {
			prev = resolved
		}
	}
	return prev
}

func reorderedOldPaths(oldBlocks, newBlocks []ParsedBlock, newToOld map[string]string) map[string]bool {
	oldIndex := make(map[string]int, len(oldBlocks))
	for i, block := range oldBlocks {
		oldIndex[block.Path] = i
	}

	type matched struct {
		oldPath string
		index   int
	}
	var ordered []matched
	for _, block := range newBlocks {
		oldPath, ok := newToOld[block.Path]
		if !ok {
			continue
		}
		ordered = append(ordered, matched{oldPath: oldPath, index: oldIndex[oldPath]})
	}
	if len(ordered) < 2 {
		return map[string]bool{}
	}

	// Longest increasing subsequence over old indexes gives the blocks whose
	// relative order is preserved across the matched old/new sequences.
	n := len(ordered)
	dp := make([]int, n)
	prev := make([]int, n)
	bestLen := 0
	bestEnd := -1
	for i := 0; i < n; i++ {
		dp[i] = 1
		prev[i] = -1
		for j := 0; j < i; j++ {
			if ordered[j].index < ordered[i].index && dp[j]+1 > dp[i] {
				dp[i] = dp[j] + 1
				prev[i] = j
			}
		}
		if dp[i] > bestLen {
			bestLen = dp[i]
			bestEnd = i
		}
	}

	inLIS := make(map[string]bool, bestLen)
	for i := bestEnd; i >= 0; i = prev[i] {
		inLIS[ordered[i].oldPath] = true
		if prev[i] == -1 {
			break
		}
	}

	reordered := make(map[string]bool)
	for _, item := range ordered {
		if !inLIS[item.oldPath] {
			reordered[item.oldPath] = true
		}
	}
	return reordered
}

func sortChanges(changes []ParsedBlockChange) {
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].OldPath != changes[j].OldPath {
			return changes[i].OldPath < changes[j].OldPath
		}
		return changes[i].NewPath < changes[j].NewPath
	})
}

func DiffParsedBlocks(oldBlocks, newBlocks []ParsedBlock) ParsedBlockDiff {
	newToOld := resolveBlockMatches(oldBlocks, newBlocks)
	reorderedOld := reorderedOldPaths(oldBlocks, newBlocks, newToOld)

	oldByPath := make(map[string]ParsedBlock, len(oldBlocks))
	newByPath := make(map[string]ParsedBlock, len(newBlocks))
	matchedOld := make(map[string]bool, len(newToOld))
	for _, block := range oldBlocks {
		oldByPath[block.Path] = block
	}
	for _, block := range newBlocks {
		newByPath[block.Path] = block
	}
	for _, oldPath := range newToOld {
		matchedOld[oldPath] = true
	}

	var diff ParsedBlockDiff
	for _, oldBlock := range oldBlocks {
		if !matchedOld[oldBlock.Path] {
			diff.Deleted = append(diff.Deleted, oldBlock.Path)
		}
	}
	for _, newBlock := range newBlocks {
		if _, ok := newToOld[newBlock.Path]; !ok {
			diff.Inserted = append(diff.Inserted, newBlock.Path)
		}
	}

	for newPath, oldPath := range newToOld {
		oldBlock := oldByPath[oldPath]
		newBlock := newByPath[newPath]
		change := ParsedBlockChange{OldPath: oldPath, NewPath: newPath}

		scopeChanged := !equalStrings(oldBlock.HeadingChain, newBlock.HeadingChain)
		edited := normalizeBlockText(oldBlock.Text) != normalizeBlockText(newBlock.Text)
		reordered := reorderedOld[oldPath]

		if scopeChanged {
			diff.ScopeChanged = append(diff.ScopeChanged, change)
		}
		if edited {
			diff.Edited = append(diff.Edited, change)
		}
		if reordered {
			diff.Reordered = append(diff.Reordered, change)
		}
		if !scopeChanged && !edited && !reordered {
			diff.Unchanged = append(diff.Unchanged, change)
		}
	}

	sort.Strings(diff.Inserted)
	sort.Strings(diff.Deleted)
	sortChanges(diff.Unchanged)
	sortChanges(diff.Edited)
	sortChanges(diff.ScopeChanged)
	sortChanges(diff.Reordered)
	return diff
}
