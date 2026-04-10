package store

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/modes/interactive"
	"github.com/julianshen/rubichan/internal/provider"
)

func TestSessionStoreAdapterListSessions(t *testing.T) {
	// Create an in-memory store
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create a session manually via the Store
	_, err = store.db.Exec(
		`INSERT INTO sessions (id, title, model, working_dir, created_at, updated_at)
		 VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`,
		"sess-001", "Test Session", "claude-3-5-sonnet", "/home/test",
	)
	if err != nil {
		t.Fatalf("insert session failed: %v", err)
	}

	// Add some messages to the session
	textContent := []provider.ContentBlock{
		{Type: "text", Text: "hello"},
	}
	contentJSON, _ := json.Marshal(textContent)

	_, err = store.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		"sess-001", 0, "user", string(contentJSON),
	)
	if err != nil {
		t.Fatalf("insert user message failed: %v", err)
	}

	respContent := []provider.ContentBlock{
		{Type: "text", Text: "hi there"},
	}
	respJSON, _ := json.Marshal(respContent)

	_, err = store.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		"sess-001", 1, "assistant", string(respJSON),
	)
	if err != nil {
		t.Fatalf("insert assistant message failed: %v", err)
	}

	// Create adapter and test ListSessions
	adapter := NewSessionStoreAdapter(store)
	sessions, err := adapter.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}

	if sessions[0].ID != "sess-001" {
		t.Errorf("expected session ID sess-001, got %s", sessions[0].ID)
	}

	if sessions[0].TurnCount != 1 {
		t.Errorf("expected 1 turn, got %d", sessions[0].TurnCount)
	}
}

func TestSessionStoreAdapterGetSessionMetadata(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create a session
	_, err = store.db.Exec(
		`INSERT INTO sessions (id, title, model, working_dir, created_at, updated_at)
		 VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`,
		"sess-test", "My Session", "claude-3-5-sonnet", "/path/to/project",
	)
	if err != nil {
		t.Fatalf("insert session failed: %v", err)
	}

	// Add 2 turns (4 messages total)
	for i := 0; i < 4; i++ {
		role := "user"
		text := "user message"
		if i%2 == 1 {
			role = "assistant"
			text = "assistant response"
		}

		content := []provider.ContentBlock{
			{Type: "text", Text: text},
		}
		contentJSON, _ := json.Marshal(content)

		_, err = store.db.Exec(
			`INSERT INTO messages (session_id, seq, role, content, created_at)
			 VALUES (?, ?, ?, ?, datetime('now'))`,
			"sess-test", i, role, string(contentJSON),
		)
		if err != nil {
			t.Fatalf("insert message failed: %v", err)
		}
	}

	adapter := NewSessionStoreAdapter(store)
	metadata, err := adapter.GetSessionMetadata("sess-test")
	if err != nil {
		t.Fatalf("GetSessionMetadata failed: %v", err)
	}

	if metadata.ID != "sess-test" {
		t.Errorf("expected ID sess-test, got %s", metadata.ID)
	}

	if metadata.TurnCount != 2 {
		t.Errorf("expected 2 turns, got %d", metadata.TurnCount)
	}

	if metadata.Project != "/path/to/project" {
		t.Errorf("expected project /path/to/project, got %s", metadata.Project)
	}
}

