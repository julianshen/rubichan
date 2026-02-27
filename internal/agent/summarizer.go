package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// Summarizer condenses a sequence of messages into a short text summary.
type Summarizer interface {
	Summarize(ctx context.Context, messages []provider.Message) (string, error)
}

// LLMSummarizer implements Summarizer by calling an LLM provider.
type LLMSummarizer struct {
	provider provider.LLMProvider
	model    string
}

// NewLLMSummarizer creates a Summarizer backed by the given LLM provider.
func NewLLMSummarizer(p provider.LLMProvider, model string) *LLMSummarizer {
	return &LLMSummarizer{provider: p, model: model}
}

// Summarize sends the messages to the LLM with a summarization prompt and
// returns the condensed text.
func (s *LLMSummarizer) Summarize(ctx context.Context, messages []provider.Message) (string, error) {
	// Build a text representation of the messages for the prompt.
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s] ", msg.Role))
		for _, block := range msg.Content {
			if block.Text != "" {
				sb.WriteString(block.Text)
			} else if block.Type == "tool_use" {
				sb.WriteString(fmt.Sprintf("<tool:%s>", block.Name))
			} else if block.Type == "tool_result" {
				// Use truncated preview for tool results.
				preview := block.Text
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("<result:%s>", preview))
			}
		}
		sb.WriteString("\n")
	}

	systemPrompt := "You are a conversation summarizer. Condense the following conversation " +
		"into a brief summary capturing key facts, decisions, code changes, and important context. " +
		"Be concise but preserve critical information that would be needed to continue the conversation."

	req := provider.CompletionRequest{
		Model:     s.model,
		System:    systemPrompt,
		Messages:  []provider.Message{provider.NewUserMessage(sb.String())},
		MaxTokens: 1024,
	}

	stream, err := s.provider.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarization request: %w", err)
	}

	var result strings.Builder
	for event := range stream {
		switch event.Type {
		case "text_delta":
			result.WriteString(event.Text)
		case "error":
			return "", fmt.Errorf("summarization stream error: %w", event.Error)
		}
	}

	return result.String(), nil
}
