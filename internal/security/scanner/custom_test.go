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

func TestCustomRuleScanner_MatchesPattern(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("// TODO: HACK fix this later\n"), 0o644))

	rules := []security.CustomRule{
		{
			ID:       "custom-001",
			Pattern:  "TODO.*HACK",
			Severity: "medium",
			Title:    "TODO HACK marker",
			Category: "misconfiguration",
		},
	}

	s := NewCustomRuleScanner(rules)
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "custom-001", findings[0].ID)
	assert.Equal(t, security.SeverityMedium, findings[0].Severity)
	assert.Equal(t, "TODO HACK marker", findings[0].Title)
	assert.Contains(t, findings[0].Location.File, "main.go")
}

func TestCustomRuleScanner_NoMatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean.go"), []byte("package clean\n"), 0o644))

	rules := []security.CustomRule{
		{ID: "custom-001", Pattern: "VULNERABLE", Severity: "high", Title: "Vulnerable code"},
	}

	s := NewCustomRuleScanner(rules)
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestCustomRuleScanner_InvalidRegex(t *testing.T) {
	rules := []security.CustomRule{
		{ID: "custom-bad", Pattern: "[invalid", Severity: "high", Title: "Bad pattern"},
	}

	s := NewCustomRuleScanner(rules)
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: t.TempDir()})
	require.NoError(t, err)
	assert.Empty(t, findings, "invalid regex should be skipped, not error")
}

func TestCustomRuleScanner_EmptyRules(t *testing.T) {
	s := NewCustomRuleScanner(nil)
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: t.TempDir()})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestCustomRuleScanner_Name(t *testing.T) {
	s := NewCustomRuleScanner(nil)
	assert.Equal(t, "custom-rules", s.Name())
}
