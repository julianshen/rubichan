package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func filterEvents(events []TurnEvent, typ string) []TurnEvent {
	var filtered []TurnEvent
	for _, e := range events {
		if e.Type == typ {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func TestIntegrationFileReadWriteFlow(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("original content"), 0644)
	require.NoError(t, err)

	p := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			// Turn 1: read file
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t1", Name: "file"}},
				{Type: "text_delta", Text: `{"operation":"read","path":"test.txt"}`},
				{Type: "stop"},
			},
			// Turn 2: write file
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "t2", Name: "file"}},
				{Type: "text_delta", Text: `{"operation":"write","path":"test.txt","content":"modified content"}`},
				{Type: "stop"},
			},
			// Turn 3: confirm
			{
				{Type: "text_delta", Text: "Done! File has been updated."},
				{Type: "stop"},
			},
		},
	}

	r := tools.NewRegistry()
	err = r.Register(tools.NewFileTool(dir))
	require.NoError(t, err)
	err = r.Register(tools.NewShellTool(dir, 30*time.Second))
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	a := New(p, r, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "Update test.txt")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Verify file was modified
	data, err := os.ReadFile(filepath.Join(dir, "test.txt"))
	require.NoError(t, err)
	assert.Equal(t, "modified content", string(data))

	// Verify event flow includes done
	doneEvents := filterEvents(events, "done")
	assert.Len(t, doneEvents, 1)

	// Verify tool calls happened
	toolCallEvents := filterEvents(events, "tool_call")
	assert.GreaterOrEqual(t, len(toolCallEvents), 2)

	// Verify tool results happened
	toolResultEvents := filterEvents(events, "tool_result")
	assert.GreaterOrEqual(t, len(toolResultEvents), 2)

	// Verify text_delta events present
	textDeltaEvents := filterEvents(events, "text_delta")
	assert.GreaterOrEqual(t, len(textDeltaEvents), 1)

	// Verify no error events
	errorEvents := filterEvents(events, "error")
	assert.Empty(t, errorEvents)
}
