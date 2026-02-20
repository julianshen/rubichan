# SQLite Conversation Persistence Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add session and message persistence to the existing SQLite store so agent conversations can be saved and resumed.

**Architecture:** Extend `internal/store/Store` with `sessions` and `messages` tables. Session IDs are UUIDs. Messages store serialized `[]provider.ContentBlock` as JSON. Agent wiring is optional via `WithStore` option — persistence errors are non-fatal (logged, not crashed).

**Tech Stack:** Go 1.26, `modernc.org/sqlite`, `google/uuid`, `database/sql`, `encoding/json`

---

## Task 1: Enable SQLite Foreign Key Enforcement

**Files:**
- Modify: `internal/store/store.go:63-78` (NewStore function)
- Modify: `internal/store/store_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/store/store_test.go

func TestForeignKeyEnforcement(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Attempt to insert a message referencing a non-existent session.
	// If foreign keys are enforced, this must fail.
	_, err = s.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content) VALUES (?, ?, ?, ?)`,
		"nonexistent-session", 0, "user", `[{"type":"text","text":"hi"}]`,
	)
	require.Error(t, err, "foreign key constraint should reject orphan message")
}
```

Note: This test requires the `messages` table to exist (Task 2 creates it), so this test will be written but only pass after Task 2. We include it here to validate FK enforcement.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestForeignKeyEnforcement -v`
Expected: FAIL — `messages` table doesn't exist yet

**Step 3: Add PRAGMA foreign_keys = ON to NewStore**

In `internal/store/store.go`, add after `db.SetMaxOpenConns(1)`:

```go
// Enable foreign key enforcement (off by default in SQLite).
if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
	db.Close()
	return nil, fmt.Errorf("enable foreign keys: %w", err)
}
```

**Step 4: Run all existing tests to confirm no regressions**

Run: `go test ./internal/store/ -v`
Expected: All existing tests PASS (FK pragma doesn't affect existing tables which have no FKs)

**Step 5: Commit**

```
[BEHAVIORAL] Enable SQLite foreign key enforcement in store
```

---

## Task 2: Add Session and Message Tables

**Files:**
- Modify: `internal/store/store.go:85-113` (createTables function)
- Modify: `internal/store/store_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/store/store_test.go

func TestSessionAndMessageTablesExist(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Verify sessions table exists by inserting a row.
	_, err = s.db.Exec(
		`INSERT INTO sessions (id, model) VALUES (?, ?)`,
		"test-id", "gpt-4",
	)
	require.NoError(t, err, "sessions table should exist")

	// Verify messages table exists and FK works.
	_, err = s.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content) VALUES (?, ?, ?, ?)`,
		"test-id", 0, "user", `[]`,
	)
	require.NoError(t, err, "messages table should exist and accept valid FK")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSessionAndMessageTablesExist -v`
Expected: FAIL — `no such table: sessions`

**Step 3: Add table creation statements**

Add to the `stmts` slice in `createTables`:

```go
`CREATE TABLE IF NOT EXISTS sessions (
	id            TEXT PRIMARY KEY,
	title         TEXT NOT NULL DEFAULT '',
	model         TEXT NOT NULL,
	working_dir   TEXT NOT NULL DEFAULT '',
	system_prompt TEXT NOT NULL DEFAULT '',
	created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at    DATETIME NOT NULL DEFAULT (datetime('now')),
	token_count   INTEGER NOT NULL DEFAULT 0
)`,
`CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	seq        INTEGER NOT NULL,
	role       TEXT NOT NULL,
	content    TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	UNIQUE(session_id, seq)
)`,
`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq)`,
```

**Step 4: Run tests**

Run: `go test ./internal/store/ -v`
Expected: ALL PASS including new tests and the FK test from Task 1

**Step 5: Commit**

```
[BEHAVIORAL] Add sessions and messages tables to store schema
```

---

## Task 3: Session and StoredMessage Types

**Files:**
- Modify: `internal/store/store.go` (add types after existing type declarations)

**Step 1: Write the failing test**

```go
// Add to internal/store/store_test.go

func TestSessionTypeZeroValue(t *testing.T) {
	var s Session
	assert.Empty(t, s.ID)
	assert.Empty(t, s.Title)
	assert.Empty(t, s.Model)
	assert.True(t, s.CreatedAt.IsZero())
	assert.Equal(t, 0, s.TokenCount)
}

