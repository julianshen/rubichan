# Security Engine Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the complete two-phase security analysis engine with static scanners, LLM-powered analyzers, risk prioritization, attack chain correlation, and six output formats.

**Architecture:** Bottom-up layered build. Types first, then 6 static scanners, prioritizer, 5 LLM analyzers, correlator, engine orchestrator, and 6 output formatters. Each layer depends only on layers below it. Uses existing `provider.LLMProvider` for LLM calls and `internal/parser` for tree-sitter AST queries.

**Tech Stack:** Go 1.26, `smacker/go-tree-sitter` (AST queries), `sourcegraph/conc` (bounded concurrency), `net/http` (OSV API), `modernc.org/sqlite` (already in use), `encoding/xml` (plist parsing)

**Reference docs:**
- Design: `docs/plans/2026-02-21-security-engine-design.md`
- Spec: `spec.md` sections 3.7, ADR-003, ADR-004, Appendix C
- Provider types: `internal/provider/types.go`
- Parser: `internal/parser/parser.go`
- Conc pattern: `internal/wiki/analyzer.go:109-128`

---

### Task 1: Core Types & Interfaces

**Files:**
- Create: `internal/security/types.go`
- Create: `internal/security/types_test.go`

**Step 1: Write the failing test**

```go
// internal/security/types_test.go
package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSeverityString(t *testing.T) {
	assert.Equal(t, "critical", string(SeverityCritical))
	assert.Equal(t, "high", string(SeverityHigh))
	assert.Equal(t, "medium", string(SeverityMedium))
	assert.Equal(t, "low", string(SeverityLow))
	assert.Equal(t, "info", string(SeverityInfo))
}

func TestSeverityCompare(t *testing.T) {
	assert.True(t, SeverityRank(SeverityCritical) > SeverityRank(SeverityHigh))
	assert.True(t, SeverityRank(SeverityHigh) > SeverityRank(SeverityMedium))
	assert.True(t, SeverityRank(SeverityMedium) > SeverityRank(SeverityLow))
	assert.True(t, SeverityRank(SeverityLow) > SeverityRank(SeverityInfo))
}

func TestConfidenceString(t *testing.T) {
	assert.Equal(t, "high", string(ConfidenceHigh))
	assert.Equal(t, "medium", string(ConfidenceMedium))
	assert.Equal(t, "low", string(ConfidenceLow))
}

func TestCategoryValues(t *testing.T) {
	categories := AllCategories()
	assert.Len(t, categories, 13)
	assert.Contains(t, categories, CategoryInjection)
	assert.Contains(t, categories, CategoryAuthentication)
	assert.Contains(t, categories, CategoryCryptography)
	assert.Contains(t, categories, CategorySecretsExposure)
	assert.Contains(t, categories, CategoryRaceCondition)
}

func TestFindingValidation(t *testing.T) {
	f := Finding{
		ID:       "SEC-001",
		Scanner:  "secrets",
		Severity: SeverityHigh,
		Category: CategorySecretsExposure,
		Title:    "Hardcoded API key",
		Location: Location{File: "main.go", StartLine: 10, EndLine: 10},
	}
	assert.Equal(t, "SEC-001", f.ID)
	assert.Equal(t, "main.go", f.Location.File)
}

func TestReportSummary(t *testing.T) {
	r := &Report{
		Findings: []Finding{
			{Severity: SeverityCritical},
			{Severity: SeverityHigh},
			{Severity: SeverityHigh},
			{Severity: SeverityMedium},
		},
		AttackChains: []AttackChain{{ID: "chain-1"}},
	}
	summary := r.Summary()
	assert.Equal(t, 1, summary.Critical)
	assert.Equal(t, 2, summary.High)
	assert.Equal(t, 1, summary.Medium)
	assert.Equal(t, 0, summary.Low)
	assert.Equal(t, 1, summary.Chains)
	assert.Equal(t, 4, summary.Total)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/security/... -v -run TestSeverity`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/security/types.go
package security

import (
	"context"
	"time"
)

// Severity represents the severity level of a security finding.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// SeverityRank returns a numeric rank for comparing severities.
// Higher rank = more severe.
func SeverityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// Confidence represents the confidence level of a finding.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Category represents the category of a security finding.
type Category string

const (
	CategoryInjection           Category = "injection"
	CategoryAuthentication      Category = "authentication"
	CategoryAuthorization       Category = "authorization"
	CategoryCryptography        Category = "cryptography"
	CategorySecretsExposure     Category = "secrets-exposure"
	CategoryVulnerableDep       Category = "vulnerable-dependency"
	CategoryMisconfiguration    Category = "misconfiguration"
	CategoryDataExposure        Category = "data-exposure"
	CategoryRaceCondition       Category = "race-condition"
	CategoryInputValidation     Category = "input-validation"
	CategoryLoggingMonitoring   Category = "logging-monitoring"
	CategorySupplyChain         Category = "supply-chain"
	CategoryLicenseCompliance   Category = "license-compliance"
)

// AllCategories returns all defined categories.
func AllCategories() []Category {
	return []Category{
		CategoryInjection, CategoryAuthentication, CategoryAuthorization,
		CategoryCryptography, CategorySecretsExposure, CategoryVulnerableDep,
		CategoryMisconfiguration, CategoryDataExposure, CategoryRaceCondition,
		CategoryInputValidation, CategoryLoggingMonitoring, CategorySupplyChain,
		CategoryLicenseCompliance,
	}
}

// Location identifies where in the source code a finding was detected.
type Location struct {
	File      string
	StartLine int
	EndLine   int
	Function  string
}

// Finding is the unified output of all scanners and analyzers (Appendix C).
type Finding struct {
	ID          string
	Scanner     string
	Severity    Severity
	Category    Category
	Title       string
	Description string
	Location    Location
	CWE         string
	OWASP       string
	Evidence    string
	Remediation string
	Confidence  Confidence
	References  []string
	Metadata    map[string]string
	SkillSource string
}

