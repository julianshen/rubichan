package wiki

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- helpers ----------

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
}

func gitAdd(t *testing.T, dir, file string) {
	t.Helper()
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-m", "add "+file, "--allow-empty")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(out))
}

// ---------- tests ----------

func TestScanEmptyDir(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestScanGoFiles(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	src := `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}

func Goodbye() {
	fmt.Println("goodbye")
}
`
	writeFile(t, filepath.Join(dir, "main.go"), src)
	gitAdd(t, dir, "main.go")

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	require.Len(t, files, 1)

	f := files[0]
	assert.Equal(t, "main.go", f.Path)
	assert.Equal(t, "go", f.Language)
	assert.Equal(t, "root", f.Module)
	assert.Greater(t, f.Size, int64(0))

	// Should have extracted functions
	require.Len(t, f.Functions, 2)
	names := []string{f.Functions[0].Name, f.Functions[1].Name}
	assert.Contains(t, names, "Hello")
	assert.Contains(t, names, "Goodbye")

	// Should have extracted imports
	assert.Contains(t, f.Imports, "fmt")
}

func TestScanSkipsVendorAndNodeModules(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Create files in directories that should be skipped
	writeFile(t, filepath.Join(dir, "vendor", "lib.go"), "package lib")
	writeFile(t, filepath.Join(dir, "node_modules", "index.js"), "module.exports = {}")
	writeFile(t, filepath.Join(dir, "real.go"), "package main\n\nfunc Main() {}\n")

	gitAdd(t, dir, ".")

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)

	// Only real.go should appear
	require.Len(t, files, 1)
	assert.Equal(t, "real.go", files[0].Path)
}

func TestScanUnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "README.md"), "# Hello")
	gitAdd(t, dir, "README.md")

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	require.Len(t, files, 1)

	f := files[0]
	assert.Equal(t, "README.md", f.Path)
	assert.Equal(t, "unknown", f.Language)
	assert.Nil(t, f.Functions)
	assert.Nil(t, f.Imports)
}

func TestScanMultipleModules(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n\nfunc Main() {}\n")
	writeFile(t, filepath.Join(dir, "internal", "lib.go"), "package internal\n\nfunc Lib() {}\n")

	gitAdd(t, dir, ".")

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	require.Len(t, files, 2)

	modules := map[string]bool{}
	for _, f := range files {
		modules[f.Module] = true
	}
	assert.True(t, modules["cmd"], "expected module 'cmd'")
	assert.True(t, modules["internal"], "expected module 'internal'")
}

func TestScanFallbackWalkDir(t *testing.T) {
	// Create a directory that is NOT a git repo
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "app.go"), "package app\n\nfunc Run() {}\n")
	writeFile(t, filepath.Join(dir, "lib", "helper.go"), "package lib\n\nfunc Help() {}\n")

	p := parser.NewParser()
	files, err := Scan(context.Background(), dir, p)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 2)

	paths := map[string]bool{}
	for _, f := range files {
		paths[f.Path] = true
	}
	assert.True(t, paths["app.go"], "expected app.go")
	assert.True(t, paths[filepath.Join("lib", "helper.go")], "expected lib/helper.go")
}
