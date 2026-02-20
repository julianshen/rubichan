# SQLite Conversation Persistence — Design Document

**Date:** 2026-02-20
**Scope:** FR-1.8 — Session persistence and conversation history with resume capability
**Location:** `internal/store/` (extend existing Store)

---

## Goal

Add conversation/session persistence to the existing SQLite store so agent sessions can be saved, listed, and resumed. Every message (user, assistant, tool calls, tool results) plus session metadata (model, working directory, token counts, timestamps) is stored.

## Approach

**Extend the existing `internal/store/Store`** with two new tables and corresponding CRUD methods. The store is already the persistence layer for skill approvals, install state, and registry cache. Adding conversations follows the spec's layout: `internal/store` = "conversations + skill approvals".

## Schema

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL,
    working_dir   TEXT NOT NULL DEFAULT '',
    system_prompt TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    token_count   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    role       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq);
```

Key decisions:
- **Session ID as UUID** — CLI-friendly for `--resume <id>`, avoids auto-increment issues
- **`seq` for ordering** — integer sequence per session, simpler than timestamp ordering
- **`content` as JSON** — `provider.ContentBlock` slice serialized via `json.Marshal`
- **CASCADE delete** — deleting a session removes its messages automatically

## Store API

### Types

```go
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

type StoredMessage struct {
    ID        int64
    SessionID string
    Seq       int
    Role      string
    Content   []provider.ContentBlock
    CreatedAt time.Time
}
```

### Methods

```go
func (s *Store) CreateSession(sess Session) error
func (s *Store) GetSession(id string) (*Session, error)
func (s *Store) UpdateSession(sess Session) error
func (s *Store) DeleteSession(id string) error
func (s *Store) ListSessions(limit int) ([]Session, error)
func (s *Store) AppendMessage(sessionID string, role string, content []provider.ContentBlock) error
func (s *Store) GetMessages(sessionID string) ([]StoredMessage, error)
```

- `AppendMessage` auto-increments `seq` via `COALESCE(MAX(seq), -1) + 1`
- `ListSessions` returns most-recent-first (`updated_at DESC`), limited
- `UpdateSession` touches `title`, `token_count`, and sets `updated_at` to now

## Agent Integration

### Wiring

- New `WithStore(*store.Store)` option on `Agent`
- New `sessionID string` field on `Agent`
- On `Run()`: create new session (or load existing if resuming)
- After each message exchange: `AppendMessage()` for user, assistant, and tool result messages
- Periodically update `session.TokenCount` and `session.UpdatedAt`

### Resume Flow

- `Conversation.LoadFromMessages([]StoredMessage)` hydrates message history
- Existing `AddUser/AddAssistant/AddToolResult` unchanged — persistence is a side-effect in the agent loop

### CLI (future PR, out of scope)

- `rubichan --resume <session-id>`
- `rubichan sessions list`
- `rubichan sessions delete <id>`

## Error Handling

- **Persistence errors are non-fatal** — logged but don't crash the active session
- **Concurrent access** — existing `MaxOpenConns(1)` serializes SQLite access
- **UUID generation** — `google/uuid` (already indirect dependency, promote to direct)
- **Content serialization** — `json.Marshal/Unmarshal` on `[]provider.ContentBlock`; schema evolution accepted as trade-off vs complex versioning

## Dependencies

- `google/uuid` — promote from indirect to direct
- `modernc.org/sqlite` — already in use
- `internal/provider` — for `ContentBlock` type (one-way dependency, store is leaf)

## Testing

All new methods tested with `:memory:` SQLite (same pattern as existing store tests). Key test cases:
- Session CRUD (create, get, update, delete, list)
- Message append and retrieval ordering
- Cascade delete (session deletion removes messages)
- JSON round-trip for ContentBlock serialization
- Edge cases: empty sessions, duplicate seq prevention, missing session on append
