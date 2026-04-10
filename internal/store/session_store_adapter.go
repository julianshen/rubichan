package store

import (
	"fmt"

	"github.com/julianshen/rubichan/internal/modes/interactive"
	"github.com/julianshen/rubichan/internal/provider"
)

// SessionStoreAdapter wraps Store to implement the interactive.SessionStore interface.
// It adapts Store's session-focused methods to the SessionStore interface.
type SessionStoreAdapter struct {
	store *Store
}

// NewSessionStoreAdapter creates a new adapter around a Store.
func NewSessionStoreAdapter(store *Store) *SessionStoreAdapter {
	return &SessionStoreAdapter{store: store}
}

// ListSessions implements interactive.SessionStore.ListSessions.
// It returns all saved sessions' metadata with minimal filtering.
func (ssa *SessionStoreAdapter) ListSessions() ([]interactive.SessionMetadata, error) {
	// Fetch all recent sessions (use a large limit for the adapter)
	storeSessions, err := ssa.store.ListSessions(10000)
	if err != nil {
		return nil, fmt.Errorf("list sessions from store: %w", err)
	}

	var metadata []interactive.SessionMetadata
	for _, sess := range storeSessions {
		// Count the turns (messages) in this session
		messages, err := ssa.store.GetMessages(sess.ID)
		if err != nil {
			// If we can't get messages, skip turn count but continue
			// The session still exists, we just won't know the turn count
			messages = []StoredMessage{}
		}

		// Each turn is represented as a pair of messages (user + agent response)
		// So TurnCount = number of complete exchanges = number of "user" messages
		turnCount := 0
		for _, msg := range messages {
			if msg.Role == "user" {
				turnCount++
			}
		}

		metadata = append(metadata, interactive.SessionMetadata{
			ID:        sess.ID,
			CreatedAt: sess.CreatedAt,
			TurnCount: turnCount,
			Project:   sess.WorkingDir, // Use working directory as project proxy
		})
	}

	return metadata, nil
}

// LoadSession implements interactive.SessionStore.LoadSession.
// It converts stored messages back to Turn objects.
func (ssa *SessionStoreAdapter) LoadSession(id string) ([]interactive.Turn, error) {
	// Verify session exists (Issue 2: consolidate into single ListSessions call)
	allSessions, err := ssa.store.ListSessions(10000)
	if err != nil {
		return nil, fmt.Errorf("load session: check existence: %w", err)
	}

	var sessionFound bool
	for _, s := range allSessions {
		if s.ID == id {
			sessionFound = true
			break
		}
	}

	if !sessionFound {
		return nil, fmt.Errorf("load session: session %s not found", id)
	}

	// Get all messages for this session
	messages, err := ssa.store.GetMessages(id)
	if err != nil {
		return nil, fmt.Errorf("load session messages: %w", err)
	}

	// Group messages into turns (user message + agent response)
	var turns []interactive.Turn
	var currentTurnID string
	var userInput string
	var turnIdx int

	for _, msg := range messages {
		if msg.Role == "user" {
			// Start of a new turn
			if userInput != "" && currentTurnID != "" {
				// We have a pending turn - shouldn't happen if messages are well-formed
				// but handle it gracefully
				turns = append(turns, interactive.Turn{
					ID:        currentTurnID,
					Timestamp: msg.CreatedAt,
					UserInput: userInput,
					AgentResp: "",
				})
			}
			turnIdx++
			currentTurnID = fmt.Sprintf("turn-%d", turnIdx)
			// Convert []provider.ContentBlock to []interface{}
			contentAsIface := make([]interface{}, len(msg.Content))
			for i := range msg.Content {
				contentAsIface[i] = msg.Content[i]
			}
			userInput = ssa.extractTextFromContentBlock(contentAsIface)
		} else if msg.Role == "assistant" && currentTurnID != "" {
			// Completion of current turn
			// Convert []provider.ContentBlock to []interface{}
			contentAsIface := make([]interface{}, len(msg.Content))
			for i := range msg.Content {
				contentAsIface[i] = msg.Content[i]
			}
			agentResp := ssa.extractTextFromContentBlock(contentAsIface)
			turns = append(turns, interactive.Turn{
				ID:        currentTurnID,
				Timestamp: msg.CreatedAt,
				UserInput: userInput,
				AgentResp: agentResp,
			})
			currentTurnID = ""
			userInput = ""
		}
	}

	// Append any pending turn that lacks an agent response (Issue 1)
	if userInput != "" && currentTurnID != "" {
		turns = append(turns, interactive.Turn{
			ID:        currentTurnID,
			Timestamp: messages[len(messages)-1].CreatedAt,
			UserInput: userInput,
			AgentResp: "",
		})
	}

	return turns, nil
}

// SaveSession implements interactive.SessionStore.SaveSession.
// It's not used in the resume feature but must be implemented for the interface.
func (ssa *SessionStoreAdapter) SaveSession(id string, turns []interactive.Turn) error {
	// For now, this is a no-op as the Store manages sessions differently.
	// If needed in the future, this would insert turns back into messages.
	return nil
}

// GetSessionMetadata implements interactive.SessionStore.GetSessionMetadata.
func (ssa *SessionStoreAdapter) GetSessionMetadata(id string) (interactive.SessionMetadata, error) {
	// Fetch the session from store
	allSessions, err := ssa.store.ListSessions(10000)
	if err != nil {
		return interactive.SessionMetadata{}, fmt.Errorf("get session metadata: %w", err)
	}

	var sess *Session
	for i := range allSessions {
		if allSessions[i].ID == id {
			sess = &allSessions[i]
			break
		}
	}

	if sess == nil {
		return interactive.SessionMetadata{}, fmt.Errorf("get session metadata: session %s not found", id)
	}

	// Count turns
	messages, err := ssa.store.GetMessages(id)
	if err != nil {
		messages = []StoredMessage{} // Default to no messages if we can't retrieve
	}

	turnCount := 0
	for _, msg := range messages {
		if msg.Role == "user" {
			turnCount++
		}
	}

	return interactive.SessionMetadata{
		ID:        sess.ID,
		CreatedAt: sess.CreatedAt,
		TurnCount: turnCount,
		Project:   sess.WorkingDir,
	}, nil
}

// extractTextFromContentBlock extracts plain text from provider.ContentBlock slices.
// ContentBlock can be text or tool use; we extract only text blocks.
func (ssa *SessionStoreAdapter) extractTextFromContentBlock(blocks []interface{}) string {
	// Provider.ContentBlock is a struct that can be:
	// - Text content: has Type="text" and Text field
	// - Tool use: has Type="tool_use"

	if len(blocks) == 0 {
		return ""
	}

	var textParts []string
	for _, block := range blocks {
		// First try as map (JSON decoded)
		if m, ok := block.(map[string]interface{}); ok {
			if blockType, ok := m["type"].(string); ok && blockType == "text" {
				if text, ok := m["text"].(string); ok {
					textParts = append(textParts, text)
				}
			}
			continue
		}

		// Try as provider.ContentBlock struct
		if cb, ok := block.(provider.ContentBlock); ok {
			if cb.Type == "text" && cb.Text != "" {
				textParts = append(textParts, cb.Text)
			}
		}
	}

	// Join all text parts into a single string
	result := ""
	for _, part := range textParts {
		if result == "" {
			result = part
		} else {
			result += "\n" + part
		}
	}
	return result
}
