package agentsdk

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// systemCapturingProvider records the System prompt of every request it
// receives and replies with a canned text response, so a test can assert
// what reached the model.
type systemCapturingProvider struct {
	mu       sync.Mutex
	systems  []string
	response []StreamEvent
}

func (p *systemCapturingProvider) Stream(_ context.Context, req CompletionRequest) (<-chan StreamEvent, error) {
	p.mu.Lock()
	p.systems = append(p.systems, req.System)
	p.mu.Unlock()
	ch := make(chan StreamEvent, len(p.response))
	for _, ev := range p.response {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (p *systemCapturingProvider) captured() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.systems...)
}

// staticStrategy is a ContextStrategy that returns fixed sections and
// records the PromptContext it was handed.
type staticStrategy struct {
	sections []PromptSection
	mu       sync.Mutex
	seen     []PromptContext
}

func (s *staticStrategy) ContributePromptSections(_ context.Context, info PromptContext) []PromptSection {
	s.mu.Lock()
	s.seen = append(s.seen, info)
	s.mu.Unlock()
	return s.sections
}

func (s *staticStrategy) contexts() []PromptContext {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]PromptContext(nil), s.seen...)
}

func TestAgentContextStrategyContributesToSystemPrompt(t *testing.T) {
	p := &systemCapturingProvider{response: textResponse("ok")}
	strat := &staticStrategy{sections: []PromptSection{{
		Title:   "Deployment Window",
		Content: "Deploys are frozen this week.",
		Reason:  "operational state varies per run",
	}}}

	a := NewAgent(p, WithSystemPrompt("BASE PROMPT"), WithContextStrategies(strat))

	ch, err := a.Turn(context.Background(), "ship it")
	require.NoError(t, err)
	for range ch {
	}

	systems := p.captured()
	require.Len(t, systems, 1)
	sys := systems[0]
	assert.Contains(t, sys, "BASE PROMPT", "base system prompt must be preserved")
	assert.Contains(t, sys, "Deployment Window", "strategy section title must reach the prompt")
	assert.Contains(t, sys, "Deploys are frozen this week.", "strategy section content must reach the prompt")
}

func TestAgentContextStrategyReceivesTurnContext(t *testing.T) {
	p := &systemCapturingProvider{response: textResponse("ok")}
	strat := &staticStrategy{sections: []PromptSection{{Title: "S", Content: "c"}}}

	a := NewAgent(p, WithContextStrategies(strat))
	ch, err := a.Turn(context.Background(), "ship it")
	require.NoError(t, err)
	for range ch {
	}

	seen := strat.contexts()
	require.Len(t, seen, 1)
	assert.Equal(t, "ship it", seen[0].UserMessage, "the turn's user message must be handed to strategies")
	assert.Equal(t, 100000, seen[0].TokenBudget, "TokenBudget must carry the config context budget")
}

func TestAgentContextStrategySkipsBlankSections(t *testing.T) {
	p := &systemCapturingProvider{response: textResponse("ok")}
	strat := &staticStrategy{sections: []PromptSection{
		{Title: "Blank", Content: "   \n\t"},
		{Title: "Real", Content: "kept"},
	}}

	a := NewAgent(p, WithSystemPrompt("BASE"), WithContextStrategies(strat))
	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	for range ch {
	}

	sys := p.captured()[0]
	assert.NotContains(t, sys, "## Blank", "whitespace-only sections must be skipped")
	assert.Contains(t, sys, "## Real", "non-blank sections must render")
}

func TestAgentContextStrategyNilIgnored(t *testing.T) {
	p := &systemCapturingProvider{response: textResponse("ok")}
	real := &staticStrategy{sections: []PromptSection{{Title: "Real", Content: "kept"}}}

	// A nil strategy interleaved with a real one must not panic.
	a := NewAgent(p, WithSystemPrompt("BASE"), WithContextStrategies(nil, real))
	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	for range ch {
	}

	assert.Contains(t, p.captured()[0], "kept")
}

func TestAgentNoContextStrategiesLeavesPromptUnchanged(t *testing.T) {
	p := &systemCapturingProvider{response: textResponse("ok")}
	a := NewAgent(p, WithSystemPrompt("BASE PROMPT"))

	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	for range ch {
	}

	require.Len(t, p.captured(), 1)
	assert.Equal(t, "BASE PROMPT", p.captured()[0], "with no strategies the base prompt must pass through byte-for-byte")
}

// panicStrategy panics when asked for sections.
type panicStrategy struct{}

func (panicStrategy) ContributePromptSections(context.Context, PromptContext) []PromptSection {
	panic("boom")
}

func TestAgentContextStrategyPanicRecovered(t *testing.T) {
	p := &systemCapturingProvider{response: textResponse("ok")}
	logger := &captureLogger{}
	real := &staticStrategy{sections: []PromptSection{{Title: "Real", Content: "kept"}}}

	a := NewAgent(p, WithSystemPrompt("BASE"), WithLogger(logger), WithContextStrategies(panicStrategy{}, real))
	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)

	var sawDone bool
	for ev := range ch {
		if ev.Type == "done" {
			sawDone = true
		}
	}

	assert.True(t, sawDone, "a panicking strategy must not abort the turn")
	sys := p.captured()[0]
	assert.Contains(t, sys, "BASE", "base prompt survives a strategy panic")
	assert.Contains(t, sys, "kept", "sibling strategies still contribute after one panics")
	require.Len(t, logger.warns, 1)
	assert.Contains(t, logger.warns[0], "panicked")
}
