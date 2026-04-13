package kb

import (
	"context"
	"strconv"

	"sevens/internal/graphops"
	"sevens/internal/sevtypes"
)

// GatherSpec is a type alias for sevtypes.GatherSpec.
type GatherSpec = sevtypes.GatherSpec

// WalkNode is a type alias for sevtypes.WalkNode.
type WalkNode = sevtypes.WalkNode

// WalkResult is a type alias for sevtypes.WalkResult.
type WalkResult = sevtypes.WalkResult

// Walk gathers graph context around a node according to the given GatherSpec.
func (k *KB) Walk(ctx context.Context, root, title string, gather GatherSpec) (*WalkResult, error) {
	subject := NodeSubject(root, title)

	// Always get the target node.
	content, _, _ := k.graph.Lookup(ctx, subject, PredNodeContent)
	role, _, _ := k.graph.Lookup(ctx, subject, PredNodeRole)
	charCount := k.lookupCharCount(ctx, subject)

	// Children titles (needed for most shapes).
	childSubjects, _ := k.graph.Compose(ctx, subject,
		graphops.ParsePath([]string{PredNodeParent + "~"}))
	childTitles, childRoles := k.resolveTitlesAndRoles(ctx, childSubjects)

	// Cross-refs.
	linkSubjects, _ := k.graph.Compose(ctx, subject,
		graphops.ParsePath([]string{PredNodeLink}))
	crossRefs := k.resolveTitleList(ctx, linkSubjects)

	result := &WalkResult{
		Target: WalkNode{
			Title:     title,
			Content:   content,
			CharCount: charCount,
			Role:      role,
			Children:  childTitles,
		},
		CrossRefs:  crossRefs,
		ChildRoles: childRoles,
	}

	// Always resolve parent title for the header.
	if p, ok, _ := k.graph.Lookup(ctx, subject, PredNodeParent); ok {
		pt, err := k.resolveTitle(ctx, p)
		if err == nil && pt != "" {
			pr, _, _ := k.graph.Lookup(ctx, p, PredNodeRole)
			parent := &WalkNode{Title: pt, Role: pr}
			if gather.Parent {
				pc, _, _ := k.graph.Lookup(ctx, p, PredNodeContent)
				parent.Content = pc
				parent.CharCount = k.lookupCharCount(ctx, p)
			}
			result.Parent = parent
		}
	}

	if !gather.Parent && !gather.Children && !gather.Siblings && !gather.Subtree {
		return result, nil
	}

	// Children with content.
	if gather.Children || gather.Subtree {
		for i, cs := range childSubjects {
			cc, _, _ := k.graph.Lookup(ctx, cs, PredNodeContent)
			cr, _, _ := k.graph.Lookup(ctx, cs, PredNodeRole)
			ccc := k.lookupCharCount(ctx, cs)

			// Get grandchildren titles for subtree display.
			var grandchildTitles []string
			if gather.Subtree {
				gcSubjects, _ := k.graph.Compose(ctx, cs,
					graphops.ParsePath([]string{PredNodeParent + "~"}))
				grandchildTitles = k.resolveTitleList(ctx, gcSubjects)
			}

			node := WalkNode{
				Title:     childTitles[i],
				Content:   cc,
				CharCount: ccc,
				Role:      cr,
				Children:  grandchildTitles,
			}
			result.Children = append(result.Children, node)
		}
	}

	// Siblings with content.
	if gather.Siblings {
		sibSubjects, _ := k.graph.Compose(ctx, subject,
			graphops.ParsePath([]string{PredNodeParent, PredNodeParent + "~"}))
		sibSubjects = exclude(sibSubjects, subject)
		sibTitles, sibRoles := k.resolveTitlesAndRoles(ctx, sibSubjects)
		result.SiblingRoles = sibRoles

		for i, ss := range sibSubjects {
			sc, _, _ := k.graph.Lookup(ctx, ss, PredNodeContent)
			sr, _, _ := k.graph.Lookup(ctx, ss, PredNodeRole)
			scc := k.lookupCharCount(ctx, ss)
			result.Siblings = append(result.Siblings, WalkNode{
				Title:     sibTitles[i],
				Content:   sc,
				CharCount: scc,
				Role:      sr,
			})
		}
	}

	// Full subtree (recursive).
	if gather.Subtree {
		allNodes, err := k.collectSubtree(ctx, root, title)
		if err == nil {
			result.SubtreeNodes = allNodes
		}
	}

	return result, nil
}

// collectSubtree gathers all nodes in the subtree rooted at the given title.
func (k *KB) collectSubtree(ctx context.Context, root, title string) ([]WalkNode, error) {
	subject := NodeSubject(root, title)
	var nodes []WalkNode

	content, _, _ := k.graph.Lookup(ctx, subject, PredNodeContent)
	role, _, _ := k.graph.Lookup(ctx, subject, PredNodeRole)
	charCount := k.lookupCharCount(ctx, subject)

	childSubjects, _ := k.graph.Compose(ctx, subject,
		graphops.ParsePath([]string{PredNodeParent + "~"}))
	childTitles := k.resolveTitleList(ctx, childSubjects)

	nodes = append(nodes, WalkNode{
		Title:     title,
		Content:   content,
		CharCount: charCount,
		Role:      role,
		Children:  childTitles,
	})

	for _, ct := range childTitles {
		subtree, err := k.collectSubtree(ctx, root, ct)
		if err != nil {
			continue
		}
		nodes = append(nodes, subtree...)
	}

	return nodes, nil
}

// Well-known gather specs for convenience.
var (
	GatherMinimal      = GatherSpec{Target: true}
	GatherNeighborhood = GatherSpec{Target: true, Parent: true, Children: true, Siblings: true}
	GatherChildren     = GatherSpec{Target: true, Children: true}
	GatherSubtree      = GatherSpec{Target: true, Subtree: true}
)

func (k *KB) lookupCharCount(ctx context.Context, subject string) int {
	cc, ok, _ := k.graph.Lookup(ctx, subject, PredNodeCharCount)
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(cc)
	return n
}
