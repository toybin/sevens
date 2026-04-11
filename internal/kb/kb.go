package kb

import (
	"context"
	"fmt"
	"strconv"

	"sevens/internal/graphops"
	"sevens/internal/triple"
)

// KB is the sevens knowledge base.
type KB struct {
	graph *graphops.Graph
}

// New creates a KB and registers all sevens predicate specs.
func New(graph *graphops.Graph) *KB {
	for _, spec := range allSpecs() {
		graph.RegisterPredicate(spec)
	}
	return &KB{graph: graph}
}

// Graph returns the underlying graphops.Graph.
func (k *KB) Graph() *graphops.Graph { return k.graph }

// --- Node operations ---

// CreateNode creates a node with the given title, content, and optional parent.
// Returns the computed subject.
func (k *KB) CreateNode(ctx context.Context, root, title, content string, parent *string) (string, error) {
	subject := NodeSubject(root, title)

	if err := k.graph.Set(ctx, subject, PredNodeTitle, title); err != nil {
		return "", fmt.Errorf("kb: create node: %w", err)
	}
	if err := k.graph.Set(ctx, subject, PredNodeRoot, root); err != nil {
		return "", err
	}
	if err := k.graph.Set(ctx, subject, PredNodeContent, content); err != nil {
		return "", err
	}
	if err := k.graph.Set(ctx, subject, PredNodeCharCount, strconv.Itoa(len(content))); err != nil {
		return "", err
	}
	if parent != nil {
		parentSubject := NodeSubject(root, *parent)
		if err := k.graph.Set(ctx, subject, PredNodeParent, parentSubject); err != nil {
			return "", err
		}
	}
	return subject, nil
}

// DeleteNode removes all triples for a node.
func (k *KB) DeleteNode(ctx context.Context, root, title string) error {
	subject := NodeSubject(root, title)
	return k.graph.Store().RetractBySubject(ctx, subject)
}

// SetContent updates a node's content and char count.
func (k *KB) SetContent(ctx context.Context, root, title, content string) error {
	subject := NodeSubject(root, title)
	if err := k.graph.Set(ctx, subject, PredNodeContent, content); err != nil {
		return err
	}
	return k.graph.Set(ctx, subject, PredNodeCharCount, strconv.Itoa(len(content)))
}

// MoveNode changes a node's parent.
func (k *KB) MoveNode(ctx context.Context, root, title, newParentTitle string) error {
	subject := NodeSubject(root, title)
	parentSubject := NodeSubject(root, newParentTitle)
	return k.graph.Set(ctx, subject, PredNodeParent, parentSubject)
}

// LinkNodes creates a cross-reference between two nodes.
func (k *KB) LinkNodes(ctx context.Context, root, sourceTitle, targetTitle string) error {
	src := NodeSubject(root, sourceTitle)
	tgt := NodeSubject(root, targetTitle)
	return k.graph.Store().Assert(ctx, triple.Triple{
		Subject: src, Predicate: PredNodeLink, Object: tgt,
	})
}

// UnlinkNodes removes a cross-reference.
func (k *KB) UnlinkNodes(ctx context.Context, root, sourceTitle, targetTitle string) error {
	src := NodeSubject(root, sourceTitle)
	tgt := NodeSubject(root, targetTitle)
	return k.graph.Store().Retract(ctx, triple.Triple{
		Subject: src, Predicate: PredNodeLink, Object: tgt,
	})
}

// SetRole sets a node's sibling role.
func (k *KB) SetRole(ctx context.Context, root, title, role string) error {
	subject := NodeSubject(root, title)
	return k.graph.Store().Assert(ctx, triple.Triple{
		Subject: subject, Predicate: PredNodeRole, Object: role,
	})
}

// ClearRoot retracts all node and block triples belonging to a root.
func (k *KB) ClearRoot(ctx context.Context, root string) error {
	if err := k.graph.RetractSubgraph(ctx, PredNodeRoot, root); err != nil {
		return err
	}
	return k.graph.RetractSubgraph(ctx, PredBlockRoot, root)
}
