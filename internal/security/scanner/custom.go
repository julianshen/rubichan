package scanner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/julianshen/rubichan/internal/security"
)

// compiledCustomRule holds a pre-compiled regex alongside its metadata.
type compiledCustomRule struct {
	rule security.CustomRule
	re   *regexp.Regexp
}

// CustomRuleScanner applies project-specific regex patterns defined in
// .security.yaml to detect custom security issues.
type CustomRuleScanner struct {
	rules []compiledCustomRule
}

// NewCustomRuleScanner creates a scanner from project-specific custom rules.
// Invalid regex patterns are silently skipped.
func NewCustomRuleScanner(rules []security.CustomRule) *CustomRuleScanner {
	var compiled []compiledCustomRule
	for _, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue
		}
		compiled = append(compiled, compiledCustomRule{rule: r, re: re})
	}
	return &CustomRuleScanner{rules: compiled}
}

func (s *CustomRuleScanner) Name() string {
	return "custom-rules"
}

func (s *CustomRuleScanner) Scan(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	if len(s.rules) == 0 {
		return nil, nil
	}

	files, err := security.CollectFiles(target, nil)
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	var findings []security.Finding
	for _, relPath := range files {
		if ctx.Err() != nil {
			return findings, ctx.Err()
		}

		absPath := filepath.Join(target.RootDir, relPath)
		fileFindings, scanErr := s.scanFile(absPath, relPath)
		if scanErr != nil {
			continue
		}
		findings = append(findings, fileFindings...)
	}
	return findings, nil
}

func (s *CustomRuleScanner) scanFile(absPath, relPath string) ([]security.Finding, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var findings []security.Finding
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		for _, cr := range s.rules {
			if cr.re.MatchString(line) {
				findings = append(findings, security.Finding{
					ID:       cr.rule.ID,
					Scanner:  "custom-rules",
					Severity: security.Severity(cr.rule.Severity),
					Category: security.Category(cr.rule.Category),
					Title:    cr.rule.Title,
					Location: security.Location{
						File:      relPath,
						StartLine: lineNum,
						EndLine:   lineNum,
					},
					Evidence:   line,
					Confidence: security.ConfidenceHigh,
				})
			}
		}
	}
	return findings, scanner.Err()
}
