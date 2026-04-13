package types

import (
	"context"
	"strings"

	"sevens/internal/graphops"
	"sevens/internal/kb"
)

// ConformanceResult describes how well a node matches a type.
type ConformanceResult struct {
	TypeName    string
	Conforms    bool     // all required predicates present + structure valid
	Present     []string // required predicates that are present
	Missing     []string // required predicates that are missing
	Extra       []string // predicates present but not in required/optional
	StructureOK bool     // structural constraints satisfied
}

// CheckConformance checks whether a node conforms to a type definition.
// Queries the graph for predicates on the given subject.
func CheckConformance(ctx context.Context, k *kb.KB, subject string, td *TypeDef) (*ConformanceResult, error) {
	result := &ConformanceResult{TypeName: td.Name}

	// Collect all meta/* predicates on this subject.
	allTriples, err := k.Graph().Store().BySubject(ctx, subject)
	if err != nil {
		return nil, err
	}
	metaPreds := make(map[string]bool)
	for _, t := range allTriples {
		if strings.HasPrefix(t.Predicate, "meta/") {
			key := strings.TrimPrefix(t.Predicate, "meta/")
			metaPreds[key] = true
		}
	}

	// Check required predicates.
	knownPreds := make(map[string]bool)
	for _, p := range td.Predicates.Required {
		knownPreds[p] = true
		if metaPreds[p] {
			result.Present = append(result.Present, p)
		} else {
			result.Missing = append(result.Missing, p)
		}
	}
	for _, p := range td.Predicates.Optional {
		knownPreds[p] = true
	}

	// Extra: meta predicates not in required or optional.
	for p := range metaPreds {
		if !knownPreds[p] {
			result.Extra = append(result.Extra, p)
		}
	}

	// Structural constraints.
	result.StructureOK = true

	// Parent type check.
	if td.Structure.ParentType != "" {
		parentOK, err := checkParentType(ctx, k, subject, td.Structure.ParentType)
		if err != nil {
			return nil, err
		}
		if !parentOK {
			result.StructureOK = false
		}
	}

	// Children count check.
	if td.Structure.ChildrenMin > 0 || td.Structure.ChildrenMax > 0 {
		children, err := k.Graph().Compose(ctx, subject,
			graphops.ParsePath([]string{kb.PredNodeParent + "~"}))
		if err != nil {
			return nil, err
		}
		count := len(children)
		if td.Structure.ChildrenMin > 0 && count < td.Structure.ChildrenMin {
			result.StructureOK = false
		}
		if td.Structure.ChildrenMax > 0 && count > td.Structure.ChildrenMax {
			result.StructureOK = false
		}
	}

	result.Conforms = len(result.Missing) == 0 && result.StructureOK
	return result, nil
}

// checkParentType verifies that the node's parent has all required
// predicates for the named parent type. Returns false if no parent
// exists or the parent doesn't have the required meta predicates.
//
// This is a shallow check: it loads the parent type definition and
// checks predicate presence, but does not recurse into the parent
// type's own structural constraints.
func checkParentType(ctx context.Context, k *kb.KB, subject, parentTypeName string) (bool, error) {
	parentSubj, ok, err := k.Graph().Lookup(ctx, subject, kb.PredNodeParent)
	if err != nil {
		return false, err
	}
	if !ok || parentSubj == "" {
		return false, nil // no parent at all
	}

	// Load the parent type definition.
	parentTD, err := LoadTypeDef(parentTypeName)
	if err != nil {
		// If the parent type doesn't exist, we can't verify.
		return false, nil
	}

	// Check that the parent has all required predicates.
	parentTriples, err := k.Graph().Store().BySubject(ctx, parentSubj)
	if err != nil {
		return false, err
	}
	parentMeta := make(map[string]bool)
	for _, t := range parentTriples {
		if strings.HasPrefix(t.Predicate, "meta/") {
			parentMeta[strings.TrimPrefix(t.Predicate, "meta/")] = true
		}
	}
	for _, req := range parentTD.Predicates.Required {
		if !parentMeta[req] {
			return false, nil
		}
	}
	return true, nil
}

// FindConformingNodes finds all nodes in a root that conform to a type.
func FindConformingNodes(ctx context.Context, k *kb.KB, root string, td *TypeDef) ([]string, error) {
	subjects, err := k.Graph().Store().ByPredicateObject(ctx, kb.PredNodeRoot, root)
	if err != nil {
		return nil, err
	}

	var conforming []string
	for _, subj := range subjects {
		result, err := CheckConformance(ctx, k, subj, td)
		if err != nil {
			return nil, err
		}
		if result.Conforms {
			title, _, _ := k.Graph().Lookup(ctx, subj, kb.PredNodeTitle)
			if title != "" {
				conforming = append(conforming, title)
			}
		}
	}
	return conforming, nil
}

// InferTypes checks a node against all loaded type definitions and
// returns conformance results for each type.
func InferTypes(ctx context.Context, k *kb.KB, subject string, types map[string]*TypeDef) ([]ConformanceResult, error) {
	var results []ConformanceResult
	for _, td := range types {
		result, err := CheckConformance(ctx, k, subject, td)
		if err != nil {
			return nil, err
		}
		results = append(results, *result)
	}
	return results, nil
}
