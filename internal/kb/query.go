package kb

import (
	"context"
	"strconv"

	"sevens/internal/graphops"
	"sevens/internal/sevtypes"
)


// OverviewNode is a type alias for sevtypes.OverviewNode.
type OverviewNode = sevtypes.OverviewNode

// Overview returns the full tree structure for a root.
func (k *KB) Overview(ctx context.Context, root string) ([]OverviewNode, error) {
	// Find all node subjects in this root
	subjects, err := k.graph.Store().ByPredicateObject(ctx, PredNodeRoot, root)
	if err != nil {
		return nil, err
	}

	var nodes []OverviewNode
	for _, subj := range subjects {
		title, _, _ := k.graph.Lookup(ctx, subj, PredNodeTitle)
		if title == "" {
			continue
		}

		var parent *string
		if p, ok, _ := k.graph.Lookup(ctx, subj, PredNodeParent); ok {
			pt, err := k.resolveTitle(ctx, p)
			if err == nil && pt != "" {
				parent = &pt
			}
		}

		childSubjects, _ := k.graph.Compose(ctx, subj,
			graphops.ParsePath([]string{PredNodeParent + "~"}))
		childTitles := k.resolveTitleList(ctx, childSubjects)

		linkSubjects, _ := k.graph.Compose(ctx, subj,
			graphops.ParsePath([]string{PredNodeLink}))
		linkTitles := k.resolveTitleList(ctx, linkSubjects)

		charCount := 0
		if cc, ok, _ := k.graph.Lookup(ctx, subj, PredNodeCharCount); ok {
			charCount, _ = strconv.Atoi(cc)
		}

		nodes = append(nodes, OverviewNode{
			Title:      title,
			Parent:     parent,
			Children:   childTitles,
			ChildCount: len(childTitles),
			CrossRefs:  linkTitles,
			CharCount:  charCount,
		})
	}
	return nodes, nil
}

// Children returns a node's child titles.
func (k *KB) Children(ctx context.Context, root, title string) ([]string, error) {
	subject := NodeSubject(root, title)
	childSubjects, err := k.graph.Compose(ctx, subject,
		graphops.ParsePath([]string{PredNodeParent + "~"}))
	if err != nil {
		return nil, err
	}
	return k.resolveTitleList(ctx, childSubjects), nil
}

// Parent returns a node's parent title, or nil if root.
func (k *KB) Parent(ctx context.Context, root, title string) (*string, error) {
	subject := NodeSubject(root, title)
	p, ok, err := k.graph.Lookup(ctx, subject, PredNodeParent)
	if err != nil || !ok {
		return nil, err
	}
	pt, err := k.resolveTitle(ctx, p)
	if err != nil || pt == "" {
		return nil, err
	}
	return &pt, nil
}

// Siblings returns titles of nodes sharing the same parent, excluding self.
func (k *KB) Siblings(ctx context.Context, root, title string) ([]string, error) {
	subject := NodeSubject(root, title)
	sibSubjects, err := k.graph.Compose(ctx, subject,
		graphops.ParsePath([]string{PredNodeParent, PredNodeParent + "~"}))
	if err != nil {
		return nil, err
	}
	return k.resolveTitleList(ctx, exclude(sibSubjects, subject)), nil
}

// Resolve finds a node subject by title within a root.
// Returns "" if not found.
func (k *KB) Resolve(ctx context.Context, root, title string) string {
	subject := NodeSubject(root, title)
	if _, ok, _ := k.graph.Lookup(ctx, subject, PredNodeTitle); ok {
		return subject
	}
	return ""
}

// --- Validation ---

// Violation describes a structural problem.
type Violation struct {
	Kind    string // "orphan", "overflow", "overlength", "missing-parent"
	Title   string
	Detail  string
}

