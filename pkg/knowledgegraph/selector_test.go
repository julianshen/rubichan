package knowledgegraph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockSelector is a test implementation of ContextSelector
type mockSelector struct {
	results []ScoredEntity
}

func (m *mockSelector) Select(ctx context.Context, query string, budget int) ([]ScoredEntity, error) {
	return m.results, nil
}

func TestContextSelector(t *testing.T) {
	m := &mockSelector{
		results: []ScoredEntity{
			{
				Entity: &Entity{ID: "test-001", Title: "Test"},
				Score: 0.95,
				EstimatedTokens: 100,
			},
		},
	}

	results, err := m.Select(context.Background(), "test query", 2000)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "test-001", results[0].Entity.ID)
}
