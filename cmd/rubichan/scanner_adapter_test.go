package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/julianshen/rubichan/internal/skills"
)

func TestSkillScannerAdapterName(t *testing.T) {
	adapter := &skillScannerAdapter{
		scanner: skills.RegisteredScanner{
			SkillName: "my-skill",
			Name:      "secret-scanner",
		},
	}
	assert.Equal(t, "secret-scanner", adapter.Name())
}

func TestSkillScannerAdapterScan(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "app.go"), []byte("password = \"secret123\""), 0644))

	adapter := &skillScannerAdapter{
		scanner: skills.RegisteredScanner{
			SkillName: "cred-skill",
			Name:      "cred-scanner",
			Scan: func(_ context.Context, content string) ([]skills.SecurityFinding, error) {
				if len(content) > 0 {
					return []skills.SecurityFinding{{
						Rule:     "hardcoded-password",
						Message:  "Found hardcoded password",
						Severity: "high",
					}}, nil
				}
				return nil, nil
			},
		},
	}

	findings, err := adapter.Scan(context.Background(), security.ScanTarget{
		RootDir: tmpDir,
		Files:   []string{"app.go"},
	})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "cred-scanner", findings[0].Scanner)
	assert.Equal(t, security.SeverityHigh, findings[0].Severity)
	assert.Equal(t, "hardcoded-password", findings[0].Title)
	assert.Equal(t, "Found hardcoded password", findings[0].Description)
	assert.Equal(t, "app.go", findings[0].Location.File)
	assert.Equal(t, "cred-skill", findings[0].SkillSource)
}

func TestSkillScannerAdapterSkipsUnreadableFiles(t *testing.T) {
	adapter := &skillScannerAdapter{
		scanner: skills.RegisteredScanner{
			SkillName: "test",
			Name:      "test-scanner",
			Scan: func(_ context.Context, content string) ([]skills.SecurityFinding, error) {
				return nil, nil
			},
		},
	}

	findings, err := adapter.Scan(context.Background(), security.ScanTarget{
		RootDir: "/nonexistent",
		Files:   []string{"missing.go"},
	})
	require.NoError(t, err)
	assert.Empty(t, findings)
}