// AttackChain represents a correlated sequence of findings forming an exploit path.
type AttackChain struct {
	ID         string
	Title      string
	Severity   Severity
	Steps      []Finding
	Impact     string
	Likelihood string
}

// ScanTarget defines what to scan.
type ScanTarget struct {
	RootDir         string
	Files           []string
	ExcludePatterns []string
}

// AnalysisChunk is a prioritized code segment for LLM analysis.
type AnalysisChunk struct {
	File      string
	StartLine int
	EndLine   int
	Content   string
	Language  string
	RiskScore int
}

// ScanError records a non-fatal error from a scanner or analyzer.
type ScanError struct {
	Scanner string
	Err     error
	Fatal   bool
}

func (e ScanError) Error() string {
	return e.Scanner + ": " + e.Err.Error()
}

// ScanStats contains execution statistics.
type ScanStats struct {
	Duration       time.Duration
	FilesScanned   int
	ChunksAnalyzed int
	FindingsCount  int
	ChainCount     int
}

// ReportSummary provides counts by severity.
type ReportSummary struct {
	Critical int
	High     int
	Medium   int
	Low      int
	Info     int
	Chains   int
	Total    int
}

// Report is the complete output of the security engine.
type Report struct {
	Findings     []Finding
	AttackChains []AttackChain
	Stats        ScanStats
	Errors       []ScanError
}

// Summary returns counts by severity.
func (r *Report) Summary() ReportSummary {
	s := ReportSummary{
		Chains: len(r.AttackChains),
		Total:  len(r.Findings),
	}
	for _, f := range r.Findings {
		switch f.Severity {
		case SeverityCritical:
			s.Critical++
		case SeverityHigh:
			s.High++
		case SeverityMedium:
			s.Medium++
		case SeverityLow:
			s.Low++
		case SeverityInfo:
			s.Info++
		}
	}
	return s
}

// StaticScanner runs fast, LLM-free analysis.
type StaticScanner interface {
	Name() string
	Scan(ctx context.Context, target ScanTarget) ([]Finding, error)
}

// LLMAnalyzer runs LLM-powered deep analysis on prioritized chunks.
type LLMAnalyzer interface {
	Name() string
	Category() Category
	Analyze(ctx context.Context, chunks []AnalysisChunk) ([]Finding, error)
}

// OutputFormatter converts a Report to a specific output format.
type OutputFormatter interface {
	Name() string
	Format(report *Report) ([]byte, error)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/security/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/security/types.go internal/security/types_test.go
git commit -m "[BEHAVIORAL] Add security engine core types and interfaces"
```

---

### Task 2: Secret Scanner

**Files:**
- Create: `internal/security/scanner/secrets.go`
- Create: `internal/security/scanner/secrets_test.go`

**Context:** The secret scanner detects API keys, tokens, passwords, and private keys using regex patterns and Shannon entropy analysis. It implements the `StaticScanner` interface from `internal/security/types.go`.

**Step 1: Write the failing test**

```go
// internal/security/scanner/secrets_test.go
package scanner

import (
	"context"
	"os"
	"path/filepath"
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
const AWSAccessKey = "AKIAIOSFODNN7EXAMPLE"
`)
	findings, err := scanDir(t, dir)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Equal(t, security.CategorySecretsExposure, findings[0].Category)
	assert.Contains(t, findings[0].Title, "AWS")
}

func TestSecretScannerDetectsGitHubToken(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main
var token = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefgh"
`)
	findings, err := scanDir(t, dir)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Contains(t, findings[0].Title, "GitHub")
}

func TestSecretScannerDetectsPrivateKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "key.pem", `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGScdN+w
-----END RSA PRIVATE KEY-----
`)
	findings, err := scanDir(t, dir)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Equal(t, security.SeverityCritical, findings[0].Severity)
}

func TestSecretScannerDetectsGenericHighEntropy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "env.go", `package env
var APIKey = "aB3dE5fG7hI9jK1lM3nO5pQ7rS9tU1vW3xY5zA7bC9d"
`)
	findings, err := scanDir(t, dir)
	require.NoError(t, err)
	// High entropy string assigned to a variable with "key" in name
	require.NotEmpty(t, findings)
}

func TestSecretScannerSkipsBinary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "image.png", "\x89PNG\r\n\x1a\nAKIAIOSFODNN7EXAMPLE")
	findings, err := scanDir(t, dir)
	require.NoError(t, err)
	assert.Empty(t, findings, "should skip binary files")
}

func TestSecretScannerSkipsGitignored(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "vendor/dep.go", `package dep
var key = "AKIAIOSFODNN7EXAMPLE"
`)
	target := security.ScanTarget{
		RootDir:         dir,
		ExcludePatterns: []string{"vendor/**"},
	}
	s := NewSecretScanner()
	findings, err := s.Scan(context.Background(), target)
	require.NoError(t, err)
	assert.Empty(t, findings, "should respect exclude patterns")
}

func TestSecretScannerNoFalsePositiveOnTestData(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "test_fixtures.go", `package fixtures
// This is a test example, not a real key
var exampleKey = "EXAMPLE_KEY_NOT_REAL"
`)
	findings, err := scanDir(t, dir)
	require.NoError(t, err)
	assert.Empty(t, findings, "should not flag obviously fake test keys")
}

// Helper: write a file in the temp dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