func TestStoredMessageTypeZeroValue(t *testing.T) {
	var m StoredMessage
	assert.Empty(t, m.SessionID)
	assert.Empty(t, m.Role)
	assert.Nil(t, m.Content)
	assert.Equal(t, 0, m.Seq)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run "TestSessionTypeZeroValue|TestStoredMessageTypeZeroValue" -v`
Expected: FAIL — types undefined

**Step 3: Add the type definitions**

Add to `internal/store/store.go` after the existing `RegistryEntry` type:

```go
// Session represents a persisted agent session.
type Session struct {
	ID           string
	Title        string
	Model        string
	WorkingDir   string
	SystemPrompt string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	TokenCount   int
}

// StoredMessage represents a persisted message within a session.
type StoredMessage struct {
	ID        int64
	SessionID string
	Seq       int
	Role      string
	Content   []provider.ContentBlock
	CreatedAt time.Time
}
```

Add `"github.com/julianshen/rubichan/internal/provider"` to the imports.

**Step 4: Run tests**

Run: `go test ./internal/store/ -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add Session and StoredMessage types to store
```

---

## Task 4: CreateSession and GetSession

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/store/store_test.go

func TestCreateAndGetSession(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{
		ID:           "sess-001",
		Title:        "My first session",
		Model:        "claude-3-opus",
		WorkingDir:   "/home/user/project",
		SystemPrompt: "You are helpful.",
		TokenCount:   0,
	}
	err = s.CreateSession(sess)
	require.NoError(t, err)

	got, err := s.GetSession("sess-001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "sess-001", got.ID)
	assert.Equal(t, "My first session", got.Title)
	assert.Equal(t, "claude-3-opus", got.Model)
	assert.Equal(t, "/home/user/project", got.WorkingDir)
	assert.Equal(t, "You are helpful.", got.SystemPrompt)
	assert.Equal(t, 0, got.TokenCount)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestGetSessionNotFound(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	got, err := s.GetSession("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got, "should return nil for missing session")
}

func TestCreateSessionDuplicateID(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "dup-id", Model: "gpt-4"}
	require.NoError(t, s.CreateSession(sess))

	err = s.CreateSession(sess)
	require.Error(t, err, "duplicate session ID should error")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run "TestCreateAndGetSession|TestGetSessionNotFound|TestCreateSessionDuplicateID" -v`
Expected: FAIL — methods undefined

**Step 3: Implement CreateSession and GetSession**

```go
// CreateSession inserts a new session. The ID must be unique.
func (s *Store) CreateSession(sess Session) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, title, model, working_dir, system_prompt, token_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		sess.ID, sess.Title, sess.Model, sess.WorkingDir, sess.SystemPrompt, sess.TokenCount,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetSession retrieves a session by ID. Returns nil if not found.
func (s *Store) GetSession(id string) (*Session, error) {
	var sess Session
	var createdStr, updatedStr string
	err := s.db.QueryRow(
		`SELECT id, title, model, working_dir, system_prompt, created_at, updated_at, token_count
		 FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.Title, &sess.Model, &sess.WorkingDir, &sess.SystemPrompt,
		&createdStr, &updatedStr, &sess.TokenCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	sess.CreatedAt, _ = parseSQLiteDatetime(createdStr)
	sess.UpdatedAt, _ = parseSQLiteDatetime(updatedStr)
	return &sess, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/store/ -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add CreateSession and GetSession to store
```

---

## Task 5: UpdateSession

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

**Step 1: Write the failing test**

```go
func TestUpdateSession(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "sess-upd", Model: "gpt-4", Title: "Original"}
	require.NoError(t, s.CreateSession(sess))

	// Update title and token count.
	sess.Title = "Updated Title"
	sess.TokenCount = 5000
	err = s.UpdateSession(sess)
	require.NoError(t, err)

	got, err := s.GetSession("sess-upd")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Updated Title", got.Title)
	assert.Equal(t, 5000, got.TokenCount)
}

