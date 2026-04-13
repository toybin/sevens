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
	Action  string            `json:"action"`            // "create" or "edit"
	Title   string            `json:"title,omitempty"`   // for create: new node title
	Parent  string            `json:"parent,omitempty"`  // for create: parent title
	File    string            `json:"file,omitempty"`    // for edit: target node title
	OldText string            `json:"old_text,omitempty"` // for edit: text to find
	NewText string            `json:"new_text,omitempty"` // for edit: replacement
	Content string            `json:"content,omitempty"` // for create: markdown body
	Extra   map[string]string `json:"extra,omitempty"`   // additional frontmatter
}
