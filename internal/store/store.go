// Package store provides SQLite-backed persistence for skill permission
// approvals, skill install state, and registry cache entries.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	_ "modernc.org/sqlite"
)

// sqliteDatetimeFormats are the formats that SQLite's datetime() can produce.
// We try them in order when parsing DATETIME columns scanned as strings.
var sqliteDatetimeFormats = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	time.RFC3339,
}

// parseSQLiteDatetime parses a DATETIME string from SQLite into a time.Time.
func parseSQLiteDatetime(s string) (time.Time, error) {
	for _, layout := range sqliteDatetimeFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse datetime %q", s)
}

// Approval represents a persisted permission approval for a skill.
type Approval struct {
	Skill      string
	Permission string
	Scope      string
	ApprovedAt time.Time
}

// SkillInstallState tracks the installed state of a skill.
type SkillInstallState struct {
	Name        string
	Version     string
	Source      string
	InstalledAt time.Time
}

// RegistryEntry is a cached skill entry from the remote registry.
type RegistryEntry struct {
	Name        string
	Version     string
	Description string
	CachedAt    time.Time
}

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

// Memory represents a persisted cross-session memory entry.
type Memory struct {
	ID         int64
	WorkingDir string
	Tag        string
	Content    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Store wraps a SQLite database for skill system persistence.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) a SQLite database at dbPath and ensures
// all required tables exist. Use ":memory:" for an in-memory database.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Serialize access to prevent SQLITE_BUSY from concurrent goroutines.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func createTables(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS permission_approvals (
			skill       TEXT NOT NULL,
			permission  TEXT NOT NULL,
			scope       TEXT NOT NULL,
			approved_at DATETIME NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (skill, permission)
		)`,
		`CREATE TABLE IF NOT EXISTS skill_state (
			name         TEXT PRIMARY KEY,
			version      TEXT NOT NULL,
			source       TEXT NOT NULL,
			installed_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS registry_cache (
			name        TEXT PRIMARY KEY,
			version     TEXT NOT NULL,
			description TEXT NOT NULL,
			cached_at   DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
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
		`CREATE TABLE IF NOT EXISTS memories (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			working_dir TEXT NOT NULL,
			tag         TEXT NOT NULL,
			content     TEXT NOT NULL,
			created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at  DATETIME NOT NULL DEFAULT (datetime('now')),
			UNIQUE(working_dir, tag)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_dir ON memories(working_dir)`,
		`CREATE TABLE IF NOT EXISTS tool_result_blobs (
			id         TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			tool_name  TEXT NOT NULL,
			content    BLOB NOT NULL,
			byte_size  INTEGER NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_blobs_session ON tool_result_blobs(session_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}
	return nil
}

// IsApproved returns true if the skill has a permanent ("always") approval
// for the given permission.
func (s *Store) IsApproved(skill, permission string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM permission_approvals
		 WHERE skill = ? AND permission = ? AND scope = 'always'`,
		skill, permission,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query approval: %w", err)
	}
	return count > 0, nil
}

// Approve records a permission approval for the given skill. If the
// skill+permission pair already exists, it is replaced.
func (s *Store) Approve(skill, permission, scope string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO permission_approvals (skill, permission, scope, approved_at)
		 VALUES (?, ?, ?, datetime('now'))`,
		skill, permission, scope,
	)
	if err != nil {
		return fmt.Errorf("approve: %w", err)
	}
	return nil
}

// Revoke removes all permission approvals for the given skill.
func (s *Store) Revoke(skill string) error {
	_, err := s.db.Exec(
		`DELETE FROM permission_approvals WHERE skill = ?`, skill,
	)
	if err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	return nil
}

// ListApprovals returns all permission approvals for the given skill.
func (s *Store) ListApprovals(skill string) ([]Approval, error) {
	rows, err := s.db.Query(
		`SELECT skill, permission, scope, approved_at
		 FROM permission_approvals WHERE skill = ?`, skill,
	)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	defer rows.Close()

	var approvals []Approval
	for rows.Next() {
		var a Approval
		var approvedAtStr string
		if err := rows.Scan(&a.Skill, &a.Permission, &a.Scope, &approvedAtStr); err != nil {
			return nil, fmt.Errorf("scan approval: %w", err)
		}
		a.ApprovedAt, _ = parseSQLiteDatetime(approvedAtStr)
		approvals = append(approvals, a)
	}
	return approvals, rows.Err()
}

// SaveSkillState persists the install state for a skill. If the skill
// already exists, its record is replaced.
func (s *Store) SaveSkillState(state SkillInstallState) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO skill_state (name, version, source, installed_at)
		 VALUES (?, ?, ?, datetime('now'))`,
		state.Name, state.Version, state.Source,
	)
	if err != nil {
		return fmt.Errorf("save skill state: %w", err)
	}
	return nil
}

// GetSkillState retrieves the install state for a skill by name.
// Returns nil if the skill is not found.
func (s *Store) GetSkillState(name string) (*SkillInstallState, error) {
	var st SkillInstallState
	var installedAtStr string
	err := s.db.QueryRow(
		`SELECT name, version, source, installed_at
		 FROM skill_state WHERE name = ?`, name,
	).Scan(&st.Name, &st.Version, &st.Source, &installedAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get skill state: %w", err)
	}
	st.InstalledAt, _ = parseSQLiteDatetime(installedAtStr)
	return &st, nil
}

