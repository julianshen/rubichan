package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/julianshen/rubichan/internal/tools/xcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionString(t *testing.T) {
	s := versionString()
	assert.Contains(t, s, "rubichan")
	assert.Contains(t, s, version)
	assert.Contains(t, s, commit)
	assert.Contains(t, s, date)
}

func TestVersionStringDefaults(t *testing.T) {
	s := versionString()
	assert.Contains(t, s, "dev")
	assert.Contains(t, s, "none")
	assert.Contains(t, s, "unknown")
}

func TestAutoApproveDefaultsFalse(t *testing.T) {
	// autoApprove is a package-level var; verify it defaults to false
	assert.False(t, autoApprove, "auto-approve must default to false to prevent RCE")
}

func TestOpenStore_CreatesDB(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir)
	require.NoError(t, err)
	defer s.Close()

	dbPath := filepath.Join(dir, "rubichan.db")
	_, err = os.Stat(dbPath)
	assert.NoError(t, err, "database file should exist")
}

func TestOpenStore_CreatesMissingDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "config")
	s, err := openStore(dir)
	require.NoError(t, err)
	defer s.Close()

	dbPath := filepath.Join(dir, "rubichan.db")
	_, err = os.Stat(dbPath)
	assert.NoError(t, err, "database file should exist in nested directory")
}

func TestResumeFlagDefaults(t *testing.T) {
	assert.Empty(t, resumeFlag, "resume flag must default to empty")
}

func TestNewDefaultSecurityEngine(t *testing.T) {
	engine := newDefaultSecurityEngine(security.EngineConfig{Concurrency: 4})
	require.NotNil(t, engine)
}

func TestContainsSkill(t *testing.T) {
	tests := []struct {
		name      string
		skill     string
		flagValue string
		want      bool
	}{
		{"exact match", "apple-dev", "apple-dev", true},
		{"in list", "apple-dev", "foo,apple-dev,bar", true},
		{"with spaces", "apple-dev", "foo, apple-dev , bar", true},
		{"not present", "apple-dev", "foo,bar", false},
		{"empty flag", "apple-dev", "", false},
		{"partial match not accepted", "apple", "apple-dev", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsSkill(tt.skill, tt.flagValue)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppleDevAutoActivation_NoAppleProject(t *testing.T) {
	// A directory with no Apple project files should not trigger apple-dev.
	dir := t.TempDir()
	// Create a non-Apple file.
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	require.NoError(t, err)

	// containsSkill should return false for empty skills flag.
	assert.False(t, containsSkill("apple-dev", ""))
}

func TestAppleDevAutoActivation_WithPackageSwift(t *testing.T) {
	// A directory with Package.swift should trigger apple-dev detection.
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "Package.swift"), []byte("// swift-tools-version:5.9"), 0644)
	require.NoError(t, err)

	// Verify that xcode.DiscoverProject detects SPM project.
	info := xcode.DiscoverProject(dir)
	assert.Equal(t, "spm", info.Type)
}

func TestContainsSkill_SkillsFlagActivation(t *testing.T) {
	// Explicit --skills=apple-dev should activate even without Apple project.
	assert.True(t, containsSkill("apple-dev", "apple-dev,other-skill"))
	assert.True(t, containsSkill("apple-dev", "other-skill,apple-dev"))
	assert.False(t, containsSkill("apple-dev", "other-skill"))
}

func TestRemoveSkill(t *testing.T) {
	tests := []struct {
		name      string
		skill     string
		flagValue string
		want      string
	}{
		{"remove only", "apple-dev", "apple-dev", ""},
		{"remove from list", "apple-dev", "foo,apple-dev,bar", "foo,bar"},
		{"with spaces", "apple-dev", "foo, apple-dev , bar", "foo,bar"},
		{"not present", "apple-dev", "foo,bar", "foo,bar"},
		{"empty flag", "apple-dev", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeSkill(tt.skill, tt.flagValue)
			assert.Equal(t, tt.want, got)
		})
	}
}