func TestUpdateSessionNotFound(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.UpdateSession(Session{ID: "nonexistent", Model: "gpt-4"})
	require.Error(t, err, "updating non-existent session should error")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run "TestUpdateSession" -v`
Expected: FAIL — method undefined

**Step 3: Implement UpdateSession**

```go
// UpdateSession updates a session's title, token count, and updated_at timestamp.
// Returns an error if the session does not exist.
func (s *Store) UpdateSession(sess Session) error {
	result, err := s.db.Exec(
		`UPDATE sessions SET title = ?, token_count = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		sess.Title, sess.TokenCount, sess.ID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("update session: session %q not found", sess.ID)
	}
	return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/store/ -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add UpdateSession to store
```

---

## Task 6: DeleteSession with CASCADE and ListSessions

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

**Step 1: Write the failing tests**

```go
func TestDeleteSession(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "sess-del", Model: "gpt-4"}
	require.NoError(t, s.CreateSession(sess))

	err = s.DeleteSession("sess-del")
	require.NoError(t, err)

	got, err := s.GetSession("sess-del")
	require.NoError(t, err)
	assert.Nil(t, got, "deleted session should not be found")

	// Deleting non-existent session should not error.
	err = s.DeleteSession("nonexistent")
	assert.NoError(t, err)
}

func TestDeleteSessionCascadesMessages(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "sess-cascade", Model: "gpt-4"}
	require.NoError(t, s.CreateSession(sess))

	// Insert messages directly (AppendMessage not yet implemented).
	_, err = s.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content) VALUES (?, ?, ?, ?)`,
		"sess-cascade", 0, "user", `[{"type":"text","text":"hi"}]`,
	)
	require.NoError(t, err)

	// Delete the session — messages should be gone.
	require.NoError(t, s.DeleteSession("sess-cascade"))

	var count int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, "sess-cascade").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "messages should be cascade-deleted")
}

func TestListSessions(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Empty list.
	sessions, err := s.ListSessions(10)
	require.NoError(t, err)
	assert.Empty(t, sessions)

	// Add sessions (use direct SQL to control updated_at ordering).
	_, err = s.db.Exec(
		`INSERT INTO sessions (id, title, model, created_at, updated_at) VALUES
		 ('s1', 'First', 'gpt-4', datetime('now', '-2 minutes'), datetime('now', '-2 minutes')),
		 ('s2', 'Second', 'gpt-4', datetime('now', '-1 minute'), datetime('now', '-1 minute')),
		 ('s3', 'Third', 'gpt-4', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	// Should return most recent first.
	sessions, err = s.ListSessions(10)
	require.NoError(t, err)
	require.Len(t, sessions, 3)
	assert.Equal(t, "s3", sessions[0].ID)
	assert.Equal(t, "s2", sessions[1].ID)
	assert.Equal(t, "s1", sessions[2].ID)

	// Respect limit.
	sessions, err = s.ListSessions(2)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	assert.Equal(t, "s3", sessions[0].ID)
	assert.Equal(t, "s2", sessions[1].ID)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run "TestDeleteSession|TestListSessions" -v`
Expected: FAIL — methods undefined

**Step 3: Implement DeleteSession and ListSessions**

```go
// DeleteSession removes a session and its messages (via CASCADE).
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// ListSessions returns the most recently updated sessions, limited to n.
func (s *Store) ListSessions(limit int) ([]Session, error) {
	rows, err := s.db.Query(
		`SELECT id, title, model, working_dir, system_prompt, created_at, updated_at, token_count
		 FROM sessions ORDER BY updated_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		var createdStr, updatedStr string
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Model, &sess.WorkingDir,
			&sess.SystemPrompt, &createdStr, &updatedStr, &sess.TokenCount); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.CreatedAt, _ = parseSQLiteDatetime(createdStr)
		sess.UpdatedAt, _ = parseSQLiteDatetime(updatedStr)
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}
```

**Step 4: Run tests**

Run: `go test ./internal/store/ -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add DeleteSession and ListSessions to store
```

---

## Task 7: AppendMessage and GetMessages

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

**Step 1: Write the failing tests**

```go
func TestAppendAndGetMessages(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "sess-msg", Model: "gpt-4"}
	require.NoError(t, s.CreateSession(sess))

	// Append a user message.
	userContent := []provider.ContentBlock{
		{Type: "text", Text: "Hello!"},
	}
	err = s.AppendMessage("sess-msg", "user", userContent)
	require.NoError(t, err)

	// Append an assistant message with tool use.
	assistantContent := []provider.ContentBlock{
		{Type: "text", Text: "Let me check."},
		{Type: "tool_use", ID: "t1", Name: "file", Input: json.RawMessage(`{"op":"read"}`)},
	}
	err = s.AppendMessage("sess-msg", "assistant", assistantContent)
	require.NoError(t, err)

	// Retrieve messages.
	msgs, err := s.GetMessages("sess-msg")
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	// First message: user.
	assert.Equal(t, "sess-msg", msgs[0].SessionID)
	assert.Equal(t, 0, msgs[0].Seq)
	assert.Equal(t, "user", msgs[0].Role)
	require.Len(t, msgs[0].Content, 1)
	assert.Equal(t, "text", msgs[0].Content[0].Type)
	assert.Equal(t, "Hello!", msgs[0].Content[0].Text)
	assert.False(t, msgs[0].CreatedAt.IsZero())

	// Second message: assistant.
	assert.Equal(t, 1, msgs[1].Seq)
	assert.Equal(t, "assistant", msgs[1].Role)
	require.Len(t, msgs[1].Content, 2)
	assert.Equal(t, "text", msgs[1].Content[0].Type)
	assert.Equal(t, "tool_use", msgs[1].Content[1].Type)
	assert.Equal(t, "t1", msgs[1].Content[1].ID)
}

