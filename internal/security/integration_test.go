package security_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/julianshen/rubichan/internal/security/output"
	"github.com/julianshen/rubichan/internal/security/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFixture materializes one corpus file under dir.
func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// fixtureTree writes the scan corpus at runtime, replacing a checked-in
// testdata directory. The fixture credentials are assembled from split
// literals so the repository never contains a contiguous credential-shaped
// string: GitHub secret scanning (and tools like gitleaks) flag such strings
// even in clearly fake fixtures. The scanners still see the joined patterns
// in the files written here.
func fixtureTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, "Dockerfile", `FROM golang:1.22-alpine
WORKDIR /app
COPY . .
RUN go build -o server .
USER root
CMD ["/app/server"]
`)
	writeFixture(t, dir, "crypto.go", `package testdata

import (
	"crypto/md5"
	"fmt"
)

// HashPassword is a TEST FIXTURE using weak cryptography (MD5).
func HashPassword(password string) string {
	h := md5.Sum([]byte(password))
	return fmt.Sprintf("%x", h)
}
`)
	writeFixture(t, dir, "handler.go", `package testdata

import "database/sql"

// GetUser is a TEST FIXTURE handler with SQL injection vulnerability.
func GetUser(db *sql.DB, id string) (*sql.Row, error) {
	row := db.QueryRow("SELECT * FROM users WHERE id=" + id)
	return row, nil
}
`)
	writeFixture(t, dir, "go.sum", "")
	writeFixture(t, dir, "secrets.go", `package testdata

// This file is a TEST FIXTURE for the secret scanner.
// It contains FAKE credentials that match detection patterns.

const awsAccessKey = "`+"AKIA"+"IOSFODNN7"+"REALKEY1"+`"

const dbConnection = "postgres://admin:`+"s3cret"+"Pass"+`@db.internal:5432/production"
`)
	return dir
}

func TestFullPipelineIntegration(t *testing.T) {
	e := security.NewEngine(security.EngineConfig{
		MaxLLMChunks: 50,
		MinRiskScore: 0,
		Concurrency:  4,
	})

	// Add static scanners (no LLM analyzers -- those need a real provider).
	e.AddScanner(scanner.NewSecretScanner())
	e.AddScanner(scanner.NewSASTScanner())
	e.AddScanner(scanner.NewConfigScanner())
	e.AddScanner(scanner.NewLicenseScanner())

	target := security.ScanTarget{RootDir: fixtureTree(t)}
	report, err := e.Run(context.Background(), target)
	require.NoError(t, err)
	require.NotNil(t, report)

	// Expect at least:
	// - secrets: AWS key + DB connection string in secrets.go
	// - sast: SQL injection in handler.go + weak crypto (md5) in crypto.go
	// - config: USER root in Dockerfile
	// - license: missing LICENSE file
	assert.GreaterOrEqual(t, len(report.Findings), 2,
		"should find at least the hardcoded secret and weak crypto, got %d findings", len(report.Findings))

	summary := report.Summary()
	assert.Greater(t, summary.Total, 0, "summary total should be positive")

	// Verify specific categories are represented in findings.
	categories := make(map[security.Category]bool)
	for _, f := range report.Findings {
		categories[f.Category] = true
	}
	assert.True(t, categories[security.CategorySecretsExposure],
		"should detect secrets (AWS key / DB conn string)")
	assert.True(t, categories[security.CategoryCryptography],
		"should detect weak crypto (md5 import)")

	// Verify all output formatters work on the report.
	formatters := []security.OutputFormatter{
		output.NewJSONFormatter(),
		output.NewMarkdownFormatter(),
		output.NewSARIFFormatter(),
		output.NewWikiFormatter(),
		output.NewGitHubPRFormatter(),
		output.NewCycloneDXFormatter(),
	}
	for _, formatter := range formatters {
		data, err := formatter.Format(report)
		require.NoError(t, err, "formatter %s failed", formatter.Name())
		assert.NotEmpty(t, data, "formatter %s produced empty output", formatter.Name())
	}
}
