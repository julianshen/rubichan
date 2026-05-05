package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
)

func TestHeadTailSnip_PreservesHeadAndTail(t *testing.T) {
	s := NewHeadTailSnipStrategy()
	msgs := makeMessages(9)
	result, err := s.Compact(context.Background(), msgs, 1)
	assert.NoError(t, err)
	assert.Less(t, len(result), len(msgs), "should reduce message count")
	assert.Equal(t, msgs[0].Content[0].Text, result[0].Content[0].Text, "should preserve first message (head)")
	assert.Equal(t, msgs[len(msgs)-1].Content[0].Text, result[len(result)-1].Content[0].Text, "should preserve last message (tail)")
}

func TestHeadTailSnip_DoesNotOverShrink(t *testing.T) {
	s := NewHeadTailSnipStrategy()
	msgs := makeMessages(9)
	result, err := s.Compact(context.Background(), msgs, estimateTokens(msgs)+1000)
	assert.NoError(t, err)
	assert.Equal(t, msgs, result, "should not remove messages when within budget")
}

func TestHeadTailSnip_MinimumMessages(t *testing.T) {
	s := NewHeadTailSnipStrategy()
	msgs := makeMessages(4)
	result, err := s.Compact(context.Background(), msgs, 1)
	assert.NoError(t, err)
	assert.LessOrEqual(t, len(result), 4)
}

func TestHeadTailSnip_SkipsToolUseBoundary(t *testing.T) {
	s := NewHeadTailSnipStrategy()
	msgs := makeMessages(9)
	msgs[3] = agentsdk.Message{Role: "assistant", Content: []agentsdk.ContentBlock{
		{Type: "tool_use", ID: "tu1", Name: "read_file"},
	}}
	result, err := s.Compact(context.Background(), msgs, 1)
	assert.NoError(t, err)
	for _, m := range result {
		if m.Role == "assistant" {
			for _, b := range m.Content {
				if b.Type == "tool_use" {
					hasResult := false
					for _, m2 := range result {
						for _, b2 := range m2.Content {
							if b2.Type == "tool_result" && b2.ToolUseID == b.ID {
								hasResult = true
							}
						}
					}
					assert.True(t, hasResult, "tool_use should have matching tool_result")
				}
			}
		}
	}
}

func TestHeadTailSnip_BoundaryMarkerInserted(t *testing.T) {
	s := NewHeadTailSnipStrategy()
	msgs := makeMessages(9)
	result, err := s.Compact(context.Background(), msgs, 1)
	assert.NoError(t, err)

	foundBoundary := false
	for _, m := range result {
		for _, b := range m.Content {
			if b.Type == "text" && strings.Contains(b.Text, "[Context snipped:") {
				foundBoundary = true
			}
		}
	}
	assert.True(t, foundBoundary, "should insert boundary marker")
}

func TestHeadTailSnip_TokensFreedTracked(t *testing.T) {
	strategy := &headTailSnipStrategy{}
	msgs := makeMessages(9)
	result := strategy.Snip(msgs, 1)
	assert.Greater(t, result.TokensFreed, 0, "should track tokens freed")
	assert.NotEmpty(t, result.SnippedUUIDs, "should collect snipped UUIDs")
	assert.NotNil(t, result.BoundaryMsg, "should have boundary message")
}

func TestInjectMessageIDTags(t *testing.T) {
	msgs := []agentsdk.Message{
		{Role: "user", Metadata: map[string]any{"uuid": "abc12345-def"}, Content: []agentsdk.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []agentsdk.ContentBlock{{Type: "text", Text: "hi"}}},
	}
	result := InjectMessageIDTags(msgs)
	assert.True(t, strings.HasPrefix(result[0].Content[0].Text, "[id:abc12345]"))
	assert.Equal(t, "hi", result[1].Content[0].Text)
}

func makeMessages(n int) []agentsdk.Message {
	msgs := make([]agentsdk.Message, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = agentsdk.Message{
			Role: role,
			Content: []agentsdk.ContentBlock{
				{Type: "text", Text: string(rune('a' + i))},
			},
			Metadata: map[string]any{"uuid": string(rune('a' + i))},
		}
	}
	return msgs
}
