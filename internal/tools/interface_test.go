package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventStageConstants(t *testing.T) {
	assert.Equal(t, EventStage(0), EventBegin)
	assert.Equal(t, EventStage(1), EventDelta)
	assert.Equal(t, EventStage(2), EventEnd)
}

func TestToolEventFields(t *testing.T) {
	ev := ToolEvent{Stage: EventDelta, Content: "line 1", IsError: false}
	assert.Equal(t, EventDelta, ev.Stage)
	assert.Equal(t, "line 1", ev.Content)
	assert.False(t, ev.IsError)
}

func TestWithEmitterRoundTrip(t *testing.T) {
	var received []ToolEvent
	emit := func(ev ToolEvent) {
		received = append(received, ev)
	}

	ctx := WithEmitter(context.Background(), emit)
	got := EmitterFromContext(ctx)
	require.NotNil(t, got)

	got(ToolEvent{Stage: EventBegin, Content: "start"})
	got(ToolEvent{Stage: EventDelta, Content: "data"})

	assert.Len(t, received, 2)
	assert.Equal(t, EventBegin, received[0].Stage)
	assert.Equal(t, "data", received[1].Content)
}

func TestEmitterFromContextReturnsNilWhenNotSet(t *testing.T) {
	emit := EmitterFromContext(context.Background())
	assert.Nil(t, emit)
}

func TestToolResultDisplayContentFallback(t *testing.T) {
	// When DisplayContent is empty, consumers should fall back to Content.
	r := ToolResult{Content: "LLM-facing content", DisplayContent: ""}
	assert.Equal(t, "LLM-facing content", r.Display())

	// When DisplayContent is set, Display() returns it.
	r2 := ToolResult{Content: "compact", DisplayContent: "rich output"}
	assert.Equal(t, "rich output", r2.Display())
}
