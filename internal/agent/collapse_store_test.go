package agent

import (
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestNewCollapseStore(t *testing.T) {
	s := NewCollapseStore(true)
	require.True(t, s.IsEnabled())
	stats := s.GetStats()
	require.Equal(t, 0, stats.TotalCommits)
	require.Equal(t, 0, stats.StagedCount)
	require.True(t, stats.IsEnabled)
}

func TestCollapseStoreStage(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{
		StartUUID: "msg-1",
		EndUUID:   "msg-3",
		Summary:   "summary text",
		StagedAt:  time.Now(),
	})
	stats := s.GetStats()
	require.Equal(t, 1, stats.StagedCount)
	require.Equal(t, 0, stats.TotalCommits)
}

func TestCollapseStoreCommit(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{
		StartUUID: "msg-1",
		EndUUID:   "msg-3",
		Summary:   "first summary",
	})
	s.Stage(CollapseStagedSpan{
		StartUUID: "msg-4",
		EndUUID:   "msg-6",
		Summary:   "second summary",
	})

	commits := s.Commit()
	require.Len(t, commits, 2)
	require.NotEmpty(t, commits[0].CollapseID)
	require.Equal(t, "first summary", commits[0].SummaryContent)
	require.Equal(t, "msg-1", commits[0].FirstArchivedUUID)
	require.Equal(t, "msg-3", commits[0].LastArchivedUUID)

	stats := s.GetStats()
	require.Equal(t, 2, stats.TotalCommits)
	require.Equal(t, 0, stats.StagedCount)
}

func TestCollapseStoreDrainAll(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{StartUUID: "a", EndUUID: "b", Summary: "s1"})
	s.Stage(CollapseStagedSpan{StartUUID: "c", EndUUID: "d", Summary: "s2"})

	count := s.DrainAll()
	require.Equal(t, 2, count)
	require.Equal(t, 2, s.GetStats().TotalCommits)
	require.Equal(t, 0, s.GetStats().StagedCount)
}

func TestCollapseStoreProjectView(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{
		StartUUID: "msg-2",
		EndUUID:   "msg-3",
		Summary:   "archived summary",
	})
	s.Commit()

	messages := []provider.Message{
		{Metadata: map[string]any{"id": "msg-1"}, Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Metadata: map[string]any{"id": "msg-2"}, Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "old"}}},
		{Metadata: map[string]any{"id": "msg-3"}, Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "old2"}}},
		{Metadata: map[string]any{"id": "msg-4"}, Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "new"}}},
	}

	result := s.ProjectView(messages)
	require.Len(t, result, 3)
	require.Equal(t, "msg-1", result[0].Metadata["id"])
	require.Equal(t, "assistant", result[1].Role)
	require.Equal(t, "archived summary", result[1].Content[0].Text)
	require.Equal(t, "msg-4", result[2].Metadata["id"])
}

func TestCollapseStoreProjectViewNoCommits(t *testing.T) {
	s := NewCollapseStore(true)
	messages := []provider.Message{
		{Metadata: map[string]any{"id": "msg-1"}, Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
	}
	result := s.ProjectView(messages)
	require.Len(t, result, 1)
	require.Equal(t, "msg-1", result[0].Metadata["id"])
}

func TestCollapseStoreGetStats(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{StartUUID: "a", EndUUID: "b", Summary: "s1"})
	s.Commit()

	stats := s.GetStats()
	require.Equal(t, 1, stats.TotalCommits)
	require.Equal(t, 0, stats.TotalTokensFreed)
	require.Equal(t, 0, stats.StagedCount)
	require.True(t, stats.IsEnabled)
}

func TestCollapseStoreReset(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{StartUUID: "a", EndUUID: "b", Summary: "s1"})
	s.Commit()
	require.Equal(t, 1, s.GetStats().TotalCommits)

	s.Reset()
	stats := s.GetStats()
	require.Equal(t, 0, stats.TotalCommits)
	require.Equal(t, 0, stats.StagedCount)
}

func TestCollapseStoreRestoreFromEntries(t *testing.T) {
	s := NewCollapseStore(true)
	commits := []CollapseCommit{
		{CollapseID: "c1", SummaryContent: "s1"},
		{CollapseID: "c2", SummaryContent: "s2"},
	}
	s.RestoreFromEntries(commits)
	require.Equal(t, 2, s.GetStats().TotalCommits)
}

func TestCollapseStoreDisabled(t *testing.T) {
	s := NewCollapseStore(false)
	require.False(t, s.IsEnabled())
}
