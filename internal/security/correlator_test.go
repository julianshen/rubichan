package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorrelatorDetectsUnauthSQLI(t *testing.T) {
	c := NewCorrelator()

	findings := []Finding{
		{
			ID:       "F-1",
			Scanner:  "sast",
			Severity: SeverityMedium,
			Category: CategoryAuthentication,
			Title:    "Missing authentication check",
			Location: Location{
				File:      "handler.go",
				StartLine: 10,
				EndLine:   20,
				Function:  "HandleRequest",
			},
			CWE:        "CWE-306",
			Confidence: ConfidenceHigh,
		},
		{
			ID:       "F-2",
			Scanner:  "sast",
			Severity: SeverityHigh,
			Category: CategoryInjection,
			Title:    "SQL injection",
			Location: Location{
				File:      "handler.go",
				StartLine: 15,
				EndLine:   25,
				Function:  "HandleRequest",
			},
			CWE:        "CWE-89",
			Confidence: ConfidenceHigh,
		},
	}

	chains, deduped := c.Correlate(findings)

	require.Len(t, chains, 1)
	assert.Equal(t, "Unauthenticated Injection", chains[0].Title)
	assert.Equal(t, SeverityCritical, chains[0].Severity)
	assert.Len(t, chains[0].Steps, 2)
	assert.Len(t, deduped, 2)
}

func TestCorrelatorDetectsUnauthDataAccess(t *testing.T) {
	c := NewCorrelator()

	findings := []Finding{
		{
			ID:       "F-1",
			Scanner:  "auth-analyzer",
			Severity: SeverityMedium,
			Category: CategoryAuthentication,
			Title:    "Missing authentication",
			Location: Location{
				File:      "api.go",
				StartLine: 50,
				EndLine:   60,
				Function:  "GetUser",
			},
			CWE:        "CWE-306",
			Confidence: ConfidenceHigh,
		},
		{
			ID:       "F-2",
			Scanner:  "dataflow-analyzer",
			Severity: SeverityHigh,
			Category: CategoryDataExposure,
			Title:    "User data exposure",
			Location: Location{
				File:      "api.go",
				StartLine: 55,
				EndLine:   65,
				Function:  "GetUser",
			},
			CWE:        "CWE-200",
			Confidence: ConfidenceMedium,
		},
	}

	chains, deduped := c.Correlate(findings)

	require.Len(t, chains, 1)
	assert.Equal(t, "Unauthenticated Data Access", chains[0].Title)
	assert.Equal(t, SeverityCritical, chains[0].Severity)
	assert.Len(t, chains[0].Steps, 2)
	assert.Len(t, deduped, 2)
}

func TestCorrelatorDetectsRecoverableSecret(t *testing.T) {
	c := NewCorrelator()

	findings := []Finding{
		{
			ID:       "F-1",
			Scanner:  "crypto-analyzer",
			Severity: SeverityMedium,
			Category: CategoryCryptography,
			Title:    "Weak encryption algorithm",
			Location: Location{
				File:      "crypto.go",
				StartLine: 10,
				EndLine:   20,
				Function:  "Encrypt",
			},
			CWE:        "CWE-327",
			Confidence: ConfidenceHigh,
		},
		{
			ID:       "F-2",
			Scanner:  "secrets",
			Severity: SeverityHigh,
			Category: CategorySecretsExposure,
			Title:    "API key in source",
			Location: Location{
				File:      "crypto.go",
				StartLine: 100,
				EndLine:   105,
				Function:  "LoadConfig",
			},
			CWE:        "CWE-798",
			Confidence: ConfidenceHigh,
		},
	}

	chains, deduped := c.Correlate(findings)

	require.Len(t, chains, 1)
	assert.Equal(t, "Recoverable Secret", chains[0].Title)
	assert.Equal(t, SeverityHigh, chains[0].Severity)
	assert.Len(t, chains[0].Steps, 2)
	assert.Len(t, deduped, 2)
}

