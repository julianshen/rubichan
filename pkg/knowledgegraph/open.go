package knowledgegraph

import (
	"context"
)

// Open creates or opens the knowledge graph at the given project root.
// The SQLite index is stored at .knowledge/.index.db (not committed).
// Markdown files are read from/written to .knowledge/ (committed to git).
//
// This function is implemented by internal/knowledgegraph.
// We use a package-level variable to avoid circular imports.
func Open(ctx context.Context, projectRoot string, opts ...Option) (Graph, error) {
	if openImpl == nil {
		panic("knowledgegraph: Open not initialized (internal/knowledgegraph must be imported)")
	}
	return openImpl(ctx, projectRoot, opts)
}

// openImpl is set by internal/knowledgegraph.
var openImpl func(context.Context, string, []Option) (Graph, error)

// RegisterOpenImpl is called by internal/knowledgegraph to set the implementation.
func RegisterOpenImpl(fn func(context.Context, string, []Option) (Graph, error)) {
	openImpl = fn
}
