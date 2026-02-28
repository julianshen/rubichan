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

// classifyMessageImportance returns HIGH, MEDIUM, or LOW for a message.
// The highest importance block in the message wins.
func classifyMessageImportance(msg provider.Message) string {
	highest := "MEDIUM"
	for _, block := range msg.Content {
		var level string
		switch {
		case block.Type == "tool_result" && block.IsError:
			return "HIGH" // error results are always highest
		case block.Type == "text" && msg.Role == "user" && len(block.Text) < 100:
			level = "HIGH" // short user text — likely correction/follow-up
		case block.Type == "tool_result" && len(block.Text) > 500:
			level = "LOW" // large successful output — routine
		default:
			level = "MEDIUM"
		}
		if level == "HIGH" {
			highest = "HIGH"
		}
		if level == "LOW" && highest != "HIGH" {
			highest = "LOW"
		}
	}
	return highest
}

// Summarize sends the messages to the LLM with a summarization prompt and
// returns the condensed text.
func (s *LLMSummarizer) Summarize(ctx context.Context, messages []provider.Message) (string, error) {
	// Build a text representation of the messages with importance tags.
	var sb strings.Builder
	for _, msg := range messages {
		importance := classifyMessageImportance(msg)
		sb.WriteString(fmt.Sprintf("[%s] [%s] ", importance, msg.Role))
		for _, block := range msg.Content {
			switch block.Type {
			case "tool_use":
				sb.WriteString(fmt.Sprintf("<tool:%s>", block.Name))
			case "tool_result":
				preview := block.Text
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				sb.WriteString(fmt.Sprintf("<result:%s>", preview))
			default:
				if block.Text != "" {
					sb.WriteString(block.Text)
				}
			}
		}
		sb.WriteString("\n")
	}

	systemPrompt := "You are a conversation summarizer. Condense the following conversation " +
		"into a brief summary capturing key facts, decisions, code changes, and important context. " +
		"Each message is tagged with an importance level:\n" +
		"- [HIGH]: Preserve details fully — these contain errors, corrections, or critical decisions.\n" +
		"- [MEDIUM]: Summarize intent and outcome.\n" +
		"- [LOW]: Mention the action succeeded, omit raw output.\n" +
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
