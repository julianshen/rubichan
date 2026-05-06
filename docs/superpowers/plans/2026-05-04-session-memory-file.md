# Session Memory File

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port ccgo's `sessionmemory/sessionmemory.go` to rubichan. A `SessionMemoryService` maintains a `session-notes.md` file that the agent updates periodically with structured session state.

**Architecture:** A `SessionMemoryService` writes/reads a `session-notes.md` file in the working directory. Triggered every N tool calls (default 3) or token threshold. Uses a forked agent call (via `callModel` callback) to update the notes file via Edit tool. `TruncateSessionMemoryForCompact` truncates sections for compact injection.

**Tech Stack:** Go, existing `Agent` and `Conversation` types, `provider.CompletionRequest`/`StreamEvent`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/agent/session_memory_service.go` | `SessionMemoryService`, `SessionMemoryConfig`, template, truncation |
| `internal/agent/session_memory_service_test.go` | Unit tests for ShouldExtract, Extract, truncation |
| `internal/agent/agent.go` | Integrate into `runLoop`, call after N tool calls |
| `pkg/agentsdk/compaction.go` | Add `SessionMemoryConfig` type if needed for SDK exposure |

---

## Chunk 1: SessionMemoryService Core

### Task 1: Define SessionMemoryConfig and SessionMemoryService

**Files:**
- Create: `internal/agent/session_memory_service.go`

**Code:**

```go
package agent

import (
	"context"
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
	mu              sync.Mutex
	config          SessionMemoryConfig
	initialized     bool
	inProgress      bool
	lastMessageUUID string
	turnsSinceLast  int
	tokensAtLast    int
	homeDir         string
}

// NewSessionMemoryService creates a service attached to homeDir.
func NewSessionMemoryService(homeDir string) *SessionMemoryService {
	return &SessionMemoryService{
		config:  DefaultSessionMemoryConfig(),
		homeDir: homeDir,
	}
}

// Config returns a copy of the current config.
func (s *SessionMemoryService) Config() SessionMemoryConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

// SetConfig replaces the config.
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

func (s *SessionMemoryService) writeInitialTemplate() error {
	if err := os.MkdirAll(s.homeDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.GetMemoryPath(), []byte(DefaultSessionMemoryTemplate), 0o600)
}

// MarkInitialized marks the service as initialized.
func (s *SessionMemoryService) MarkInitialized() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialized = true
}

// IsInitialized reports whether the service has been initialized.
func (s *SessionMemoryService) IsInitialized() bool {
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
	s.lastMessageUUID = ""
	s.turnsSinceLast = 0
	s.tokensAtLast = 0
}
```

**Test:**

```go
func TestSessionMemoryService_DefaultConfig(t *testing.T) {
	s := NewSessionMemoryService("/tmp/test-session")
	cfg := s.Config()
	assert.Equal(t, 3, cfg.ToolCallsBetweenUpdates)
	assert.Equal(t, 10000, cfg.MinMessageTokensToInit)
}

func TestSessionMemoryService_GetMemoryPath(t *testing.T) {
	s := NewSessionMemoryService("/tmp/test-session")
	assert.Equal(t, "/tmp/test-session/session-notes.md", s.GetMemoryPath())
}

func TestSessionMemoryService_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	s := NewSessionMemoryService(dir)
	err := s.writeInitialTemplate()
	require.NoError(t, err)
	content, err := s.ReadCurrentMemory()
	require.NoError(t, err)
	assert.Contains(t, content, "# Session Title")
	assert.Contains(t, content, "# Worklog")
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestSessionMemoryService -v
```

**Expected:** PASS.

---

### Task 2: Implement ShouldExtract and Extract

**Files:**
- Modify: `internal/agent/session_memory_service.go`

**Code:**

```go
// ShouldExtract returns true if enough tool calls have passed since last update.
func (s *SessionMemoryService) ShouldExtract(messages []provider.Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(messages) < 3 {
		return false
	}
	if s.inProgress {
		return false
	}
	s.turnsSinceLast++
	return s.turnsSinceLast >= s.config.ToolCallsBetweenUpdates
}

// Extract triggers a model call to update the session notes file.
func (s *SessionMemoryService) Extract(
	ctx context.Context,
	messages []provider.Message,
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

	if !s.ShouldExtract(messages) {
		return nil, nil
	}

	s.MarkInitialized()

	notesPath := s.GetMemoryPath()
	if _, err := os.Stat(notesPath); os.IsNotExist(err) {
		if writeErr := s.writeInitialTemplate(); writeErr != nil {
			return nil, fmt.Errorf("write initial template: %w", writeErr)
		}
	}

	notes, err := s.ReadCurrentMemory()
	if err != nil {
		return nil, fmt.Errorf("read current memory: %w", err)
	}

	prompt := BuildSessionMemoryUpdatePrompt(notes, notesPath)
	userMsg := provider.NewUserMessage(prompt)

	agentMessages := make([]provider.Message, len(messages), len(messages)+1)
	copy(agentMessages, messages)
	agentMessages = append(agentMessages, userMsg)

	stream, err := callModel(ctx, provider.CompletionRequest{
		Messages: agentMessages,
		System:   systemPrompt,
		MaxTokens: 16384,
	})
	if err != nil {
		return nil, fmt.Errorf("session memory model call: %w", err)
	}

	var assistantBlocks []provider.ContentBlock
	for evt := range stream {
		switch evt.Type {
		case "content_block_delta":
			if evt.Text != "" {
				assistantBlocks = append(assistantBlocks, provider.ContentBlock{Type: "text", Text: evt.Text})
			}
		case "tool_use":
			if evt.ToolUse != nil {
				assistantBlocks = append(assistantBlocks, provider.ContentBlock{
					Type: "tool_use",
					ID:   evt.ToolUse.ID,
					Name: evt.ToolUse.Name,
					Input: evt.ToolUse.Input,
				})
			}
		}
	}

	writtenPaths := extractWrittenPaths(assistantBlocks)

	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		s.mu.Lock()
		if id, ok := lastMsg.Metadata["uuid"].(string); ok {
			s.lastMessageUUID = id
		}
		s.turnsSinceLast = 0
		s.mu.Unlock()
	}

	return writtenPaths, nil
}