func TestAppendMessageAutoSequence(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateSession(Session{ID: "seq-test", Model: "m"}))

	for i := 0; i < 5; i++ {
		err := s.AppendMessage("seq-test", "user", []provider.ContentBlock{
			{Type: "text", Text: fmt.Sprintf("msg %d", i)},
		})
		require.NoError(t, err)
	}

	msgs, err := s.GetMessages("seq-test")
	require.NoError(t, err)
	require.Len(t, msgs, 5)
	for i, m := range msgs {
		assert.Equal(t, i, m.Seq, "seq should auto-increment")
	}
}

func TestAppendMessageInvalidSession(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.AppendMessage("nonexistent", "user", []provider.ContentBlock{
		{Type: "text", Text: "orphan"},
	})
	require.Error(t, err, "should reject message for non-existent session")
}

func TestGetMessagesEmpty(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateSession(Session{ID: "empty", Model: "m"}))

	msgs, err := s.GetMessages("empty")
	require.NoError(t, err)
	assert.Empty(t, msgs)
}
```

Note: This test file needs `"encoding/json"` and `"fmt"` in imports, plus `"github.com/julianshen/rubichan/internal/provider"`.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run "TestAppendAndGetMessages|TestAppendMessageAutoSequence|TestAppendMessageInvalidSession|TestGetMessagesEmpty" -v`
Expected: FAIL — methods undefined

**Step 3: Implement AppendMessage and GetMessages**

```go
// AppendMessage adds a message to a session, auto-incrementing the sequence number.
// The content blocks are serialized to JSON for storage.
func (s *Store) AppendMessage(sessionID, role string, content []provider.ContentBlock) error {
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("marshal content: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content, created_at)
		 VALUES (?, COALESCE((SELECT MAX(seq) FROM messages WHERE session_id = ?), -1) + 1, ?, ?, datetime('now'))`,
		sessionID, sessionID, role, string(contentJSON),
	)
	if err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	return nil
}

// GetMessages retrieves all messages for a session, ordered by sequence number.
func (s *Store) GetMessages(sessionID string) ([]StoredMessage, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, seq, role, content, created_at
		 FROM messages WHERE session_id = ? ORDER BY seq`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer rows.Close()

	var messages []StoredMessage
	for rows.Next() {
		var m StoredMessage
		var contentJSON, createdStr string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Role, &contentJSON, &createdStr); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if err := json.Unmarshal([]byte(contentJSON), &m.Content); err != nil {
			return nil, fmt.Errorf("unmarshal content: %w", err)
		}
		m.CreatedAt, _ = parseSQLiteDatetime(createdStr)
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
```

Add `"encoding/json"` to imports in `store.go`.

**Step 4: Run tests**

Run: `go test ./internal/store/ -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add AppendMessage and GetMessages to store
```

---

## Task 8: Conversation.LoadFromStored

**Files:**
- Modify: `internal/agent/conversation.go`
- Modify: `internal/agent/conversation_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/agent/conversation_test.go

func TestConversationLoadFromStored(t *testing.T) {
	conv := NewConversation("system prompt")
	conv.AddUser("existing message")

	storedMsgs := []provider.Message{
		{
			Role: "user",
			Content: []provider.ContentBlock{
				{Type: "text", Text: "Hello from history"},
			},
		},
		{
			Role: "assistant",
			Content: []provider.ContentBlock{
				{Type: "text", Text: "Hi there!"},
			},
		},
		{
			Role: "user",
			Content: []provider.ContentBlock{
				{Type: "tool_result", ToolUseID: "t1", Text: "file content"},
			},
		},
	}

	conv.LoadFromMessages(storedMsgs)

	msgs := conv.Messages()
	require.Len(t, msgs, 3, "should replace existing messages with stored ones")
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "Hello from history", msgs[0].Content[0].Text)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "Hi there!", msgs[1].Content[0].Text)
	assert.Equal(t, "system prompt", conv.SystemPrompt(), "system prompt should be preserved")
}

func TestConversationLoadFromStoredEmpty(t *testing.T) {
	conv := NewConversation("system")
	conv.AddUser("will be replaced")

	conv.LoadFromMessages(nil)
	assert.Empty(t, conv.Messages())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run "TestConversationLoadFromStored" -v`
Expected: FAIL — method undefined

**Step 3: Implement LoadFromMessages**

Add to `internal/agent/conversation.go`:

```go
// LoadFromMessages replaces the current message history with the given messages.
// The system prompt is preserved. This is used when resuming a saved session.
func (c *Conversation) LoadFromMessages(msgs []provider.Message) {
	c.messages = make([]provider.Message, len(msgs))
	copy(c.messages, msgs)
}
```

**Step 4: Run tests**

Run: `go test ./internal/agent/ -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add LoadFromMessages to Conversation for session resume
```

---

## Task 9: Agent WithStore Option and Session Lifecycle

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/agent/agent_test.go

func TestWithStoreOption(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}

	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "Hello"},
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))
	assert.NotEmpty(t, a.sessionID, "session should be auto-created")

	// Verify session was persisted.
	sess, err := s.GetSession(a.sessionID)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "test-model", sess.Model)
}

