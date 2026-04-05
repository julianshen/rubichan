package knowledgegraph

import (
	"database/sql"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	require.NoError(t, err)
	require.NoError(t, createTables(db))
	return db
}

func TestCreateTables(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	// Verify tables exist by querying sqlite_master
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('entities', 'relationships', 'embeddings')`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func TestEncodeDecodeVector(t *testing.T) {
	tests := [][]float32{
		{},
		{0.0},
		{1.0, 2.0, 3.0},
		{-0.5, 0.0, 0.5},
		{float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN())},
	}

	for _, v := range tests {
		encoded := encodeVector(v)
		decoded := decodeVector(encoded)

		require.Len(t, decoded, len(v))
		for i := range v {
			if math.IsNaN(float64(v[i])) {
				require.True(t, math.IsNaN(float64(decoded[i])))
			} else {
				require.Equal(t, v[i], decoded[i])
			}
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		a        []float32
		b        []float32
		expected float64
	}{
		// Identical vectors → 1.0
		{[]float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		// Orthogonal vectors → 0.0
		{[]float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		// Opposite vectors → -1.0
		{[]float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		// Same direction, different magnitude → 1.0
		{[]float32{1, 1}, []float32{2, 2}, 1.0},
		// Mixed
		{[]float32{0.6, 0.8}, []float32{0.8, 0.6}, 0.96},
		// Zero magnitude → 0.0
		{[]float32{0, 0, 0}, []float32{1, 0, 0}, 0.0},
		// Empty → 0.0
		{[]float32{}, []float32{}, 0.0},
		// Length mismatch → 0.0
		{[]float32{1, 0}, []float32{1, 0, 0}, 0.0},
	}

	for _, tc := range tests {
		sim := cosineSimilarity(tc.a, tc.b)
		if math.IsNaN(tc.expected) {
			require.True(t, math.IsNaN(sim))
		} else {
			require.InDelta(t, tc.expected, sim, 0.0001)
		}
	}
}

func TestPopulateFTS(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	// Insert a test entity
	_, err := db.Exec(
		`INSERT INTO entities(id, kind, title, body, tags_json) VALUES(?, ?, ?, ?, ?)`,
		"test-001", "architecture", "Test Title", "Test body content", `["tag1", "tag2"]`,
	)
	require.NoError(t, err)

	// Populate FTS
	err = populateFTS(db, "test-001", "Test Title", "Test body content", `["tag1", "tag2"]`)
	require.NoError(t, err)

	// Verify FTS was populated
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM entities_fts WHERE id = 'test-001'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestRebuildFTS(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	// Insert multiple test entities
	stmt := `INSERT INTO entities(id, kind, title, body, tags_json) VALUES(?, ?, ?, ?, ?)`
	_, err := db.Exec(stmt, "test-001", "architecture", "Test 1", "Body 1", `["arch"]`)
	require.NoError(t, err)
	_, err = db.Exec(stmt, "test-002", "decision", "Test 2", "Body 2", `["decision"]`)
	require.NoError(t, err)

	// Rebuild FTS
	err = rebuildFTS(db)
	require.NoError(t, err)

	// Verify both entities are in FTS
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM entities_fts`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	// Verify FTS search works
	var id string
	err = db.QueryRow(`SELECT id FROM entities_fts WHERE entities_fts MATCH 'Test' LIMIT 1`).Scan(&id)
	require.NoError(t, err)
	require.True(t, id == "test-001" || id == "test-002")
}
