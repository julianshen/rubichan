// Package store provides SQLite-backed persistence for skill permission
// approvals, skill install state, and registry cache entries.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

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
		if err := rows.Scan(&a.Skill, &a.Permission, &a.Scope, &a.ApprovedAt); err != nil {
			return nil, fmt.Errorf("scan approval: %w", err)
		}
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
	err := s.db.QueryRow(
		`SELECT name, version, source, installed_at
		 FROM skill_state WHERE name = ?`, name,
	).Scan(&st.Name, &st.Version, &st.Source, &st.InstalledAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get skill state: %w", err)
	}
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
		if err := rows.Scan(&st.Name, &st.Version, &st.Source, &st.InstalledAt); err != nil {
			return nil, fmt.Errorf("scan skill state: %w", err)
		}
		states = append(states, st)
	}
	return states, rows.Err()
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

// GetCachedRegistry retrieves a cached registry entry by name.
// Returns nil if the entry is not found.
func (s *Store) GetCachedRegistry(name string) (*RegistryEntry, error) {
	var e RegistryEntry
	err := s.db.QueryRow(
		`SELECT name, version, description, cached_at
		 FROM registry_cache WHERE name = ?`, name,
	).Scan(&e.Name, &e.Version, &e.Description, &e.CachedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cached registry: %w", err)
	}
	return &e, nil
}