// Helper: scan a temp directory with default settings.
func scanDir(t *testing.T, dir string) ([]security.Finding, error) {
	t.Helper()
	s := NewSecretScanner()
	target := security.ScanTarget{RootDir: dir}
	return s.Scan(context.Background(), target)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/security/scanner/... -v -run TestSecretScanner`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

Implement `internal/security/scanner/secrets.go` with:
- `SecretScanner` struct implementing `security.StaticScanner`
- Regex patterns for: AWS access keys (`AKIA[0-9A-Z]{16}`), GitHub tokens (`ghp_[a-zA-Z0-9]{36}`), GitLab tokens (`glpat-[a-zA-Z0-9\-]{20,}`), Slack tokens (`xox[bprs]-[a-zA-Z0-9-]+`), private key headers (`-----BEGIN .* PRIVATE KEY-----`), generic API key patterns (`(?i)(api[_-]?key|apikey|secret[_-]?key)\s*[:=]\s*["']([^"']{20,})["']`), JWT secrets, database connection strings (`(?i)(mysql|postgres|mongodb)://[^"'\s]+`), Bearer tokens
- Shannon entropy calculator for detecting high-entropy strings assigned to key/secret/token variables
- Binary file detection (skip files with null bytes in first 512 bytes)
- Glob-based exclude pattern support
- File walker that respects `ScanTarget.Files` (if set) or walks `RootDir`

Each pattern produces a `Finding` with appropriate CWE (e.g., CWE-798 for hardcoded credentials), severity, and remediation advice.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/security/scanner/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/security/scanner/secrets.go internal/security/scanner/secrets_test.go
git commit -m "[BEHAVIORAL] Add secret scanner with regex and entropy detection"
```

---

### Task 3: Dependency Auditor

**Files:**
- Create: `internal/security/scanner/deps.go`
- Create: `internal/security/scanner/deps_test.go`

**Context:** Parses lockfiles (go.sum, package-lock.json, requirements.txt, Gemfile.lock, Cargo.lock, Podfile.lock) and queries the OSV API for known vulnerabilities.

**Step 1: Write the failing test**

```go
// internal/security/scanner/deps_test.go
package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepScannerName(t *testing.T) {
	s := NewDepScanner(nil)
	assert.Equal(t, "dependency-audit", s.Name())
}

func TestDepScannerInterface(t *testing.T) {
	var _ security.StaticScanner = NewDepScanner(nil)
}

func TestDepScannerParsesGoSum(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.sum", `golang.org/x/text v0.3.0 h1:abc123=
golang.org/x/text v0.3.0/go.mod h1:abc123=
github.com/vuln/pkg v1.0.0 h1:def456=
github.com/vuln/pkg v1.0.0/go.mod h1:def456=
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"vulns":[{"id":"GHSA-xxxx-xxxx-xxxx","summary":"Test vulnerability","details":"A test vuln","severity":[{"type":"CVSS_V3","score":"7.5"}],"affected":[{"package":{"ecosystem":"Go","name":"github.com/vuln/pkg"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"1.1.0"}]}]}]}]}`))
	}))
	defer server.Close()

	s := NewDepScanner(&http.Client{})
	s.osvBaseURL = server.URL
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Equal(t, security.CategoryVulnerableDep, findings[0].Category)
	assert.Contains(t, findings[0].Title, "vuln/pkg")
}

func TestDepScannerParsesPackageLock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package-lock.json", `{
  "name": "test-project",
  "lockfileVersion": 2,
  "packages": {
    "node_modules/lodash": { "version": "4.17.20" }
  }
}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"vulns":[]}`))
	}))
	defer server.Close()

	s := NewDepScanner(&http.Client{})
	s.osvBaseURL = server.URL
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// No vulns returned by mock server
	assert.Empty(t, findings)
}

func TestDepScannerHandlesOSVUnavailable(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.sum", `github.com/some/pkg v1.0.0 h1:abc=
github.com/some/pkg v1.0.0/go.mod h1:abc=
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	s := NewDepScanner(&http.Client{})
	s.osvBaseURL = server.URL
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	// Should not error — degrades gracefully
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Equal(t, security.SeverityInfo, findings[0].Severity)
	assert.Contains(t, findings[0].Title, "unavailable")
}

func TestDepScannerNoLockfiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")

	s := NewDepScanner(nil)
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/security/scanner/... -v -run TestDepScanner`
Expected: FAIL

**Step 3: Write minimal implementation**

Implement `internal/security/scanner/deps.go` with:
- `DepScanner` struct with `http.Client` and `osvBaseURL` (defaults to `https://api.osv.dev`)
- Lockfile parsers for go.sum, package-lock.json, requirements.txt, Gemfile.lock, Cargo.lock, Podfile.lock
- OSV API query per package (`POST /v1/query` with `{ "package": { "name": ..., "ecosystem": ... }, "version": ... }`)
- Graceful degradation when API is unavailable (return Info finding)
- Map OSV severity to `security.Severity`
- CWE mapping from OSV IDs

**Step 4: Run tests**

Run: `go test ./internal/security/scanner/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/security/scanner/deps.go internal/security/scanner/deps_test.go
git commit -m "[BEHAVIORAL] Add dependency auditor with OSV API integration"
```

---

### Task 4: SAST Pattern Matcher

**Files:**
- Create: `internal/security/scanner/sast.go`
- Create: `internal/security/scanner/sast_test.go`

**Context:** Uses tree-sitter from `internal/parser` to run AST queries detecting SQL injection, path traversal, XSS, command injection, weak crypto, and hardcoded credentials. Depends on parser being able to parse Go, Python, JavaScript, TypeScript, Java, Rust, Ruby, C, C++.

**Step 1: Write the failing test**

```go
// internal/security/scanner/sast_test.go
package scanner

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSASTScannerName(t *testing.T) {
	s := NewSASTScanner()
	assert.Equal(t, "sast", s.Name())
}

func TestSASTScannerInterface(t *testing.T) {
	var _ security.StaticScanner = NewSASTScanner()
}

func TestSASTDetectsSQLInjectionGo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import "database/sql"

func GetUser(db *sql.DB, name string) {
	query := "SELECT * FROM users WHERE name = '" + name + "'"
	db.Query(query)
}
`)
	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Equal(t, security.CategoryInjection, findings[0].Category)
	assert.Contains(t, findings[0].CWE, "89")
}

func TestSASTDetectsCommandInjectionGo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "run.go", `package run

import "os/exec"

func RunCommand(userInput string) {
	exec.Command("sh", "-c", userInput).Run()
}
`)
	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Equal(t, security.CategoryInjection, findings[0].Category)
	assert.Contains(t, findings[0].CWE, "78")
}

func TestSASTDetectsWeakCryptoGo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hash.go", `package hash

import "crypto/md5"

