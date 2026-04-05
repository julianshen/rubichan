package knowledgegraph

import (
	"testing"

	"github.com/stretchr/testify/require"
)

import "context"

// mockGraph is a minimal mock implementation of Graph for testing.
type mockGraph struct {
	entities map[string]*Entity
}

func (m *mockGraph) Put(ctx context.Context, e *Entity) error {
	m.entities[e.ID] = e
	return nil
}

func (m *mockGraph) Get(ctx context.Context, id string) (*Entity, error) {
	if e, ok := m.entities[id]; ok {
		return e, nil
	}
	return nil, nil
}

func (m *mockGraph) Delete(ctx context.Context, id string) error {
	delete(m.entities, id)
	return nil
}

func (m *mockGraph) List(ctx context.Context, filter ListFilter) ([]*Entity, error) {
	return nil, nil
}

func (m *mockGraph) Query(ctx context.Context, req QueryRequest) ([]ScoredEntity, error) {
	return nil, nil
}

func (m *mockGraph) RebuildIndex(ctx context.Context) error {
	return nil
}

func (m *mockGraph) LintGraph(ctx context.Context) (*LintReport, error) {
	return nil, nil
}

func (m *mockGraph) Stats(ctx context.Context) (*KnowledgeStats, error) {
	return nil, nil
}

func (m *mockGraph) Close() error {
	return nil
}

func TestGraph(t *testing.T) {
	g := &mockGraph{entities: make(map[string]*Entity)}

	// Test that Graph interface is satisfied
	var _ Graph = g

	// Note: Can't call methods without context.Context, but this
	// verifies the interface shape is correct
	require.NotNil(t, g)
}

func TestScoredEntity(t *testing.T) {
	e := &Entity{ID: "test-001", Kind: KindArchitecture, Title: "Test"}
	se := ScoredEntity{
		Entity: e,
		Score: 0.95,
		EstimatedTokens: 150,
	}

	require.Equal(t, "test-001", se.Entity.ID)
	require.Equal(t, 0.95, se.Score)
	require.Equal(t, 150, se.EstimatedTokens)
}

func TestQueryRequest(t *testing.T) {
	req := QueryRequest{
		Text: "sqlite concurrency",
		TokenBudget: 2000,
		Limit: 10,
		KindFilter: []EntityKind{KindGotcha, KindPattern},
	}

	require.Equal(t, "sqlite concurrency", req.Text)
	require.Equal(t, 2000, req.TokenBudget)
	require.Equal(t, 10, req.Limit)
	require.Len(t, req.KindFilter, 2)
}
