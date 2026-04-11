package interactive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestACPClientInitWithResumeFlagLoadsSession(t *testing.T) {
	mockStore := &testMockSessionStore{
		sessions: []SessionMetadata{
			{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 3},
		},
		sessionTurns: map[string][]Turn{
			"sess-1": {
				{ID: "turn-1", Timestamp: time.Now(), UserInput: "hello", AgentResp: "hi"},
			},
		},
	}

	mgr := NewSessionManager(mockStore)
	client := NewACPClientWithResume(mgr, "sess-1")

	turns, err := client.LoadedTurns()
	if err != nil {
		t.Fatalf("LoadedTurns failed: %v", err)
	}

	if len(turns) != 1 {
		t.Errorf("expected 1 loaded turn, got %d", len(turns))
	}

	if turns[0].UserInput != "hello" {
		t.Errorf("expected turn input 'hello', got %s", turns[0].UserInput)
	}

	// Verify no load error on successful load
	loadErr := client.LoadError()
	if loadErr != nil {
		t.Errorf("expected LoadError to be nil on successful load, got: %v", loadErr)
	}
}

func TestACPClientNoSessionLoading(t *testing.T) {
	mgr := NewSessionManager(&testMockSessionStore{sessions: []SessionMetadata{}})
	client := NewACPClientWithResume(mgr, "")

	turns, err := client.LoadedTurns()
	if err != nil {
		t.Fatalf("LoadedTurns failed: %v", err)
	}

	if len(turns) != 0 {
		t.Errorf("expected 0 turns when not resuming, got %d", len(turns))
	}

	// Verify no load error when no resume attempted
	loadErr := client.LoadError()
	if loadErr != nil {
		t.Errorf("expected LoadError to be nil when no resume attempted, got: %v", loadErr)
	}
}

func TestACPClientLoadSessionError(t *testing.T) {
	mockStore := &testMockSessionStore{
		sessions:     []SessionMetadata{},
		sessionTurns: map[string][]Turn{},
	}

	mgr := NewSessionManager(mockStore)
	client := NewACPClientWithResume(mgr, "nonexistent-session")

	turns, err := client.LoadedTurns()
	if err != nil {
		t.Fatalf("LoadedTurns failed: %v", err)
	}

	// Should return empty slice when session doesn't exist
	if len(turns) != 0 {
		t.Errorf("expected 0 turns on load error, got %d", len(turns))
	}

	// Verify that the load error is captured
	loadErr := client.LoadError()
	if loadErr == nil {
		t.Errorf("expected LoadError to return an error for nonexistent session, got nil")
	}
	// Error is wrapped twice: once by SessionManager.Load, once by ACPClient
	errMsg := loadErr.Error()
	if !strings.Contains(errMsg, "session not found") {
		t.Errorf("expected error containing 'session not found', got: %s", errMsg)
	}
}

// testMockSessionStore implements SessionStore for testing
type testMockSessionStore struct {
	sessions     []SessionMetadata
	sessionTurns map[string][]Turn
}

func (m *testMockSessionStore) ListSessions() ([]SessionMetadata, error) {
	return m.sessions, nil
}

func (m *testMockSessionStore) LoadSession(id string) ([]Turn, error) {
	if turns, ok := m.sessionTurns[id]; ok {
		return turns, nil
	}
	return nil, fmt.Errorf("session not found")
}

func (m *testMockSessionStore) SaveSession(id string, turns []Turn) error {
	return nil
}

func (m *testMockSessionStore) GetSessionMetadata(id string) (SessionMetadata, error) {
	for _, s := range m.sessions {
		if s.ID == id {
			return s, nil
		}
	}
	return SessionMetadata{}, fmt.Errorf("not found")
}

// --- Approval delegation tests ---

func TestACPClientApprovalRequestDelegates(t *testing.T) {
	approvalFunc := func(_ context.Context, tool string, input json.RawMessage) (bool, error) {
		return true, nil
	}

	client := NewACPClientWithApprovalFunc(approvalFunc)
	approved, err := client.ApprovalRequest(context.Background(), "shell", json.RawMessage(`{"command":"ls"}`))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Error("expected approval to be true when callback returns true")
	}
}

func TestACPClientApprovalRequestDenied(t *testing.T) {
	approvalFunc := func(_ context.Context, tool string, input json.RawMessage) (bool, error) {
		return false, nil
	}

	client := NewACPClientWithApprovalFunc(approvalFunc)
	approved, err := client.ApprovalRequest(context.Background(), "shell", json.RawMessage(`{"command":"rm -rf /"}`))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Error("expected approval to be false when callback returns false")
	}
}

func TestACPClientApprovalRequestCallbackError(t *testing.T) {
	expectedErr := errors.New("user cancelled")
	approvalFunc := func(_ context.Context, tool string, input json.RawMessage) (bool, error) {
		return false, expectedErr
	}

	client := NewACPClientWithApprovalFunc(approvalFunc)
	_, err := client.ApprovalRequest(context.Background(), "shell", json.RawMessage(`{"command":"ls"}`))

	if err == nil {
		t.Fatal("expected error from callback, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected wrapped error %v, got %v", expectedErr, err)
	}
	if !strings.Contains(err.Error(), "shell") {
		t.Errorf("expected error to contain tool name 'shell', got: %s", err.Error())
	}
}

func TestACPClientApprovalRequestPassesToolAndInput(t *testing.T) {
	var receivedTool string
	var receivedInput json.RawMessage
	approvalFunc := func(_ context.Context, tool string, input json.RawMessage) (bool, error) {
		receivedTool = tool
		receivedInput = input
		return true, nil
	}

	client := NewACPClientWithApprovalFunc(approvalFunc)
	inputJSON := json.RawMessage(`{"command":"git status"}`)
	_, err := client.ApprovalRequest(context.Background(), "shell", inputJSON)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedTool != "shell" {
		t.Errorf("expected tool 'shell', got %q", receivedTool)
	}
	if string(receivedInput) != string(inputJSON) {
		t.Errorf("expected input %s, got %s", inputJSON, receivedInput)
	}
}

func TestACPClientDefaultApprovalAutoApproves(t *testing.T) {
	// When no approval func is set, ApprovalRequest should auto-approve
	client := NewACPClientWithApprovalFunc(nil)
	approved, err := client.ApprovalRequest(context.Background(), "shell", json.RawMessage(`{"command":"ls"}`))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Error("expected auto-approve when no approval func is set")
	}
}

func TestNewACPClientWithApprovalFunc(t *testing.T) {
	called := false
	approvalFunc := func(_ context.Context, tool string, input json.RawMessage) (bool, error) {
		called = true
		return true, nil
	}

	client := NewACPClientWithApprovalFunc(approvalFunc)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify the func is wired by calling ApprovalRequest
	_, _ = client.ApprovalRequest(context.Background(), "read", json.RawMessage(`{}`))
	if !called {
		t.Error("expected approval func to be called")
	}
}
