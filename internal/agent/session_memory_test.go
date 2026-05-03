package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateMessagesToKeepIndex_AllMessagesUnderBudget(t *testing.T) {
	t.Parallel()
	messages := make([]provider.Message, 10)
	for i := range messages {
		messages[i] = provider.NewUserMessage(fmt.Sprintf("message %d", i))
	}

	c := &SessionMemoryCompactor{}
	counter := func(msgs []provider.Message) int {
		return len(msgs) * 1000
	}

	idx := c.calculateMessagesToKeepIndex(messages, counter)
	require.Equal(t, 1, idx)
}

func TestCalculateMessagesToKeepIndex_SomeMessagesOverBudget(t *testing.T) {
	t.Parallel()
	messages := make([]provider.Message, 20)
	for i := range messages {
		messages[i] = provider.NewUserMessage(fmt.Sprintf("message %d", i))
	}

	c := &SessionMemoryCompactor{}
	counter := func(msgs []provider.Message) int {
		return len(msgs) * 1000
	}

	idx := c.calculateMessagesToKeepIndex(messages, counter)
	require.Equal(t, 10, idx)
}

func TestCalculateMessagesToKeepIndex_RespectsLastSummarizedIndex(t *testing.T) {
	t.Parallel()
	messages := make([]provider.Message, 20)
	for i := range messages {
		messages[i] = provider.NewUserMessage(fmt.Sprintf("message %d", i))
	}

	c := &SessionMemoryCompactor{lastSummarizedCount: 5}
	counter := func(msgs []provider.Message) int {
		return len(msgs) * 1000
	}

	idx := c.calculateMessagesToKeepIndex(messages, counter)
	require.Equal(t, 10, idx)
}

func TestCalculateMessagesToKeepIndex_MinTextBlockMessages(t *testing.T) {
	t.Parallel()
	messages := make([]provider.Message, 20)
	for i := range messages {
		if i%7 == 0 {
			messages[i] = provider.NewUserMessage(fmt.Sprintf("text message %d", i))
		} else {
			messages[i] = provider.Message{Role: "assistant", Content: []provider.ContentBlock{
				{Type: "tool_use", ID: fmt.Sprintf("tool-%d", i)},
			}}
		}
	}

	c := &SessionMemoryCompactor{}
	counter := func(msgs []provider.Message) int {
		return len(msgs) * 1000
	}

	idx := c.calculateMessagesToKeepIndex(messages, counter)
	require.Equal(t, 20, idx)
}

func TestAdjustIndexPreservesToolPairs(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{
		provider.NewUserMessage("user message"),
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1", Name: "shell"}}},
		provider.NewToolResultMessage("t1", "result data", false),
		provider.NewUserMessage("follow-up"),
	}

	// Trying to split at idx=2 (between tool_use and tool_result).
	idx := adjustIndexToPreserveAPIInvariants(messages, 2)
	// Should move to idx=1 to include the assistant with tool_use.
	require.Equal(t, 1, idx)
}

func TestAdjustIndexPreservesToolPairs_NoToolResult(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{
		provider.NewUserMessage("user message"),
		provider.NewUserMessage("another user"),
		provider.NewUserMessage("follow-up"),
	}

	idx := adjustIndexToPreserveAPIInvariants(messages, 1)
	// No tool_result at boundary, idx unchanged.
	require.Equal(t, 1, idx)
}

func TestAdjustIndexPreservesThinkingBlocks(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{
		provider.NewUserMessage("user message"),
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "thinking", ID: "think-1", Text: "thinking..."},
		}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "thinking", ID: "think-1", Text: "more thinking..."},
		}},
		provider.NewUserMessage("follow-up"),
	}

	// Split at idx=2 (between two thinking blocks sharing ID).
	idx := adjustIndexToPreserveAPIInvariants(messages, 2)
	// The boundary is at idx=2. messages[2] has a thinking block with ID "think-1".
	// We scan backward: messages[1] has a thinking block with ID "think-1" — match.
	// messages[0] has no thinking block with matching ID — no match.
	// So idx moves to 0 + 1 = 1 to keep thinking blocks together.
	require.Equal(t, 1, idx)
}

