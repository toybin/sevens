package kb

import (
	"crypto/sha1"
	"fmt"
)

// NodeSubject computes the canonical subject string for a node.
// Format: node:<6-byte-sha1-of-root>:<title>
//
// Constructable from (root, title) without querying the graph.
func NodeSubject(root, title string) string {
	return fmt.Sprintf("node:%s:%s", RootHash(root), title)
}

// BlockSubject computes the canonical subject string for a block.
// Format: block:<6-byte-sha1-of-root>:<nodeTitle>:<path>
func BlockSubject(root, nodeTitle, path string) string {
	return fmt.Sprintf("block:%s:%s:%s", RootHash(root), nodeTitle, path)
}

// RootHash returns the first 6 bytes of the SHA1 of root, hex-encoded.
func RootHash(root string) string {
	h := sha1.Sum([]byte(root))
	return fmt.Sprintf("%x", h[:6])
}
