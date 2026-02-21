package security

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTestFile creates a file under dir with the given relative name and content.
func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestPrioritizerScoresAuthCode(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "auth.go", `package main

func authenticate(user string) bool {
	// auth logic with password check
	return true
}
`)

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	assert.GreaterOrEqual(t, chunks[0].RiskScore, 10)
}

func TestPrioritizerScoresExecCode(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "runner.go", `package main

import "os/exec"

func runCommand() {
	exec.Command("ls", "-la")
}
`)

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	assert.GreaterOrEqual(t, chunks[0].RiskScore, 9)
}

func TestPrioritizerAddsScores(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "combined.go", `package main

import "database/sql"

func loginHandler(db *sql.DB) {
	// auth + database/sql means additive scoring
	password := "secret"
	_ = password
	db.Query("SELECT * FROM users")
}
`)

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	// auth(10) + database/sql(7) = 17
	assert.GreaterOrEqual(t, chunks[0].RiskScore, 17)
}

func TestPrioritizerRespectsMinScore(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "boring.go", `package main

func hello() {
	println("hello world")
}
`)

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 5, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	assert.Empty(t, chunks, "low-risk file should be filtered out")
}

func TestPrioritizerRespectsBudgetCap(t *testing.T) {
	dir := t.TempDir()
	// Create multiple high-risk files to generate many chunks.
	for i := 0; i < 10; i++ {
		writeTestFile(t, dir, filepath.Join("pkg", "file"+string(rune('a'+i))+".go"), `package pkg

import "os/exec"

func run() {
	exec.Command("test")
}
`)
	}

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 3})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	assert.Len(t, chunks, 3, "should cap at MaxChunks")
}

func TestPrioritizerBoostedByStaticFindings(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "flagged.go", `package main

func hello() {
	println("hello")
}
`)

	staticFindings := []Finding{
		{Location: Location{File: "flagged.go", StartLine: 1}},
	}

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, staticFindings)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	// File has no keyword signals but gets +3 from static findings.
	assert.GreaterOrEqual(t, chunks[0].RiskScore, 3)
}

func TestPrioritizerSortsHighestFirst(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "low.go", `package main

func sayHi() {
	println("hi")
}
`)
	writeTestFile(t, dir, "high.go", `package main

import "os/exec"

func dangerous() {
	exec.Command("rm", "-rf", "/")
	// auth credential password
}
`)

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	assert.Greater(t, chunks[0].RiskScore, chunks[1].RiskScore)
}

func TestPrioritizerEmptyDir(t *testing.T) {
	dir := t.TempDir()

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestPrioritizerSplitsFunctions(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "multi.go", `package main

func first() {
	// auth check
}

func second() {
	// another function
}

func third() {
	// third function
}
`)

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	assert.Len(t, chunks, 3, "should split into one chunk per function")

	// Verify each chunk has the correct file reference.
	for _, c := range chunks {
		assert.Equal(t, "multi.go", c.File)
		assert.NotEmpty(t, c.Content)
		assert.Equal(t, "go", c.Language)
	}

	// Verify function names appear in the right chunks.
	assert.Contains(t, chunks[0].Content, "first")
}

func TestPrioritizerExcludesPatterns(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", `package main

func loginHandler() {
	// auth
}
`)
	writeTestFile(t, dir, "vendor/dep.go", `package dep

func auth() {
	// credential check
}
`)

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	target := ScanTarget{
		RootDir:         dir,
		ExcludePatterns: []string{"vendor/**"},
	}
	chunks, err := p.Prioritize(context.Background(), target, nil)
	require.NoError(t, err)
	for _, c := range chunks {
		assert.False(t, filepath.HasPrefix(c.File, "vendor/"),
			"vendor files should be excluded")
	}
}

func TestPrioritizerUnsupportedFileType(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "config.yaml", `auth:
  password: secret
  jwt_secret: mykey
`)

	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, chunks, "unsupported file types should be included as whole-file chunks")
	assert.Equal(t, 1, chunks[0].StartLine)
}
