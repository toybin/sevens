// Package sevtypes defines types shared across concept boundaries.
//
// These are the data types that flow through syncs -- the exchange
// format between concepts. They live here because no single concept
// owns them; they're the vocabulary of inter-concept communication.
package sevtypes

// FileOp is a single file operation flowing from Function to Projection.
// The Function concept produces these (as transformation output).
// The Projection concept consumes them (to create/edit files).
type FileOp struct {
	Action  string            // "create" or "edit"
	Title   string            // for create: new node title
	Parent  string            // for create: parent title
	File    string            // for edit: target node title
	OldText string            // for edit: text to find
	NewText string            // for edit: replacement
	Content string            // for create: markdown body
	Extra   map[string]string // additional frontmatter
}
