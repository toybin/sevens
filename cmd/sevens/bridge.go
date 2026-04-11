package main

// bridge.go provides helpers for the new package architecture.
// Commands can be migrated one at a time from old packages (store,
// graph, apply, engine) to new packages (triple, graphops, kb,
// function, projection) by switching to these helpers.

import (
	"fmt"

	"sevens/internal/graphops"
	"sevens/internal/kb"
	"sevens/internal/projection/md"
	"sevens/internal/triple"
)

// kbStack holds the full initialized stack so callers don't have to
// manage three layers of initialization.
type kbStack struct {
	Store      *triple.Store
	Graph      *graphops.Graph
	KB         *kb.KB
	close      func()
}

// openKB creates the full Layer 1-3 stack using the existing sevens
// database. Returns a kbStack whose Close() must be deferred.
func openKB() (*kbStack, error) {
	// Reuse the existing openDB() which handles config dir, path,
	// WAL mode, etc. The new triple.Store wraps the same *sql.DB.
	db, err := openDB()
	if err != nil {
		return nil, err
	}

	store, err := triple.New(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initialising triple store: %w", err)
	}

	graph := graphops.New(store)
	k := kb.New(graph)

	return &kbStack{
		Store: store,
		Graph: graph,
		KB:    k,
		close: func() { db.Close() },
	}, nil
}

func (s *kbStack) Close() {
	if s.close != nil {
		s.close()
	}
}

// openProjection creates a markdown projection backed by a kbStack.
func openProjection(stack *kbStack) *md.MarkdownProjection {
	return md.New(stack.KB, stack.Store)
}
