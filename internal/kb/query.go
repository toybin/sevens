package kb

import (
	"context"
	"strconv"

	"sevens/internal/graphops"
)

// WalkContext is the full local context of a node.
type WalkContext struct {
	Subject      string
	Title        string
	Content      string
	Parent       *string
	Children     []string // titles
	Siblings     []string // titles
	CrossRefs    []string // titles
	ChildRoles   map[string]string
	SiblingRoles map[string]string
	Role         string
}

// Walk returns full local context for a node: content, parent,
// children, siblings, cross-references, and roles.
func (k *KB) Walk(ctx context.Context, root, title string) (*WalkContext, error) {
	subject := NodeSubject(root, title)

	content, _, _ := k.graph.Lookup(ctx, subject, PredNodeContent)
	role, _, _ := k.graph.Lookup(ctx, subject, PredNodeRole)

	// Parent
	var parent *string
	if p, ok, _ := k.graph.Lookup(ctx, subject, PredNodeParent); ok {
		pt, err := k.resolveTitle(ctx, p)
		if err == nil && pt != "" {
			parent = &pt
		}
	}

	// Children: follow node/parent inverse
	childSubjects, _ := k.graph.Compose(ctx, subject,
		graphops.ParsePath([]string{PredNodeParent + "~"}))
	children, childRoles := k.resolveTitlesAndRoles(ctx, childSubjects)

	// Siblings: parent forward, then parent inverse, minus self
	siblingSubjects, _ := k.graph.Compose(ctx, subject,
		graphops.ParsePath([]string{PredNodeParent, PredNodeParent + "~"}))
	siblingSubjects = exclude(siblingSubjects, subject)
	siblings, siblingRoles := k.resolveTitlesAndRoles(ctx, siblingSubjects)

	// Cross-references: node/link forward
	linkSubjects, _ := k.graph.Compose(ctx, subject,
		graphops.ParsePath([]string{PredNodeLink}))
	crossRefs := k.resolveTitleList(ctx, linkSubjects)

	return &WalkContext{
		Subject:      subject,
		Title:        title,
		Content:      content,
		Parent:       parent,
		Children:     children,
		Siblings:     siblings,
		CrossRefs:    crossRefs,
		ChildRoles:   childRoles,
		SiblingRoles: siblingRoles,
		Role:         role,
	}, nil
}

// OverviewNode is one node's metadata in a full-graph overview.
type OverviewNode struct {
	Title      string
	Parent     *string
	Children   []string
	ChildCount int
	CrossRefs  []string
	CharCount  int
}

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
	isChild := make(map[string]bool)

	for _, subj := range subjects {
		title, _, _ := k.graph.Lookup(ctx, subj, PredNodeTitle)
		if title == "" {
			continue
		}

		// Missing parent check: has parent predicate, but parent doesn't exist
		if parentSubj, ok, _ := k.graph.Lookup(ctx, subj, PredNodeParent); ok {
			hasParent[subj] = true
			isChild[subj] = true
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

		// Cycle check: is this node its own ancestor?
		ancestors, _ := k.graph.Reachable(ctx, subj, PredNodeParent, false)
		for _, a := range ancestors {
			if a == subj && len(ancestors) > 1 {
				violations = append(violations, Violation{
					Kind: "cycle", Title: title,
					Detail: "node is its own ancestor via node/parent",
				})
				break
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
