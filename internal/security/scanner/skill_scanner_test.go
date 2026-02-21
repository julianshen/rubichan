package scanner

import (
	"context"
	"errors"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillScannerAdapterName(t *testing.T) {
	adapter := NewSkillScannerAdapter("my-skill-scanner", func(_ context.Context, _ security.ScanTarget) ([]security.Finding, error) {
		return nil, nil
	})
	assert.Equal(t, "my-skill-scanner", adapter.Name())
}

func TestSkillScannerAdapterInterface(t *testing.T) {
	var _ security.StaticScanner = NewSkillScannerAdapter("test", func(_ context.Context, _ security.ScanTarget) ([]security.Finding, error) {
		return nil, nil
	})
}

func TestSkillScannerAdapterCallsFunction(t *testing.T) {
	called := false
	expectedFindings := []security.Finding{
		{
			ID:       "SKILL-001",
			Scanner:  "my-skill",
			Severity: security.SeverityHigh,
			Category: security.CategoryInjection,
			Title:    "Custom skill finding",
		},
	}

	adapter := NewSkillScannerAdapter("my-skill", func(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
		called = true
		assert.Equal(t, "/some/dir", target.RootDir)
		return expectedFindings, nil
	})

	findings, err := adapter.Scan(context.Background(), security.ScanTarget{RootDir: "/some/dir"})
	require.NoError(t, err)
	assert.True(t, called, "scan function should have been called")
	assert.Equal(t, expectedFindings, findings)
}

func TestSkillScannerAdapterPropagatesError(t *testing.T) {
	expectedErr := errors.New("skill scan failed")

	adapter := NewSkillScannerAdapter("failing-skill", func(_ context.Context, _ security.ScanTarget) ([]security.Finding, error) {
		return nil, expectedErr
	})

	findings, err := adapter.Scan(context.Background(), security.ScanTarget{RootDir: "/tmp"})
	assert.ErrorIs(t, err, expectedErr)
	assert.Nil(t, findings)
}
