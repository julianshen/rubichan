package agentsdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventStageString(t *testing.T) {
	tests := []struct {
		stage EventStage
		want  string
	}{
		{EventBegin, "begin"},
		{EventDelta, "delta"},
		{EventEnd, "end"},
		{EventStage(99), "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.stage.String())
	}
}

func TestToolResultDisplay(t *testing.T) {
	// Falls back to Content when DisplayContent is empty.
	r := ToolResult{Content: "raw output"}
	assert.Equal(t, "raw output", r.Display())

	// Uses DisplayContent when set.
	r.DisplayContent = "pretty output"
	assert.Equal(t, "pretty output", r.Display())
}

func TestToolResultDisplayEmpty(t *testing.T) {
	r := ToolResult{}
	assert.Equal(t, "", r.Display())
}

// mockTool verifies the Tool interface can be implemented.
type mockTool struct{}

func (m *mockTool) Name() string                 { return "mock" }
func (m *mockTool) Description() string          { return "a mock tool" }
func (m *mockTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	return ToolResult{Content: "ok"}, nil
}

func TestToolInterfaceSatisfied(t *testing.T) {
	var tool Tool = &mockTool{}
	assert.Equal(t, "mock", tool.Name())
	assert.Equal(t, "a mock tool", tool.Description())

	result, err := tool.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.Equal(t, "ok", result.Content)
}

// mockStreamingTool verifies StreamingTool extends Tool.
type mockStreamingTool struct {
	mockTool
}

func (m *mockStreamingTool) ExecuteStream(_ context.Context, _ json.RawMessage, emit ToolEventEmitter) (ToolResult, error) {
	emit(ToolEvent{Stage: EventBegin})
	emit(ToolEvent{Stage: EventDelta, Content: "progress"})
	emit(ToolEvent{Stage: EventEnd})
	return ToolResult{Content: "streamed"}, nil
}

func TestStreamingToolEmitsEvents(t *testing.T) {
	var tool StreamingTool = &mockStreamingTool{}

	var events []ToolEvent
	result, err := tool.ExecuteStream(context.Background(), nil, func(ev ToolEvent) {
		events = append(events, ev)
	})

	assert.NoError(t, err)
	assert.Equal(t, "streamed", result.Content)
	assert.Len(t, events, 3)
	assert.Equal(t, EventBegin, events[0].Stage)
	assert.Equal(t, "progress", events[1].Content)
	assert.False(t, events[1].IsError)
	assert.Equal(t, EventEnd, events[2].Stage)
}

func TestStreamingToolEmitsErrorEvent(t *testing.T) {
	var events []ToolEvent
	emit := ToolEventEmitter(func(ev ToolEvent) { events = append(events, ev) })

	emit(ToolEvent{Stage: EventDelta, Content: "something went wrong", IsError: true})

	require.Len(t, events, 1)
	assert.True(t, events[0].IsError)
	assert.Equal(t, "something went wrong", events[0].Content)
}
