package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryExtractorWithMockSummarizer(t *testing.T) {
	ms := &mockSummarizer{
		summary: "TAG: go-conventions\nCONTENT: Use table-driven tests\n---\nTAG: api-pattern\nCONTENT: Always wrap errors with context\n---",
	}
	extractor := NewMemoryExtractor(ms)

	messages := make([]provider.Message, 6)
	for i := range messages {
		messages[i] = provider.Message{
			Role:    "user",
			Content: []provider.ContentBlock{{Type: "text", Text: fmt.Sprintf("msg %d", i)}},
		}
	}

	memories, err := extractor.Extract(context.Background(), messages)
	require.NoError(t, err)
	assert.Len(t, memories, 2)
	assert.Equal(t, "go-conventions", memories[0].Tag)
	assert.Equal(t, "Use table-driven tests", memories[0].Content)
	assert.Equal(t, "api-pattern", memories[1].Tag)
}

func TestMemoryExtractorNilSummarizer(t *testing.T) {
	extractor := NewMemoryExtractor(nil)

	messages := make([]provider.Message, 6)
	for i := range messages {
		messages[i] = provider.Message{
			Role:    "user",
			Content: []provider.ContentBlock{{Type: "text", Text: "msg"}},
		}
	}

	memories, err := extractor.Extract(context.Background(), messages)
	require.NoError(t, err)
	assert.Nil(t, memories)
}

func TestMemoryExtractorTooFewMessages(t *testing.T) {
	ms := &mockSummarizer{summary: "should not be called"}
	extractor := NewMemoryExtractor(ms)

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}

	memories, err := extractor.Extract(context.Background(), messages)
	require.NoError(t, err)
	assert.Nil(t, memories)
	assert.False(t, ms.called)
}

func TestMemoryExtractorSummarizerError(t *testing.T) {
	ms := &mockSummarizer{err: fmt.Errorf("API down")}
	extractor := NewMemoryExtractor(ms)

	messages := make([]provider.Message, 6)
	for i := range messages {
		messages[i] = provider.Message{
			Role:    "user",
			Content: []provider.ContentBlock{{Type: "text", Text: "msg"}},
		}
	}

	_, err := extractor.Extract(context.Background(), messages)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "memory extraction")
}

func TestParseMemories(t *testing.T) {
	input := "TAG: pattern-a\nCONTENT: Always use interfaces\n---\nTAG: gotcha-b\nCONTENT: SQLite needs single connection\n---"
	memories := parseMemories(input)

	assert.Len(t, memories, 2)
	assert.Equal(t, "pattern-a", memories[0].Tag)
	assert.Equal(t, "Always use interfaces", memories[0].Content)
	assert.Equal(t, "gotcha-b", memories[1].Tag)
	assert.Equal(t, "SQLite needs single connection", memories[1].Content)
}

func TestParseMemoriesEmpty(t *testing.T) {
	memories := parseMemories("")
	assert.Nil(t, memories)
}

func TestParseMemoriesMissingFields(t *testing.T) {
	input := "TAG: only-tag\n---\nCONTENT: only-content\n---"
	memories := parseMemories(input)
	assert.Nil(t, memories, "entries missing tag or content should be skipped")
}
