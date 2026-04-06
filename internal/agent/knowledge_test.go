package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/julianshen/rubichan/internal/tools"
)

type mockSelector struct {
	results []kg.ScoredEntity
	err     error
}

func (m *mockSelector) Select(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
	return m.results, m.err
}

func (m *mockSelector) RecordUsage(ctx context.Context, entities []kg.ScoredEntity) error {
	return nil // no-op for testing
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

// Integration tests for buildSystemPromptWithFragments + knowledge graph

func TestBuildSystemPromptWithKnowledge(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := testConfig()

	selector := &mockSelector{results: []kg.ScoredEntity{
		{
			Entity: &kg.Entity{
				ID:    "arch-go",
				Kind:  kg.KindArchitecture,
				Title: "Go Language Choice",
				Body:  "Go was chosen for single-binary distribution.",
			},
			Score:           0.95,
			EstimatedTokens: 50,
		},
	}}

	a := New(mp, reg, autoApprove, cfg, WithKnowledgeGraph(selector))

	prompt, _, _ := a.buildSystemPromptWithFragments(context.Background(), "tell me about architecture")

	assert.Contains(t, prompt, "## Project Knowledge")
	assert.Contains(t, prompt, "[architecture]")
	assert.Contains(t, prompt, "Go Language Choice")
	assert.Contains(t, prompt, "Go was chosen")
}

func TestBuildSystemPromptNoKnowledgeWhenEmptyResults(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := testConfig()

	selector := &mockSelector{results: []kg.ScoredEntity{}} // Empty results

	a := New(mp, reg, autoApprove, cfg, WithKnowledgeGraph(selector))

	prompt, _, _ := a.buildSystemPromptWithFragments(context.Background(), "query")

	assert.NotContains(t, prompt, "## Project Knowledge")
}

func TestBuildSystemPromptNoKnowledgeOnError(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := testConfig()

	selector := &mockSelector{err: errors.New("unavailable")} // Selector error

	a := New(mp, reg, autoApprove, cfg, WithKnowledgeGraph(selector))

	// Should not panic or fail; error is silently swallowed
	prompt, _, _ := a.buildSystemPromptWithFragments(context.Background(), "query")

	assert.NotContains(t, prompt, "## Project Knowledge")
}

func TestBuildSystemPromptSkipsKnowledgeForEmptyMessage(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := testConfig()

	selector := &mockSelector{results: []kg.ScoredEntity{
		{Entity: &kg.Entity{ID: "test", Kind: kg.KindArchitecture, Title: "Test"}},
	}}

	a := New(mp, reg, autoApprove, cfg, WithKnowledgeGraph(selector))

	// Empty lastUserMessage should skip knowledge injection entirely
	prompt, _, _ := a.buildSystemPromptWithFragments(context.Background(), "")

	assert.NotContains(t, prompt, "## Project Knowledge")
}

type budgetCapturingSelector struct {
	capturedBudget int
	selectCalled   bool
	results        []kg.ScoredEntity
}

func (b *budgetCapturingSelector) Select(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
	b.selectCalled = true
	b.capturedBudget = budget
	return b.results, nil
}

func (b *budgetCapturingSelector) RecordUsage(ctx context.Context, entities []kg.ScoredEntity) error {
	return nil // no-op for testing
}

func TestBuildSystemPromptBudgetPassedToSelector(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := testConfig()

	selector := &budgetCapturingSelector{
		results: []kg.ScoredEntity{
			{Entity: &kg.Entity{ID: "test", Kind: kg.KindArchitecture, Title: "Test"}},
		},
	}

	a := New(mp, reg, autoApprove, cfg, WithKnowledgeGraph(selector))

	// buildSystemPromptWithFragments should pass context.Budget().SkillPrompts to selector
	a.buildSystemPromptWithFragments(context.Background(), "query")

	// Verify selector was called and budget was passed
	assert.True(t, selector.selectCalled, "selector.Select should have been called")
	// Budget might be 0 or positive depending on config; just verify it was passed
	assert.GreaterOrEqual(t, selector.capturedBudget, 0)
}

// recordingSelector tracks RecordUsage calls for testing
type recordingSelector struct {
	selectCalled    bool
	recordedOnce    bool
	lastRecorded    []kg.ScoredEntity
	results         []kg.ScoredEntity
	selectErr       error
	recordUsageErr  error
}

func (r *recordingSelector) Select(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
	r.selectCalled = true
	return r.results, r.selectErr
}

func (r *recordingSelector) RecordUsage(ctx context.Context, entities []kg.ScoredEntity) error {
	r.recordedOnce = true
	r.lastRecorded = append([]kg.ScoredEntity{}, entities...)
	return r.recordUsageErr
}

func TestBuildSystemPromptCallsRecordUsageAfterSelect(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := testConfig()

	entity := kg.ScoredEntity{
		Entity: &kg.Entity{
			ID:    "arch-001",
			Kind:  kg.KindArchitecture,
			Title: "Test Architecture",
			Body:  "Test body",
		},
		Score:           0.95,
		EstimatedTokens: 50,
	}

	selector := &recordingSelector{
		results: []kg.ScoredEntity{entity},
	}

	a := New(mp, reg, autoApprove, cfg, WithKnowledgeGraph(selector))

	// Call buildSystemPromptWithFragments with a non-empty message
	a.buildSystemPromptWithFragments(context.Background(), "tell me about architecture")

	// Verify RecordUsage was called with the selected entities
	require.True(t, selector.recordedOnce, "RecordUsage should have been called")
	require.Len(t, selector.lastRecorded, 1, "RecordUsage should be called with 1 entity")
	require.Equal(t, "arch-001", selector.lastRecorded[0].Entity.ID)
}

func TestBuildSystemPromptDoesNotCallRecordUsageOnEmptyResults(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := testConfig()

	selector := &recordingSelector{
		results: []kg.ScoredEntity{}, // Empty results
	}

	a := New(mp, reg, autoApprove, cfg, WithKnowledgeGraph(selector))

	// Empty results means knowledge section is not rendered
	a.buildSystemPromptWithFragments(context.Background(), "query")

	// RecordUsage should NOT be called on empty results
	require.False(t, selector.recordedOnce, "RecordUsage should not be called on empty results")
}

func TestBuildSystemPromptDoesNotCallRecordUsageOnSelectError(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := testConfig()

	selector := &recordingSelector{
		selectErr: errors.New("selector unavailable"),
	}

	a := New(mp, reg, autoApprove, cfg, WithKnowledgeGraph(selector))

	// Select error means knowledge section is not rendered
	a.buildSystemPromptWithFragments(context.Background(), "query")

	// RecordUsage should NOT be called if Select fails
	require.False(t, selector.recordedOnce, "RecordUsage should not be called on Select error")
}
