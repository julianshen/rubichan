package integrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// LLMCompleter wraps an LLMProvider to collect streamed text into a single string.
type LLMCompleter struct {
	provider provider.LLMProvider
	model    string
}

// NewLLMCompleter creates a new LLMCompleter.
func NewLLMCompleter(p provider.LLMProvider, model string) *LLMCompleter {
	return &LLMCompleter{provider: p, model: model}
}

// Complete sends a prompt to the LLM and returns the full response text.
func (c *LLMCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	req := provider.CompletionRequest{
		Model:     c.model,
		Messages:  []provider.Message{provider.NewUserMessage(prompt)},
		MaxTokens: 4096,
	}

	ch, err := c.provider.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("llm complete: %w", err)
	}

	var parts []string
	for evt := range ch {
		switch evt.Type {
		case "text_delta":
			parts = append(parts, evt.Text)
		case "error":
			return "", fmt.Errorf("llm stream error: %w", evt.Error)
		}
	}

	return strings.Join(parts, ""), nil
}
