package graph

import (
	"fmt"
	"sort"
)

// ConstructionNode is a minimal identity-bearing node for testing reconstruction
// and diff behavior across snapshots. IDs are stable across snapshots; parent,
// previous-sibling, and inbound links may change.
type ConstructionNode struct {
	ID        string
	ParentID  string
	PrevID    string
	InboundID string
}

// ConstructionDiff classifies how stable IDs changed between two snapshots.
type ConstructionDiff struct {
	Unchanged      []string
	Inserted       []string
	Deleted        []string
	Reparented     []string
	Reordered      []string
	InboundChanged []string
}

func indexConstruction(nodes []ConstructionNode) (map[string]ConstructionNode, error) {
	index := make(map[string]ConstructionNode, len(nodes))
	for _, node := range nodes {
		if node.ID == "" {
			return nil, fmt.Errorf("construction node ID must not be empty")
		}
		if _, exists := index[node.ID]; exists {
			return nil, fmt.Errorf("duplicate construction node ID %q", node.ID)
		}
		index[node.ID] = node
	}
	return index, nil
}

func sortStrings(values []string) {
	sort.Strings(values)
}

// DiffConstruction compares two snapshots of a construction using stable node
// IDs. It assumes identity has already been resolved; this function classifies
// how those identities moved or changed structurally.
func DiffConstruction(oldNodes, newNodes []ConstructionNode) (ConstructionDiff, error) {
	oldIndex, err := indexConstruction(oldNodes)
	if err != nil {
		return ConstructionDiff{}, err
	}
	newIndex, err := indexConstruction(newNodes)
	if err != nil {
		return ConstructionDiff{}, err
	}

	var diff ConstructionDiff

	for id, oldNode := range oldIndex {
		newNode, exists := newIndex[id]
		if !exists {
			diff.Deleted = append(diff.Deleted, id)
			continue
		}

		if oldNode.ParentID == newNode.ParentID &&
			oldNode.PrevID == newNode.PrevID &&
			oldNode.InboundID == newNode.InboundID {
			diff.Unchanged = append(diff.Unchanged, id)
			continue
		}

		if oldNode.ParentID != newNode.ParentID {
			diff.Reparented = append(diff.Reparented, id)
		}
		if oldNode.PrevID != newNode.PrevID {
			diff.Reordered = append(diff.Reordered, id)
		}
		if oldNode.InboundID != newNode.InboundID {
			diff.InboundChanged = append(diff.InboundChanged, id)
		}
	}

	for id := range newIndex {
		if _, exists := oldIndex[id]; !exists {
			diff.Inserted = append(diff.Inserted, id)
		}
	}

	sortStrings(diff.Unchanged)
	sortStrings(diff.Inserted)
	sortStrings(diff.Deleted)
	sortStrings(diff.Reparented)
	sortStrings(diff.Reordered)
	sortStrings(diff.InboundChanged)
	return diff, nil
}
