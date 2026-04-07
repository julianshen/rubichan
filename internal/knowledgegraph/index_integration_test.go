package knowledgegraph

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestIndex_CreatedWithCorrectSchema verifies index file is created with correct SQLite schema
func TestIndex_CreatedWithCorrectSchema(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	// Index should be created when graph is opened
	indexPath := filepath.Join(fixture.Dir, ".knowledge", ".index.db")

	// Verify index file exists
	require.FileExists(t, indexPath)

	// Check schema validity
	err := AssertIndexValid(t, indexPath)
	require.NoError(t, err)
}

// TestIndex_RebuildFromEntities verifies index can be rebuilt from entity files on disk
func TestIndex_RebuildFromEntities(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	indexPath := filepath.Join(fixture.Dir, ".knowledge", ".index.db")
	require.FileExists(t, indexPath)

	// Get reference to graph
	graph := fixture.Graph.(*KnowledgeGraph)

	// Rebuild index explicitly
	err := graph.RebuildIndex(context.Background())
	require.NoError(t, err)

	// Verify index is still valid
	err = AssertIndexValid(t, indexPath)
	require.NoError(t, err)

	// Verify graph is still usable - get stats
	stats, err := graph.Stats(context.Background())
	require.NoError(t, err)
	require.NotNil(t, stats)
	require.Greater(t, stats.TotalEntities, 0, "should have entities from fixture")
}

// TestIndex_CorruptionDetectionAndRecovery verifies corrupted index causes rebuild failure
// and can be recovered by removing and re-opening the graph
func TestIndex_CorruptionDetectionAndRecovery(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	indexPath := filepath.Join(fixture.Dir, ".knowledge", ".index.db")
	require.FileExists(t, indexPath)

	// Corrupt the index by truncating to 100 bytes
	err := os.Truncate(indexPath, 100)
	require.NoError(t, err)

	// Verify corruption by trying to open the db directly
	db, err := sql.Open("sqlite", indexPath)
	require.NoError(t, err)
	defer db.Close()

	// Query should fail on corrupted index
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&count)
	require.Error(t, err, "should fail on corrupted index")

	// Rebuild attempt on corrupted index will fail
	err = fixture.Graph.(*KnowledgeGraph).RebuildIndex(context.Background())
	require.Error(t, err, "rebuild should fail on corrupted index")

	// Recovery: delete corrupted index and re-create from entity files
	err = os.Remove(indexPath)
	require.NoError(t, err)
	require.NoFileExists(t, indexPath)

	// Re-opening graph creates fresh index from entity files
	g, err := openGraph(context.Background(), fixture.Dir, nil)
	require.NoError(t, err, "should create new index from entity files")
	require.NotNil(t, g)

	// Verify recovered index is valid
	err = AssertIndexValid(t, indexPath)
	require.NoError(t, err)
}

// TestIndex_StatisticsCalculation verifies stats are calculated correctly from index
func TestIndex_StatisticsCalculation(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph.(*KnowledgeGraph)

	// Get stats from the graph
	stats, err := graph.Stats(context.Background())
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Verify basic stats are populated
	require.Greater(t, stats.TotalEntities, 0, "should have entities from fixture")

	// Confidence metrics should be valid ranges
	require.GreaterOrEqual(t, stats.AvgScore, 0.0, "avg confidence should be >= 0")
	require.LessOrEqual(t, stats.AvgScore, 1.0, "avg confidence should be <= 1")

	// High confidence count should not exceed total
	require.LessOrEqual(t, stats.HighConfidenceCount, stats.TotalEntities)

	// Verify stats structure is well-formed
	require.NotNil(t, stats.ByKind)
	require.NotNil(t, stats.ByLayer)
}

// TestIndex_EntitiesLoadedFromFiles verifies entities are loaded from markdown files into index
func TestIndex_EntitiesLoadedFromFiles(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	indexPath := filepath.Join(fixture.Dir, ".knowledge", ".index.db")

	// Open SQLite database to verify entities table
	db, err := sql.Open("sqlite", indexPath)
	require.NoError(t, err)
	defer db.Close()

	// Count entities in index
	var indexCount int
	err = db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&indexCount)
	require.NoError(t, err)
	require.Greater(t, indexCount, 0, "should have entities in index")

	// Verify relationships table is populated (if any relationships exist)
	var relCount int
	err = db.QueryRow("SELECT COUNT(*) FROM relationships").Scan(&relCount)
	require.NoError(t, err)
	// relCount may be 0 if fixture has no relationships, that's ok

	// Verify FTS index is populated
	var ftsCount int
	err = db.QueryRow("SELECT COUNT(*) FROM entities_fts").Scan(&ftsCount)
	require.NoError(t, err)
	require.Equal(t, indexCount, ftsCount, "FTS index should have same entity count")

	// Verify all index columns have proper types and constraints
	var colName, colType string
	requiredCols := map[string]bool{
		"id":         true,
		"kind":       true,
		"layer":      true,
		"title":      true,
		"body":       true,
		"confidence": true,
	}

	rows, err := db.Query("PRAGMA table_info(entities)")
	require.NoError(t, err)
	defer rows.Close()

	foundCols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var notnull int
		var dfltValue, pk interface{}
		err := rows.Scan(&cid, &colName, &colType, &notnull, &dfltValue, &pk)
		require.NoError(t, err)
		if requiredCols[colName] {
			foundCols[colName] = true
		}
	}

	for col := range requiredCols {
		require.True(t, foundCols[col], "column %s should exist in entities table", col)
	}
}
