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

func TestAddColumnIfMissing(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	// Test 1: Add a column that doesn't exist
	err := addColumnIfMissing(db, "entities", "version", "ALTER TABLE entities ADD COLUMN version TEXT DEFAULT ''")
	require.NoError(t, err)

	// Verify column exists
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('entities') WHERE name='version'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Test 2: Call again with the same column (should be idempotent)
	err = addColumnIfMissing(db, "entities", "version", "ALTER TABLE entities ADD COLUMN version TEXT DEFAULT ''")
	require.NoError(t, err)

	// Verify column still exists (unchanged)
	err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('entities') WHERE name='version'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Test 3: Add multiple different columns
	err = addColumnIfMissing(db, "entities", "usage_count", "ALTER TABLE entities ADD COLUMN usage_count INTEGER DEFAULT 0")
	require.NoError(t, err)

	err = addColumnIfMissing(db, "entities", "confidence", "ALTER TABLE entities ADD COLUMN confidence REAL DEFAULT 0.0")
	require.NoError(t, err)

	// Verify all columns exist
	err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('entities') WHERE name IN ('version', 'usage_count', 'confidence')`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func TestStatsBasic(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	// Insert some test entities
	stmt := `INSERT INTO entities(id, kind, title, confidence, usage_count) VALUES(?, ?, ?, ?, ?)`
	_, err := db.Exec(stmt, "arch-001", "architecture", "Arch 1", 0.9, 5)
	require.NoError(t, err)
	_, err = db.Exec(stmt, "gotcha-001", "gotcha", "Gotcha 1", 0.7, 2)
	require.NoError(t, err)
	_, err = db.Exec(stmt, "pattern-001", "pattern", "Pattern 1", 0.0, 0)
	require.NoError(t, err)

	// Compute stats (using knowledgegraph.Stats function from internal package)
	// For now, just verify the data is in the database
	var totalCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&totalCount)
	require.NoError(t, err)
	require.Equal(t, 3, totalCount)

	// Verify by kind breakdown
	var archCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE kind='architecture'`).Scan(&archCount)
	require.NoError(t, err)
	require.Equal(t, 1, archCount)

	// Verify confidence metrics
	var avgConfidence float64
	err = db.QueryRow(`SELECT AVG(confidence) FROM entities WHERE confidence > 0`).Scan(&avgConfidence)
	require.NoError(t, err)
	require.InDelta(t, 0.8, avgConfidence, 0.01) // (0.9 + 0.7) / 2 = 0.8

	// Verify usage tracking
	var totalUsage int
	err = db.QueryRow(`SELECT SUM(usage_count) FROM entities`).Scan(&totalUsage)
	require.NoError(t, err)
	require.Equal(t, 7, totalUsage)

	// Verify never-used count
	var neverUsedCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM entities WHERE usage_count = 0`).Scan(&neverUsedCount)
	require.NoError(t, err)
	require.Equal(t, 1, neverUsedCount)
}