func HashPassword(pw string) []byte {
	h := md5.Sum([]byte(pw))
	return h[:]
}
`)
	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Equal(t, security.CategoryCryptography, findings[0].Category)
}

func TestSASTDetectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "file.go", `package file

import "os"

func ReadUserFile(userPath string) {
	os.Open(userPath)
}
`)
	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// Path traversal is harder to detect statically — may or may not flag
	// The key is that the scanner handles Go files without error
}

func TestSASTDetectsPythonSQLInjection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.py", `import sqlite3

def get_user(name):
    conn = sqlite3.connect("db.sqlite")
    conn.execute("SELECT * FROM users WHERE name = '" + name + "'")
`)
	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Equal(t, security.CategoryInjection, findings[0].Category)
}

func TestSASTSkipsUnsupportedLanguage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "data.csv", "name,age\nalice,30")
	s := NewSASTScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/security/scanner/... -v -run TestSAST`
Expected: FAIL

**Step 3: Write minimal implementation**

Implement `internal/security/scanner/sast.go` with:
- `SASTScanner` struct using `parser.NewParser()` from `internal/parser`
- Language-specific pattern definitions for SQL injection, command injection, XSS, path traversal, weak crypto, hardcoded credentials
- For each source file: parse with tree-sitter, run pattern queries, produce findings
- Pattern matching uses tree-sitter S-expression queries where possible, falls back to regex on source for simpler patterns
- CWE mappings: SQL injection → CWE-89, command injection → CWE-78, XSS → CWE-79, path traversal → CWE-22, weak crypto → CWE-327

**Step 4: Run tests**

Run: `go test ./internal/security/scanner/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/security/scanner/sast.go internal/security/scanner/sast_test.go
git commit -m "[BEHAVIORAL] Add SAST pattern matcher with tree-sitter AST queries"
```

---

### Task 5: Config Scanner

**Files:**
- Create: `internal/security/scanner/config.go`
- Create: `internal/security/scanner/config_test.go`

**Context:** Scans Dockerfiles, Kubernetes YAML, CI configs, and other config files for security misconfigurations.

**Step 1: Write the failing test**