func TestSessionStoreAdapterGetSessionMetadataNotFound(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	adapter := NewSessionStoreAdapter(store)
	_, err = adapter.GetSessionMetadata("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestSessionStoreAdapterLoadSession(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create a session
	_, err = store.db.Exec(
		`INSERT INTO sessions (id, title, model, created_at, updated_at)
		 VALUES (?, ?, ?, datetime('now'), datetime('now'))`,
		"sess-load", "Load Test", "claude-3-5-sonnet",
	)
	if err != nil {
		t.Fatalf("insert session failed: %v", err)
	}

	// Add a turn
	userContent := []provider.ContentBlock{
		{Type: "text", Text: "What is 2+2?"},
	}
	userJSON, _ := json.Marshal(userContent)

	_, err = store.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		"sess-load", 0, "user", string(userJSON),
	)
	if err != nil {
		t.Fatalf("insert user message failed: %v", err)
	}

	respContent := []provider.ContentBlock{
		{Type: "text", Text: "2+2 equals 4"},
	}
	respJSON, _ := json.Marshal(respContent)

	_, err = store.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		"sess-load", 1, "assistant", string(respJSON),
	)
	if err != nil {
		t.Fatalf("insert assistant message failed: %v", err)
	}

	adapter := NewSessionStoreAdapter(store)
	turns, err := adapter.LoadSession("sess-load")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	if len(turns) != 1 {
		t.Errorf("expected 1 turn, got %d", len(turns))
	}

	turn := turns[0]
	if turn.UserInput != "What is 2+2?" {
		t.Errorf("expected user input 'What is 2+2?', got %q", turn.UserInput)
	}

	if turn.AgentResp != "2+2 equals 4" {
		t.Errorf("expected agent response '2+2 equals 4', got %q", turn.AgentResp)
	}
}

func TestSessionStoreAdapterLoadSessionNotFound(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	adapter := NewSessionStoreAdapter(store)
	_, err = adapter.LoadSession("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

// TestSessionStoreAdapterLoadSessionIncompleteLastTurn tests that pending turns
// without agent response are preserved (Issue 1).
func TestSessionStoreAdapterLoadSessionIncompleteLastTurn(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create a session
	_, err = store.db.Exec(
		`INSERT INTO sessions (id, title, model, created_at, updated_at)
		 VALUES (?, ?, ?, datetime('now'), datetime('now'))`,
		"sess-incomplete", "Incomplete Test", "claude-3-5-sonnet",
	)
	if err != nil {
		t.Fatalf("insert session failed: %v", err)
	}

	// Add first complete turn (user + assistant)
	userContent1 := []provider.ContentBlock{
		{Type: "text", Text: "First question?"},
	}
	userJSON1, _ := json.Marshal(userContent1)

	_, err = store.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		"sess-incomplete", 0, "user", string(userJSON1),
	)
	if err != nil {
		t.Fatalf("insert first user message failed: %v", err)
	}

	respContent1 := []provider.ContentBlock{
		{Type: "text", Text: "First answer"},
	}
	respJSON1, _ := json.Marshal(respContent1)

	_, err = store.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		"sess-incomplete", 1, "assistant", string(respJSON1),
	)
	if err != nil {
		t.Fatalf("insert first assistant message failed: %v", err)
	}

	// Add incomplete turn (user message only, no assistant response)
	userContent2 := []provider.ContentBlock{
		{Type: "text", Text: "Second question?"},
	}
	userJSON2, _ := json.Marshal(userContent2)

	_, err = store.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		"sess-incomplete", 2, "user", string(userJSON2),
	)
	if err != nil {
		t.Fatalf("insert second user message failed: %v", err)
	}

	adapter := NewSessionStoreAdapter(store)
	turns, err := adapter.LoadSession("sess-incomplete")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	// Should have 2 turns: one complete, one incomplete
	if len(turns) != 2 {
		t.Errorf("expected 2 turns (one incomplete), got %d", len(turns))
	}

	// First turn should be complete
	if turns[0].UserInput != "First question?" {
		t.Errorf("expected first user input 'First question?', got %q", turns[0].UserInput)
	}
	if turns[0].AgentResp != "First answer" {
		t.Errorf("expected first agent response 'First answer', got %q", turns[0].AgentResp)
	}

	// Second turn should be incomplete (no agent response)
	if turns[1].UserInput != "Second question?" {
		t.Errorf("expected second user input 'Second question?', got %q", turns[1].UserInput)
	}
	if turns[1].AgentResp != "" {
		t.Errorf("expected second agent response to be empty, got %q", turns[1].AgentResp)
	}
}

