# Mailbox + SendMessage Tool

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port ccgo's `team/mailbox.go` to rubichan. A `Mailbox` uses file-based JSON for async A2A messaging between agents. Each agent has an inbox file. Messages have types (text, shutdown_request, task_update, etc.).

**Architecture:** A single `Mailbox` instance manages per-agent JSON inboxes under a directory. Thread-safe via `sync.Mutex`. Messages are appended to JSON arrays. ReadUnread filters by `Read=false`. MarkRead/MarkAllRead update the `Read` flag. FormatMessagesAsXML serializes messages for prompt injection.

**Tech Stack:** Go, standard library (`encoding/json`, `os`, `path/filepath`, `strings`, `sync`, `time`).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/team/mailbox.go` | `Mailbox`, `MailboxMessage`, `MessageType`, `FormatMessagesAsXML` |
| `internal/team/mailbox_test.go` | Tests for Write/Read/ReadUnread/MarkRead/MarkAllRead/Clear/FormatMessagesAsXML |
| `pkg/agentsdk/team.go` | `MailboxMessage` struct, `MessageType` constants (public SDK types) |

---

## Chunk 1: SDK Types

### Task 1: Define MailboxMessage and MessageType in SDK

**Files:**
- Create: `pkg/agentsdk/team.go`

**Code:**

```go
package agentsdk

import "time"

// MessageType categorizes messages in the team mailbox.
type MessageType string

const (
	MessageTypeText             MessageType = "text"
	MessageTypeShutdownRequest  MessageType = "shutdown_request"
	MessageTypeShutdownApproved MessageType = "shutdown_approved"
	MessageTypeShutdownRejected MessageType = "shutdown_rejected"
	MessageTypeTaskUpdate       MessageType = "task_update"
	MessageTypeIdle             MessageType = "idle"
	MessageTypePlanApproval     MessageType = "plan_approval"
)

// MailboxMessage is a single message in an agent's inbox.
type MailboxMessage struct {
	From      string                 `json:"from"`
	To        string                 `json:"to"`
	Text      string                 `json:"text,omitempty"`
	Type      MessageType            `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Color     string                 `json:"color,omitempty"`
	Summary   string                 `json:"summary,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Read      bool                   `json:"read"`
}
```

**Test:**

```go
package agentsdk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMailboxMessageDefaults(t *testing.T) {
	msg := MailboxMessage{
		From: "alice",
		To:   "bob",
		Text: "hello",
		Type: MessageTypeText,
	}
	require.Equal(t, "alice", msg.From)
	require.Equal(t, "bob", msg.To)
	require.Equal(t, "hello", msg.Text)
	require.Equal(t, MessageTypeText, msg.Type)
	require.False(t, msg.Read)
	require.Nil(t, msg.Data)
}

func TestMessageTypeConstants(t *testing.T) {
	require.Equal(t, MessageType("text"), MessageTypeText)
	require.Equal(t, MessageType("shutdown_request"), MessageTypeShutdownRequest)
	require.Equal(t, MessageType("shutdown_approved"), MessageTypeShutdownApproved)
	require.Equal(t, MessageType("shutdown_rejected"), MessageTypeShutdownRejected)
	require.Equal(t, MessageType("task_update"), MessageTypeTaskUpdate)
	require.Equal(t, MessageType("idle"), MessageTypeIdle)
	require.Equal(t, MessageType("plan_approval"), MessageTypePlanApproval)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestMailbox -v
```

**Expected:** PASS.

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Write minimal implementation**
- [ ] **Step 4: Run test to verify it passes**
- [ ] **Step 5: Commit**

```bash
git add pkg/agentsdk/team.go pkg/agentsdk/team_test.go
git commit -m "[STRUCTURAL] Add MailboxMessage and MessageType to agentsdk"
```

---

## Chunk 2: Mailbox Implementation

### Task 2: Implement Mailbox with Write and Read

**Files:**
- Create: `internal/team/mailbox.go`

**Code:**

```go
package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Mailbox manages per-agent JSON inboxes for async A2A messaging.
type Mailbox struct {
	dir string
	mu  sync.Mutex
}

// NewMailbox creates a Mailbox that stores inboxes under dir.
func NewMailbox(dir string) *Mailbox {
	return &Mailbox{dir: dir}
}

// EnsureDir creates the mailbox directory if it doesn't exist.
func (m *Mailbox) EnsureDir() error {
	return os.MkdirAll(m.dir, 0o755)
}

// InboxPath returns the file path for an agent's inbox.
func (m *Mailbox) InboxPath(agentName string) string {
	return filepath.Join(m.dir, agentName+".json")
}

// Write appends a message to the agent's inbox.
func (m *Mailbox) Write(agentName string, msg agentsdk.MailboxMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.InboxPath(agentName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mailbox mkdir: %w", err)
	}

	var messages []agentsdk.MailboxMessage
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &messages)
	}

	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	messages = append(messages, msg)

	encoded, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("mailbox marshal: %w", err)
	}

	return os.WriteFile(path, encoded, 0o644)
}

