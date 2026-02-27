package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMemoryStore implements MemoryStore for testing.
type mockMemoryStore struct {
	saved   []MemoryEntry
	loaded  []MemoryEntry
	loadDir string
	saveErr error
}

func (m *mockMemoryStore) SaveMemory(_, tag, content string) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, MemoryEntry{Tag: tag, Content: content})
	return nil
}

func (m *mockMemoryStore) LoadMemories(workingDir string) ([]MemoryEntry, error) {
	m.loadDir = workingDir
	return m.loaded, nil
}

func testConfig() *config.Config {
	return &config.Config{
		Agent: config.AgentConfig{
			ContextBudget: 100000,
			MaxTurns:      10,
		},
		Provider: config.ProviderConfig{
			Model: "test-model",
		},
	}
}

func TestWithCompactionStrategies(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	s := &mockStrategy{name: "custom"}

	a := New(nil, reg, nil, cfg, WithCompactionStrategies(s, &truncateStrategy{}))
	assert.NotNil(t, a)
}

func TestWithSummarizer(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	ms := &mockSummarizer{summary: "test"}

	a := New(nil, reg, nil, cfg, WithSummarizer(ms))
	assert.NotNil(t, a)
	assert.Equal(t, ms, a.summarizer)
}

func TestWithMemoryStoreLoadOnStart(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	ms := &mockMemoryStore{
		loaded: []MemoryEntry{
			{Tag: "pattern", Content: "use interfaces"},
		},
	}

	a := New(nil, reg, nil, cfg, WithMemoryStore(ms))
	assert.NotNil(t, a)

	// System prompt should contain the loaded memory
	prompt := a.conversation.SystemPrompt()
	assert.Contains(t, prompt, "Prior Session Insights")
	assert.Contains(t, prompt, "pattern")
	assert.Contains(t, prompt, "use interfaces")
}

func TestWithMemoryStoreEmptyNoSection(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	ms := &mockMemoryStore{loaded: nil}

	a := New(nil, reg, nil, cfg, WithMemoryStore(ms))
	prompt := a.conversation.SystemPrompt()
	assert.NotContains(t, prompt, "Prior Session Insights")
}

func TestSaveMemoriesNilStore(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()

	a := New(nil, reg, nil, cfg)
	err := a.SaveMemories(context.Background())
	assert.NoError(t, err)
}

func TestSaveMemoriesNilSummarizer(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	ms := &mockMemoryStore{}

	a := New(nil, reg, nil, cfg, WithMemoryStore(ms))
	err := a.SaveMemories(context.Background())
	assert.NoError(t, err)
}

func TestSaveMemoriesExtractsAndSaves(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	ms := &mockMemoryStore{}
	summarizer := &mockSummarizer{
		summary: "TAG: test-insight\nCONTENT: Always write tests first\n---",
	}

	a := New(nil, reg, nil, cfg, WithMemoryStore(ms), WithSummarizer(summarizer))

	// Add enough messages for extraction
	for i := 0; i < 6; i++ {
		a.conversation.AddUser("test message")
		a.conversation.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response"}})
	}

	err := a.SaveMemories(context.Background())
	require.NoError(t, err)

	assert.Len(t, ms.saved, 1)
	assert.Equal(t, "test-insight", ms.saved[0].Tag)
}

func TestSaveMemoriesReturnsStoreError(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	ms := &mockMemoryStore{saveErr: fmt.Errorf("disk full")}
	summarizer := &mockSummarizer{
		summary: "TAG: insight\nCONTENT: test\n---",
	}

	a := New(nil, reg, nil, cfg, WithMemoryStore(ms), WithSummarizer(summarizer))
	for i := 0; i < 6; i++ {
		a.conversation.AddUser("msg")
		a.conversation.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "resp"}})
	}

	err := a.SaveMemories(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "saving memory")
	assert.Contains(t, err.Error(), "disk full")
}

func TestScratchpadAccessMethod(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	a := New(nil, reg, nil, cfg)

	sp := a.ScratchpadAccess()
	require.NotNil(t, sp)

	sp.Set("test", "value")
	assert.Equal(t, "value", sp.Get("test"))
}

func TestBuildSystemPromptWithScratchpad(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	a := New(nil, reg, nil, cfg)

	a.scratchpad.Set("note1", "important info")

	prompt := a.buildSystemPromptWithFragments()
	assert.Contains(t, prompt, "Agent Notes")
	assert.Contains(t, prompt, "note1")
	assert.Contains(t, prompt, "important info")
}

func TestBuildSystemPromptEmptyScratchpad(t *testing.T) {
	cfg := testConfig()
	reg := tools.NewRegistry()
	a := New(nil, reg, nil, cfg)

	prompt := a.buildSystemPromptWithFragments()
	assert.NotContains(t, prompt, "Agent Notes")
}

// mockProvider implements provider.LLMProvider for testing.
type mockLLMProvider struct {
	response string
	err      error
}

func (m *mockLLMProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: "text_delta", Text: m.response}
	ch <- provider.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

func TestLLMSummarizerSummarize(t *testing.T) {
	mp := &mockLLMProvider{response: "Summary: discussed Go patterns"}
	s := NewLLMSummarizer(mp, "test-model")

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "hi"}}},
	}

	result, err := s.Summarize(context.Background(), messages)
	require.NoError(t, err)
	assert.Equal(t, "Summary: discussed Go patterns", result)
}

func TestLLMSummarizerStreamError(t *testing.T) {
	mp := &mockLLMProvider{err: assert.AnError}
	s := NewLLMSummarizer(mp, "test-model")

	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
	}

	_, err := s.Summarize(context.Background(), messages)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "summarization request")
}
