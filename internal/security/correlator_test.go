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

func TestLinesClose(t *testing.T) {
	tests := []struct {
		name      string
		a, b      Location
		threshold int
		want      bool
	}{
		{
			name:      "overlapping ranges",
			a:         Location{StartLine: 10, EndLine: 20},
			b:         Location{StartLine: 15, EndLine: 25},
			threshold: 20,
			want:      true,
		},
		{
			name:      "adjacent within threshold",
			a:         Location{StartLine: 10, EndLine: 15},
			b:         Location{StartLine: 30, EndLine: 35},
			threshold: 20,
			want:      true,
		},
		{
			name:      "too far apart",
			a:         Location{StartLine: 10, EndLine: 15},
			b:         Location{StartLine: 50, EndLine: 55},
			threshold: 20,
			want:      false,
		},
		{
			name:      "single-line findings close together",
			a:         Location{StartLine: 10},
			b:         Location{StartLine: 12},
			threshold: 20,
			want:      true,
		},
		{
			name:      "zero start line returns false",
			a:         Location{StartLine: 0},
			b:         Location{StartLine: 10},
			threshold: 20,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, linesClose(tt.a, tt.b, tt.threshold))
		})
	}
}

func TestProximateLinesFallbackWhenNoFunction(t *testing.T) {
	// When sameFunc is required but function names are empty,
	// should fall back to line proximity.
	a := Finding{
		Location: Location{File: "handler.go", StartLine: 10, EndLine: 15},
	}
	b := Finding{
		Location: Location{File: "handler.go", StartLine: 20, EndLine: 25},
	}
	assert.True(t, proximate(a, b, true), "close lines without function names should be proximate")

	// Too far apart should not match.
	c := Finding{
		Location: Location{File: "handler.go", StartLine: 100, EndLine: 105},
	}
	assert.False(t, proximate(a, c, true), "distant lines without function names should not be proximate")
}

func TestProximateFunctionMatchOverridesLineDistance(t *testing.T) {
	// When both have function names, function match is authoritative.
	a := Finding{
		Location: Location{File: "handler.go", StartLine: 10, Function: "A"},
	}
	b := Finding{
		Location: Location{File: "handler.go", StartLine: 12, Function: "B"},
	}
	assert.False(t, proximate(a, b, true), "different function names should not match even if close")
}
