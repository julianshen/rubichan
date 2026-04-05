package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

type mockSelector struct {
	results []kg.ScoredEntity
	err     error
}

func (m *mockSelector) Select(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
	return m.results, m.err
}

func TestWithKnowledgeGraph(t *testing.T) {
	selector := &mockSelector{}
	opt := WithKnowledgeGraph(selector)

	// Create a minimal agent to apply the option
	agent := &Agent{
		conversation: NewConversation("test"),
	}

	opt(agent)
	require.Equal(t, selector, agent.knowledgeSelector)
}

func TestRenderKnowledgeSection(t *testing.T) {
	entities := []kg.ScoredEntity{
		{
			Entity: &kg.Entity{
				ID:    "arch-go",
				Kind:  kg.KindArchitecture,
				Title: "Go Language Choice",
				Body:  "Go was chosen for single-binary distribution.",
				Relationships: []kg.Relationship{
					{
						Kind:   kg.RelJustifies,
						Target: "module-core",
					},
				},
			},
			Score:           0.95,
			EstimatedTokens: 50,
		},
	}

	rendered := renderKnowledgeSection(entities)
	require.NotEmpty(t, rendered)
	require.Contains(t, rendered, "## Project Knowledge")
	require.Contains(t, rendered, "[architecture]")
	require.Contains(t, rendered, "Go Language Choice")
	require.Contains(t, rendered, "Go was chosen")
	require.Contains(t, rendered, "justifies: module-core")
}

func TestRenderKnowledgeSectionEmpty(t *testing.T) {
	rendered := renderKnowledgeSection([]kg.ScoredEntity{})
	require.Empty(t, rendered)
}

func TestRenderKnowledgeSectionMultiple(t *testing.T) {
	entities := []kg.ScoredEntity{
		{
			Entity: &kg.Entity{
				ID:    "entity-1",
				Kind:  kg.KindDecision,
				Title: "First Decision",
				Body:  "This was decided.",
			},
		},
		{
			Entity: &kg.Entity{
				ID:    "entity-2",
				Kind:  kg.KindModule,
				Title: "Second Module",
				Body:  "This is a module.",
			},
		},
	}

	rendered := renderKnowledgeSection(entities)
	require.Contains(t, rendered, "First Decision")
	require.Contains(t, rendered, "Second Module")
	require.Contains(t, rendered, "[decision]")
	require.Contains(t, rendered, "[module]")
}

func TestRenderKnowledgeSectionNoRelationships(t *testing.T) {
	entities := []kg.ScoredEntity{
		{
			Entity: &kg.Entity{
				ID:            "arch-1",
				Kind:          kg.KindArchitecture,
				Title:         "Architecture",
				Body:          "Description",
				Relationships: []kg.Relationship{}, // Empty relationships
			},
		},
	}

	rendered := renderKnowledgeSection(entities)
	require.NotContains(t, rendered, "Relationships")
}
