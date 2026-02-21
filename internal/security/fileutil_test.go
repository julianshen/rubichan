package security

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectFilesReturnsTargetFiles(t *testing.T) {
	target := ScanTarget{
		RootDir: "/unused",
		Files:   []string{"a.go", "b.py", "c.txt"},
	}
	files, err := CollectFiles(target, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"a.go", "b.py", "c.txt"}, files)
}

func TestCollectFilesFiltersTargetFilesByExtension(t *testing.T) {
	target := ScanTarget{
		RootDir: "/unused",
		Files:   []string{"a.go", "b.py", "c.txt", "d.go"},
	}
	files, err := CollectFiles(target, []string{".go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a.go", "d.go"}, files)
}

func TestCollectFilesWalksDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "package main")
	writeTestFile(t, dir, "lib.py", "print('hello')")
	writeTestFile(t, dir, "data.txt", "some data")

	files, err := CollectFiles(ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	sort.Strings(files)
	assert.Equal(t, []string{"data.txt", "lib.py", "main.go"}, files)
}

func TestCollectFilesWithExtensionFilter(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "package main")
	writeTestFile(t, dir, "lib.py", "print('hello')")
	writeTestFile(t, dir, "data.txt", "some data")

	files, err := CollectFiles(ScanTarget{RootDir: dir}, []string{".go", ".py"})
	require.NoError(t, err)
	sort.Strings(files)
	assert.Equal(t, []string{"lib.py", "main.go"}, files)
}

func TestCollectFilesRespectsExcludePatterns(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "package main")
	writeTestFile(t, dir, "vendor/dep.go", "package dep")
	writeTestFile(t, dir, "vendor/sub/sub.go", "package sub")

	target := ScanTarget{
		RootDir:         dir,
		ExcludePatterns: []string{"vendor/**"},
	}
	files, err := CollectFiles(target, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, files)
}

func TestCollectFilesExcludePatternsWithExtensions(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "package main")
	writeTestFile(t, dir, "test.go", "package main")
	writeTestFile(t, dir, "vendor/dep.go", "package dep")
	writeTestFile(t, dir, "lib.py", "print('hello')")

	target := ScanTarget{
		RootDir:         dir,
		ExcludePatterns: []string{"vendor/**"},
	}
	files, err := CollectFiles(target, []string{".go"})
	require.NoError(t, err)
	sort.Strings(files)
	assert.Equal(t, []string{"main.go", "test.go"}, files)
}

func TestCollectFilesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	files, err := CollectFiles(ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestCollectFilesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "a/b/c.go", "package c")
	writeTestFile(t, dir, "d.go", "package d")

	files, err := CollectFiles(ScanTarget{RootDir: dir}, []string{".go"})
	require.NoError(t, err)
	sort.Strings(files)
	assert.Equal(t, []string{filepath.Join("a", "b", "c.go"), "d.go"}, files)
}

func TestIsExcludedExactMatch(t *testing.T) {
	assert.True(t, IsExcluded("secret.env", []string{"secret.env"}))
	assert.False(t, IsExcluded("main.go", []string{"secret.env"}))
}

func TestIsExcludedGlobPattern(t *testing.T) {
	assert.True(t, IsExcluded("config.yaml", []string{"*.yaml"}))
	assert.False(t, IsExcluded("config.toml", []string{"*.yaml"}))
}

func TestIsExcludedDoubleStarPattern(t *testing.T) {
	assert.True(t, IsExcluded("vendor/dep.go", []string{"vendor/**"}))
	assert.True(t, IsExcluded("vendor/sub/deep.go", []string{"vendor/**"}))
	assert.False(t, IsExcluded("main.go", []string{"vendor/**"}))
}

func TestIsExcludedMultiplePatterns(t *testing.T) {
	patterns := []string{"vendor/**", "*.test", "node_modules/**"}
	assert.True(t, IsExcluded("vendor/dep.go", patterns))
	assert.True(t, IsExcluded("foo.test", patterns))
	assert.True(t, IsExcluded("node_modules/pkg/index.js", patterns))
	assert.False(t, IsExcluded("main.go", patterns))
}

func TestIsExcludedEmptyPatterns(t *testing.T) {
	assert.False(t, IsExcluded("anything.go", nil))
	assert.False(t, IsExcluded("anything.go", []string{}))
}

func TestCollectFilesNonexistentDir(t *testing.T) {
	target := ScanTarget{RootDir: filepath.Join(os.TempDir(), "nonexistent-dir-abc123")}
	files, err := CollectFiles(target, nil)
	// filepath.Walk returns an error for non-existent root directories.
	// If it doesn't, we at least expect no files.
	if err == nil {
		assert.Empty(t, files)
	}
}