func TestAgentWithStorePersistsMessages(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}

	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "I am well!"},
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s))
	ch, err := a.Turn(context.Background(), "How are you?")
	require.NoError(t, err)

	// Drain events.
	for range ch {
	}

	// Verify messages were persisted.
	msgs, err := s.GetMessages(a.sessionID)
	require.NoError(t, err)
	require.Len(t, msgs, 2, "should have user + assistant messages")
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "assistant", msgs[1].Role)
}
```

Note: Test file needs import `"github.com/julianshen/rubichan/internal/store"`.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run "TestWithStoreOption|TestAgentWithStorePersistsMessages" -v`
Expected: FAIL — `WithStore` undefined

**Step 3: Implement WithStore, session auto-creation, and message persistence**

Add to `internal/agent/agent.go`:

Import: `"github.com/julianshen/rubichan/internal/store"`, `"log"`, `"github.com/google/uuid"`

Add field to Agent struct:
```go
store     *store.Store
sessionID string
```

Add option function:
```go
// WithStore attaches a persistence store to the agent, enabling automatic
// session and message saving.
func WithStore(st *store.Store) AgentOption {
	return func(a *Agent) {
		a.store = st
	}
}
```

Modify the `New` function — after the options loop, create a session if store is set:
```go
// After opts loop:
if a.store != nil {
	a.sessionID = uuid.New().String()
	wd, _ := os.Getwd()
	sess := store.Session{
		ID:           a.sessionID,
		Model:        a.model,
		WorkingDir:   wd,
		SystemPrompt: a.conversation.SystemPrompt(),
	}
	if err := a.store.CreateSession(sess); err != nil {
		log.Printf("warning: failed to create session: %v", err)
		a.store = nil // disable persistence for this session
	}
}
```

Add a helper method:
```go
// persistMessage saves a message to the store. Errors are logged but non-fatal.
func (a *Agent) persistMessage(role string, content []provider.ContentBlock) {
	if a.store == nil {
		return
	}
	if err := a.store.AppendMessage(a.sessionID, role, content); err != nil {
		log.Printf("warning: failed to persist message: %v", err)
	}
}
```

Modify `Turn` method — persist user message after adding to conversation:
```go
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error) {
	a.conversation.AddUser(userMessage)
	a.persistMessage("user", []provider.ContentBlock{{Type: "text", Text: userMessage}})
	a.context.Truncate(a.conversation)
	// ... rest unchanged
}
```

Modify `runLoop` — persist assistant message after `a.conversation.AddAssistant(blocks)`:
```go
if len(blocks) > 0 {
	a.conversation.AddAssistant(blocks)
	a.persistMessage("assistant", blocks)
}
```

Persist tool result messages after `a.conversation.AddToolResult(...)`:
```go
a.conversation.AddToolResult(tc.ID, ..., ...)
a.persistMessage("user", []provider.ContentBlock{{Type: "tool_result", ToolUseID: tc.ID, Text: ..., IsError: ...}})
```

