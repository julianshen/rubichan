package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestToolResultDisplayContentFallback(t *testing.T) {
	// When DisplayContent is empty, consumers should fall back to Content.
	r := ToolResult{Content: "LLM-facing content", DisplayContent: ""}
	assert.Equal(t, "LLM-facing content", r.Display())

	// When DisplayContent is set, Display() returns it.
	r2 := ToolResult{Content: "compact", DisplayContent: "rich output"}
	assert.Equal(t, "rich output", r2.Display())
}
