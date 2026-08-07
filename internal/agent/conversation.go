package agent

import (
	"sync"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Conversation manages the message history for an agent session.
//
// It is safe for concurrent use. That is a requirement, not a courtesy: a turn
// runs its loop on its own goroutine while the periodic summarizer reads the
// same conversation from a timer goroutine, so reads and writes genuinely
// overlap for the length of every turn.
//
// The lock makes each method atomic, which is what memory safety needs. It
// does not make a read-then-write pair across two calls atomic — a caller that
// reads Messages, transforms them and writes them back can still lose a
// concurrent append in between. Compaction does exactly that, and is safe today
// only because it runs before the turn goroutine starts. Anything new that
// mutates from a second goroutine needs a method that holds the lock across the
// whole operation, not two calls in a row.
type Conversation struct {
	// mu guards messages. systemPrompt is written once by NewConversation and
	// never again, so SystemPrompt reads it without locking.
	mu           sync.RWMutex
	systemPrompt string
	messages     []provider.Message
}

// NewConversation creates a new Conversation with the given system prompt.
func NewConversation(systemPrompt string) *Conversation {
	return &Conversation{
		systemPrompt: systemPrompt,
	}
}

// SystemPrompt returns the system prompt for this conversation.
func (c *Conversation) SystemPrompt() string {
	return c.systemPrompt
}

// Messages returns a copy of all messages in the conversation.
//
// The copy is shallow: the returned slice is independent, but each message's
// Content slice and Metadata map still alias the conversation's own. Callers
// must treat everything reachable from a returned Message as read-only. A
// caller that mutates a Content element or a Metadata entry writes through to
// the conversation, outside c.mu, and races every other holder of that message.
//
// This is an ownership rule the type states rather than enforces. Deep-cloning
// each message at every egress would enforce it, at the cost of copying every
// content block on a path that runs before each LLM request — and no current
// reader mutates. If that stops being true, clone at ingress and egress rather
// than hoping.
func (c *Conversation) Messages() []provider.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make([]provider.Message, len(c.messages))
	copy(cp, c.messages)
	return cp
}

// Len returns the number of messages without allocating a copy.
func (c *Conversation) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.messages)
}

// AddUser appends a user message to the conversation.
func (c *Conversation) AddUser(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, provider.NewUserMessage(text))
}

// AddAssistant appends an assistant message with the given content blocks.
func (c *Conversation) AddAssistant(blocks []provider.ContentBlock) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, provider.Message{
		Role:    "assistant",
		Content: blocks,
	})
}

// AddToolResult appends a tool result message to the conversation.
func (c *Conversation) AddToolResult(toolUseID, content string, isError bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, provider.NewToolResultMessage(toolUseID, content, isError))
}

// AddSystem appends a system message to the conversation.
// Empty strings are ignored to avoid polluting the conversation.
func (c *Conversation) AddSystem(text string) {
	if text == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, provider.Message{
		Role:    "system",
		Content: []provider.ContentBlock{{Type: "text", Text: text}},
	})
}

// LoadFromMessages replaces the current message history with the given messages.
// The system prompt is preserved. This is used when resuming a saved session.
func (c *Conversation) LoadFromMessages(msgs []provider.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = make([]provider.Message, len(msgs))
	copy(c.messages, msgs)
}

// DrainMessages removes the oldest message pairs until only minPairsToKeep
// remain. Returns true if any messages were removed. Copies into a fresh
// slice to avoid retaining the drained messages in the backing array.
// Ensures the kept slice starts on a non-tool_result boundary.
func (c *Conversation) DrainMessages(minPairsToKeep int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.messages) <= minPairsToKeep*2 {
		return false
	}
	cutoff := len(c.messages) - minPairsToKeep*2
	if cutoff <= 0 {
		return false
	}
	for cutoff < len(c.messages) && cutoff > 0 && c.messages[cutoff].Role == "tool_result" {
		cutoff++
	}
	if cutoff >= len(c.messages) {
		return false
	}
	kept := make([]provider.Message, len(c.messages)-cutoff)
	copy(kept, c.messages[cutoff:])
	c.messages = kept
	return true
}

// Clear removes all messages but preserves the system prompt.
func (c *Conversation) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = nil
}

// Tombstone replaces messages in the range [startIdx:endIdx] with
// tombstone markers. The message slots are retained in the slice
// (preserving indices for history) but their content is replaced
// with a marker and skipped when building API requests.
func (c *Conversation) Tombstone(startIdx, endIdx int, reason agentsdk.TombstoneReason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tombstoneLocked(startIdx, endIdx, reason)
}

// tombstoneLocked is Tombstone's body, split out because
// TombstoneSinceLastAssistant needs it while already holding the lock and Go's
// mutexes are not reentrant.
func (c *Conversation) tombstoneLocked(startIdx, endIdx int, reason agentsdk.TombstoneReason) {
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(c.messages) {
		endIdx = len(c.messages)
	}
	if startIdx >= endIdx {
		return
	}

	for i := startIdx; i < endIdx; i++ {
		c.messages[i].Content = []provider.ContentBlock{{
			Type: "text",
			Text: agentsdk.TombstoneMarker,
		}}
		c.messages[i].Metadata = map[string]any{
			"tombstoned": true,
			"reason":     reason,
		}
	}
}

// TombstoneSinceLastAssistant tombstones all messages since the last
// non-tombstoned assistant message. Used when model fallback occurs
// mid-stream to prevent partial responses from polluting the fallback
// model's context.
//
// The scan and the write happen under one lock. Splitting them would let a
// concurrent append land between choosing the boundary and applying it, and the
// new message would be tombstoned despite arriving after the failure this is
// cleaning up.
func (c *Conversation) TombstoneSinceLastAssistant(reason agentsdk.TombstoneReason) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find the last complete assistant message
	lastAssistantIdx := -1
	for i := len(c.messages) - 1; i >= 0; i-- {
		if c.messages[i].Role == "assistant" && !c.isTombstonedLocked(i) {
			lastAssistantIdx = i
			break
		}
	}

	if lastAssistantIdx < 0 {
		// No complete assistant message — tombstone everything
		c.tombstoneLocked(0, len(c.messages), reason)
		return len(c.messages)
	}

	// Tombstone messages after the last assistant
	startIdx := lastAssistantIdx + 1
	c.tombstoneLocked(startIdx, len(c.messages), reason)
	return len(c.messages) - startIdx
}

// isTombstonedLocked reports whether the message at idx is tombstoned.
// Out-of-bounds and empty-content messages are conservatively treated
// as not tombstoned to avoid false positives during filtering.
// Callers must hold c.mu.
func (c *Conversation) isTombstonedLocked(idx int) bool {
	if idx < 0 || idx >= len(c.messages) {
		return false
	}
	if len(c.messages[idx].Content) == 0 {
		return false
	}
	return agentsdk.IsTombstonedMessage(c.messages[idx])
}
