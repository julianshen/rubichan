package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/julianshen/rubichan/internal/skills"
)

// skillScannerAdapter wraps a skills.RegisteredScanner as a security.StaticScanner
// so that security-rule skills participate in the security engine scan phase.
type skillScannerAdapter struct {
	scanner skills.RegisteredScanner
}

func (a *skillScannerAdapter) Name() string {
	return a.scanner.Name
}

func (a *skillScannerAdapter) Scan(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	var findings []security.Finding

	for _, file := range target.Files {
		fullPath := filepath.Join(target.RootDir, file)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue // Skip unreadable files.
		}

		skillFindings, err := a.scanner.Scan(ctx, string(content))
		if err != nil {
			return nil, err
		}

		for _, sf := range skillFindings {
			findings = append(findings, security.Finding{
				Scanner:     a.scanner.Name,
				Severity:    security.Severity(sf.Severity),
				Title:       sf.Rule,
				Description: sf.Message,
				Location:    security.Location{File: file},
				SkillSource: a.scanner.SkillName,
			})
		}
	}

	return findings, nil
}
