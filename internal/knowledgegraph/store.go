package knowledgegraph

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// createTables creates all required tables and indices for the knowledge graph index.
// Based on patterns from internal/store/store.go.
func createTables(db *sql.DB) error {
	stmts := []string{
		// Main entities table
		`CREATE TABLE IF NOT EXISTS entities (
			id         TEXT PRIMARY KEY,
			kind       TEXT NOT NULL,
			title      TEXT NOT NULL,
			tags_json  TEXT NOT NULL DEFAULT '[]',
			body       TEXT NOT NULL DEFAULT '',
			source     TEXT NOT NULL DEFAULT 'manual',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_kind ON entities(kind)`,

		// Relationships between entities
		`CREATE TABLE IF NOT EXISTS relationships (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
			kind      TEXT NOT NULL,
			target_id TEXT NOT NULL,
			UNIQUE(source_id, kind, target_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_rel_source ON relationships(source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rel_target ON relationships(target_id)`,

		// Vector embeddings for semantic search
		`CREATE TABLE IF NOT EXISTS embeddings (
			entity_id TEXT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
			vector    BLOB NOT NULL,
			dims      INTEGER NOT NULL
		)`,

		// FTS5 virtual table for keyword/BM25 search (fallback)
		`CREATE VIRTUAL TABLE IF NOT EXISTS entities_fts USING fts5(
			id UNINDEXED,
			title,
			body,
			tags
		)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("createTables: %w", err)
		}
	}

	// Schema migrations would go here via addColumnIfMissing() like in internal/store

	return nil
}

// encodeVector packs float32 slice into little-endian bytes for storage.
func encodeVector(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// decodeVector unpacks little-endian bytes into float32 slice.
func decodeVector(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// cosineSimilarity computes cosine similarity between two float32 vectors.
// Returns 0 if either vector has zero magnitude.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}

	magA = math.Sqrt(magA)
	magB = math.Sqrt(magB)

	if magA == 0 || magB == 0 {
		return 0
	}

	return dot / (magA * magB)
}

// populateFTS populates the FTS5 virtual table with entity data.
// Call this after inserting/updating entities.
func populateFTS(db *sql.DB, entityID string, title, body, tagsJSON string) error {
	// Split tags from JSON array string for FTS indexing
	tags := strings.Trim(tagsJSON, "[]")
	tags = strings.ReplaceAll(tags, `"`, "")

	stmt := `INSERT OR REPLACE INTO entities_fts(id, title, body, tags) VALUES(?, ?, ?, ?)`
	_, err := db.Exec(stmt, entityID, title, body, tags)
	return err
}

// rebuildFTS rebuilds the entire FTS5 virtual table from entities.
func rebuildFTS(db *sql.DB) error {
	// Rebuild the FTS5 table
	stmts := []string{
		`DELETE FROM entities_fts`,
		`INSERT INTO entities_fts(id, title, body, tags)
			SELECT entities.id, entities.title, entities.body,
				   REPLACE(REPLACE(REPLACE(tags_json, '[', ''), ']', ''), '"', '')
			FROM entities`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("rebuildFTS: %w", err)
		}
	}

	return nil
}
