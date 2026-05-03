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

func (m *mockSelector) RecordUsage(ctx context.Context, entities []ScoredEntity) error {
	return nil
}

func TestSelectorKinds(t *testing.T) {
	// Verify all selector kinds are defined
	kinds := []SelectorKind{
		SelectorByScore,
		SelectorByRecency,
		SelectorByUsage,
		SelectorByConfidence,
	}

	for _, kind := range kinds {
		require.NotEmpty(t, kind)
	}
}

func TestSelectorConfig(t *testing.T) {
	cfg := SelectorConfig{
		Kind:   SelectorByUsage,
		Budget: 2000,
		Limit:  10,
	}

	require.Equal(t, SelectorByUsage, cfg.Kind)
	require.Equal(t, 2000, cfg.Budget)
	require.Equal(t, 10, cfg.Limit)
}

func TestContextSelector(t *testing.T) {
	m := &mockSelector{
		results: []ScoredEntity{
			{
				Entity:          &Entity{ID: "test-001", Title: "Test"},
				Score:           0.95,
				EstimatedTokens: 100,
			},
		},
	}

	results, err := m.Select(context.Background(), "test query", 2000)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "test-001", results[0].Entity.ID)
}
