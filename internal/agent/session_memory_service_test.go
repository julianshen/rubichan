package agent

import (
	"strings"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionMemoryService_DefaultConfig(t *testing.T) {
	s := NewSessionMemoryService("/tmp/test-session")
	cfg := s.Config()
	assert.Equal(t, 3, cfg.ToolCallsBetweenUpdates)
	assert.Equal(t, 10000, cfg.MinMessageTokensToInit)
}

func TestSessionMemoryService_GetMemoryPath(t *testing.T) {
	s := NewSessionMemoryService("/tmp/test-session")
	assert.Equal(t, "/tmp/test-session/session-notes.md", s.GetMemoryPath())
}

func TestSessionMemoryService_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	s := NewSessionMemoryService(dir)
	_, err := s.writeInitialTemplate()
	require.NoError(t, err)
	content, err := s.ReadCurrentMemory()
	require.NoError(t, err)
	assert.Contains(t, content, "# Session Title")
	assert.Contains(t, content, "# Worklog")
}

func TestShouldExtract(t *testing.T) {
	dir := t.TempDir()
	s := NewSessionMemoryService(dir)
	_, _ = s.writeInitialTemplate()

	// turnsSinceLast starts at 0, not enough
	assert.False(t, s.ShouldExtract(5))
	// Simulate 3 turns
	s.RecordTurn()
	s.RecordTurn()
	s.RecordTurn()
	assert.True(t, s.ShouldExtract(5))
}

func TestTruncateSessionMemoryForCompact(t *testing.T) {
	content := "# Section A\nline1\nline2\n# Section B\n" + strings.Repeat("x", 100)
	truncated, wasTruncated := TruncateSessionMemoryForCompact(content, 50)
	assert.True(t, wasTruncated)
	assert.Contains(t, truncated, "[... section truncated for length ...]")
	assert.Contains(t, truncated, "# Section A")
	assert.Contains(t, truncated, "# Section B")
}

func TestCountToolCallsSince(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "assistant", Metadata: map[string]any{"uuid": "m1"}, Content: []agentsdk.ContentBlock{{Type: "tool_use", ID: "t1"}}},
		{Role: "assistant", Metadata: map[string]any{"uuid": "m2"}, Content: []agentsdk.ContentBlock{{Type: "tool_use", ID: "t2"}}},
		{Role: "assistant", Metadata: map[string]any{"uuid": "m3"}, Content: []agentsdk.ContentBlock{{Type: "tool_use", ID: "t3"}}},
	}
	assert.Equal(t, 2, CountToolCallsSince(msgs, "m1"))
	assert.Equal(t, 1, CountToolCallsSince(msgs, "m2"))
	assert.Equal(t, 3, CountToolCallsSince(msgs, ""))
}
