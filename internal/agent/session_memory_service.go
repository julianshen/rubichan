package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// SessionMemoryConfig controls when and how session memory is extracted.
type SessionMemoryConfig struct {
	MinMessageTokensToInit  int
	MinTokensBetweenUpdate  int
	ToolCallsBetweenUpdates int
}

// DefaultSessionMemoryConfig returns the default configuration.
func DefaultSessionMemoryConfig() SessionMemoryConfig {
	return SessionMemoryConfig{
		MinMessageTokensToInit:  10000,
		MinTokensBetweenUpdate:  5000,
		ToolCallsBetweenUpdates: 3,
	}
}

// DefaultSessionMemoryTemplate is the initial content of session-notes.md.
const DefaultSessionMemoryTemplate = `# Session Title
_A short and distinctive 5-10 word descriptive title for the session._

# Current State
_What is actively being worked on right now?_

# Task specification
_What did the user ask to build?_

# Files and Functions
_What are the important files?_

# Workflow
_What bash commands are usually run?_

# Errors & Corrections
_Errors encountered and how they were fixed._

# Codebase and System Documentation
_What are the important system components?_

# Learnings
_What has worked well? What has not?_

# Key results
_If the user asked a specific output, repeat the exact result here._

# Worklog
_Step by step, what was attempted, done?_
`

// SessionMemoryService maintains a session-notes.md file with structured state.
type SessionMemoryService struct {
	mu             sync.Mutex
	config         SessionMemoryConfig
	initialized    bool
	inProgress     bool
	turnsSinceLast int
	homeDir        string
}

// NewSessionMemoryService creates a service attached to homeDir.
func NewSessionMemoryService(homeDir string) *SessionMemoryService {
	return &SessionMemoryService{
		config:  DefaultSessionMemoryConfig(),
		homeDir: homeDir,
	}
}

func (s *SessionMemoryService) Config() SessionMemoryConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *SessionMemoryService) SetConfig(cfg SessionMemoryConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
}

// GetMemoryPath returns the path to session-notes.md.
func (s *SessionMemoryService) GetMemoryPath() string {
	return filepath.Join(s.homeDir, "session-notes.md")
}

// ReadCurrentMemory reads the current notes file.
func (s *SessionMemoryService) ReadCurrentMemory() (string, error) {
	data, err := os.ReadFile(s.GetMemoryPath())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *SessionMemoryService) writeInitialTemplate() (string, error) {
	if err := os.MkdirAll(s.homeDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(s.GetMemoryPath(), []byte(DefaultSessionMemoryTemplate), 0o600); err != nil {
		return "", err
	}
	return DefaultSessionMemoryTemplate, nil
}

func (s *SessionMemoryService) markInitialized() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialized = true
}

func (s *SessionMemoryService) isInitialized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initialized
}

// Reset clears all state.
func (s *SessionMemoryService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialized = false
	s.inProgress = false
	s.turnsSinceLast = 0
}

// ShouldExtract returns true if enough turns have passed since last update.
func (s *SessionMemoryService) ShouldExtract(messageCount int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if messageCount < 3 {
		return false
	}
	if s.inProgress {
		return false
	}
	return s.turnsSinceLast >= s.config.ToolCallsBetweenUpdates
}

// RecordTurn increments the turn counter. Call after each tool execution.
func (s *SessionMemoryService) RecordTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnsSinceLast++
}

// Extract triggers a model call to update the session notes file.
func (s *SessionMemoryService) Extract(
	ctx context.Context,
	messages []Message,
	callModel func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error),
	systemPrompt string,
) ([]string, error) {
	s.mu.Lock()
	if s.inProgress {
		s.mu.Unlock()
		return nil, fmt.Errorf("session memory extraction already in progress")
	}
	s.inProgress = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.inProgress = false
		s.mu.Unlock()
	}()

	s.markInitialized()

	notesPath := s.GetMemoryPath()
	notes, err := s.ReadCurrentMemory()
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read current memory: %w", err)
		}
		notes, err = s.writeInitialTemplate()
		if err != nil {
			return nil, fmt.Errorf("write initial template: %w", err)
		}
	}

	prompt := BuildSessionMemoryUpdatePrompt(notes, notesPath)

	stream, err := callModel(ctx, provider.CompletionRequest{
		Messages:  append(messages, Message{Role: "user", Content: []agentsdk.ContentBlock{{Type: agentsdk.BlockTypeText, Text: prompt}}}),
		System:    systemPrompt,
		MaxTokens: 16384,
	})
	if err != nil {
		return nil, fmt.Errorf("session memory model call: %w", err)
	}

	var assistantBlocks []agentsdk.ContentBlock
	for evt := range stream {
		switch evt.Type {
		case agentsdk.EventTextDelta:
			if evt.Text != "" {
				assistantBlocks = append(assistantBlocks, agentsdk.ContentBlock{Type: agentsdk.BlockTypeText, Text: evt.Text})
			}
		case agentsdk.EventToolUse:
			if evt.ToolUse != nil {
				assistantBlocks = append(assistantBlocks, agentsdk.ContentBlock{
					Type:  agentsdk.BlockTypeToolUse,
					ID:    evt.ToolUse.ID,
					Name:  evt.ToolUse.Name,
					Input: evt.ToolUse.Input,
				})
			}
		}
	}

	writtenPaths := extractWrittenPaths(assistantBlocks)

	s.mu.Lock()
	s.turnsSinceLast = 0
	s.mu.Unlock()

	return writtenPaths, nil
}