func TestCorrelatorDetectsTOCTOU(t *testing.T) {
	c := NewCorrelator()

	findings := []Finding{
		{
			ID:       "F-1",
			Scanner:  "concurrency-analyzer",
			Severity: SeverityMedium,
			Category: CategoryRaceCondition,
			Title:    "Race condition on shared state",
			Location: Location{
				File:      "authz.go",
				StartLine: 30,
				EndLine:   40,
				Function:  "CheckAndExecute",
			},
			CWE:        "CWE-362",
			Confidence: ConfidenceHigh,
		},
		{
			ID:       "F-2",
			Scanner:  "auth-analyzer",
			Severity: SeverityMedium,
			Category: CategoryAuthorization,
			Title:    "Improper authorization check",
			Location: Location{
				File:      "authz.go",
				StartLine: 35,
				EndLine:   45,
				Function:  "CheckAndExecute",
			},
			CWE:        "CWE-863",
			Confidence: ConfidenceMedium,
		},
	}

	chains, deduped := c.Correlate(findings)

	require.Len(t, chains, 1)
	assert.Equal(t, "TOCTOU Authorization Bypass", chains[0].Title)
	assert.Equal(t, SeverityHigh, chains[0].Severity)
	assert.Len(t, chains[0].Steps, 2)
	assert.Len(t, deduped, 2)
}

func TestCorrelatorDeduplicates(t *testing.T) {
	c := NewCorrelator()

	findings := []Finding{
		{
			ID:         "F-1",
			Scanner:    "sast",
			Severity:   SeverityHigh,
			Category:   CategoryInjection,
			Title:      "SQL injection from SAST",
			Location:   Location{File: "db.go", StartLine: 42, EndLine: 50, Function: "Query"},
			CWE:        "CWE-89",
			Confidence: ConfidenceMedium,
		},
		{
			ID:         "F-2",
			Scanner:    "secrets",
			Severity:   SeverityHigh,
			Category:   CategoryInjection,
			Title:      "SQL injection from secrets scanner",
			Location:   Location{File: "db.go", StartLine: 42, EndLine: 55, Function: "Query"},
			CWE:        "CWE-89",
			Confidence: ConfidenceMedium,
		},
	}

	_, deduped := c.Correlate(findings)

	assert.Len(t, deduped, 1)
	// Should keep first when confidence is tied
	assert.Equal(t, "F-1", deduped[0].ID)
}

func TestCorrelatorDeduplicatesKeepsHighestConfidence(t *testing.T) {
	c := NewCorrelator()

	findings := []Finding{
		{
			ID:         "F-1",
			Scanner:    "sast",
			Severity:   SeverityHigh,
			Category:   CategoryInjection,
			Title:      "SQL injection (low confidence)",
			Location:   Location{File: "db.go", StartLine: 42, EndLine: 50, Function: "Query"},
			CWE:        "CWE-89",
			Confidence: ConfidenceLow,
		},
		{
			ID:         "F-2",
			Scanner:    "dataflow-analyzer",
			Severity:   SeverityHigh,
			Category:   CategoryInjection,
			Title:      "SQL injection (high confidence)",
			Location:   Location{File: "db.go", StartLine: 42, EndLine: 55, Function: "Query"},
			CWE:        "CWE-89",
			Confidence: ConfidenceHigh,
		},
		{
			ID:         "F-3",
			Scanner:    "auth-analyzer",
			Severity:   SeverityHigh,
			Category:   CategoryInjection,
			Title:      "SQL injection (medium confidence)",
			Location:   Location{File: "db.go", StartLine: 42, EndLine: 48, Function: "Query"},
			CWE:        "CWE-89",
			Confidence: ConfidenceMedium,
		},
	}

	_, deduped := c.Correlate(findings)

	require.Len(t, deduped, 1)
	assert.Equal(t, "F-2", deduped[0].ID)
	assert.Equal(t, ConfidenceHigh, deduped[0].Confidence)
}