func TestAdjustIndexPreservesThinkingBlocks_NoSplitNeeded(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{
		provider.NewUserMessage("user message"),
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "thinking", ID: "think-1", Text: "thinking..."},
		}},
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "text", ID: "think-1", Text: "response"},
		}},
		provider.NewUserMessage("follow-up"),
	}

	// Split at idx=2. messages[2] has Type="text", not "thinking".
	// boundaryIDs is empty, so no adjustment needed.
	idx := adjustIndexToPreserveAPIInvariants(messages, 2)
	require.Equal(t, 2, idx)
}

func TestAdjustIndexBoundaryChecks(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{
		provider.NewUserMessage("only message"),
	}

	// idx=0: nothing to adjust.
	require.Equal(t, 0, adjustIndexToPreserveAPIInvariants(messages, 0))
	// idx=len: nothing to adjust.
	require.Equal(t, 1, adjustIndexToPreserveAPIInvariants(messages, 1))
}

func TestHasTextBlock(t *testing.T) {
	t.Parallel()
	assert.True(t, hasTextBlock(provider.NewUserMessage("hello")))
	assert.False(t, hasTextBlock(provider.Message{
		Role:    "assistant",
		Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1"}},
	}))
	assert.False(t, hasTextBlock(provider.Message{
		Role:    "user",
		Content: []provider.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Text: ""}},
	}))
}

func TestIsToolResultMessage(t *testing.T) {
	t.Parallel()
	assert.True(t, hasToolResult(provider.NewToolResultMessage("t1", "result", false)))
	assert.False(t, hasToolResult(provider.NewUserMessage("hello")))
	assert.False(t, hasToolResult(provider.Message{
		Role:    "assistant",
		Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1"}},
	}))
}

func TestHasToolUseBlock(t *testing.T) {
	t.Parallel()
	assert.True(t, hasToolUse(provider.Message{
		Role:    "assistant",
		Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1"}},
	}))
	assert.False(t, hasToolUse(provider.NewUserMessage("hello")))
	assert.False(t, hasToolUse(provider.NewToolResultMessage("t1", "result", false)))
}

func TestSessionMemoryCompactor_Compact(t *testing.T) {
	t.Parallel()
	conv := NewConversation("system prompt")
	for i := 0; i < 600; i++ {
		conv.AddUser(fmt.Sprintf("message %d with enough content to be counted here", i))
	}

	c := &SessionMemoryCompactor{}
	mockSummarizer := func(_ context.Context, msgs []provider.Message) (string, error) {
		return fmt.Sprintf("Summary of %d messages", len(msgs)), nil
	}

	err := c.Compact(context.Background(), conv, mockSummarizer)
	require.NoError(t, err)

	msgs := conv.Messages()
	require.Greater(t, len(msgs), 1, "should have summary + kept messages")
	assert.Equal(t, "user", msgs[0].Role)
	assert.Contains(t, msgs[0].Content[0].Text, "Summary of")
	lastMsg := msgs[len(msgs)-1]
	assert.Contains(t, lastMsg.Content[0].Text, "message 599")
}

func TestSessionMemoryCompactor_Compact_NoCompactionNeeded(t *testing.T) {
	t.Parallel()
	conv := NewConversation("system prompt")
	for i := 0; i < 5; i++ {
		conv.AddUser(fmt.Sprintf("msg %d", i))
	}

	c := &SessionMemoryCompactor{}
	mockSummarizer := func(_ context.Context, msgs []provider.Message) (string, error) {
		return "should not be called", nil
	}

	err := c.Compact(context.Background(), conv, mockSummarizer)
	require.NoError(t, err)

	msgs := conv.Messages()
	require.Len(t, msgs, 5)
}

func TestSessionMemoryCompactor_Compact_SummarizerError(t *testing.T) {
	t.Parallel()
	conv := NewConversation("system prompt")
	for i := 0; i < 600; i++ {
		conv.AddUser(fmt.Sprintf("message %d with enough content to be counted here", i))
	}

	c := &SessionMemoryCompactor{}
	mockSummarizer := func(_ context.Context, msgs []provider.Message) (string, error) {
		return "", fmt.Errorf("summarizer failed")
	}

	err := c.Compact(context.Background(), conv, mockSummarizer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "summarize messages")

	msgs := conv.Messages()
	require.Len(t, msgs, 600)
}

