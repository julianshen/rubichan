package knowledgegraph

import "context"

// Open creates or opens the knowledge graph at the given project root.
// The SQLite index is stored at .knowledge/.index.db (not committed).
// Markdown files are read from/written to .knowledge/ (committed to git).
//
// This function delegates to the internal implementation to avoid
// exposing concrete types that depend on sqlite or http clients.
func Open(ctx context.Context, projectRoot string, opts ...Option) (Graph, error) {
	// This will be implemented by internal/knowledgegraph.Open()
	// and must return a Graph-implementing type that is opaque to the caller.
	panic("not implemented: internal/knowledgegraph.Open() must be wired here")
}
