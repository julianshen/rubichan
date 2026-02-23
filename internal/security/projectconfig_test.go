package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadProjectConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := `
rules:
  - id: custom-001
    pattern: "TODO.*HACK"
    severity: medium
    title: "TODO HACK marker found"
    category: misconfiguration
  - id: custom-002
    pattern: "password\\s*=\\s*\"[^\"]+\""
    severity: high
    title: "Hardcoded password"
    category: secrets-exposure

overrides:
  - finding_id: SEC-001
    severity: info
    reason: "Known false positive"

ci:
  fail_on: critical
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".security.yaml"), []byte(yaml), 0o644))

	cfg, err := LoadProjectConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Len(t, cfg.Rules, 2)
	assert.Equal(t, "custom-001", cfg.Rules[0].ID)
	assert.Equal(t, "TODO.*HACK", cfg.Rules[0].Pattern)
	assert.Equal(t, "medium", cfg.Rules[0].Severity)

	assert.Len(t, cfg.Overrides, 1)
	assert.Equal(t, "SEC-001", cfg.Overrides[0].FindingID)
	assert.Equal(t, "info", cfg.Overrides[0].Severity)

	assert.Equal(t, "critical", cfg.CI.FailOn)
}

func TestLoadProjectConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()

	cfg, err := LoadProjectConfig(dir)
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestLoadProjectConfig_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".security.yaml"), []byte(""), 0o644))

	cfg, err := LoadProjectConfig(dir)
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestLoadProjectConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".security.yaml"), []byte("{{invalid"), 0o644))

	_, err := LoadProjectConfig(dir)
	assert.Error(t, err)
}

func TestApplyOverrides_ChangesSeverity(t *testing.T) {
	findings := []Finding{
		{ID: "SEC-001", Severity: SeverityHigh, Title: "High finding"},
		{ID: "SEC-002", Severity: SeverityMedium, Title: "Medium finding"},
	}
	overrides := []Override{
		{FindingID: "SEC-001", Severity: "info", Reason: "known false positive"},
	}

	count := ApplyOverrides(findings, overrides)
	assert.Equal(t, 1, count)
	assert.Equal(t, SeverityInfo, findings[0].Severity)
	assert.Equal(t, SeverityMedium, findings[1].Severity, "unmatched finding unchanged")
}

func TestApplyOverrides_NoMatches(t *testing.T) {
	findings := []Finding{
		{ID: "SEC-001", Severity: SeverityHigh, Title: "High finding"},
	}
	overrides := []Override{
		{FindingID: "SEC-999", Severity: "low"},
	}

	count := ApplyOverrides(findings, overrides)
	assert.Equal(t, 0, count)
	assert.Equal(t, SeverityHigh, findings[0].Severity)
}

func TestApplyOverrides_EmptyInputs(t *testing.T) {
	assert.Equal(t, 0, ApplyOverrides(nil, nil))
	assert.Equal(t, 0, ApplyOverrides([]Finding{}, nil))
	assert.Equal(t, 0, ApplyOverrides(nil, []Override{{FindingID: "x"}}))
}

func TestLoadProjectConfig_InvalidRuleSeverity(t *testing.T) {
	dir := t.TempDir()
	yaml := `
rules:
  - id: custom-001
    pattern: "test"
    severity: banana
    title: "Bad severity"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".security.yaml"), []byte(yaml), 0o644))

	_, err := LoadProjectConfig(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid severity")
	assert.Contains(t, err.Error(), "banana")
}

func TestLoadProjectConfig_InvalidOverrideSeverity(t *testing.T) {
	dir := t.TempDir()
	yaml := `
overrides:
  - finding_id: SEC-001
    severity: typo
    reason: "test"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".security.yaml"), []byte(yaml), 0o644))

	_, err := LoadProjectConfig(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid severity")
}

func TestLoadProjectConfig_MissingRuleID(t *testing.T) {
	dir := t.TempDir()
	yaml := `
rules:
  - pattern: "test"
    severity: high
    title: "No ID"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".security.yaml"), []byte(yaml), 0o644))

	_, err := LoadProjectConfig(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required id")
}
