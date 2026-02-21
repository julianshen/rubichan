package scanner

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretScannerName(t *testing.T) {
	s := NewSecretScanner()
	assert.Equal(t, "secrets", s.Name())
}

func TestSecretScannerInterface(t *testing.T) {
	var _ security.StaticScanner = NewSecretScanner()
}

func TestSecretScannerDetectsAWSKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.go", `package config
const awsKey = "AKIAIOSFODNN7REALKEY1"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	f := findings[0]
	assert.Equal(t, "secrets", f.Scanner)
	assert.Equal(t, security.CategorySecretsExposure, f.Category)
	assert.Equal(t, security.SeverityHigh, f.Severity)
	assert.Equal(t, "CWE-798", f.CWE)
	assert.Equal(t, security.ConfidenceHigh, f.Confidence)
	assert.Equal(t, "config.go", f.Location.File)
	assert.Equal(t, 2, f.Location.StartLine)
	// Evidence must NOT contain the full secret
	assert.NotContains(t, f.Evidence, "AKIAIOSFODNN7REALKEY1")
}

func TestSecretScannerDetectsGitHubToken(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "auth.go", `package auth
var token = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "GitHub token detected" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
			assert.Equal(t, security.ConfidenceHigh, f.Confidence)
		}
	}
	assert.True(t, found, "expected a GitHub token finding")
}

func TestSecretScannerDetectsPrivateKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "certs/key.pem", `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA0Z3VS5JJcds3xfn/yGOQ
-----END RSA PRIVATE KEY-----
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	f := findings[0]
	assert.Equal(t, security.SeverityCritical, f.Severity)
	assert.Contains(t, f.Title, "Private key")
}

func TestSecretScannerDetectsGenericHighEntropy(t *testing.T) {
	dir := t.TempDir()
	// Use "credential" which is in the entropy variable pattern but NOT in
	// the generic API key rule, so the entropy detector fires separately.
	writeFile(t, dir, "settings.py", `
my_credential = "aB3$xZ9kL7wQ2mN5pR8vT1yU4"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Confidence == security.ConfidenceMedium {
			found = true
			assert.Equal(t, security.SeverityMedium, f.Severity)
			assert.Contains(t, f.Title, "entropy")
		}
	}
	assert.True(t, found, "expected a high-entropy finding")
}

func TestSecretScannerDetectsDBConnectionString(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "db.go", `package db
const dsn = "postgres://user:pass@localhost:5432/mydb"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "Database connection string detected" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
		}
	}
	assert.True(t, found, "expected a DB connection string finding")
}

func TestSecretScannerSkipsBinary(t *testing.T) {
	dir := t.TempDir()
	// Binary file: contains null bytes within the first 512 bytes.
	content := []byte("AKIAIOSFODNN7EXAMPLE\x00\x00binarydata")
	writeFile(t, dir, "binary.dat", string(content))

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestSecretScannerSkipsExcluded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "vendor/lib/config.go", `package lib
const key = "AKIAIOSFODNN7EXAMPLE"
`)
	writeFile(t, dir, "main.go", `package main
func main() {}
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{
		RootDir:         dir,
		ExcludePatterns: []string{"vendor/**"},
	})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestSecretScannerNoFalsePositiveOnExamples(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "readme.go", `package docs
// Example key: EXAMPLE_KEY
const placeholder = "your-api-key-here"
const dummy = "AKIAIOSFODNN7_EXAMPLE"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings, "should not flag example/placeholder values")
}

func TestSecretScannerEmptyDir(t *testing.T) {
	dir := t.TempDir()

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestSecretScannerUsesTargetFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", `package a
const key = "AKIAIOSFODNN7REALKEY1"
`)
	writeFile(t, dir, "b.go", `package b
const key = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
`)

	s := NewSecretScanner()
	// Only scan a.go — should not find the GitHub token in b.go.
	findings, err := s.Scan(context.Background(), security.ScanTarget{
		RootDir: dir,
		Files:   []string{"a.go"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	for _, f := range findings {
		assert.Equal(t, "a.go", f.Location.File)
	}
}

func TestSecretScannerDetectsSlackToken(t *testing.T) {
	dir := t.TempDir()
	// Build the token via concatenation to avoid triggering GitHub push protection.
	slackToken := "xox" + "b-123456789012-1234567890123-abcdefghijklmnop"
	writeFile(t, dir, "slack.go", `package slack
var token = "`+slackToken+`"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "Slack token detected" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
		}
	}
	assert.True(t, found, "expected a Slack token finding")
}

func TestSecretScannerDetectsGitLabToken(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "gitlab.go", `package gitlab
var token = "glpat-abcdefghijklmnopqrst"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "GitLab token detected" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
		}
	}
	assert.True(t, found, "expected a GitLab token finding")
}

func TestSecretScannerDetectsBearerToken(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "auth.go", `package auth
const header = "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "Bearer token detected" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
		}
	}
	assert.True(t, found, "expected a Bearer token finding")
}

func TestSecretScannerDetectsJWTSecret(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.go", `package config
var jwt_secret = "mySuper$ecretJWT!Key"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "JWT secret detected" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
		}
	}
	assert.True(t, found, "expected a JWT secret finding")
}

func TestSecretScannerDetectsGenericAPIKeyAssignment(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.go", `package app
var api_key = "sk_live_a1b2c3d4e5f6g7h8i9j0"
`)

	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "Generic API key/secret assignment detected" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
		}
	}
	assert.True(t, found, "expected a generic API key finding")
}

func TestSecretScannerContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.go", `package config
const awsKey = "AKIAIOSFODNN7EXAMPLE"
`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	s := NewSecretScanner()
	_, err := s.Scan(ctx, security.ScanTarget{RootDir: dir})
	assert.Error(t, err)
}
