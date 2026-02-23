package main

import (
	"os"
	"path/filepath"
	"testing"

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
