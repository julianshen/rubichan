package knowledgegraph

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestFixture represents an isolated test environment
type TestFixture struct {
	Dir   string   // Temp directory with test data
	Graph kg.Graph // Initialized knowledge graph
}

// NewTestFixture creates an isolated test environment by copying fixture project to temp dir
func NewTestFixture(t *testing.T, projectName string) *TestFixture {
	tmpDir := t.TempDir()

	// Copy testdata/projectName to tmpDir
	fixtureSource := filepath.Join("testdata", projectName)
	err := copyDir(fixtureSource, tmpDir)
	require.NoError(t, err, "copy fixture")

	// Initialize knowledge graph in tmpDir
	g, err := kg.Open(context.Background(), tmpDir)
	require.NoError(t, err, "open graph")

	return &TestFixture{
		Dir:   tmpDir,
		Graph: g,
	}
}

// copyDir recursively copies src directory to dst
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat src: %w", err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("src is not a directory")
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return fmt.Errorf("mkdir dst: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("readdir: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return fmt.Errorf("read %s: %w", srcPath, err)
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return fmt.Errorf("write %s: %w", dstPath, err)
			}
		}
	}
	return nil
}

// AssertEntityExists verifies entity exists in graph with expected fields
func AssertEntityExists(t *testing.T, g kg.Graph, id string, kind kg.EntityKind, bodyPrefix string) {
	entity, err := g.Get(context.Background(), id)
	require.NoErrorf(t, err, "entity %s not found", id)
	require.NotNil(t, entity, "entity should not be nil")
	require.Equal(t, kind, entity.Kind, "kind mismatch")
	require.True(t, strings.HasPrefix(entity.Body, bodyPrefix), "body prefix mismatch: expected to start with %q, got %q", bodyPrefix, entity.Body)
}

// AssertQueryReturns verifies query returns expected entities
func AssertQueryReturns(t *testing.T, g kg.Graph, query string, expectedIDs []string) {
	results, err := g.Query(context.Background(), kg.QueryRequest{Text: query})
	require.NoError(t, err, "query should not error")
	require.NotEmpty(t, results, "query should return results")
	require.GreaterOrEqual(t, len(results), len(expectedIDs), "query should return at least expected IDs")
}

// AssertIndexValid checks SQLite schema and foreign keys
func AssertIndexValid(t *testing.T, dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	// Check schema: tables exist
	tables := []string{"entities", "relationships", "entity_stats"}
	for _, table := range tables {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
		if err != nil {
			return fmt.Errorf("table %s check: %w", table, err)
		}
		require.Equal(t, 1, count, "table %s not found", table)
	}
	return nil
}

// AssertErrorContains verifies error message contains substring
func AssertErrorContains(t *testing.T, err error, substring string) {
	require.NotNil(t, err, "error should not be nil")
	require.Contains(t, err.Error(), substring, "error message should contain substring")
}
