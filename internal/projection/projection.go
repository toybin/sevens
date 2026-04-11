// Package projection defines the contract that all presentational
// surfaces must satisfy: the natural transformation between graph
// state and human-editable forms.
//
// This package contains only the interface and shared types.
// Implementations live in sub-packages (e.g., projection/md).
package projection

import (
	"context"

	"sevens/internal/sevtypes"
)

// FileOp is re-exported from sevtypes for convenience.
type FileOp = sevtypes.FileOp

// Projection is the contract for a presentational surface.
// Transparent to the user -- they edit files, not projections.
type Projection interface {
	// Sync reads the presentational surface, parses everything,
	// reconciles against current graph state, and applies changes.
	Sync(ctx context.Context, root string) (*SyncResult, error)

	// Write renders a single node from graph state to the surface.
	Write(ctx context.Context, root, nodeTitle string) error

	// WriteAll renders all nodes in a root to the surface.
	WriteAll(ctx context.Context, root string) error

	// ApplyOps executes file operations against the surface.
	ApplyOps(ctx context.Context, root string, ops []FileOp) (*ApplyResult, error)

	// Commit records the current surface state in version control.
	// Returns a commit reference (e.g., git short hash). No-op for
	// surfaces without version control.
	Commit(ctx context.Context, root, message string) (string, error)

	// Revert undoes a previous commit.
	Revert(ctx context.Context, root, commitRef string) error

	// HasChanges returns true if the surface has uncommitted changes.
	HasChanges(ctx context.Context, root string) (bool, error)
}

// SyncResult summarizes what changed during sync.
type SyncResult struct {
	NodesScanned   int
	TriplesWritten int
	Errors         []string
}

// ApplyResult summarizes file operations applied.
type ApplyResult struct {
	FilesCreated []string
	FilesEdited  []string
}
