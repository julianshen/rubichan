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

	// Add static scanners (no LLM analyzers -- those need a real provider).
	e.AddScanner(scanner.NewSecretScanner())
	e.AddScanner(scanner.NewSASTScanner())
	e.AddScanner(scanner.NewConfigScanner())
	e.AddScanner(scanner.NewLicenseScanner())

	target := security.ScanTarget{RootDir: "testdata"}
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
