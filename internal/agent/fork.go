package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ForkedAgent runs a subagent that shares the parent's prompt cache.
// It copies only the fields needed from the parent, avoiding GC retention.
type ForkedAgent struct {
	provider   provider.LLMProvider
	tools      *tools.Registry
	approve    ApprovalFunc
	logger     Logger
	workingDir string
	params     agentsdk.ForkParams
	shareState bool
}

// Fork creates a ForkedAgent from the parent with cache-safe params.
func (a *Agent) Fork(params agentsdk.ForkParams) *ForkedAgent {
	return &ForkedAgent{
		provider:   a.provider,
		tools:      a.tools,
		approve:    a.approve,
		logger:     a.logger,
		workingDir: a.workingDir,
		params:     params,
	}
}

// WithSharedCallbacks enables sharing parent's state callbacks (opt-in).
func (f *ForkedAgent) WithSharedCallbacks() *ForkedAgent {
	f.shareState = true
	return f
}

// Run executes the forked agent with an isolated context.
func (f *ForkedAgent) Run(ctx context.Context, userMessage string) (*agentsdk.ForkResult, error) {
	child, err := f.createSubagentContext()
	if err != nil {
		return nil, fmt.Errorf("create fork context: %w", err)
	}

	ch, err := child.Turn(ctx, userMessage)
	if err != nil {
		return nil, fmt.Errorf("forked agent turn: %w", err)
	}

	var result agentsdk.ForkResult
	var summaryBuf strings.Builder
	for evt := range ch {
		switch evt.Type {
		case agentsdk.EventTextDelta:
			summaryBuf.WriteString(evt.Text)
		case agentsdk.EventError:
			result.Error = evt.Error
		case agentsdk.EventStop:
			result.InputTokens += evt.InputTokens
			result.OutputTokens += evt.OutputTokens
		}
	}
	result.Summary = summaryBuf.String()

	return &result, nil
}

// createSubagentContext creates an isolated Agent that shares cache keys.
func (f *ForkedAgent) createSubagentContext() (*Agent, error) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Model: f.params.Model,
		},
		Agent: config.AgentConfig{
			MaxTurns:               10,
			MaxOutputTokens:        f.params.MaxTokens,
			ContextBudget:          100000,
			ResultOffloadThreshold: 4096,
		},
	}

	child := New(f.provider, f.tools, f.approve, cfg)
	child.workingDir = f.workingDir
	child.basePrompt = f.params.SystemPrompt
	child.conversation = NewConversation(f.params.SystemPrompt)

	if !f.shareState {
		child.summaryCallback = nil
	}

	return child, nil
}