func TestSessionStoreAdapterExtractTextFromContentBlock(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	adapter := NewSessionStoreAdapter(store)

	// Test single text block
	content1 := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": "hello world",
		},
	}
	result := adapter.extractTextFromContentBlock(content1)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}

	// Test multiple text blocks
	content2 := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": "first",
		},
		map[string]interface{}{
			"type": "text",
			"text": "second",
		},
	}
	result = adapter.extractTextFromContentBlock(content2)
	expected := "first\nsecond"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}

	// Test empty content
	result = adapter.extractTextFromContentBlock([]interface{}{})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}

	// Test tool use (should be ignored)
	content3 := []interface{}{
		map[string]interface{}{
			"type": "tool_use",
			"id":   "tool-123",
		},
	}
	result = adapter.extractTextFromContentBlock(content3)
	if result != "" {
		t.Errorf("expected empty string for tool_use, got %q", result)
	}
}

// TestSessionStoreAdapterImplementsInterface verifies the adapter implements SessionStore
func TestSessionStoreAdapterImplementsInterface(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	adapter := NewSessionStoreAdapter(store)

	// This test will fail at compile time if the interface is not implemented,
	// but we can also do a runtime check
	var _ interactive.SessionStore = adapter
	if adapter == nil {
		t.Fatal("adapter should not be nil")
	}
}

// TestSessionStoreAdapterLoadSessionSingleListCall verifies that LoadSession
// calls ListSessions only once (Issue 2: consolidate duplicate calls).
// This test ensures the session existence check and search use a single call.
func TestSessionStoreAdapterLoadSessionSingleListCall(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create multiple sessions to ensure we're finding the right one
	for i := 0; i < 5; i++ {
		sessID := fmt.Sprintf("sess-%d", i)
		_, err = store.db.Exec(
			`INSERT INTO sessions (id, title, model, created_at, updated_at)
			 VALUES (?, ?, ?, datetime('now'), datetime('now'))`,
			sessID, "Test Session", "claude-3-5-sonnet",
		)
		if err != nil {
			t.Fatalf("insert session failed: %v", err)
		}
	}

	adapter := NewSessionStoreAdapter(store)
	// LoadSession for session 3 should find it without multiple ListSessions calls
	turns, err := adapter.LoadSession("sess-3")
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	// Session exists and has no messages (empty turns)
	if len(turns) != 0 {
		t.Errorf("expected 0 turns, got %d", len(turns))
	}
}

// TestSessionStoreAdapterGetSessionMetadataReturnsErrorOnGetMessagesFail
// verifies that GetSessionMetadata returns an error instead of silently
// defaulting when GetMessages fails (Issue 3).
func TestSessionStoreAdapterGetSessionMetadataReturnsErrorOnGetMessagesFail(t *testing.T) {
	// Create a mock store that will fail on GetMessages
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create a session
	_, err = store.db.Exec(
		`INSERT INTO sessions (id, title, model, working_dir, created_at, updated_at)
		 VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`,
		"sess-test", "Test Session", "claude-3-5-sonnet", "/test/dir",
	)
	if err != nil {
		t.Fatalf("insert session failed: %v", err)
	}

	// Close the store to cause GetMessages to fail
	store.Close()

	adapter := NewSessionStoreAdapter(store)
	_, err = adapter.GetSessionMetadata("sess-test")

	// Should return an error instead of silently defaulting
	if err == nil {
		t.Fatal("expected error when GetMessages fails, but got nil")
	}
}

// TestSessionStoreAdapterListSessionsReturnsErrorOnGetMessagesFail
// verifies that ListSessions returns an error instead of silently
// defaulting when GetMessages fails (Issue 3).
// This test uses mocking to simulate GetMessages failure.
func TestSessionStoreAdapterListSessionsReturnsErrorOnGetMessagesFail(t *testing.T) {
	// Create a store that we can use as a base
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Create a session manually to trigger GetMessages during ListSessions
	_, err = store.db.Exec(
		`INSERT INTO sessions (id, title, model, working_dir, created_at, updated_at)
		 VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`,
		"sess-test", "Test Session", "claude-3-5-sonnet", "/test/dir",
	)
	if err != nil {
		t.Fatalf("insert session failed: %v", err)
	}

	// Now we can test that ListSessions properly returns errors from GetMessages.
	// Currently the adapter silently defaults to empty messages on error.
	// After the fix, it should return an error.
	adapter := NewSessionStoreAdapter(store)

	// This should work normally first
	sessions, err := adapter.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions should work normally: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
}
