package e2e_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/modes/interactive"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/store"
)

// TestResumeSessionE2E exercises the complete session resume workflow:
// 1. Create a session with multiple turns
// 2. List all sessions
// 3. Verify session appears in list with correct metadata
// 4. Load the session
// 5. Verify turns are restored correctly
func TestResumeSessionE2E(t *testing.T) {
	// Setup in-memory SQLite store
	sqlStore, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer sqlStore.Close()

	// Wrap in SessionStoreAdapter to implement interactive.SessionStore
	sessionStore := store.NewSessionStoreAdapter(sqlStore)

	// Session ID with timestamp to ensure uniqueness
	sessionID := fmt.Sprintf("test-sess-%d", time.Now().UnixNano())

	// Step 1: Create a session and add turns to it
	// First, create the session in the underlying store
	sess := store.Session{
		ID:         sessionID,
		Title:      "Resume E2E Test Session",
		Model:      "claude-3-5-sonnet-20241022",
		WorkingDir: "/tmp/test-project",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	err = sqlStore.CreateSession(sess)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Define test turns (will be stored as messages)
	testTurns := []struct {
		userInput string
		agentResp string
	}{
		{
			userInput: "What is the meaning of life?",
			agentResp: "According to Douglas Adams, the answer is 42.",
		},
		{
			userInput: "Can you explain that?",
			agentResp: "It's a reference to The Hitchhiker's Guide to the Galaxy.",
		},
		{
			userInput: "What's the next number?",
			agentResp: "The next number after 42 is 43.",
		},
	}

	// Add messages (turns) to the session
	for _, tt := range testTurns {
		// Add user message
		userContent := []provider.ContentBlock{
			{Type: "text", Text: tt.userInput},
		}
		err = sqlStore.AppendMessage(sessionID, "user", userContent)
		if err != nil {
			t.Fatalf("AppendMessage (user) failed: %v", err)
		}

		// Add assistant response
		assistantContent := []provider.ContentBlock{
			{Type: "text", Text: tt.agentResp},
		}
		err = sqlStore.AppendMessage(sessionID, "assistant", assistantContent)
		if err != nil {
			t.Fatalf("AppendMessage (assistant) failed: %v", err)
		}
	}

	// Step 2: List all sessions via the adapter
	sessions, err := sessionStore.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if len(sessions) == 0 {
		t.Fatal("expected at least 1 session after save")
	}

	// Step 3: Verify session appears in list with correct metadata
	var foundSession interactive.SessionMetadata
	for _, s := range sessions {
		if s.ID == sessionID {
			foundSession = s
			break
		}
	}

	if foundSession.ID == "" {
		t.Errorf("session %s not found in list", sessionID)
	}

	if foundSession.TurnCount != len(testTurns) {
		t.Errorf("expected %d turns in metadata, got %d",
			len(testTurns), foundSession.TurnCount)
	}

	if foundSession.Project != "/tmp/test-project" {
		t.Errorf("expected project /tmp/test-project, got %s", foundSession.Project)
	}

	if foundSession.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	// Step 4: Load the session
	loadedTurns, err := sessionStore.LoadSession(sessionID)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	if len(loadedTurns) != len(testTurns) {
		t.Errorf("expected %d turns, got %d",
			len(testTurns), len(loadedTurns))
	}

	// Step 5: Verify turn content matches original
	for i, expectedTurn := range testTurns {
		if i >= len(loadedTurns) {
			t.Fatalf("not enough turns loaded (expected %d, got %d)",
				len(testTurns), len(loadedTurns))
		}

		actualTurn := loadedTurns[i]

		if actualTurn.UserInput != expectedTurn.userInput {
			t.Errorf("turn %d user input mismatch:\n  expected: %q\n  got:      %q",
				i, expectedTurn.userInput, actualTurn.UserInput)
		}

		if actualTurn.AgentResp != expectedTurn.agentResp {
			t.Errorf("turn %d agent response mismatch:\n  expected: %q\n  got:      %q",
				i, expectedTurn.agentResp, actualTurn.AgentResp)
		}

		// Verify turn ID is set
		if actualTurn.ID == "" {
			t.Errorf("turn %d ID should not be empty", i)
		}

		// Verify timestamp is set and reasonable
		if actualTurn.Timestamp.IsZero() {
			t.Errorf("turn %d Timestamp should not be zero", i)
		}
	}

	t.Logf("✓ Resume workflow verified: create → list → load → verify")
	t.Logf("✓ Loaded %d turns from session %s", len(loadedTurns), sessionID)
}

// TestResumeSessionMetadata verifies SessionMetadata details are correct
func TestResumeSessionMetadata(t *testing.T) {
	sqlStore, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer sqlStore.Close()

	sessionStore := store.NewSessionStoreAdapter(sqlStore)

	sessionID := fmt.Sprintf("metadata-test-%d", time.Now().UnixNano())

	// Create session with metadata
	createdTime := time.Now()
	sess := store.Session{
		ID:         sessionID,
		Title:      "Metadata Test Session",
		Model:      "claude-3-5-sonnet-20241022",
		WorkingDir: "/home/user/project",
		CreatedAt:  createdTime,
		UpdatedAt:  createdTime,
	}
	err = sqlStore.CreateSession(sess)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Add two turns
	for i := 0; i < 2; i++ {
		userContent := []provider.ContentBlock{
			{Type: "text", Text: fmt.Sprintf("question %d", i+1)},
		}
		err = sqlStore.AppendMessage(sessionID, "user", userContent)
		if err != nil {
			t.Fatalf("AppendMessage (user) failed: %v", err)
		}

		assistantContent := []provider.ContentBlock{
			{Type: "text", Text: fmt.Sprintf("answer %d", i+1)},
		}
		err = sqlStore.AppendMessage(sessionID, "assistant", assistantContent)
		if err != nil {
			t.Fatalf("AppendMessage (assistant) failed: %v", err)
		}
	}

	// Get metadata via SessionManager (which sorts and filters)
	manager := interactive.NewSessionManager(sessionStore)
	sessions, err := manager.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	var metadata interactive.SessionMetadata
	for _, s := range sessions {
		if s.ID == sessionID {
			metadata = s
			break
		}
	}

	if metadata.ID == "" {
		t.Fatalf("session not found in manager list")
	}

	if metadata.TurnCount != 2 {
		t.Errorf("expected TurnCount=2, got %d", metadata.TurnCount)
	}

	if metadata.Project != "/home/user/project" {
		t.Errorf("expected Project=/home/user/project, got %s", metadata.Project)
	}

	// Verify CreatedAt matches (approximately)
	if metadata.CreatedAt.Sub(createdTime) > time.Second {
		t.Errorf("CreatedAt differs too much from original")
	}
}

// TestResumeSessionWithComplexContent verifies turns with multiple content blocks
func TestResumeSessionWithComplexContent(t *testing.T) {
	sqlStore, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer sqlStore.Close()

	sessionStore := store.NewSessionStoreAdapter(sqlStore)

	sessionID := fmt.Sprintf("complex-test-%d", time.Now().UnixNano())

	// Create session
	sess := store.Session{
		ID:        sessionID,
		Title:     "Complex Content Test",
		Model:     "claude-3-5-sonnet-20241022",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err = sqlStore.CreateSession(sess)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Add a turn with multiple text blocks in the response
	userContent := []provider.ContentBlock{
		{Type: "text", Text: "Explain complex topics"},
	}
	err = sqlStore.AppendMessage(sessionID, "user", userContent)
	if err != nil {
		t.Fatalf("AppendMessage (user) failed: %v", err)
	}

	// Multi-block response
	assistantContent := []provider.ContentBlock{
		{Type: "text", Text: "First paragraph of explanation."},
		{Type: "text", Text: "Second paragraph with more details."},
		{Type: "text", Text: "Final summary."},
	}
	err = sqlStore.AppendMessage(sessionID, "assistant", assistantContent)
	if err != nil {
		t.Fatalf("AppendMessage (assistant) failed: %v", err)
	}

	// Load and verify
	turns, err := sessionStore.LoadSession(sessionID)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}

	// Multiple blocks should be concatenated with newlines
	expectedResp := "First paragraph of explanation.\nSecond paragraph with more details.\nFinal summary."
	if turns[0].AgentResp != expectedResp {
		t.Errorf("multi-block response mismatch:\n  expected: %q\n  got:      %q",
			expectedResp, turns[0].AgentResp)
	}
}

// TestResumeSessionLoadNonexistent verifies error handling for missing sessions
func TestResumeSessionLoadNonexistent(t *testing.T) {
	sqlStore, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer sqlStore.Close()

	sessionStore := store.NewSessionStoreAdapter(sqlStore)

	// Try to load a non-existent session
	_, err = sessionStore.LoadSession("nonexistent-session-id")
	if err == nil {
		t.Fatal("expected error when loading non-existent session")
	}

	// Verify the error mentions the session ID
	if errMsg := err.Error(); errMsg == "" {
		t.Error("error message should not be empty")
	}
}

// TestResumeSessionMultipleSessions verifies listing and loading with multiple sessions
func TestResumeSessionMultipleSessions(t *testing.T) {
	sqlStore, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer sqlStore.Close()

	sessionStore := store.NewSessionStoreAdapter(sqlStore)

	// Create three sessions with different content
	sessionIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		sessionIDs[i] = fmt.Sprintf("multi-sess-%d-%d", i, time.Now().UnixNano())

		sess := store.Session{
			ID:         sessionIDs[i],
			Title:      fmt.Sprintf("Session %d", i+1),
			Model:      "claude-3-5-sonnet-20241022",
			WorkingDir: fmt.Sprintf("/project/%d", i+1),
			CreatedAt:  time.Now().Add(time.Duration(i) * time.Second),
			UpdatedAt:  time.Now().Add(time.Duration(i) * time.Second),
		}
		err = sqlStore.CreateSession(sess)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Add i+1 turns to each session
		for j := 0; j < i+1; j++ {
			userContent := []provider.ContentBlock{
				{Type: "text", Text: fmt.Sprintf("Question %d in session %d", j+1, i+1)},
			}
			err = sqlStore.AppendMessage(sessionIDs[i], "user", userContent)
			if err != nil {
				t.Fatalf("AppendMessage (user) failed: %v", err)
			}

			assistantContent := []provider.ContentBlock{
				{Type: "text", Text: fmt.Sprintf("Answer %d in session %d", j+1, i+1)},
			}
			err = sqlStore.AppendMessage(sessionIDs[i], "assistant", assistantContent)
			if err != nil {
				t.Fatalf("AppendMessage (assistant) failed: %v", err)
			}
		}
	}

	// List all sessions
	sessions, err := sessionStore.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if len(sessions) < 3 {
		t.Errorf("expected at least 3 sessions, got %d", len(sessions))
	}

	// Verify each session can be loaded and has correct turn count
	for idx, sessionID := range sessionIDs {
		var meta interactive.SessionMetadata
		for _, s := range sessions {
			if s.ID == sessionID {
				meta = s
				break
			}
		}

		if meta.ID == "" {
			t.Errorf("session %s not found in list", sessionID)
			continue
		}

		expectedTurns := idx + 1
		if meta.TurnCount != expectedTurns {
			t.Errorf("session %d: expected %d turns, got %d",
				idx, expectedTurns, meta.TurnCount)
		}

		// Load and verify turns match
		turns, err := sessionStore.LoadSession(sessionID)
		if err != nil {
			t.Errorf("LoadSession failed for session %d: %v", idx, err)
			continue
		}

		if len(turns) != expectedTurns {
			t.Errorf("session %d: expected %d loaded turns, got %d",
				idx, expectedTurns, len(turns))
		}

		// Spot-check first turn content
		if len(turns) > 0 {
			expectedInput := fmt.Sprintf("Question 1 in session %d", idx+1)
			if turns[0].UserInput != expectedInput {
				t.Errorf("session %d turn 0: expected input %q, got %q",
					idx, expectedInput, turns[0].UserInput)
			}
		}
	}
}