func TestSessionMemoryCompactor_Compact_ToolPairPreservation(t *testing.T) {
	t.Parallel()
	conv := NewConversation("system prompt")
	for i := 0; i < 600; i++ {
		conv.AddUser(fmt.Sprintf("message %d with enough content to be counted here", i))
	}
	conv.AddAssistant([]provider.ContentBlock{
		{Type: "tool_use", ID: "tool-1", Name: "shell"},
	})
	conv.AddToolResult("tool-1", "result data", false)
	conv.AddUser("final message with enough content to be counted here")

	c := &SessionMemoryCompactor{}
	mockSummarizer := func(_ context.Context, msgs []provider.Message) (string, error) {
		return fmt.Sprintf("Summary of %d messages", len(msgs)), nil
	}

	err := c.Compact(context.Background(), conv, mockSummarizer)
	require.NoError(t, err)

	msgs := conv.Messages()
	require.Equal(t, "user", msgs[0].Role)
	assert.Contains(t, msgs[0].Content[0].Text, "Summary")

	var foundToolUse, foundToolResult bool
	for _, m := range msgs[1:] {
		if hasToolUse(m) {
			foundToolUse = true
		}
		if hasToolResult(m) {
			foundToolResult = true
		}
	}
	assert.True(t, foundToolUse, "tool_use should be preserved")
	assert.True(t, foundToolResult, "tool_result should be preserved")
}

func TestSessionMemoryCompactor_TracksLastSummarizedIndex(t *testing.T) {
	t.Parallel()
	conv := NewConversation("system prompt")
	for i := 0; i < 600; i++ {
		conv.AddUser(fmt.Sprintf("message %d with enough content to be counted here", i))
	}

	c := &SessionMemoryCompactor{}
	mockSummarizer := func(_ context.Context, msgs []provider.Message) (string, error) {
		return fmt.Sprintf("Summary of %d messages", len(msgs)), nil
	}

	err := c.Compact(context.Background(), conv, mockSummarizer)
	require.NoError(t, err)
	require.Greater(t, c.lastSummarizedCount, 0)

	err = c.Compact(context.Background(), conv, mockSummarizer)
	require.NoError(t, err)
	msgs := conv.Messages()
	require.Greater(t, len(msgs), 1)
}

func TestSessionMemoryCompactionStrategy_Compact(t *testing.T) {
	t.Parallel()
	messages := makeLargeMessages(600)

	mockSummarizer := &sessionMockSummarizer{}
	strategy := NewSessionMemoryCompactionStrategy(mockSummarizer)

	result, err := strategy.Compact(context.Background(), messages, 0)
	require.NoError(t, err)

	require.Less(t, len(result), len(messages), "compaction should reduce message count")
	assert.Equal(t, "session_memory", strategy.Name())
	assert.True(t, mockSummarizer.called)
}

func TestSessionMemoryCompactionStrategy_Compact_NilSummarizer(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{
		provider.NewUserMessage("hello"),
	}

	strategy := NewSessionMemoryCompactionStrategy(nil)
	result, err := strategy.Compact(context.Background(), messages, 0)
	require.NoError(t, err)
	require.Equal(t, messages, result)
}

func TestSessionMemoryCompactionStrategy_Compact_Error(t *testing.T) {
	t.Parallel()
	messages := makeLargeMessages(600)

	mockSummarizer := &sessionMockSummarizer{err: fmt.Errorf("summarizer failed")}
	strategy := NewSessionMemoryCompactionStrategy(mockSummarizer)

	result, err := strategy.Compact(context.Background(), messages, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "summarize messages")
	require.Equal(t, messages, result)
}