// ListAllSkillStates returns all installed skill states, sorted by name.
func (s *Store) ListAllSkillStates() ([]SkillInstallState, error) {
	rows, err := s.db.Query(
		`SELECT name, version, source, installed_at
		 FROM skill_state ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list skill states: %w", err)
	}
	defer rows.Close()

	var states []SkillInstallState
	for rows.Next() {
		var st SkillInstallState
		var installedAtStr string
		if err := rows.Scan(&st.Name, &st.Version, &st.Source, &installedAtStr); err != nil {
			return nil, fmt.Errorf("scan skill state: %w", err)
		}
		st.InstalledAt, _ = parseSQLiteDatetime(installedAtStr)
		states = append(states, st)
	}
	return states, rows.Err()
}

// DeleteSkillState removes the install state for a skill by name.
func (s *Store) DeleteSkillState(name string) error {
	_, err := s.db.Exec(`DELETE FROM skill_state WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete skill state: %w", err)
	}
	return nil
}

// CacheRegistryEntry stores a registry entry in the local cache.
// If the entry already exists, it is replaced.
func (s *Store) CacheRegistryEntry(entry RegistryEntry) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO registry_cache (name, version, description, cached_at)
		 VALUES (?, ?, ?, datetime('now'))`,
		entry.Name, entry.Version, entry.Description,
	)
	if err != nil {
		return fmt.Errorf("cache registry entry: %w", err)
	}
	return nil
}

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

// UpdateSession updates a session's title, token_count, and updated_at timestamp.
// Only Title and TokenCount from the Session struct are written; other fields
// (Model, WorkingDir, SystemPrompt) are immutable after creation.
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

// GetCachedRegistry retrieves a cached registry entry by name.
// Returns nil if the entry is not found.
func (s *Store) GetCachedRegistry(name string) (*RegistryEntry, error) {
	var e RegistryEntry
	var cachedAtStr string
	err := s.db.QueryRow(
		`SELECT name, version, description, cached_at
		 FROM registry_cache WHERE name = ?`, name,
	).Scan(&e.Name, &e.Version, &e.Description, &cachedAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cached registry: %w", err)
	}
	e.CachedAt, _ = parseSQLiteDatetime(cachedAtStr)
	return &e, nil
}

// AppendMessage adds a message to a session, auto-incrementing the sequence number.
// The content blocks are serialized to JSON for storage.
func (s *Store) AppendMessage(sessionID, role string, content []provider.ContentBlock) error {
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("marshal content: %w", err)
	}

	// The COALESCE subquery computes the next sequence number. This is safe
	// because MaxOpenConns(1) serializes all database access, preventing
	// concurrent callers from computing the same MAX(seq). Do not increase
	// MaxOpenConns without wrapping this in an explicit transaction.
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

// SaveMemory upserts a memory entry. If a memory with the same working_dir
// and tag exists, its content and updated_at are updated.
func (s *Store) SaveMemory(workingDir, tag, content string) error {
	_, err := s.db.Exec(
		`INSERT INTO memories (working_dir, tag, content, created_at, updated_at)
		 VALUES (?, ?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(working_dir, tag)
		 DO UPDATE SET content = excluded.content, updated_at = datetime('now')`,
		workingDir, tag, content,
	)
	if err != nil {
		return fmt.Errorf("save memory: %w", err)
	}
	return nil
}

// LoadMemories retrieves all memories for the given working directory.
func (s *Store) LoadMemories(workingDir string) ([]Memory, error) {
	rows, err := s.db.Query(
		`SELECT id, working_dir, tag, content, created_at, updated_at
		 FROM memories WHERE working_dir = ? ORDER BY tag`,
		workingDir,
	)
	if err != nil {
		return nil, fmt.Errorf("load memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		var createdStr, updatedStr string
		if err := rows.Scan(&m.ID, &m.WorkingDir, &m.Tag, &m.Content, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		m.CreatedAt, _ = parseSQLiteDatetime(createdStr)
		m.UpdatedAt, _ = parseSQLiteDatetime(updatedStr)
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

// DeleteMemory removes a memory by ID.
func (s *Store) DeleteMemory(id int64) error {
	_, err := s.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	return nil
}

// SaveBlob stores a large tool result blob.
func (s *Store) SaveBlob(id, sessionID, toolName, content string, byteSize int) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO tool_result_blobs (id, session_id, tool_name, content, byte_size)
		 VALUES (?, ?, ?, ?, ?)`,
		id, sessionID, toolName, content, byteSize,
	)
	if err != nil {
		return fmt.Errorf("save blob: %w", err)
	}
	return nil
}

// GetBlob retrieves a stored tool result by reference ID.
// Returns empty string if not found.
func (s *Store) GetBlob(id string) (string, error) {
	var content string
	err := s.db.QueryRow(`SELECT content FROM tool_result_blobs WHERE id = ?`, id).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get blob: %w", err)
	}
	return content, nil
}