// Read returns all messages in the agent's inbox.
func (m *Mailbox) Read(agentName string) ([]agentsdk.MailboxMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.readLocked(agentName)
}

func (m *Mailbox) readLocked(agentName string) ([]agentsdk.MailboxMessage, error) {
	path := m.InboxPath(agentName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("mailbox read: %w", err)
	}

	var messages []agentsdk.MailboxMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("mailbox unmarshal: %w", err)
	}
	return messages, nil
}

// ReadUnread returns only unread messages.
func (m *Mailbox) ReadUnread(agentName string) ([]agentsdk.MailboxMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	messages, err := m.readLocked(agentName)
	if err != nil {
		return nil, err
	}

	var unread []agentsdk.MailboxMessage
	for _, msg := range messages {
		if !msg.Read {
			unread = append(unread, msg)
		}
	}
	return unread, nil
}

// MarkRead marks a single message at index as read.
func (m *Mailbox) MarkRead(agentName string, index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	messages, err := m.readLocked(agentName)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(messages) {
		return fmt.Errorf("index %d out of range [0, %d)", index, len(messages))
	}

	messages[index].Read = true
	return m.writeLocked(agentName, messages)
}

// MarkAllRead marks all messages as read.
func (m *Mailbox) MarkAllRead(agentName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	messages, err := m.readLocked(agentName)
	if err != nil {
		return err
	}

	for i := range messages {
		messages[i].Read = true
	}
	return m.writeLocked(agentName, messages)
}

// Clear removes an agent's inbox file.
func (m *Mailbox) Clear(agentName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.InboxPath(agentName)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Mailbox) writeLocked(agentName string, messages []agentsdk.MailboxMessage) error {
	path := m.InboxPath(agentName)
	encoded, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("mailbox marshal: %w", err)
	}
	return os.WriteFile(path, encoded, 0o644)
}

// FormatMessagesAsXML serializes messages as XML for prompt injection.
func FormatMessagesAsXML(messages []agentsdk.MailboxMessage) string {
	var sb strings.Builder
	for _, msg := range messages {
		colorAttr := ""
		if msg.Color != "" {
			colorAttr = fmt.Sprintf(` color="%s"`, msg.Color)
		}
		summaryAttr := ""
		if msg.Summary != "" {
			summaryAttr = fmt.Sprintf(` summary="%s"`, msg.Summary)
		}
		sb.WriteString(fmt.Sprintf(`<teammate_message teammate_id="%s"%s%s>\n%s\n</teammate_message>\n`, msg.From, colorAttr, summaryAttr, msg.Text))
	}
	return sb.String()
}
```

**Test:**

```go
package team

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestMailboxWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	msg := agentsdk.MailboxMessage{
		From: "alice",
		To:   "bob",
		Text: "hello",
		Type: agentsdk.MessageTypeText,
	}
	require.NoError(t, mb.Write("bob", msg))

	messages, err := mb.Read("bob")
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "alice", messages[0].From)
	require.Equal(t, "hello", messages[0].Text)
}