// BuildSessionMemoryUpdatePrompt constructs the prompt for the model.
func BuildSessionMemoryUpdatePrompt(currentNotes string, notesPath string) string {
	return fmt.Sprintf(`IMPORTANT: This message and these instructions are NOT part of the actual user conversation.

The file %s has already been read for you. Here are its current contents:
<current_notes_content>
%s
</current_notes_content>

Your ONLY task is to use the Edit tool to update the notes file, then stop. Make all Edit tool calls in parallel in a single message.

CRITICAL RULES FOR EDITING:
- Do not modify, delete, or add section headers (lines starting with #)
- Do not modify or delete the italic _section description_ lines
- ONLY update the actual content below the italic descriptions within each section
- Write DETAILED, INFO-DENSE content for each section
- Keep each section under ~2000 tokens
- Always update "Current State" to reflect the most recent work

Use the Edit tool with file_path: %s

REMEMBER: Only include insights from the actual user conversation. Do not delete or change section headers or italic descriptions.`, notesPath, currentNotes, notesPath)
}

// TruncateSessionMemoryForCompact truncates each section to maxCharsPerSection.
func TruncateSessionMemoryForCompact(content string, maxCharsPerSection int) (string, bool) {
	if maxCharsPerSection <= 0 {
		maxCharsPerSection = 8000
	}
	lines := strings.Split(content, "\n")
	var outputLines []string
	var currentSectionLines []string
	currentSectionHeader := ""
	wasTruncated := false

	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			result := flushSection(currentSectionHeader, currentSectionLines, maxCharsPerSection)
			outputLines = append(outputLines, result.lines...)
			wasTruncated = wasTruncated || result.wasTruncated
			currentSectionHeader = line
			currentSectionLines = nil
		} else {
			currentSectionLines = append(currentSectionLines, line)
		}
	}

	result := flushSection(currentSectionHeader, currentSectionLines, maxCharsPerSection)
	outputLines = append(outputLines, result.lines...)
	wasTruncated = wasTruncated || result.wasTruncated

	return strings.Join(outputLines, "\n"), wasTruncated
}

type flushResult struct {
	lines        []string
	wasTruncated bool
}

func flushSection(header string, sectionLines []string, maxChars int) flushResult {
	if header == "" {
		return flushResult{lines: sectionLines, wasTruncated: false}
	}

	sectionContent := strings.Join(sectionLines, "\n")
	if len(sectionContent) <= maxChars {
		return flushResult{lines: append([]string{header}, sectionLines...), wasTruncated: false}
	}

	charCount := 0
	keptLines := []string{header}
	for _, line := range sectionLines {
		if charCount+len(line)+1 > maxChars {
			break
		}
		keptLines = append(keptLines, line)
		charCount += len(line) + 1
	}
	keptLines = append(keptLines, "\n[... section truncated for length ...]")
	return flushResult{lines: keptLines, wasTruncated: true}
}

// CountToolCallsSince counts tool_use blocks in messages after sinceUUID.
func CountToolCallsSince(messages []Message, sinceUUID string) int {
	n := 0
	foundStart := sinceUUID == ""
	for _, msg := range messages {
		if !foundStart {
			if id, ok := msg.Metadata["uuid"].(string); ok && id == sinceUUID {
				foundStart = true
			}
			continue
		}
		if msg.Role == "assistant" {
			for _, block := range msg.Content {
				if block.Type == agentsdk.BlockTypeToolUse {
					n++
				}
			}
		}
	}
	return n
}

func extractWrittenPaths(blocks []agentsdk.ContentBlock) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, block := range blocks {
		if block.Type != agentsdk.BlockTypeToolUse || (block.Name != "Edit" && block.Name != "Write") {
			continue
		}
		var input map[string]interface{}
		if err := json.Unmarshal(block.Input, &input); err != nil {
			continue
		}
		if fp, ok := input["file_path"].(string); ok && fp != "" && !seen[fp] {
			seen[fp] = true
			paths = append(paths, fp)
		}
	}
	return paths
}