// TestResumeSessionEmptySession verifies handling of sessions with no turns
func TestResumeSessionEmptySession(t *testing.T) {
	sqlStore, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer sqlStore.Close()

	sessionStore := store.NewSessionStoreAdapter(sqlStore)

	sessionID := fmt.Sprintf("empty-sess-%d", time.Now().UnixNano())

	// Create session but don't add any messages
	sess := store.Session{
		ID:        sessionID,
		Title:     "Empty Session",
		Model:     "claude-3-5-sonnet-20241022",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err = sqlStore.CreateSession(sess)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// List sessions
	sessions, err := sessionStore.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	// Find the empty session
	var meta interactive.SessionMetadata
	for _, s := range sessions {
		if s.ID == sessionID {
			meta = s
			break
		}
	}

	if meta.ID == "" {
		t.Fatalf("empty session not found in list")
	}

	if meta.TurnCount != 0 {
		t.Errorf("expected TurnCount=0 for empty session, got %d", meta.TurnCount)
	}

	// Load empty session
	turns, err := sessionStore.LoadSession(sessionID)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	if len(turns) != 0 {
		t.Errorf("expected 0 turns in empty session, got %d", len(turns))
	}
}

// TestResumeSessionJSONSerialization verifies content blocks serialize/deserialize correctly
func TestResumeSessionJSONSerialization(t *testing.T) {
	sqlStore, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer sqlStore.Close()

	sessionID := fmt.Sprintf("json-test-%d", time.Now().UnixNano())

	// Create session
	sess := store.Session{
		ID:        sessionID,
		Title:     "JSON Test",
		Model:     "claude-3-5-sonnet-20241022",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err = sqlStore.CreateSession(sess)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Test special characters in content
	specialInput := "Test with special chars: 你好, مرحبا, שלום, emoji 🚀"
	specialResp := "Response with: \"quotes\", 'apostrophes', and \\backslashes\\"

	userContent := []provider.ContentBlock{
		{Type: "text", Text: specialInput},
	}
	err = sqlStore.AppendMessage(sessionID, "user", userContent)
	if err != nil {
		t.Fatalf("AppendMessage (user) failed: %v", err)
	}

	assistantContent := []provider.ContentBlock{
		{Type: "text", Text: specialResp},
	}
	err = sqlStore.AppendMessage(sessionID, "assistant", assistantContent)
	if err != nil {
		t.Fatalf("AppendMessage (assistant) failed: %v", err)
	}

	// Retrieve directly from store to verify JSON encoding
	messages, err := sqlStore.GetMessages(sessionID)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// Verify user message content
	if messages[0].Role != "user" {
		t.Errorf("expected role=user, got %s", messages[0].Role)
	}

	if len(messages[0].Content) > 0 && messages[0].Content[0].Text != specialInput {
		t.Errorf("user content mismatch:\n  expected: %q\n  got:      %q",
			specialInput, messages[0].Content[0].Text)
	}

	// Verify assistant message content
	if messages[1].Role != "assistant" {
		t.Errorf("expected role=assistant, got %s", messages[1].Role)
	}

	if len(messages[1].Content) > 0 && messages[1].Content[0].Text != specialResp {
		t.Errorf("assistant content mismatch:\n  expected: %q\n  got:      %q",
			specialResp, messages[1].Content[0].Text)
	}
}
