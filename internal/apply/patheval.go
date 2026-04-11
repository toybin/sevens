package apply

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"

	"sevens/internal/store"
)

// PathResult holds the terminal objects reached by evaluating a path.
type PathResult struct {
	As    string         // template variable name
	Nodes []ResolvedNode // terminal objects with requested predicates
}

// EvalPath evaluates a single path spec starting from the given subject.
func EvalPath(db *sql.DB, startSubject string, spec PathSpec) (*PathResult, error) {
	// Start with a set of current subjects
	current := []string{startSubject}

	for _, pred := range spec.Path {
		inverse := strings.HasSuffix(pred, "~")
		if inverse {
			pred = strings.TrimSuffix(pred, "~")
		}

		var next []string
		for _, subj := range current {
			if inverse {
				// Find subjects that have this predicate pointing to subj
				results, err := store.GetSubjects(db, pred, subj)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[path] error traversing %s~ from %s: %v\n", pred, subj, err)
					continue
				}
				next = append(next, results...)
			} else {
				// Follow predicate forward: subj → objects
				results, err := store.GetObjects(db, subj, pred)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[path] error traversing %s from %s: %v\n", pred, subj, err)
					continue
				}
				next = append(next, results...)
			}
		}
		current = next
	}

	// Exclude self if requested
	if spec.ExcludeSelf {
		filtered := make([]string, 0, len(current))
		for _, s := range current {
			if s != startSubject {
				filtered = append(filtered, s)
			}
		}
		current = filtered
	}

	// Deduplicate and sort
	seen := make(map[string]bool)
	var unique []string
	for _, s := range current {
		if !seen[s] {
			seen[s] = true
			unique = append(unique, s)
		}
	}
	sort.Strings(unique)

	// Fetch requested predicates from terminal nodes
	var nodes []ResolvedNode
	for _, subject := range unique {
		title, err := store.NodeTitle(db, subject)
		if err != nil || title == "" {
			title = subject
		}
		node := ResolvedNode{Title: title}
		// Always fetch sibling/role if it exists
		role, _ := store.GetObject(db, subject, "sibling/role")
		node.Role = role
		for _, withPred := range spec.With {
			val, err := store.GetObject(db, subject, withPred)
			if err != nil || val == "" {
				continue
			}
			if withPred == "node/content" {
				node.Content = val
			}
		}
		nodes = append(nodes, node)
	}

	return &PathResult{As: spec.As, Nodes: nodes}, nil
}

// EvalPaths evaluates all path specs for a function and returns results keyed by :as name.
func EvalPaths(db *sql.DB, startSubject string, specs []PathSpec) (map[string]*PathResult, error) {
	results := make(map[string]*PathResult)
	for _, spec := range specs {
		result, err := EvalPath(db, startSubject, spec)
		if err != nil {
			return nil, fmt.Errorf("evaluating path %q: %w", spec.As, err)
		}
		results[spec.As] = result
	}
	return results, nil
}
