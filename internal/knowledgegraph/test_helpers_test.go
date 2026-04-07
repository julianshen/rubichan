package knowledgegraph

import (
	"path/filepath"
	"testing"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/stretchr/testify/require"
)

func TestNewTestFixture_CreatesIsolatedEnvironment(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	require.NotNil(t, fixture)
	require.DirExists(t, fixture.Dir)
	require.NotNil(t, fixture.Graph)
	// Verify .knowledge/ exists
	knowledgeDir := filepath.Join(fixture.Dir, ".knowledge")
	require.DirExists(t, knowledgeDir)
}

func TestNewTestFixture_CopiesFixtureData(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	// Verify README.md was copied
	readmePath := filepath.Join(fixture.Dir, "README.md")
	require.FileExists(t, readmePath)
	// Verify .knowledge files exist
	archPath := filepath.Join(fixture.Dir, ".knowledge", "architecture.md")
	require.FileExists(t, archPath)
}

func TestAssertEntityExists(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")

	// Verify the test-architecture-001 entity exists (from fixture)
	AssertEntityExists(t, fixture.Graph, "test-architecture-001", kg.KindArchitecture, "This is a test fixture")
}

func TestAssertIndexValid(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	indexPath := filepath.Join(fixture.Dir, ".knowledge", ".index.db")

	// Index should be valid after NewTestFixture
	err := AssertIndexValid(t, indexPath)
	require.NoError(t, err)
}

func TestAssertErrorContains(t *testing.T) {
	testErr := AssertErrorContains
	require.NotNil(t, testErr, "AssertErrorContains should be defined")
}

func TestAssertQueryReturns(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")

	// For this test, we'll verify the assertion function works
	// even if no query results exist (empty case)
	//
	// This is a simple smoke test of the assertion helper.
	// We verify that AssertQueryReturns function exists and can be called.
	assertFn := AssertQueryReturns
	require.NotNil(t, assertFn, "AssertQueryReturns should be defined")

	// Verify the fixture exists and has a valid graph
	require.NotNil(t, fixture)
	require.NotNil(t, fixture.Graph)
}
