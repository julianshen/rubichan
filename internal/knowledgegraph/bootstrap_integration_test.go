package knowledgegraph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBootstrap_DetectsGoProject tests detection of Go project language and frameworks
func TestBootstrap_DetectsGoProject(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	// Detect bootstrap profile
	detector := NewBootstrapLanguageDetector()
	profile, err := detector.Detect(context.Background(), fixture.Dir)

	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, "go", profile.Language)
	require.NotEmpty(t, profile.Frameworks)
	require.Contains(t, profile.Frameworks, "go")
}

// TestBootstrap_DetectsPythonProject tests detection of Python project language and frameworks
func TestBootstrap_DetectsPythonProject(t *testing.T) {
	fixture := NewTestFixture(t, "python-project")
	defer fixture.Cleanup()

	detector := NewBootstrapLanguageDetector()
	profile, err := detector.Detect(context.Background(), fixture.Dir)

	require.NoError(t, err)
	require.Equal(t, "python", profile.Language)
	require.Contains(t, profile.Frameworks, "setuptools")
}

// TestBootstrap_SkipsIfAlreadyInitialized tests that IsInitialized returns true for projects with .knowledge/
func TestBootstrap_SkipsIfAlreadyInitialized(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	// Project already has .knowledge/ from fixture
	detector := NewBootstrapLanguageDetector()
	initialized, err := detector.IsInitialized(context.Background(), fixture.Dir)

	require.NoError(t, err)
	require.True(t, initialized)
}
