package knowledgegraph

import (
	"context"
	"errors"
)

// Embedder converts text to a dense vector. Implementations must be
// safe for concurrent use.
type Embedder interface {
	// Embed returns a float32 vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Dims returns the dimensionality of vectors this embedder produces.
	// Required for validating stored embeddings on schema change.
	Dims() int
}

// NullEmbedder is a no-op embedder that always returns an error.
// Used as a default when no embedder is configured, triggering FTS5 fallback.
type NullEmbedder struct{}

func (NullEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrNoEmbedder
}

func (NullEmbedder) Dims() int {
	return 0
}

// ErrNoEmbedder is returned when no embedder is configured or available.
var ErrNoEmbedder = errors.New("knowledgegraph: no embedder configured")
