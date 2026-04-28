package agent

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
)

func TestReactiveCompact_ReducesMessages(t *testing.T) {
	cm := NewContextManager(30, 0)
	cm.SetStrategies([]CompactionStrategy{&mockStrategy{name: "test", removeN: 10}})

	conv := NewConversation("system")
	for i := 0; i < 20; i++ {
		conv.AddUser("message with enough content to have tokens")
		conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response"}})
	}
	initialLen := conv.Len()

	result := reactiveCompact(context.Background(), cm, conv)
	assert.True(t, result, "should have compacted")
	assert.Less(t, conv.Len(), initialLen, "messages should be reduced")
}

func TestReactiveCompact_EmptyConversation(t *testing.T) {
	cm := NewContextManager(30, 0)
	conv := NewConversation("system")

	result := reactiveCompact(context.Background(), cm, conv)
	assert.False(t, result, "empty conversation should not compact")
}

func TestReactiveCompact_NoReduction(t *testing.T) {
	cm := NewContextManager(30000, 0)
	cm.SetStrategies([]CompactionStrategy{&mockStrategy{name: "noop"}})

	conv := NewConversation("system")
	conv.AddUser("hello")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "hi"}})

	result := reactiveCompact(context.Background(), cm, conv)
	assert.False(t, result, "strategy that doesn't reduce should return false")
}

func TestDrainMessages(t *testing.T) {
	conv := NewConversation("system")
	for i := 0; i < 20; i++ {
		conv.AddUser("msg")
		conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "resp"}})
	}
	assert.True(t, conv.DrainMessages(5))
	assert.Equal(t, 10, conv.Len(), "should keep 5 pairs = 10 messages")
}

func TestDrainMessages_SmallConversation(t *testing.T) {
	conv := NewConversation("system")
	conv.AddUser("hello")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "hi"}})
	assert.False(t, conv.DrainMessages(5), "should not drain small conversation")
}

func TestDrainMessages_ExactBoundary(t *testing.T) {
	conv := NewConversation("system")
	conv.AddUser("msg")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "resp"}})
	conv.AddUser("msg2")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "resp2"}})
	assert.False(t, conv.DrainMessages(2), "4 messages = exactly 2 pairs, should not drain")
}

func TestDrainMessages_SkipsLeadingToolResult(t *testing.T) {
	conv := NewConversation("system")
	for i := 0; i < 10; i++ {
		conv.AddUser("msg")
		conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "resp"}})
	}
	conv.AddToolResult("tool-1", "result", false)
	assert.True(t, conv.DrainMessages(2))
	assert.NotEqual(t, "tool_result", conv.messages[0].Role, "should not start with tool_result")
}
