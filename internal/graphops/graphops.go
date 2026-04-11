// Package graphops is Layer 2: predicate metadata and graph-conscious
// operations over a triple store.
//
// Knows about predicate properties (functional vs. relational, inverses)
// and path composition, but nothing about sevens, PKM, or any specific
// predicate vocabulary. Reusable outside sevens.
package graphops

import (
	"context"
	"fmt"
	"strings"

	"sevens/internal/triple"
)

// Multiplicity describes how many objects a predicate allows per subject.
type Multiplicity int

const (
	Relational Multiplicity = iota // zero or more
	Functional                     // at most one
)

// PredicateSpec declares properties of a predicate.
type PredicateSpec struct {
	Name         string
	Multiplicity Multiplicity
	Inverse      string // name used when traversing backward; empty if none
	Symmetric    bool
	Transitive   bool
}

// Graph wraps a triple.Store with predicate metadata and provides
// higher-level graph operations.
type Graph struct {
	store *triple.Store
	specs map[string]*PredicateSpec
}

// New creates a Graph backed by the given triple store.
func New(store *triple.Store) *Graph {
	return &Graph{
		store: store,
		specs: make(map[string]*PredicateSpec),
	}
}

// Store returns the underlying triple store.
func (g *Graph) Store() *triple.Store { return g.store }

// RegisterPredicate declares a predicate's metadata.
func (g *Graph) RegisterPredicate(spec PredicateSpec) {
	g.specs[spec.Name] = &spec
}

// Spec returns the registered spec for a predicate, or nil if unregistered.
func (g *Graph) Spec(predicate string) *PredicateSpec {
	return g.specs[predicate]
}

// Set writes a value for a functional predicate: retracts any existing
// value for (subject, predicate), then asserts the new one.
func (g *Graph) Set(ctx context.Context, subject, predicate, object string) error {
	// Retract existing values for this (subject, predicate)
	existing, err := g.store.BySubjectPredicate(ctx, subject, predicate)
	if err != nil {
		return fmt.Errorf("graphops: set: %w", err)
	}
	for _, old := range existing {
		if err := g.store.Retract(ctx, triple.Triple{
			Subject: subject, Predicate: predicate, Object: old,
		}); err != nil {
			return fmt.Errorf("graphops: set retract: %w", err)
		}
	}
	return g.store.Assert(ctx, triple.Triple{
		Subject: subject, Predicate: predicate, Object: object,
	})
}

// Lookup returns the single object for a (subject, predicate) pair.
// Returns ("", false, nil) if no value exists.
func (g *Graph) Lookup(ctx context.Context, subject, predicate string) (string, bool, error) {
	vals, err := g.store.BySubjectPredicate(ctx, subject, predicate)
	if err != nil {
		return "", false, err
	}
	if len(vals) == 0 {
		return "", false, nil
	}
	return vals[0], true, nil
}

// --- Path composition ---

// PathStep is one hop in a composed path.
type PathStep struct {
	Predicate string
	Inverse   bool
}

// ParsePath converts string predicates into typed PathSteps.
// A trailing "~" means inverse traversal.
func ParsePath(predicates []string) []PathStep {
	steps := make([]PathStep, len(predicates))
	for i, p := range predicates {
		if strings.HasSuffix(p, "~") {
			steps[i] = PathStep{Predicate: p[:len(p)-1], Inverse: true}
		} else {
			steps[i] = PathStep{Predicate: p, Inverse: false}
		}
	}
	return steps
}

// Compose walks a sequence of predicate hops from a starting subject,
// returning all subjects/objects reachable at the end of the path.
// This is arrow composition.
func (g *Graph) Compose(ctx context.Context, start string, path []PathStep) ([]string, error) {
	current := []string{start}
	for _, step := range path {
		var next []string
		for _, subj := range current {
			var results []string
			var err error
			if step.Inverse {
				results, err = g.store.ByPredicateObject(ctx, step.Predicate, subj)
			} else {
				results, err = g.store.BySubjectPredicate(ctx, subj, step.Predicate)
			}
			if err != nil {
				return nil, fmt.Errorf("graphops: compose step %q: %w", step.Predicate, err)
			}
			next = append(next, results...)
		}
		current = deduplicate(next)
	}
	return current, nil
}

// Reachable returns all subjects reachable from start by recursively
// following a predicate (forward or inverse). Includes start itself.
func (g *Graph) Reachable(ctx context.Context, start, predicate string, inverse bool) ([]string, error) {
	visited := map[string]struct{}{start: {}}
	queue := []string{start}
	var result []string

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		result = append(result, curr)

		var neighbors []string
		var err error
		if inverse {
			neighbors, err = g.store.ByPredicateObject(ctx, predicate, curr)
		} else {
			neighbors, err = g.store.BySubjectPredicate(ctx, curr, predicate)
		}
		if err != nil {
			return nil, err
		}
		for _, n := range neighbors {
			if _, seen := visited[n]; !seen {
				visited[n] = struct{}{}
				queue = append(queue, n)
			}
		}
	}
	return result, nil
}

// RetractSubgraph retracts all triples for subjects that belong to a
// root, identified by a membership predicate. This is "clear everything
// belonging to this root" generalized.
func (g *Graph) RetractSubgraph(ctx context.Context, membershipPredicate, root string) error {
	members, err := g.store.ByPredicateObject(ctx, membershipPredicate, root)
	if err != nil {
		return err
	}
	for _, subj := range members {
		if err := g.store.RetractBySubject(ctx, subj); err != nil {
			return err
		}
	}
	return nil
}

func deduplicate(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ss))
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}