func TestCorrelatorNoChains(t *testing.T) {
	c := NewCorrelator()

	findings := []Finding{
		{
			ID:         "F-1",
			Scanner:    "sast",
			Severity:   SeverityHigh,
			Category:   CategoryInjection,
			Title:      "SQL injection",
			Location:   Location{File: "db.go", StartLine: 10, EndLine: 20, Function: "Query"},
			CWE:        "CWE-89",
			Confidence: ConfidenceHigh,
		},
		{
			ID:         "F-2",
			Scanner:    "auth-analyzer",
			Severity:   SeverityMedium,
			Category:   CategoryAuthentication,
			Title:      "Missing auth",
			Location:   Location{File: "handler.go", StartLine: 100, EndLine: 110, Function: "Handle"},
			CWE:        "CWE-306",
			Confidence: ConfidenceHigh,
		},
	}

	chains, deduped := c.Correlate(findings)

	assert.Empty(t, chains)
	assert.Len(t, deduped, 2)
}

func TestCorrelatorEmptyInput(t *testing.T) {
	c := NewCorrelator()

	chains, deduped := c.Correlate(nil)
	assert.Empty(t, chains)
	assert.Empty(t, deduped)

	chains, deduped = c.Correlate([]Finding{})
	assert.Empty(t, chains)
	assert.Empty(t, deduped)
}

func TestCorrelatorMultipleChains(t *testing.T) {
	c := NewCorrelator()

	findings := []Finding{
		// Chain 1: Unauthenticated Injection
		{
			ID:         "F-1",
			Scanner:    "sast",
			Severity:   SeverityMedium,
			Category:   CategoryAuthentication,
			Title:      "Missing auth check",
			Location:   Location{File: "handler.go", StartLine: 10, EndLine: 20, Function: "HandleRequest"},
			CWE:        "CWE-306",
			Confidence: ConfidenceHigh,
		},
		{
			ID:         "F-2",
			Scanner:    "sast",
			Severity:   SeverityHigh,
			Category:   CategoryInjection,
			Title:      "SQL injection",
			Location:   Location{File: "handler.go", StartLine: 15, EndLine: 25, Function: "HandleRequest"},
			CWE:        "CWE-89",
			Confidence: ConfidenceHigh,
		},
		// Chain 2: Recoverable Secret
		{
			ID:         "F-3",
			Scanner:    "crypto-analyzer",
			Severity:   SeverityMedium,
			Category:   CategoryCryptography,
			Title:      "Weak encryption",
			Location:   Location{File: "crypto.go", StartLine: 10, EndLine: 20, Function: "Encrypt"},
			CWE:        "CWE-327",
			Confidence: ConfidenceHigh,
		},
		{
			ID:         "F-4",
			Scanner:    "secrets",
			Severity:   SeverityHigh,
			Category:   CategorySecretsExposure,
			Title:      "Hardcoded secret",
			Location:   Location{File: "crypto.go", StartLine: 50, EndLine: 55, Function: "LoadKeys"},
			CWE:        "CWE-798",
			Confidence: ConfidenceHigh,
		},
		// Unrelated finding
		{
			ID:         "F-5",
			Scanner:    "config",
			Severity:   SeverityLow,
			Category:   CategoryMisconfiguration,
			Title:      "Debug mode enabled",
			Location:   Location{File: "config.go", StartLine: 5, EndLine: 10, Function: "Init"},
			CWE:        "CWE-489",
			Confidence: ConfidenceMedium,
		},
	}

	chains, deduped := c.Correlate(findings)

	assert.Len(t, chains, 2)
	assert.Len(t, deduped, 5)

	// Check that both chain types are present
	titles := make(map[string]bool)
	for _, chain := range chains {
		titles[chain.Title] = true
	}
	assert.True(t, titles["Unauthenticated Injection"])
	assert.True(t, titles["Recoverable Secret"])
}
