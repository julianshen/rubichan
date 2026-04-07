package knowledgegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// LanguageProfile contains detected project language information
type LanguageProfile struct {
	Language   string   // go, python, javascript, rust, etc.
	Frameworks []string // Detected frameworks/tools
	Root       string   // Project root path
}

// BootstrapLanguageDetector identifies project type and language
type BootstrapLanguageDetector struct{}

// NewBootstrapLanguageDetector creates a new language detector
func NewBootstrapLanguageDetector() *BootstrapLanguageDetector {
	return &BootstrapLanguageDetector{}
}

// Detect analyzes project directory and returns language profile
func (d *BootstrapLanguageDetector) Detect(ctx context.Context, dir string) (*LanguageProfile, error) {
	profile := &LanguageProfile{
		Root:       dir,
		Frameworks: []string{},
	}

	// Detect language by looking for key files
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		profile.Language = "go"
		profile.Frameworks = append(profile.Frameworks, "go")
	} else if _, err := os.Stat(filepath.Join(dir, "setup.py")); err == nil {
		profile.Language = "python"
		profile.Frameworks = append(profile.Frameworks, "setuptools")
	} else if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		profile.Language = "javascript"
		profile.Frameworks = append(profile.Frameworks, "npm")
	}

	// If multiple languages detected, mark as mixed
	if len(profile.Frameworks) > 1 {
		profile.Language = "mixed"
	}

	if profile.Language == "" {
		return nil, fmt.Errorf("could not detect project language in %s", dir)
	}

	return profile, nil
}

// IsInitialized checks if .knowledge/ already has entities
func (d *BootstrapLanguageDetector) IsInitialized(ctx context.Context, dir string) (bool, error) {
	knowledgeDir := filepath.Join(dir, ".knowledge")
	entries, err := os.ReadDir(knowledgeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return len(entries) > 0, nil
}

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
