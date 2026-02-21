package scanner

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/julianshen/rubichan/internal/security"
)

// AppleScanner detects security issues in Apple platform projects by analyzing
// Info.plist files, entitlements, and Swift source code.
type AppleScanner struct {
	findingCounter int
	mu             sync.Mutex
}

// NewAppleScanner creates an AppleScanner.
func NewAppleScanner() *AppleScanner {
	return &AppleScanner{}
}

// Name returns the scanner name.
func (s *AppleScanner) Name() string {
	return "apple-platform"
}

// Scan walks the target directory for Apple platform configuration files and
// Swift source code, returning security findings.
func (s *AppleScanner) Scan(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("apple scanner cancelled: %w", err)
	}

	files, err := s.collectFiles(target)
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	var findings []security.Finding

	var plistFiles []string
	var entitlementFiles []string
	var swiftFiles []string

	for _, relPath := range files {
		base := filepath.Base(relPath)
		ext := filepath.Ext(relPath)

		switch {
		case base == "Info.plist" || strings.HasSuffix(relPath, "Info.plist"):
			plistFiles = append(plistFiles, relPath)
		case ext == ".entitlements":
			entitlementFiles = append(entitlementFiles, relPath)
		case ext == ".swift":
			swiftFiles = append(swiftFiles, relPath)
		}
	}

	// No Apple files found at all: return empty.
	if len(plistFiles) == 0 && len(entitlementFiles) == 0 && len(swiftFiles) == 0 {
		return nil, nil
	}

	for _, relPath := range plistFiles {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("apple scanner cancelled: %w", err)
		}
		absPath := filepath.Join(target.RootDir, relPath)
		findings = append(findings, s.checkPlist(absPath, relPath)...)
	}

	for _, relPath := range entitlementFiles {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("apple scanner cancelled: %w", err)
		}
		absPath := filepath.Join(target.RootDir, relPath)
		findings = append(findings, s.checkEntitlements(absPath, relPath)...)
	}

	for _, relPath := range swiftFiles {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("apple scanner cancelled: %w", err)
		}
		absPath := filepath.Join(target.RootDir, relPath)
		findings = append(findings, s.checkSwiftFile(absPath, relPath)...)
	}

	return findings, nil
}

// collectFiles builds the list of relevant files.
func (s *AppleScanner) collectFiles(target security.ScanTarget) ([]string, error) {
	if len(target.Files) > 0 {
		return target.Files, nil
	}

	var files []string
	err := filepath.Walk(target.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		relPath, relErr := filepath.Rel(target.RootDir, path)
		if relErr != nil {
			return nil
		}

		ext := filepath.Ext(path)
		base := filepath.Base(path)
		if base == "Info.plist" || ext == ".entitlements" || ext == ".swift" {
			files = append(files, relPath)
		}
		return nil
	})
	return files, err
}

// ─── Plist parsing ──────────────────────────────────────────────────────────

// plistDict represents a simple Apple XML plist dictionary structure.
type plistDict struct {
	Keys   []string
	Values []string // string representation of values (true/false for bools)
}

// checkPlist analyzes an Info.plist file for security issues.
func (s *AppleScanner) checkPlist(absPath, relPath string) []security.Finding {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	dict, err := parsePlistXML(data)
	if err != nil {
		return nil
	}

	var findings []security.Finding

	// Check for ATS bypass: NSAllowsArbitraryLoads = true
	for i, key := range dict.Keys {
		if key == "NSAllowsArbitraryLoads" && i < len(dict.Values) && dict.Values[i] == "true" {
			findings = append(findings, s.newFinding(
				"App Transport Security bypass enabled",
				"NSAllowsArbitraryLoads is set to true, which disables ATS and allows insecure HTTP connections",
				security.SeverityHigh,
				security.CategoryMisconfiguration,
				"",
				relPath,
			))
		}
	}

	return findings
}