// Validate checks structural invariants.
func (k *KB) Validate(ctx context.Context, root string, maxChildren, maxContentLength int) ([]Violation, error) {
	subjects, err := k.graph.Store().ByPredicateObject(ctx, PredNodeRoot, root)
	if err != nil {
		return nil, err
	}

	var violations []Violation
	hasParent := make(map[string]bool)

	for _, subj := range subjects {
		title, _, _ := k.graph.Lookup(ctx, subj, PredNodeTitle)
		if title == "" {
			continue
		}

		// Missing parent check: has parent predicate, but parent doesn't exist
		if parentSubj, ok, _ := k.graph.Lookup(ctx, subj, PredNodeParent); ok {
			hasParent[subj] = true
			if _, exists, _ := k.graph.Lookup(ctx, parentSubj, PredNodeTitle); !exists {
				violations = append(violations, Violation{
					Kind: "missing-parent", Title: title,
					Detail: "parent subject does not exist",
				})
			}
		}

		// Overflow check
		if maxChildren > 0 {
			children, _ := k.graph.Compose(ctx, subj,
				graphops.ParsePath([]string{PredNodeParent + "~"}))
			if len(children) > maxChildren {
				violations = append(violations, Violation{
					Kind: "overflow", Title: title,
					Detail: strconv.Itoa(len(children)) + " children (max " + strconv.Itoa(maxChildren) + ")",
				})
			}
		}

		// Overlength check
		if maxContentLength > 0 {
			if cc, ok, _ := k.graph.Lookup(ctx, subj, PredNodeCharCount); ok {
				count, _ := strconv.Atoi(cc)
				if count > maxContentLength {
					violations = append(violations, Violation{
						Kind: "overlength", Title: title,
						Detail: strconv.Itoa(count) + " chars (max " + strconv.Itoa(maxContentLength) + ")",
					})
				}
			}
		}

		// Cycle check: walk the parent chain; if we ever return to
		// this node, it's a cycle. We don't use Reachable because its
		// visited-set dedup prevents detecting a revisit of the start.
		{
			cur := subj
			visited := map[string]bool{cur: true}
			isCycle := false
			for {
				p, ok, _ := k.graph.Lookup(ctx, cur, PredNodeParent)
				if !ok || p == "" {
					break
				}
				if p == subj {
					isCycle = true
					break
				}
				if visited[p] {
					break // cycle not involving this node
				}
				visited[p] = true
				cur = p
			}
			if isCycle {
				violations = append(violations, Violation{
					Kind: "cycle", Title: title,
					Detail: "node is its own ancestor via node/parent",
				})
			}
		}
	}

	// Orphan check: nodes with no parent that aren't root nodes.
	// A root node is one with no parent and at least one child,
	// or simply the node with no parent. We flag nodes that have
	// no parent AND aren't the only parentless node (i.e., there
	// should be exactly one root per tree).
	var parentless []string
	for _, subj := range subjects {
		if !hasParent[subj] {
			title, _, _ := k.graph.Lookup(ctx, subj, PredNodeTitle)
			if title != "" {
				parentless = append(parentless, title)
			}
		}
	}
	if len(parentless) > 1 {
		// Multiple parentless nodes -- all but the first are orphans
		// (heuristic: the one with children is the real root)
		for _, title := range parentless[1:] {
			violations = append(violations, Violation{
				Kind:   "orphan",
				Title:  title,
				Detail: "node has no parent (multiple root nodes detected)",
			})
		}
	}

	return violations, nil
}

// ListNodeTitles returns all node titles in a root.
func (k *KB) ListNodeTitles(ctx context.Context, root string) ([]string, error) {
	subjects, err := k.graph.Store().ByPredicateObject(ctx, PredNodeRoot, root)
	if err != nil {
		return nil, err
	}
	var titles []string
	for _, subj := range subjects {
		if t, ok, _ := k.graph.Lookup(ctx, subj, PredNodeTitle); ok && t != "" {
			titles = append(titles, t)
		}
	}
	return titles, nil
}

// --- Helpers ---

// resolveTitle looks up the title for a subject.
func (k *KB) resolveTitle(ctx context.Context, subject string) (string, error) {
	title, _, err := k.graph.Lookup(ctx, subject, PredNodeTitle)
	return title, err
}

// resolveTitleList resolves a list of subjects to titles, dropping empty.
func (k *KB) resolveTitleList(ctx context.Context, subjects []string) []string {
	var titles []string
	for _, s := range subjects {
		if t, _ := k.resolveTitle(ctx, s); t != "" {
			titles = append(titles, t)
		}
	}
	return titles
}

// resolveTitlesAndRoles resolves subjects to titles and also collects roles.
func (k *KB) resolveTitlesAndRoles(ctx context.Context, subjects []string) ([]string, map[string]string) {
	var titles []string
	roles := make(map[string]string)
	for _, s := range subjects {
		t, _ := k.resolveTitle(ctx, s)
		if t == "" {
			continue
		}
		titles = append(titles, t)
		if r, ok, _ := k.graph.Lookup(ctx, s, PredNodeRole); ok {
			roles[t] = r
		}
	}
	return titles, roles
}

func exclude(ss []string, exclude string) []string {
	var result []string
	for _, s := range ss {
		if s != exclude {
			result = append(result, s)
		}
	}
	return result
}
