package team

import (
	"encoding/json"
	"fmt"
	"html"
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
		if unmarshalErr := json.Unmarshal(data, &messages); unmarshalErr != nil {
			return fmt.Errorf("mailbox unmarshal: %w", unmarshalErr)
		}
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
			colorAttr = fmt.Sprintf(` color="%s"`, html.EscapeString(msg.Color))
		}
		summaryAttr := ""
		if msg.Summary != "" {
			summaryAttr = fmt.Sprintf(` summary="%s"`, html.EscapeString(msg.Summary))
		}
		sb.WriteString(fmt.Sprintf("<teammate_message teammate_id=\"%s\"%s%s>\n%s\n</teammate_message>\n", html.EscapeString(msg.From), colorAttr, summaryAttr, html.EscapeString(msg.Text)))
	}
	return sb.String()
}
