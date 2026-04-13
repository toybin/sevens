package function

import (
	"context"
	"fmt"
	"strconv"

	"sevens/internal/kb"
	"sevens/internal/types"
)

// Predicate is a condition that can be evaluated against the graph.
type Predicate struct {
	Kind string   // "child-exists", "has-content", "children-count", "conforms", "otherwise"
	Args []string // predicate-specific arguments
}

// MatchClause is one branch of a match expression.
type MatchClause struct {
	Predicate Predicate
	Result    string // type name to use if predicate matches
}

// EvalMatch evaluates match clauses against the graph and returns the first
// matching type name. Clauses are evaluated in order; the first clause whose
// predicate is satisfied wins.
//
// Parameters:
//   - root: the KB root (workspace) containing the node
//   - nodeTitle: the title of the node being evaluated
//   - clauses: ordered list of match clauses
//
// Returns the result type name of the first matching clause, or an error if
// no clause matches.
func EvalMatch(ctx context.Context, k *kb.KB, root, nodeTitle string, clauses []MatchClause) (string, error) {
	for i, clause := range clauses {
		matched, err := evalPredicate(ctx, k, root, nodeTitle, clause.Predicate)
		if err != nil {
			return "", fmt.Errorf("evaluating predicate %d (%s): %w", i, clause.Predicate.Kind, err)
		}
		if matched {
			return clause.Result, nil
		}
	}
	return "", fmt.Errorf("no match clause matched for node %q", nodeTitle)
}

// evalPredicate dispatches to the appropriate predicate evaluator.
func evalPredicate(ctx context.Context, k *kb.KB, root, nodeTitle string, pred Predicate) (bool, error) {
	switch pred.Kind {
	case "child-exists":
		return evalChildExists(ctx, k, root, nodeTitle, pred.Args)
	case "has-content":
		return evalHasContent(ctx, k, root, nodeTitle)
	case "children-count":
		return evalChildrenCount(ctx, k, root, nodeTitle, pred.Args)
	case "conforms":
		return evalConforms(ctx, k, root, nodeTitle, pred.Args)
	case "otherwise":
		return true, nil
	default:
		return false, fmt.Errorf("unknown predicate kind %q", pred.Kind)
	}
}

// evalChildExists checks if a child with the given title exists under the node.
// Args[0] is the expected child title (may contain template variables, but
// those should be resolved before calling EvalMatch).
func evalChildExists(ctx context.Context, k *kb.KB, root, nodeTitle string, args []string) (bool, error) {
	if len(args) < 1 {
		return false, fmt.Errorf("child-exists predicate requires a child title argument")
	}
	childTitle := args[0]

	children, err := k.Children(ctx, root, nodeTitle)
	if err != nil {
		return false, err
	}

	for _, c := range children {
		if c == childTitle {
			return true, nil
		}
	}
	return false, nil
}

// evalHasContent checks if the node has non-empty content.
func evalHasContent(ctx context.Context, k *kb.KB, root, nodeTitle string) (bool, error) {
	subject := kb.NodeSubject(root, nodeTitle)
	content, ok, err := k.Graph().Lookup(ctx, subject, kb.PredNodeContent)
	if err != nil {
		return false, err
	}
	return ok && content != "", nil
}

// evalChildrenCount checks if the node's child count meets a threshold.
// Args[0] is the count to compare against.
// Args[1] (optional) is the comparison operator: "eq" (default), "gte", "lte", "gt", "lt".
func evalChildrenCount(ctx context.Context, k *kb.KB, root, nodeTitle string, args []string) (bool, error) {
	if len(args) < 1 {
		return false, fmt.Errorf("children-count predicate requires a count argument")
	}
	threshold, err := strconv.Atoi(args[0])
	if err != nil {
		return false, fmt.Errorf("children-count: invalid count %q: %w", args[0], err)
	}

	op := "eq"
	if len(args) >= 2 {
		op = args[1]
	}

	children, err := k.Children(ctx, root, nodeTitle)
	if err != nil {
		return false, err
	}
	count := len(children)

	switch op {
	case "eq":
		return count == threshold, nil
	case "gte":
		return count >= threshold, nil
	case "lte":
		return count <= threshold, nil
	case "gt":
		return count > threshold, nil
	case "lt":
		return count < threshold, nil
	default:
		return false, fmt.Errorf("children-count: unknown operator %q", op)
	}
}

// evalConforms checks if a node conforms to a named type definition.
// This does basic structural conformance: checks that all required predicates
// from the type definition are present as frontmatter on the node.
// Args[0] is the type name.
func evalConforms(ctx context.Context, k *kb.KB, root, nodeTitle string, args []string) (bool, error) {
	if len(args) < 1 {
		return false, fmt.Errorf("conforms predicate requires a type name argument")
	}
	typeName := args[0]

	td, err := types.LoadTypeDef(typeName)
	if err != nil {
		return false, fmt.Errorf("loading type %q: %w", typeName, err)
	}

	// For now, conformance checks that required predicates exist.
	// Predicates are stored as meta/* triples on the node subject.
	subject := kb.NodeSubject(root, nodeTitle)
	for _, pred := range td.Predicates.Required {
		metaPred := "meta/" + pred
		if _, ok, _ := k.Graph().Lookup(ctx, subject, metaPred); !ok {
			return false, nil
		}
	}

	// Check structural constraint: parent type.
	if td.Structure.ParentType != "" {
		parentTitle, err := k.Parent(ctx, root, nodeTitle)
		if err != nil || parentTitle == nil {
			return false, nil
		}
		parentConforms, err := evalConforms(ctx, k, root, *parentTitle, []string{td.Structure.ParentType})
		if err != nil || !parentConforms {
			return false, nil
		}
	}

	return true, nil
}