// parsePlistXML parses an Apple XML plist and returns a flat key-value dictionary.
// It handles nested dicts by flattening all key-value pairs.
func parsePlistXML(data []byte) (*plistDict, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	decoder.Strict = false

	dict := &plistDict{}
	var inDict bool
	var expectValue bool
	var currentKey string

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				inDict = true
			case "key":
				if inDict {
					var key string
					if err := decoder.DecodeElement(&key, &t); err == nil {
						currentKey = key
						expectValue = true
					}
				}
			case "true":
				if expectValue {
					dict.Keys = append(dict.Keys, currentKey)
					dict.Values = append(dict.Values, "true")
					expectValue = false
				}
			case "false":
				if expectValue {
					dict.Keys = append(dict.Keys, currentKey)
					dict.Values = append(dict.Values, "false")
					expectValue = false
				}
			case "string":
				if expectValue {
					var val string
					if err := decoder.DecodeElement(&val, &t); err == nil {
						dict.Keys = append(dict.Keys, currentKey)
						dict.Values = append(dict.Values, val)
					}
					expectValue = false
				}
			case "integer":
				if expectValue {
					var val string
					if err := decoder.DecodeElement(&val, &t); err == nil {
						dict.Keys = append(dict.Keys, currentKey)
						dict.Values = append(dict.Values, val)
					}
					expectValue = false
				}
			case "array":
				if expectValue {
					dict.Keys = append(dict.Keys, currentKey)
					dict.Values = append(dict.Values, "[array]")
					expectValue = false
				}
			}
		}
	}
	return dict, nil
}

// ─── Entitlements checks ────────────────────────────────────────────────────

// dangerousEntitlements maps entitlement keys to their risk descriptions.
var dangerousEntitlements = map[string]struct {
	title    string
	severity security.Severity
}{
	"com.apple.security.cs.disable-library-validation": {
		title:    "Excessive entitlement: library validation disabled",
		severity: security.SeverityHigh,
	},
	"com.apple.security.cs.allow-jit": {
		title:    "JIT compilation entitlement enabled",
		severity: security.SeverityMedium,
	},
}

func (s *AppleScanner) checkEntitlements(absPath, relPath string) []security.Finding {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	dict, err := parsePlistXML(data)
	if err != nil {
		return nil
	}

	var findings []security.Finding
	for i, key := range dict.Keys {
		ent, ok := dangerousEntitlements[key]
		if !ok {
			continue
		}
		if i < len(dict.Values) && dict.Values[i] == "true" {
			findings = append(findings, s.newFinding(
				ent.title,
				fmt.Sprintf("Entitlement %s is enabled", key),
				ent.severity,
				security.CategoryMisconfiguration,
				"",
				relPath,
			))
		}
	}

	return findings
}

// ─── Swift source checks ────────────────────────────────────────────────────

var userDefaultsSensitivePat = regexp.MustCompile(
	`(?i)UserDefaults\s*\.\s*(standard|suite).*\.\s*set\s*\(.*(?:password|token|secret|key|credential)`,
)

func (s *AppleScanner) checkSwiftFile(absPath, relPath string) []security.Finding {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	var findings []security.Finding
	content := string(data)

	if userDefaultsSensitivePat.MatchString(content) {
		findings = append(findings, s.newFinding(
			"Sensitive data stored in UserDefaults",
			"UserDefaults is not encrypted; use Keychain for sensitive data like passwords and tokens",
			security.SeverityMedium,
			security.CategoryDataExposure,
			"",
			relPath,
		))
	}

	return findings
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (s *AppleScanner) newFinding(title, description string, severity security.Severity, category security.Category, cwe, file string) security.Finding {
	s.mu.Lock()
	s.findingCounter++
	id := fmt.Sprintf("APPLE-%04d", s.findingCounter)
	s.mu.Unlock()

	return security.Finding{
		ID:          id,
		Scanner:     "apple-platform",
		Severity:    severity,
		Category:    category,
		Title:       title,
		Description: description,
		Location: security.Location{
			File: file,
		},
		CWE:        cwe,
		Confidence: security.ConfidenceHigh,
	}
}
