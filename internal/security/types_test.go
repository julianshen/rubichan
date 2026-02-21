package security

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityCritical, "critical"},
		{SeverityHigh, "high"},
		{SeverityMedium, "medium"},
		{SeverityLow, "low"},
		{SeverityInfo, "info"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.severity))
		})
	}
}

func TestSeverityCompare(t *testing.T) {
	// Verify strict ordering: Critical > High > Medium > Low > Info.
	assert.Greater(t, SeverityRank(SeverityCritical), SeverityRank(SeverityHigh))
	assert.Greater(t, SeverityRank(SeverityHigh), SeverityRank(SeverityMedium))
	assert.Greater(t, SeverityRank(SeverityMedium), SeverityRank(SeverityLow))
	assert.Greater(t, SeverityRank(SeverityLow), SeverityRank(SeverityInfo))

	// Verify exact rank values.
	assert.Equal(t, 5, SeverityRank(SeverityCritical))
	assert.Equal(t, 4, SeverityRank(SeverityHigh))
	assert.Equal(t, 3, SeverityRank(SeverityMedium))
	assert.Equal(t, 2, SeverityRank(SeverityLow))
	assert.Equal(t, 1, SeverityRank(SeverityInfo))

	// Unknown severity returns 0.
	assert.Equal(t, 0, SeverityRank(Severity("unknown")))
}

func TestConfidenceString(t *testing.T) {
	tests := []struct {
		confidence Confidence
		expected   string
	}{
		{ConfidenceHigh, "high"},
		{ConfidenceMedium, "medium"},
		{ConfidenceLow, "low"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.confidence))
		})
	}
}

func TestCategoryValues(t *testing.T) {
	categories := AllCategories()

	// Must contain exactly 13 categories.
	require.Len(t, categories, 13)

	// Verify all expected categories are present.
	expected := []Category{
		CategoryInjection,
		CategoryAuthentication,
		CategoryAuthorization,
		CategoryCryptography,
		CategorySecretsExposure,
		CategoryVulnerableDep,
		CategoryMisconfiguration,
		CategoryDataExposure,
		CategoryRaceCondition,
		CategoryInputValidation,
		CategoryLoggingMonitoring,
		CategorySupplyChain,
		CategoryLicenseCompliance,
	}
	assert.Equal(t, expected, categories)

	// Verify string values for a few key categories.
	assert.Equal(t, "injection", string(CategoryInjection))
	assert.Equal(t, "secrets-exposure", string(CategorySecretsExposure))
	assert.Equal(t, "vulnerable-dependency", string(CategoryVulnerableDep))
	assert.Equal(t, "license-compliance", string(CategoryLicenseCompliance))
}

func TestCategoryNoDuplicates(t *testing.T) {
	categories := AllCategories()
	seen := make(map[Category]bool, len(categories))
	for _, c := range categories {
		assert.False(t, seen[c], "duplicate category: %s", c)
		seen[c] = true
	}
}

func TestFindingValidation(t *testing.T) {
	f := Finding{
		ID:          "SEC-001",
		Scanner:     "secret-scanner",
		Severity:    SeverityCritical,
		Category:    CategorySecretsExposure,
		Title:       "Hardcoded API key detected",
		Description: "An API key was found hardcoded in source code.",
		Location: Location{
			File:      "cmd/main.go",
			StartLine: 42,
			EndLine:   42,
			Function:  "init",
		},
		CWE:         "CWE-798",
		OWASP:       "A07:2021",
		Evidence:    "apiKey := \"AKIA...\"",
		Remediation: "Move the API key to an environment variable or secrets manager.",
		Confidence:  ConfidenceHigh,
		References:  []string{"https://cwe.mitre.org/data/definitions/798.html"},
		Metadata:    map[string]string{"provider": "aws"},
		SkillSource: "builtin",
	}

	assert.Equal(t, "SEC-001", f.ID)
	assert.Equal(t, "secret-scanner", f.Scanner)
	assert.Equal(t, SeverityCritical, f.Severity)
	assert.Equal(t, CategorySecretsExposure, f.Category)
	assert.Equal(t, "Hardcoded API key detected", f.Title)
	assert.Equal(t, "An API key was found hardcoded in source code.", f.Description)
	assert.Equal(t, "cmd/main.go", f.Location.File)
	assert.Equal(t, 42, f.Location.StartLine)
	assert.Equal(t, 42, f.Location.EndLine)
	assert.Equal(t, "init", f.Location.Function)
	assert.Equal(t, "CWE-798", f.CWE)
	assert.Equal(t, "A07:2021", f.OWASP)
	assert.Equal(t, "apiKey := \"AKIA...\"", f.Evidence)
	assert.Equal(t, ConfidenceHigh, f.Confidence)
	assert.Len(t, f.References, 1)
	assert.Equal(t, "aws", f.Metadata["provider"])
	assert.Equal(t, "builtin", f.SkillSource)
}