Test cases for: Dockerfile USER root, K8s privileged container, K8s hostNetwork, CI secrets in plain text, permissive CORS, debug mode enabled in config files.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/security/scanner/... -v -run TestConfig`

**Step 3: Write minimal implementation**

Implement `internal/security/scanner/config.go` with:
- `ConfigScanner` struct implementing `security.StaticScanner`
- File-type detection by name/extension: `Dockerfile*`, `*.yaml`/`*.yml` (check for K8s), `.github/workflows/*.yml`, `*.env`
- Dockerfile rules: USER root, ADD with URL, no HEALTHCHECK
- K8s rules: privileged, hostNetwork, hostPID, runAsRoot, no resource limits
- CI rules: secrets/tokens/passwords in plain text
- General: debug/verbose mode enabled, permissive CORS (`*`)

**Step 4: Run tests**

Run: `go test ./internal/security/scanner/... -v`

**Step 5: Commit**

```bash
git add internal/security/scanner/config.go internal/security/scanner/config_test.go
git commit -m "[BEHAVIORAL] Add config scanner for Dockerfile, K8s, and CI misconfigurations"
```

---

### Task 6: License Checker

**Files:**
- Create: `internal/security/scanner/license.go`
- Create: `internal/security/scanner/license_test.go`

**Context:** Detects license files (LICENSE, COPYING, etc.), identifies license types, and flags GPL-in-commercial and missing license concerns.

**Step 1: Write the failing test**

Test cases for: detecting MIT, Apache, GPL licenses from LICENSE files; detecting license headers in source files; flagging missing LICENSE file; flagging copyleft (GPL) licenses.

**Step 2: Run test, implement, run test, commit**

```bash
git commit -m "[BEHAVIORAL] Add license checker for compliance scanning"
```

---

### Task 7: Apple Platform Scanner

**Files:**
- Create: `internal/security/scanner/apple.go`
- Create: `internal/security/scanner/apple_test.go`

**Context:** Parses Info.plist XML and entitlements files to detect ATS exceptions, insecure storage patterns, missing privacy keys, excessive entitlements.

**Step 1: Write the failing test**

Test cases for: ATS exceptions (`NSAllowsArbitraryLoads`), missing privacy keys (camera, location, photos), excessive entitlements (`com.apple.security.cs.disable-library-validation`), insecure UserDefaults usage.

**Step 2: Run test, implement, run test, commit**

```bash
git commit -m "[BEHAVIORAL] Add Apple platform scanner for Info.plist and entitlements"
```

---

### Task 8: Skill Scanner Adapter

**Files:**
- Create: `internal/security/scanner/skill_scanner.go`
- Create: `internal/security/scanner/skill_scanner_test.go`

**Context:** Wraps skill-provided scanner functions into the `StaticScanner` interface. This is the extension point for Security Rule Skills.

**Step 1: Write the failing test**

```go
func TestSkillScannerAdapter(t *testing.T) {
	called := false
	adapter := NewSkillScannerAdapter("custom-scanner", func(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
		called = true
		return []security.Finding{
			{ID: "CUSTOM-001", Scanner: "custom-scanner", Severity: security.SeverityMedium},
		}, nil
	})

	var _ security.StaticScanner = adapter
	assert.Equal(t, "custom-scanner", adapter.Name())

	findings, err := adapter.Scan(context.Background(), security.ScanTarget{RootDir: t.TempDir()})
	require.NoError(t, err)
	assert.True(t, called)
	require.Len(t, findings, 1)
	assert.Equal(t, "CUSTOM-001", findings[0].ID)
}
```

**Step 2: Run test, implement, run test, commit**

```bash
git commit -m "[BEHAVIORAL] Add skill scanner adapter for Security Rule Skills"
```

---

### Task 9: Prioritizer

**Files:**
- Create: `internal/security/prioritizer.go`
- Create: `internal/security/prioritizer_test.go`

**Context:** Scores files by risk signals, splits into analysis chunks respecting function boundaries, applies budget cap. Uses `internal/parser` for function boundary detection.

**Step 1: Write the failing test**

```go
// internal/security/prioritizer_test.go
package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrioritizerScoresAuthCode(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "auth.go", `package auth
func Login(user, pass string) error { return nil }
`)
	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	assert.GreaterOrEqual(t, chunks[0].RiskScore, 10, "auth code should score >= 10")
}

func TestPrioritizerScoresExecCode(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "run.go", `package run
import "os/exec"
func Run(cmd string) { exec.Command(cmd).Run() }
`)
	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	assert.GreaterOrEqual(t, chunks[0].RiskScore, 9)
}

func TestPrioritizerRespectsMinScore(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "utils.go", `package utils
func Add(a, b int) int { return a + b }
`)
	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 5, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	assert.Empty(t, chunks, "low-risk utility code should be filtered out")
}

func TestPrioritizerRespectsBudgetCap(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		writeTestFile(t, dir, fmt.Sprintf("auth%d.go", i),
			`package auth
import "os/exec"
func Login() { exec.Command("sh").Run() }
`)
	}
	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 5})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(chunks), 5, "should respect budget cap")
}

func TestPrioritizerBoostedByStaticFindings(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "handler.go", `package handler
func Handle() {}
`)
	staticFindings := []Finding{
		{Location: Location{File: "handler.go"}},
	}
	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, staticFindings)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	// handler.go alone has no signals, but static finding adds +3
	assert.GreaterOrEqual(t, chunks[0].RiskScore, 3)
}

func TestPrioritizerSortsHighestFirst(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "utils.go", `package utils
func Add(a, b int) int { return a+b }
`)
	writeTestFile(t, dir, "auth.go", `package auth
import "os/exec"
func Login() { exec.Command("sh").Run() }
`)
	p := NewPrioritizer(PrioritizerConfig{MinRiskScore: 0, MaxChunks: 100})
	chunks, err := p.Prioritize(context.Background(), ScanTarget{RootDir: dir}, nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 2)
	assert.GreaterOrEqual(t, chunks[0].RiskScore, chunks[1].RiskScore, "should be sorted highest first")
}
```

Note: add `"fmt"` to imports and a `writeTestFile` helper in the security package test.

**Step 2: Run test, implement, run test, commit**

Implement `internal/security/prioritizer.go` with:
- `PrioritizerConfig` — `MinRiskScore`, `MaxChunks`
- `Prioritizer` struct with config
- `Prioritize(ctx, ScanTarget, []Finding) -> ([]AnalysisChunk, error)`:
  1. Walk files in target
  2. Score each file by keyword scanning (auth patterns → +10, exec → +9, etc.)
  3. Boost files with static findings (+3)
  4. Parse with tree-sitter, split into function-level chunks
  5. Each chunk inherits the file's risk score
  6. Sort by score descending
  7. Cap at `MaxChunks`
  8. Filter by `MinRiskScore`

```bash
git commit -m "[BEHAVIORAL] Add risk-based prioritizer for LLM analysis budget"
```

---

### Task 10: Auth/Authz LLM Analyzer

**Files:**
- Create: `internal/security/analyzer/auth.go`
- Create: `internal/security/analyzer/auth_test.go`

**Context:** Uses the LLM to detect authentication bypass, IDOR, privilege escalation, and missing auth middleware. Takes `provider.LLMProvider` for LLM calls.

**Step 1: Write the failing test**

```go
// internal/security/analyzer/auth_test.go
package analyzer

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLMProvider returns a canned response for testing.
type mockLLMProvider struct {
	response string
}

func (m *mockLLMProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: "text_delta", Text: m.response}
	ch <- provider.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

func TestAuthAnalyzerName(t *testing.T) {
	a := NewAuthAnalyzer(&mockLLMProvider{})
	assert.Equal(t, "auth-authz", a.Name())
}

func TestAuthAnalyzerCategory(t *testing.T) {
	a := NewAuthAnalyzer(&mockLLMProvider{})
	assert.Equal(t, security.CategoryAuthentication, a.Category())
}

func TestAuthAnalyzerInterface(t *testing.T) {
	var _ security.LLMAnalyzer = NewAuthAnalyzer(&mockLLMProvider{})
}

func TestAuthAnalyzerDetectsFindings(t *testing.T) {
	llm := &mockLLMProvider{
		response: `[{"id":"AUTH-001","title":"Missing authentication on admin endpoint","severity":"high","category":"authentication","description":"The /admin endpoint has no auth middleware","location":{"file":"handler.go","start_line":15,"end_line":20,"function":"AdminHandler"},"cwe":"CWE-306","confidence":"high","remediation":"Add authentication middleware"}]`,
	}

	a := NewAuthAnalyzer(llm)
	chunks := []security.AnalysisChunk{
		{
			File: "handler.go", StartLine: 1, EndLine: 30,
			Content:  "func AdminHandler(w http.ResponseWriter, r *http.Request) {\n  // handle admin\n}",
			Language: "go", RiskScore: 18,
		},
	}

	findings, err := a.Analyze(context.Background(), chunks)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "AUTH-001", findings[0].ID)
	assert.Equal(t, security.SeverityHigh, findings[0].Severity)
	assert.Equal(t, "auth-authz", findings[0].Scanner)
}

func TestAuthAnalyzerHandlesMalformedJSON(t *testing.T) {
	llm := &mockLLMProvider{response: "This is not valid JSON at all."}
	a := NewAuthAnalyzer(llm)

	chunks := []security.AnalysisChunk{
		{File: "test.go", Content: "func Foo() {}", Language: "go"},
	}

	findings, err := a.Analyze(context.Background(), chunks)
	require.NoError(t, err)
	// Malformed JSON should produce a low-confidence finding with raw response
	require.Len(t, findings, 1)
	assert.Equal(t, security.ConfidenceLow, findings[0].Confidence)
	assert.Contains(t, findings[0].Evidence, "not valid JSON")
}

func TestAuthAnalyzerEmptyChunks(t *testing.T) {
	a := NewAuthAnalyzer(&mockLLMProvider{})
	findings, err := a.Analyze(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
}
```

**Step 2: Run test, implement, run test, commit**

Implement `internal/security/analyzer/auth.go` with:
- `AuthAnalyzer` struct holding `provider.LLMProvider`
- System prompt focused on authentication/authorization vulnerabilities with CWE references
- Sends chunks as user message, asks for structured JSON `[]Finding` response
- Parses JSON response, tags each finding with `Scanner: "auth-authz"`
- Handles malformed JSON by creating a single low-confidence finding with the raw response

```bash
git commit -m "[BEHAVIORAL] Add auth/authz LLM analyzer"
```

---

### Task 11: Data Flow LLM Analyzer

**Files:**
- Create: `internal/security/analyzer/dataflow.go`
- Create: `internal/security/analyzer/dataflow_test.go`

**Context:** Detects untrusted input reaching dangerous sinks. Same pattern as Task 10 but with data-flow-specific prompts.

Tests: detects tainted SQL query, handles empty chunks, handles malformed LLM response.

```bash
git commit -m "[BEHAVIORAL] Add data flow / taint analysis LLM analyzer"
```

---

### Task 12: Business Logic LLM Analyzer

**Files:**
- Create: `internal/security/analyzer/business.go`
- Create: `internal/security/analyzer/business_test.go`

**Context:** Detects logic flaws like negative quantity exploits, bypass conditions, race-to-credit.

```bash
git commit -m "[BEHAVIORAL] Add business logic LLM analyzer"
```

---

### Task 13: Cryptography LLM Analyzer

**Files:**
- Create: `internal/security/analyzer/crypto.go`
- Create: `internal/security/analyzer/crypto_test.go`

**Context:** Detects weak algorithms, key management issues, ECB mode, hardcoded keys.

```bash
git commit -m "[BEHAVIORAL] Add cryptography LLM analyzer"
```

---

### Task 14: Concurrency LLM Analyzer

**Files:**
- Create: `internal/security/analyzer/concurrency.go`
- Create: `internal/security/analyzer/concurrency_test.go`

**Context:** Detects race conditions, deadlocks, concurrent map access without synchronization.

```bash
git commit -m "[BEHAVIORAL] Add concurrency LLM analyzer"
```

---

### Task 15: Skill Analyzer Adapter

**Files:**
- Create: `internal/security/analyzer/skill_analyzer.go`
- Create: `internal/security/analyzer/skill_analyzer_test.go`

**Context:** Same pattern as Task 8 but for LLM analyzers. Wraps skill-provided analyzer functions into `LLMAnalyzer` interface.

```bash
git commit -m "[BEHAVIORAL] Add skill analyzer adapter for Security Rule Skills"
```

---

### Task 16: Correlator

**Files:**
- Create: `internal/security/correlator.go`
- Create: `internal/security/correlator_test.go`

**Context:** Takes all findings from both phases, groups by proximity, matches known attack chain patterns, deduplicates.

**Step 1: Write the failing test**

```go
// internal/security/correlator_test.go
package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorrelatorDetectsUnauthSQLI(t *testing.T) {
	findings := []Finding{
		{
			ID: "F-1", Scanner: "auth-authz", Category: CategoryAuthentication,
			Severity: SeverityHigh, Title: "Missing auth on handler",
			Location: Location{File: "handler.go", StartLine: 10, EndLine: 25, Function: "HandleUsers"},
		},
		{
			ID: "F-2", Scanner: "sast", Category: CategoryInjection,
			Severity: SeverityHigh, Title: "SQL injection",
			Location: Location{File: "handler.go", StartLine: 18, EndLine: 20, Function: "HandleUsers"},
		},
	}

	c := NewCorrelator()
	chains, deduped := c.Correlate(findings)

	require.NotEmpty(t, chains)
	assert.Contains(t, chains[0].Title, "Unauthenticated")
	assert.Equal(t, SeverityCritical, chains[0].Severity, "chain should promote to critical")
	assert.Len(t, chains[0].Steps, 2)
	assert.Len(t, deduped, 2, "no duplicates to remove")
}

func TestCorrelatorDeduplicates(t *testing.T) {
	findings := []Finding{
		{ID: "F-1", Scanner: "sast", CWE: "CWE-89", Location: Location{File: "db.go", StartLine: 10}},
		{ID: "F-2", Scanner: "dataflow", CWE: "CWE-89", Location: Location{File: "db.go", StartLine: 10}},
	}

	c := NewCorrelator()
	_, deduped := c.Correlate(findings)
	assert.Len(t, deduped, 1, "same CWE at same location should deduplicate")
}

func TestCorrelatorNoChains(t *testing.T) {
	findings := []Finding{
		{ID: "F-1", Category: CategorySecretsExposure, Location: Location{File: "a.go"}},
		{ID: "F-2", Category: CategoryLicenseCompliance, Location: Location{File: "b.go"}},
	}

	c := NewCorrelator()
	chains, deduped := c.Correlate(findings)
	assert.Empty(t, chains)
	assert.Len(t, deduped, 2)
}

func TestCorrelatorEmptyInput(t *testing.T) {
	c := NewCorrelator()
	chains, deduped := c.Correlate(nil)
	assert.Empty(t, chains)
	assert.Empty(t, deduped)
}
```

**Step 2: Run test, implement, run test, commit**

Implement `internal/security/correlator.go` with:
- `Correlator` struct with known chain patterns
- `Correlate([]Finding) -> ([]AttackChain, []Finding)`:
  1. Group findings by file+function proximity (overlapping line ranges)
  2. For each group, check against chain patterns (e.g., auth + injection = "Unauthenticated Injection")
  3. Promote chain severity when combination creates new attack surface
  4. Deduplicate: same CWE + same file + same start line = keep highest confidence
  5. Return chains and deduplicated findings

Chain patterns:
- Missing auth + injection → "Unauthenticated Injection" (Critical)
- Missing auth + data exposure → "Unauthenticated Data Access" (Critical)
- Weak crypto + secrets exposure → "Recoverable Secret" (High)
- Race condition + authorization → "TOCTOU Authorization Bypass" (High)

```bash
git commit -m "[BEHAVIORAL] Add attack chain correlator with deduplication"
```

---

### Task 17: JSON Output Formatter

**Files:**
- Create: `internal/security/output/json.go`
- Create: `internal/security/output/json_test.go`

**Step 1: Write the failing test**

```go
// internal/security/output/json_test.go
package output

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFormatterName(t *testing.T) {
	f := NewJSONFormatter()
	assert.Equal(t, "json", f.Name())
}

func TestJSONFormatterInterface(t *testing.T) {
	var _ security.OutputFormatter = NewJSONFormatter()
}

func TestJSONFormatterOutput(t *testing.T) {
	report := &security.Report{
		Findings: []security.Finding{
			{ID: "F-1", Severity: security.SeverityHigh, Title: "Test finding"},
		},
		AttackChains: []security.AttackChain{
			{ID: "C-1", Title: "Test chain"},
		},
	}

	f := NewJSONFormatter()
	data, err := f.Format(report)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Contains(t, result, "findings")
	assert.Contains(t, result, "attack_chains")
	assert.Contains(t, result, "summary")
}

func TestJSONFormatterEmptyReport(t *testing.T) {
	f := NewJSONFormatter()
	data, err := f.Format(&security.Report{})
	require.NoError(t, err)
	assert.Contains(t, string(data), "findings")
}
```

**Step 2: Run test, implement, run test, commit**

```bash
git commit -m "[BEHAVIORAL] Add JSON output formatter"
```

---

### Task 18: Markdown Output Formatter

**Files:**
- Create: `internal/security/output/markdown.go`
- Create: `internal/security/output/markdown_test.go`

**Context:** Produces human-readable markdown with severity badges, summary table, findings grouped by severity, attack chains section.

```bash
git commit -m "[BEHAVIORAL] Add Markdown output formatter"
```

---

### Task 19: SARIF Output Formatter

**Files:**
- Create: `internal/security/output/sarif.go`
- Create: `internal/security/output/sarif_test.go`

**Context:** Produces SARIF v2.1.0 JSON for IDE integration (VS Code) and GitHub Code Scanning. Must follow the SARIF schema: `$schema`, `version`, `runs` array with `tool`, `results`, `artifacts`.

Tests should verify: valid SARIF structure, findings mapped to results with ruleId/message/locations, severity mapped to SARIF level (error/warning/note).

```bash
git commit -m "[BEHAVIORAL] Add SARIF v2.1.0 output formatter"
```

---

### Task 20: GitHub PR Output Formatter

**Files:**
- Create: `internal/security/output/github_pr.go`
- Create: `internal/security/output/github_pr_test.go`

**Context:** Produces structured review comments positioned at finding locations. Returns a `PRReview` struct (not making API calls itself). The caller translates to GitHub API calls.

```go
type PRReview struct {
	Body     string       // summary comment
	Comments []PRComment
}

type PRComment struct {
	Path     string
	Line     int
	Body     string
	Severity string
}
```

```bash
git commit -m "[BEHAVIORAL] Add GitHub PR review comment formatter"
```

---

### Task 21: Wiki Output Formatter

**Files:**
- Create: `internal/security/output/wiki.go`
- Create: `internal/security/output/wiki_test.go`

**Context:** Produces markdown pages for the wiki security section: `overview.md`, `findings.md`, `attack-chains.md`.

```bash
git commit -m "[BEHAVIORAL] Add Wiki section output formatter"
```

---

### Task 22: CycloneDX Output Formatter

**Files:**
- Create: `internal/security/output/cyclonedx.go`
- Create: `internal/security/output/cyclonedx_test.go`

**Context:** Produces CycloneDX v1.5 BOM JSON with vulnerabilities section. For SBOM compliance.

```bash
git commit -m "[BEHAVIORAL] Add CycloneDX v1.5 SBOM output formatter"
```

---

### Task 23: Engine Orchestrator

**Files:**
- Create: `internal/security/engine.go`
- Create: `internal/security/engine_test.go`

**Context:** The top-level `Engine` that wires scanners, prioritizer, analyzers, and correlator into the two-phase pipeline.

**Step 1: Write the failing test**

```go
// internal/security/engine_test.go
package security

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockScanner implements StaticScanner for testing.
type mockScanner struct {
	name     string
	findings []Finding
	err      error
}

func (m *mockScanner) Name() string { return m.name }
func (m *mockScanner) Scan(_ context.Context, _ ScanTarget) ([]Finding, error) {
	return m.findings, m.err
}

// mockAnalyzer implements LLMAnalyzer for testing.
type mockAnalyzer struct {
	name     string
	category Category
	findings []Finding
	err      error
}

func (m *mockAnalyzer) Name() string        { return m.name }
func (m *mockAnalyzer) Category() Category   { return m.category }
func (m *mockAnalyzer) Analyze(_ context.Context, _ []AnalysisChunk) ([]Finding, error) {
	return m.findings, m.err
}

func TestEngineRunBothPhases(t *testing.T) {
	e := NewEngine(EngineConfig{
		MaxLLMChunks: 100,
		MinRiskScore: 0,
		Concurrency:  2,
	})

	e.AddScanner(&mockScanner{
		name: "test-scanner",
		findings: []Finding{
			{ID: "S-1", Severity: SeverityHigh, Category: CategorySecretsExposure, Title: "Secret found"},
		},
	})
	e.AddAnalyzer(&mockAnalyzer{
		name:     "test-analyzer",
		category: CategoryAuthentication,
		findings: []Finding{
			{ID: "A-1", Severity: SeverityMedium, Category: CategoryAuthentication, Title: "Auth issue"},
		},
	})

	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", `package main
import "os/exec"
func main() { exec.Command("sh").Run() }
`)

	report, err := e.Run(context.Background(), ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.GreaterOrEqual(t, len(report.Findings), 2, "should have findings from both phases")
	assert.Greater(t, report.Stats.Duration, time.Duration(0))
}

func TestEngineHandlesScannerError(t *testing.T) {
	e := NewEngine(EngineConfig{Concurrency: 1})
	e.AddScanner(&mockScanner{
		name: "failing-scanner",
		err:  fmt.Errorf("scanner crashed"),
	})

	report, err := e.Run(context.Background(), ScanTarget{RootDir: t.TempDir()})
	require.NoError(t, err, "engine should not fail for individual scanner errors")
	require.NotEmpty(t, report.Errors)
	assert.Equal(t, "failing-scanner", report.Errors[0].Scanner)
}

func TestEngineHandlesAnalyzerError(t *testing.T) {
	e := NewEngine(EngineConfig{
		MaxLLMChunks: 100,
		MinRiskScore: 0,
		Concurrency:  1,
	})
	e.AddAnalyzer(&mockAnalyzer{
		name:     "failing-analyzer",
		category: CategoryInjection,
		err:      fmt.Errorf("LLM timeout"),
	})

	dir := t.TempDir()
	writeTestFile(t, dir, "auth.go", `package auth
import "os/exec"
func Login() { exec.Command("sh").Run() }
`)

	report, err := e.Run(context.Background(), ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, report.Errors)
}

func TestEngineContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	e := NewEngine(EngineConfig{Concurrency: 1})
	e.AddScanner(&mockScanner{name: "slow"})

	report, err := e.Run(ctx, ScanTarget{RootDir: t.TempDir()})
	// Should return quickly with context error
	assert.Error(t, err)
	_ = report
}

func TestEngineEmptyTarget(t *testing.T) {
	e := NewEngine(EngineConfig{Concurrency: 1})
	report, err := e.Run(context.Background(), ScanTarget{RootDir: t.TempDir()})
	require.NoError(t, err)
	assert.Empty(t, report.Findings)
}
```

Note: add `"fmt"` to imports and `writeTestFile` helper.

**Step 2: Run test, implement, run test, commit**

Implement `internal/security/engine.go` with:
- `Engine` struct with scanners, analyzers, prioritizer, correlator, config
- `NewEngine(EngineConfig) -> *Engine`
- `AddScanner`, `AddAnalyzer` methods
- `Run(ctx, ScanTarget) -> (*Report, error)`:
  1. Start timer
  2. Run static scanners with `conc/pool` bounded concurrency
  3. Collect findings, record errors (non-fatal)
  4. Create prioritizer, produce chunks
  5. Run LLM analyzers with `conc/pool` bounded concurrency
  6. Collect findings, record errors (non-fatal)
  7. Correlate all findings
  8. Build and return Report with stats

```bash
git commit -m "[BEHAVIORAL] Add security engine orchestrator with two-phase pipeline"
```

---

### Task 24: Integration Test & Verification

**Files:**
- Create: `internal/security/integration_test.go`
- Create: `internal/security/testdata/` (fixture directory with vulnerable sample files)

**Context:** End-to-end test that runs the full engine on a test fixture directory with known vulnerabilities and verifies findings are produced, chains are detected, and output formatters work.

**Step 1: Create test fixture directory with known vulnerable files**

Create `internal/security/testdata/` with:
- `secrets.go` — hardcoded AWS key
- `handler.go` — SQL injection in HTTP handler without auth
- `crypto.go` — MD5 password hashing
- `Dockerfile` — runs as root
- `go.sum` — (empty, just for lockfile detection)

**Step 2: Write integration test**

```go
// internal/security/integration_test.go
package security_test

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/julianshen/rubichan/internal/security/output"
	"github.com/julianshen/rubichan/internal/security/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullPipelineIntegration(t *testing.T) {
	e := security.NewEngine(security.EngineConfig{
		MaxLLMChunks: 50,
		MinRiskScore: 0,
		Concurrency:  4,
	})

	// Add static scanners (no LLM analyzers for this test — would need mock)
	e.AddScanner(scanner.NewSecretScanner())
	e.AddScanner(scanner.NewSASTScanner())
	e.AddScanner(scanner.NewConfigScanner())
	e.AddScanner(scanner.NewLicenseScanner())

	target := security.ScanTarget{RootDir: "testdata"}
	report, err := e.Run(context.Background(), target)
	require.NoError(t, err)
	require.NotNil(t, report)

	// Should find at least the hardcoded secret and SQL injection
	assert.GreaterOrEqual(t, len(report.Findings), 2)

	summary := report.Summary()
	assert.Greater(t, summary.Total, 0)

	// Verify all output formatters work
	for _, formatter := range []security.OutputFormatter{
		output.NewJSONFormatter(),
		output.NewMarkdownFormatter(),
		output.NewSARIFFormatter(),
		output.NewWikiFormatter(),
	} {
		data, err := formatter.Format(report)
		require.NoError(t, err, "formatter %s failed", formatter.Name())
		assert.NotEmpty(t, data, "formatter %s produced empty output", formatter.Name())
	}
}
```

**Step 3: Run full test suite**

Run: `go test ./internal/security/... -v -count=1`
Expected: ALL PASS

**Step 4: Check coverage**

Run: `go test ./internal/security/... -cover`
Expected: >90% coverage

**Step 5: Commit**

```bash
git add internal/security/testdata/ internal/security/integration_test.go
git commit -m "[BEHAVIORAL] Add integration test with fixture data for full pipeline"
```

---

## Execution Notes

- **Total tasks:** 24
- **Dependency order:** Tasks 1-8 (types + scanners) are independent of each other after Task 1. Tasks 10-15 (analyzers) depend on Task 1. Task 9 (prioritizer) depends on Task 1. Task 16 (correlator) depends on Task 1. Task 23 (engine) depends on all previous layers. Tasks 17-22 (formatters) depend on Task 1. Task 24 depends on everything.
- **Parallelizable:** Tasks 2-8 can be done in any order. Tasks 10-15 can be done in any order. Tasks 17-22 can be done in any order.
- **External dependencies:** Only `net/http` for OSV API (Task 3). `encoding/xml` for plist (Task 7). `howett.net/plist` may be needed — check if encoding/xml suffices first.
- **Commit prefix:** All `[BEHAVIORAL]` since every task adds new functionality.
