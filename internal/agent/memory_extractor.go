package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// MemoryStore is the persistence interface for cross-session memories.
type MemoryStore interface {
	SaveMemory(workingDir, tag, content string) error
	LoadMemories(workingDir string) ([]MemoryEntry, error)
}

// MemoryEntry represents a single cross-session memory.
type MemoryEntry struct {
	Tag     string
	Content string
}

// MemoryExtractor uses a Summarizer to extract reusable insights from
// a conversation's message history.
type MemoryExtractor struct {
	summarizer Summarizer
}

// NewMemoryExtractor creates a MemoryExtractor backed by the given Summarizer.
func NewMemoryExtractor(s Summarizer) *MemoryExtractor {
	return &MemoryExtractor{summarizer: s}
}

// Extract identifies reusable insights from the conversation messages.
// Returns structured memories with tags.
func (e *MemoryExtractor) Extract(ctx context.Context, messages []provider.Message) ([]MemoryEntry, error) {
	if e.summarizer == nil || len(messages) < 4 {
		return nil, nil
	}

	// Build a text representation for the extraction prompt.
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s] ", msg.Role))
		for _, block := range msg.Content {
			if block.Text != "" {
				sb.WriteString(block.Text)
			}
		}
		sb.WriteString("\n")
	}

	// Use the summarizer with an extraction-specific prompt via a wrapper.
	extractionMessages := []provider.Message{
		provider.NewUserMessage(
			"Extract reusable insights from this conversation. " +
				"Identify: patterns, preferences, gotchas, architecture decisions, " +
				"and debugging insights that would be useful in future sessions.\n\n" +
				"Format each insight as:\n" +
				"TAG: <short-tag>\nCONTENT: <insight>\n---\n\n" +
				"Conversation:\n" + sb.String(),
		),
	}

	summary, err := e.summarizer.Summarize(ctx, extractionMessages)
	if err != nil {
		return nil, fmt.Errorf("memory extraction: %w", err)
	}

	return parseMemories(summary), nil
}

// parseMemories parses TAG:/CONTENT: formatted text into MemoryEntry values.
func parseMemories(text string) []MemoryEntry {
	var memories []MemoryEntry
	sections := strings.Split(text, "---")

	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		var tag, content string
		for _, line := range strings.Split(section, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "TAG:") {
				tag = strings.TrimSpace(strings.TrimPrefix(line, "TAG:"))
			} else if strings.HasPrefix(line, "CONTENT:") {
				content = strings.TrimSpace(strings.TrimPrefix(line, "CONTENT:"))
			}
		}

		if tag != "" && content != "" {
			memories = append(memories, MemoryEntry{Tag: tag, Content: content})
		}
	}

	return memories
}