func extractWrittenPaths(blocks []provider.ContentBlock) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, block := range blocks {
		if block.Type != "tool_use" || (block.Name != "Edit" && block.Name != "Write") {
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
```

**Test:**

```go
func TestShouldExtract(t *testing.T) {
	dir := t.TempDir()
	s := NewSessionMemoryService(dir)
	_ = s.writeInitialTemplate()

	msgs := make([]provider.Message, 5)
	for i := range msgs {
		msgs[i] = provider.NewUserMessage(fmt.Sprintf("msg %d", i))
	}

	// First call: turnsSinceLast becomes 1, not enough
	assert.False(t, s.ShouldExtract(msgs))
	// Simulate 2 more calls
	s.mu.Lock()
	s.turnsSinceLast = 2
	s.mu.Unlock()
	assert.True(t, s.ShouldExtract(msgs))
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestShouldExtract -v
```

**Expected:** PASS.

---

### Task 3: Add TruncateSessionMemoryForCompact

**Files:**
- Modify: `internal/agent/session_memory_service.go`

**Code:**

```go
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
func CountToolCallsSince(messages []provider.Message, sinceUUID string) int {
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
				if block.Type == "tool_use" {
					n++
				}
			}
		}
	}
	return n
}
```

**Test:**

```go
func TestTruncateSessionMemoryForCompact(t *testing.T) {
	content := "# Section A\nline1\nline2\n# Section B\n" + strings.Repeat("x", 100)
	truncated, wasTruncated := TruncateSessionMemoryForCompact(content, 50)
	assert.True(t, wasTruncated)
	assert.Contains(t, truncated, "[... section truncated for length ...]")
	assert.Contains(t, truncated, "# Section A")
	assert.Contains(t, truncated, "# Section B")
}

func TestCountToolCallsSince(t *testing.T) {
	msgs := []provider.Message{
		{Role: "assistant", Metadata: map[string]any{"uuid": "m1"}, Content: []agentsdk.ContentBlock{{Type: "tool_use", ID: "t1"}}},
		{Role: "assistant", Metadata: map[string]any{"uuid": "m2"}, Content: []agentsdk.ContentBlock{{Type: "tool_use", ID: "t2"}}},
		{Role: "assistant", Metadata: map[string]any{"uuid": "m3"}, Content: []agentsdk.ContentBlock{{Type: "tool_use", ID: "t3"}}},
	}
	assert.Equal(t, 2, CountToolCallsSince(msgs, "m1"))
	assert.Equal(t, 1, CountToolCallsSince(msgs, "m2"))
	assert.Equal(t, 3, CountToolCallsSince(msgs, ""))
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestTruncateSessionMemoryForCompact -v
go test ./internal/agent/... -run TestCountToolCallsSince -v
```

**Expected:** PASS.

---

## Chunk 2: Agent Integration

### Task 4: Wire SessionMemoryService into Agent

**Files:**
- Modify: `internal/agent/agent.go`

**Code:**

Add field to `Agent` struct:

```go
type Agent struct {
	// ... existing fields ...
	sessionMemory *SessionMemoryService
}
```

Add option:

```go
// WithSessionMemory attaches a session memory service.
func WithSessionMemory(sm *SessionMemoryService) AgentOption {
	return func(a *Agent) {
		a.sessionMemory = sm
	}
}
```

In `New`, initialize if not provided:

```go
if a.sessionMemory == nil {
	a.sessionMemory = NewSessionMemoryService(a.workingDir)
}
```

In `runLoop`, after tool execution, trigger session memory extraction:

```go
// After tool execution and before continuing to next turn:
if a.sessionMemory != nil && a.sessionMemory.ShouldExtract(a.conversation.Messages()) {
	go func() {
		_, err := a.sessionMemory.Extract(ctx, a.conversation.Messages(), func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
			return a.provider.Stream(ctx, req)
		}, a.conversation.SystemPrompt())
		if err != nil {
			a.logger.Warn("session memory extraction failed: %v", err)
		}
	}()
}
```

**Test:**

```go
func TestAgent_SessionMemoryOption(t *testing.T) {
	sm := NewSessionMemoryService(t.TempDir())
	// Agent construction with option
	_ = sm // placeholder; actual test would construct agent
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestAgent_SessionMemoryOption -v
```

**Expected:** Compile check passes.

---

## Validation Commands

```bash
go test ./internal/agent/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Session memory file with periodic updates`

**Body:**
- `SessionMemoryService` maintains a `session-notes.md` file with structured session state
- Default template with 10 sections: Session Title, Current State, Task spec, Files/Functions, Workflow, Errors, Learnings, Key results, Worklog
- Triggered every N tool calls (default 3) via `ShouldExtract`
- `Extract` uses a forked model call to update notes via Edit tool
- `TruncateSessionMemoryForCompact` truncates sections for compact injection
- `CountToolCallsSince` counts tool calls since last update
- Integrated into `Agent.runLoop` after tool execution
- Ports ccgo's `sessionmemory/sessionmemory.go` pattern to rubichan

**Commit prefix:** `[BEHAVIORAL]`
