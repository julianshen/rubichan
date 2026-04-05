package knowledgegraph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNullEmbedder(t *testing.T) {
	e := NullEmbedder{}

	result, err := e.Embed(context.Background(), "test")
	require.Equal(t, ErrNoEmbedder, err)
	require.Nil(t, result)

	require.Equal(t, 0, e.Dims())
}

// mockEmbedder is a test implementation
type mockEmbedder struct {
	dims    int
	vectors map[string][]float32
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if v, ok := m.vectors[text]; ok {
		return v, nil
	}
	return make([]float32, m.dims), nil
}

func (m *mockEmbedder) Dims() int {
	return m.dims
}

func TestMockEmbedder(t *testing.T) {
	m := &mockEmbedder{
		dims: 768,
		vectors: map[string][]float32{
			"hello": {0.1, 0.2, 0.3},
		},
	}

	result, err := m.Embed(context.Background(), "hello")
	require.NoError(t, err)
	require.Equal(t, []float32{0.1, 0.2, 0.3}, result)

	result, err = m.Embed(context.Background(), "unknown")
	require.NoError(t, err)
	require.Len(t, result, 768)

	require.Equal(t, 768, m.Dims())
}
