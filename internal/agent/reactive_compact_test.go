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
	assert.True(t, result.compacted, "should have compacted")
	assert.Less(t, conv.Len(), initialLen, "messages should be reduced")
}

func TestReactiveCompact_EmptyConversation(t *testing.T) {
	cm := NewContextManager(30, 0)
	conv := NewConversation("system")

	result := reactiveCompact(context.Background(), cm, conv)
	assert.False(t, result.compacted, "empty conversation should not compact")
}

func TestReactiveCompact_NoReduction(t *testing.T) {
	cm := NewContextManager(30000, 0)
	cm.SetStrategies([]CompactionStrategy{&mockStrategy{name: "noop"}})

	conv := NewConversation("system")
	conv.AddUser("hello")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "hi"}})

	result := reactiveCompact(context.Background(), cm, conv)
	assert.False(t, result.compacted, "strategy that doesn't reduce should return false")
}

func TestContextCollapseDrain(t *testing.T) {
	msgs := make([]int, 20)
	drained := contextCollapseDrain(msgs, 5)
	assert.Equal(t, 10, len(drained), "should keep 5 pairs = 10 messages")
}

func TestContextCollapseDrain_SmallInput(t *testing.T) {
	msgs := make([]int, 8)
	drained := contextCollapseDrain(msgs, 5)
	assert.Equal(t, 8, len(drained), "should keep all when below minPairs")
}

func TestContextCollapseDrain_ZeroMinPairs(t *testing.T) {
	msgs := make([]int, 20)
	drained := contextCollapseDrain(msgs, 0)
	assert.Equal(t, 20, len(drained), "zero minPairs keeps all")
}
