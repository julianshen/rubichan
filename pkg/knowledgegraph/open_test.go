package knowledgegraph

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegisterOpenImplAndOpen verifies the registration/dispatch mechanism
// used by internal/knowledgegraph to wire the concrete Open implementation
// into the public SDK without causing an import cycle.
func TestRegisterOpenImplAndOpen(t *testing.T) {
	// Save and restore the original implementation to avoid test interference.
	orig := openImpl
	t.Cleanup(func() { openImpl = orig })

	var (
		gotRoot string
		gotOpts []Option
	)
	stub := &mockGraph{entities: make(map[string]*Entity)}

	RegisterOpenImpl(func(_ context.Context, root string, opts []Option) (Graph, error) {
		gotRoot = root
		gotOpts = opts
		return stub, nil
	})

	g, err := Open(context.Background(), "/tmp/project",
		WithKnowledgeDir(".knowledge"),
		WithDBPath("/tmp/index.db"),
	)
	require.NoError(t, err)
	require.NotNil(t, g)
	assert.Same(t, stub, g)
	assert.Equal(t, "/tmp/project", gotRoot)
	assert.Len(t, gotOpts, 2)
}

// TestOpenPropagatesError ensures errors from the underlying implementation
// are returned to the caller unchanged.
func TestOpenPropagatesError(t *testing.T) {
	orig := openImpl
	t.Cleanup(func() { openImpl = orig })

	sentinel := errors.New("boom")
	RegisterOpenImpl(func(_ context.Context, _ string, _ []Option) (Graph, error) {
		return nil, sentinel
	})

	g, err := Open(context.Background(), "/tmp/x")
	assert.Nil(t, g)
	assert.ErrorIs(t, err, sentinel)
}

// TestOpenPanicsWhenNotInitialized verifies that calling Open before any
// implementation is registered produces a clear panic message.
func TestOpenPanicsWhenNotInitialized(t *testing.T) {
	orig := openImpl
	t.Cleanup(func() { openImpl = orig })

	openImpl = nil
	assert.PanicsWithValue(t,
		"knowledgegraph: Open not initialized (internal/knowledgegraph must be imported)",
		func() {
			_, _ = Open(context.Background(), "/tmp/x")
		},
	)
}