func TestReportSummary(t *testing.T) {
	report := &Report{
		Findings: []Finding{
			{Severity: SeverityCritical},
			{Severity: SeverityCritical},
			{Severity: SeverityHigh},
			{Severity: SeverityHigh},
			{Severity: SeverityHigh},
			{Severity: SeverityMedium},
			{Severity: SeverityMedium},
			{Severity: SeverityLow},
			{Severity: SeverityInfo},
			{Severity: SeverityInfo},
		},
		AttackChains: []AttackChain{
			{ID: "chain-1", Title: "SQL injection to data exfiltration"},
			{ID: "chain-2", Title: "Privilege escalation via misconfiguration"},
		},
	}

	summary := report.Summary()

	assert.Equal(t, 2, summary.Critical)
	assert.Equal(t, 3, summary.High)
	assert.Equal(t, 2, summary.Medium)
	assert.Equal(t, 1, summary.Low)
	assert.Equal(t, 2, summary.Info)
	assert.Equal(t, 2, summary.Chains)
	assert.Equal(t, 10, summary.Total)
}

func TestReportSummaryEmpty(t *testing.T) {
	report := &Report{}
	summary := report.Summary()

	assert.Equal(t, 0, summary.Critical)
	assert.Equal(t, 0, summary.High)
	assert.Equal(t, 0, summary.Medium)
	assert.Equal(t, 0, summary.Low)
	assert.Equal(t, 0, summary.Info)
	assert.Equal(t, 0, summary.Chains)
	assert.Equal(t, 0, summary.Total)
}

func TestScanErrorString(t *testing.T) {
	t.Run("non-fatal error", func(t *testing.T) {
		err := ScanError{
			Scanner: "secret-scanner",
			Err:     errors.New("permission denied"),
			Fatal:   false,
		}
		assert.Equal(t, "error in secret-scanner: permission denied", err.Error())
	})

	t.Run("fatal error", func(t *testing.T) {
		err := ScanError{
			Scanner: "dependency-auditor",
			Err:     errors.New("lockfile not found"),
			Fatal:   true,
		}
		assert.Equal(t, "fatal error in dependency-auditor: lockfile not found", err.Error())
	})
}

func TestScanErrorImplementsError(t *testing.T) {
	var err error = ScanError{
		Scanner: "test",
		Err:     errors.New("test error"),
	}
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "test")
}

func TestAttackChainSeverity(t *testing.T) {
	chain := AttackChain{
		ID:       "chain-001",
		Title:    "SQL injection to admin access",
		Severity: SeverityCritical,
		Steps: []Finding{
			{
				ID:       "step-1",
				Title:    "SQL injection in login form",
				Severity: SeverityHigh,
				Category: CategoryInjection,
			},
			{
				ID:       "step-2",
				Title:    "Privilege escalation via stored procedure",
				Severity: SeverityCritical,
				Category: CategoryAuthorization,
			},
		},
		Impact:     "Full database compromise and admin access",
		Likelihood: "high",
	}

	assert.Equal(t, "chain-001", chain.ID)
	assert.Equal(t, "SQL injection to admin access", chain.Title)
	assert.Equal(t, SeverityCritical, chain.Severity)
	assert.Len(t, chain.Steps, 2)
	assert.Equal(t, "step-1", chain.Steps[0].ID)
	assert.Equal(t, CategoryInjection, chain.Steps[0].Category)
	assert.Equal(t, "step-2", chain.Steps[1].ID)
	assert.Equal(t, CategoryAuthorization, chain.Steps[1].Category)
	assert.Equal(t, "Full database compromise and admin access", chain.Impact)
	assert.Equal(t, "high", chain.Likelihood)
}

func TestScanTarget(t *testing.T) {
	target := ScanTarget{
		RootDir:         "/project",
		Files:           []string{"main.go", "handler.go"},
		ExcludePatterns: []string{"vendor/**", "testdata/**"},
	}

	assert.Equal(t, "/project", target.RootDir)
	assert.Len(t, target.Files, 2)
	assert.Len(t, target.ExcludePatterns, 2)
}

func TestAnalysisChunk(t *testing.T) {
	chunk := AnalysisChunk{
		File:      "handler.go",
		StartLine: 10,
		EndLine:   50,
		Content:   "func handleRequest(w http.ResponseWriter, r *http.Request) { ... }",
		Language:  "go",
		RiskScore: 75,
	}

	assert.Equal(t, "handler.go", chunk.File)
	assert.Equal(t, 10, chunk.StartLine)
	assert.Equal(t, 50, chunk.EndLine)
	assert.Equal(t, "go", chunk.Language)
	assert.Equal(t, 75, chunk.RiskScore)
}

func TestScanStats(t *testing.T) {
	stats := ScanStats{
		Duration:       5 * time.Second,
		FilesScanned:   150,
		ChunksAnalyzed: 42,
		FindingsCount:  7,
		ChainCount:     2,
	}

	assert.Equal(t, 5*time.Second, stats.Duration)
	assert.Equal(t, 150, stats.FilesScanned)
	assert.Equal(t, 42, stats.ChunksAnalyzed)
	assert.Equal(t, 7, stats.FindingsCount)
	assert.Equal(t, 2, stats.ChainCount)
}

func TestLocationFields(t *testing.T) {
	loc := Location{
		File:      "internal/auth/jwt.go",
		StartLine: 100,
		EndLine:   120,
		Function:  "ValidateToken",
	}

	assert.Equal(t, "internal/auth/jwt.go", loc.File)
	assert.Equal(t, 100, loc.StartLine)
	assert.Equal(t, 120, loc.EndLine)
	assert.Equal(t, "ValidateToken", loc.Function)
}