func TestMailboxReadEmpty(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	messages, err := mb.Read("nobody")
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestMailboxReadUnread(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg1", Type: agentsdk.MessageTypeText}))
	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg2", Type: agentsdk.MessageTypeText}))

	unread, err := mb.ReadUnread("bob")
	require.NoError(t, err)
	require.Len(t, unread, 2)

	require.NoError(t, mb.MarkRead("bob", 0))

	unread, err = mb.ReadUnread("bob")
	require.NoError(t, err)
	require.Len(t, unread, 1)
	require.Equal(t, "msg2", unread[0].Text)
}

func TestMailboxMarkReadOutOfRange(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	err := mb.MarkRead("bob", 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

func TestMailboxMarkAllRead(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg1", Type: agentsdk.MessageTypeText}))
	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg2", Type: agentsdk.MessageTypeText}))
	require.NoError(t, mb.MarkAllRead("bob"))

	unread, err := mb.ReadUnread("bob")
	require.NoError(t, err)
	require.Empty(t, unread)
}

func TestMailboxClear(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg1", Type: agentsdk.MessageTypeText}))
	require.NoError(t, mb.Clear("bob"))

	messages, err := mb.Read("bob")
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestMailboxClearMissing(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	require.NoError(t, mb.Clear("nobody"))
}

func TestFormatMessagesAsXML(t *testing.T) {
	messages := []agentsdk.MailboxMessage{
		{From: "alice", Text: "hello", Color: "\033[34m"},
		{From: "bob", Text: "world", Summary: "greeting"},
	}
	xml := FormatMessagesAsXML(messages)
	require.Contains(t, xml, `<teammate_message teammate_id="alice" color="\033[34m">`)
	require.Contains(t, xml, `hello`)
	require.Contains(t, xml, `<teammate_message teammate_id="bob" summary="greeting">`)
	require.Contains(t, xml, `world`)
}
```

**Command:**
```bash
go test ./internal/team/... -run TestMailbox -v
```

**Expected:** PASS.

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Write minimal implementation**
- [ ] **Step 4: Run test to verify it passes**
- [ ] **Step 5: Commit**

```bash
git add internal/team/mailbox.go internal/team/mailbox_test.go
git commit -m "[BEHAVIORAL] Implement Mailbox with Write, Read, ReadUnread, MarkRead, MarkAllRead, Clear"
```

---

## Chunk 3: Integration

### Task 3: Wire Mailbox into existing team infrastructure

**Files:**
- Modify: `internal/team/mailbox.go` (no changes needed — standalone)

The Mailbox is intentionally standalone. Future chunks (Coordinator) will instantiate it.

**Command:**
```bash
go test ./internal/team/...
```

**Expected:** All tests pass.

- [ ] **Step 1: Verify tests pass**
- [ ] **Step 2: Commit**

```bash
git add internal/team/mailbox.go internal/team/mailbox_test.go
git commit -m "[STRUCTURAL] Mailbox ready for Coordinator integration"
```

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/team/...
go test -cover ./internal/team/...
golangci-lint run ./internal/team/...
gofmt -l .
```

---

## PR Description

**Title:** `[STRUCTURAL] Mailbox + SendMessage Tool for async A2A messaging`

**Body:**
- `Mailbox` manages per-agent JSON inboxes under a directory
- Thread-safe with `sync.Mutex`
- `MailboxMessage` with typed messages: text, shutdown_request, shutdown_approved, shutdown_rejected, task_update, idle, plan_approval
- `ReadUnread` filters unread messages
- `MarkRead` / `MarkAllRead` update read status
- `Clear` removes an agent's inbox
- `FormatMessagesAsXML` serializes messages for prompt injection
- Ports ccgo's `team/mailbox.go` to rubichan

**Commit prefix:** `[STRUCTURAL]`