func TestCompactMessages_EmptyInput(t *testing.T) {
	t.Parallel()
	c := &SessionMemoryCompactor{}
	result, err := c.compactMessages(context.Background(), nil, nilSummarizer)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestCompactMessages_NoCompactionNeeded(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{
		provider.NewUserMessage("hello"),
	}
	c := &SessionMemoryCompactor{}
	result, err := c.compactMessages(context.Background(), messages, nilSummarizer)
	require.NoError(t, err)
	require.Equal(t, messages, result)
}

func TestCompactMessages_ContextCancelled(t *testing.T) {
	t.Parallel()
	messages := make([]provider.Message, 20)
	for i := range messages {
		messages[i] = provider.NewUserMessage(fmt.Sprintf("message %d", i))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &SessionMemoryCompactor{}
	result, err := c.compactMessages(ctx, messages, nilSummarizer)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	require.Equal(t, messages, result)
}

func TestCompactMessages_PanickingSummarizer(t *testing.T) {
	t.Parallel()
	messages := makeLargeMessages(600)

	panicSummarizer := func(_ context.Context, _ []provider.Message) (string, error) {
		panic("summarizer panic")
	}

	c := &SessionMemoryCompactor{}
	result, err := c.compactMessages(context.Background(), messages, panicSummarizer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "summarizer panicked")
	require.Equal(t, messages, result)
}

func TestCalculateMessagesToKeepIndex_EmptyMessages(t *testing.T) {
	t.Parallel()
	c := &SessionMemoryCompactor{}
	counter := func(msgs []provider.Message) int { return len(msgs) * 1000 }
	idx := c.calculateMessagesToKeepIndex(nil, counter)
	require.Equal(t, 0, idx)
}

func TestCalculateMessagesToKeepIndex_SingleMessage(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{provider.NewUserMessage("hello")}
	c := &SessionMemoryCompactor{}
	counter := func(msgs []provider.Message) int { return len(msgs) * 1000 }
	idx := c.calculateMessagesToKeepIndex(messages, counter)
	require.Equal(t, 0, idx)
}

func TestCalculateMessagesToKeepIndex_AllToolUseMessages(t *testing.T) {
	t.Parallel()
	messages := make([]provider.Message, 20)
	for i := range messages {
		messages[i] = provider.Message{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "tool_use", ID: fmt.Sprintf("tool-%d", i)},
		}}
	}

	c := &SessionMemoryCompactor{}
	counter := func(msgs []provider.Message) int {
		return len(msgs) * 1000
	}

	idx := c.calculateMessagesToKeepIndex(messages, counter)
	require.Equal(t, 20, idx)
}

func TestAdjustIndexPreservesToolPairs_ToolResultAtIndex0(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{
		provider.NewToolResultMessage("t1", "result", false),
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1", Name: "shell"}}},
		provider.NewUserMessage("follow-up"),
	}

	idx := adjustIndexToPreserveAPIInvariants(messages, 0)
	require.Equal(t, 0, idx)
}

func TestAdjustIndexPreservesToolPairs_MultipleToolPairs(t *testing.T) {
	t.Parallel()
	messages := []provider.Message{
		provider.NewUserMessage("user 1"),
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t1", Name: "shell"}}},
		provider.NewToolResultMessage("t1", "result 1", false),
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "tool_use", ID: "t2", Name: "read"}}},
		provider.NewToolResultMessage("t2", "result 2", false),
		provider.NewUserMessage("follow-up"),
	}

	idx := adjustIndexToPreserveAPIInvariants(messages, 4)
	require.Equal(t, 3, idx)
}

// makeLargeMessages creates n messages with enough content to exceed
// minPreserveTokens when summed.
func makeLargeMessages(n int) []provider.Message {
	messages := make([]provider.Message, n)
	for i := range messages {
		messages[i] = provider.NewUserMessage(fmt.Sprintf("message %d with enough content to be counted here", i))
	}
	return messages
}

// sessionMockSummarizer is a test double for the Summarizer interface.
type sessionMockSummarizer struct {
	called bool
	err    error
}

func (m *sessionMockSummarizer) Summarize(_ context.Context, msgs []provider.Message) (string, error) {
	m.called = true
	if m.err != nil {
		return "", m.err
	}
	return fmt.Sprintf("Summary of %d messages", len(msgs)), nil
}

// nilSummarizer is a summarizer that should never be called.
var nilSummarizer = func(_ context.Context, _ []provider.Message) (string, error) {
	return "", fmt.Errorf("summarizer should not have been called")
}