Note: There are 5 places in `runLoop` where `AddToolResult` is called (hook error, hook cancel, approval error, denied, execution error) plus 1 for the success path. Add `persistMessage` after each.

Also add `"os"` to imports.

**Step 4: Run tests**

Run: `go test ./internal/agent/ -v`
Expected: ALL PASS

**Step 5: Run full test suite**

Run: `go test ./... && golangci-lint run ./... && gofmt -l .`
Expected: ALL PASS, no lint warnings, no formatting issues

**Step 6: Commit**

```
[BEHAVIORAL] Add WithStore option and message persistence to Agent
```

---

## Task 10: Agent Resume from Existing Session

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

**Step 1: Write the failing test**

```go
func TestWithResumeSession(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Create a session with history.
	require.NoError(t, s.CreateSession(store.Session{
		ID:           "resume-me",
		Model:        "gpt-4",
		SystemPrompt: "You are helpful.",
	}))
	require.NoError(t, s.AppendMessage("resume-me", "user", []provider.ContentBlock{
		{Type: "text", Text: "Hello"},
	}))
	require.NoError(t, s.AppendMessage("resume-me", "assistant", []provider.ContentBlock{
		{Type: "text", Text: "Hi there!"},
	}))

	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "gpt-4"},
		Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
	}

	mp := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "Welcome back!"},
		{Type: "stop"},
	}}

	a := New(mp, tools.NewRegistry(), autoApprove, cfg, WithStore(s), WithResumeSession("resume-me"))

	assert.Equal(t, "resume-me", a.sessionID)

	// Conversation should have been hydrated.
	msgs := a.conversation.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "Hello", msgs[0].Content[0].Text)
	assert.Equal(t, "Hi there!", msgs[1].Content[0].Text)

	// System prompt should come from the stored session.
	assert.Equal(t, "You are helpful.", a.conversation.SystemPrompt())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestWithResumeSession -v`
Expected: FAIL — `WithResumeSession` undefined

**Step 3: Implement WithResumeSession**

```go
// WithResumeSession configures the agent to resume an existing session
// instead of creating a new one.
func WithResumeSession(sessionID string) AgentOption {
	return func(a *Agent) {
		a.resumeSessionID = sessionID
	}
}
```

Add `resumeSessionID string` field to Agent struct.

Modify the session creation block in `New` (after opts loop):
```go
if a.store != nil {
	if a.resumeSessionID != "" {
		// Resume existing session.
		sess, err := a.store.GetSession(a.resumeSessionID)
		if err != nil || sess == nil {
			log.Printf("warning: failed to resume session %s: %v", a.resumeSessionID, err)
		} else {
			a.sessionID = sess.ID
			a.conversation = NewConversation(sess.SystemPrompt)
			msgs, err := a.store.GetMessages(sess.ID)
			if err != nil {
				log.Printf("warning: failed to load messages: %v", err)
			} else {
				providerMsgs := make([]provider.Message, len(msgs))
				for i, m := range msgs {
					providerMsgs[i] = provider.Message{
						Role:    m.Role,
						Content: m.Content,
					}
				}
				a.conversation.LoadFromMessages(providerMsgs)
			}
		}
	}

	if a.sessionID == "" {
		// Create new session.
		a.sessionID = uuid.New().String()
		wd, _ := os.Getwd()
		sess := store.Session{
			ID:           a.sessionID,
			Model:        a.model,
			WorkingDir:   wd,
			SystemPrompt: a.conversation.SystemPrompt(),
		}
		if err := a.store.CreateSession(sess); err != nil {
			log.Printf("warning: failed to create session: %v", err)
			a.store = nil
		}
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/agent/ -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add WithResumeSession option for session resume
```

---

## Task 11: Full Test Suite Verification and Coverage Check

**Files:**
- No new files — verification only

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: ALL PASS

**Step 2: Check coverage for store package**

Run: `go test ./internal/store/ -cover`
Expected: >90% coverage

**Step 3: Check coverage for agent package**

Run: `go test ./internal/agent/ -cover`
Expected: >90% coverage

**Step 4: Run linter and formatter**

Run: `golangci-lint run ./... && gofmt -l .`
Expected: Clean — no warnings, no unformatted files

**Step 5: Commit if any fixups needed**

```
[STRUCTURAL] Fix lint/format issues from persistence implementation
```

Only commit if there are actual fixes to make. Skip if everything is clean.
