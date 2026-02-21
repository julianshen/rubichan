package scanner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/julianshen/rubichan/internal/security"
)

// secretRule defines a single regex-based detection rule.
type secretRule struct {
	name       string
	pattern    *regexp.Regexp
	severity   security.Severity
	title      string
	matchGroup int // which submatch group to use for evidence masking (0 = full match)
}

// SecretScanner detects hard-coded secrets, API keys, and high-entropy strings
// in source files using regex patterns and Shannon entropy analysis.
type SecretScanner struct {
	rules          []secretRule
	entropyVarPat  *regexp.Regexp
	examplePat     *regexp.Regexp
	findingCounter int
	mu             sync.Mutex
}

// NewSecretScanner creates a SecretScanner with the standard detection rules.
func NewSecretScanner() *SecretScanner {
	return &SecretScanner{
		rules: []secretRule{
			{
				name:     "aws-key",
				pattern:  regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
				severity: security.SeverityHigh,
				title:    "AWS access key detected",
			},
			{
				name:     "github-token",
				pattern:  regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
				severity: security.SeverityHigh,
				title:    "GitHub token detected",
			},
			{
				name:     "gitlab-token",
				pattern:  regexp.MustCompile(`glpat-[a-zA-Z0-9\-]{20,}`),
				severity: security.SeverityHigh,
				title:    "GitLab token detected",
			},
			{
				name:     "slack-token",
				pattern:  regexp.MustCompile(`xox[bprs]-[a-zA-Z0-9-]+`),
				severity: security.SeverityHigh,
				title:    "Slack token detected",
			},
			{
				name:     "private-key",
				pattern:  regexp.MustCompile(`-----BEGIN .* PRIVATE KEY-----`),
				severity: security.SeverityCritical,
				title:    "Private key detected",
			},
			{
				name:       "generic-api-key",
				pattern:    regexp.MustCompile(`(?i)(api[_-]?key|apikey|secret[_-]?key|password|token)\s*[:=]\s*["']([^"'\s]{20,})["']`),
				severity:   security.SeverityHigh,
				title:      "Generic API key/secret assignment detected",
				matchGroup: 2,
			},
			{
				name:       "jwt-secret",
				pattern:    regexp.MustCompile(`(?i)(jwt[_-]?secret|signing[_-]?key)\s*[:=]\s*["']([^"']{16,})["']`),
				severity:   security.SeverityHigh,
				title:      "JWT secret detected",
				matchGroup: 2,
			},
			{
				name:     "db-connection-string",
				pattern:  regexp.MustCompile(`(?i)(mysql|postgres|postgresql|mongodb|redis)://[^\s"']+`),
				severity: security.SeverityHigh,
				title:    "Database connection string detected",
			},
			{
				name:     "bearer-token",
				pattern:  regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-._~+/]+=*`),
				severity: security.SeverityHigh,
				title:    "Bearer token detected",
			},
		},
		entropyVarPat: regexp.MustCompile(`(?i)(key|secret|token|password|credential|apikey)\s*[:=]\s*["']([^"']+)["']`),
		examplePat:    regexp.MustCompile(`(?i)(example|placeholder|your[-_]|sample|dummy|test[-_]|changeme|replace[-_]|insert[-_]|xxx|todo)`),
	}
}

// Name returns the scanner name.
func (s *SecretScanner) Name() string {
	return "secrets"
}

// Scan walks the target files and returns findings for detected secrets.
func (s *SecretScanner) Scan(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("secret scanner cancelled: %w", err)
	}

	files, err := security.CollectFiles(target, nil)
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	var findings []security.Finding
	for _, relPath := range files {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("secret scanner cancelled: %w", err)
		}

		absPath := filepath.Join(target.RootDir, relPath)
		fileFindings, err := s.scanFile(absPath, relPath)
		if err != nil {
			// Skip files we cannot read rather than failing the whole scan.
			continue
		}
		findings = append(findings, fileFindings...)
	}

	return findings, nil
}

// isBinary returns true if the data appears to be binary (contains null bytes
// within the first 512 bytes).
func isBinary(data []byte) bool {
	limit := 512
	if len(data) < limit {
		limit = len(data)
	}
	return bytes.ContainsRune(data[:limit], 0)
}

// isExampleValue returns true if the matched string looks like a placeholder
// or example value that should not be flagged.
func (s *SecretScanner) isExampleValue(value string) bool {
	return s.examplePat.MatchString(value)
}

// scanFile scans a single file and returns any findings.
func (s *SecretScanner) scanFile(absPath, relPath string) ([]security.Finding, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	if isBinary(data) {
		return nil, nil
	}

	var findings []security.Finding
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Check each regex rule.
		for _, rule := range s.rules {
			matches := rule.pattern.FindStringSubmatch(line)
			if matches == nil {
				continue
			}

			matchedValue := matches[0]
			if rule.matchGroup > 0 && rule.matchGroup < len(matches) {
				matchedValue = matches[rule.matchGroup]
			}

			if s.isExampleValue(matchedValue) {
				continue
			}

			findings = append(findings, s.newFinding(
				rule.title,
				rule.severity,
				security.ConfidenceHigh,
				relPath,
				lineNum,
				maskSecret(matchedValue, rule.name),
			))
		}

		// Check entropy for variable assignments matching sensitive names.
		if entropyMatches := s.entropyVarPat.FindStringSubmatch(line); entropyMatches != nil {
			value := entropyMatches[2]
			if len(value) >= 20 && shannonEntropy(value) > 4.0 && !s.isExampleValue(value) {
				// Avoid duplicate if already caught by a regex rule.
				if !s.alreadyDetected(findings, relPath, lineNum) {
					findings = append(findings, s.newFinding(
						"High entropy string detected in sensitive variable",
						security.SeverityMedium,
						security.ConfidenceMedium,
						relPath,
						lineNum,
						fmt.Sprintf("High entropy value in variable (entropy: %.2f)", shannonEntropy(value)),
					))
				}
			}
		}
	}

	return findings, scanner.Err()
}

// alreadyDetected returns true if there is already a finding for the given file and line.
func (s *SecretScanner) alreadyDetected(findings []security.Finding, file string, line int) bool {
	for _, f := range findings {
		if f.Location.File == file && f.Location.StartLine == line {
			return true
		}
	}
	return false
}

// newFinding creates a properly formatted Finding.
func (s *SecretScanner) newFinding(title string, severity security.Severity, confidence security.Confidence, file string, line int, evidence string) security.Finding {
	s.mu.Lock()
	s.findingCounter++
	id := fmt.Sprintf("SEC-%04d", s.findingCounter)
	s.mu.Unlock()

	return security.Finding{
		ID:       id,
		Scanner:  "secrets",
		Severity: severity,
		Category: security.CategorySecretsExposure,
		Title:    title,
		Description: fmt.Sprintf(
			"%s found at %s:%d",
			title, file, line,
		),
		Location: security.Location{
			File:      file,
			StartLine: line,
			EndLine:   line,
		},
		CWE:        "CWE-798",
		Evidence:   evidence,
		Confidence: confidence,
	}
}

// maskSecret masks a secret value for evidence, keeping at most the first 4
// characters and replacing the rest with asterisks.
func maskSecret(value, ruleName string) string {
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	return fmt.Sprintf("Matched %s pattern: %s%s", ruleName, value[:4], strings.Repeat("*", len(value)-4))
}

// shannonEntropy calculates the Shannon entropy of a string.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}

	freq := make(map[rune]float64)
	for _, c := range s {
		freq[c]++
	}

	length := float64(len([]rune(s)))
	entropy := 0.0
	for _, count := range freq {
		p := count / length
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	return entropy
}
