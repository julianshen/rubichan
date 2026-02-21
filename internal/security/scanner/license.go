package scanner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/julianshen/rubichan/internal/security"
)

// licenseType classifies a license by its name and compliance risk.
type licenseType struct {
	name     string
	copyleft bool
}

// knownLicenses maps keyword patterns to license classifications.
var knownLicenses = []struct {
	keyword string
	license licenseType
}{
	{"AGPL", licenseType{name: "AGPL", copyleft: true}},
	{"GNU AFFERO", licenseType{name: "AGPL", copyleft: true}},
	{"LGPL", licenseType{name: "LGPL", copyleft: true}},
	{"GNU LESSER", licenseType{name: "LGPL", copyleft: true}},
	{"GPL", licenseType{name: "GPL", copyleft: true}},
	{"GNU GENERAL PUBLIC", licenseType{name: "GPL", copyleft: true}},
	{"MIT ", licenseType{name: "MIT", copyleft: false}},
	{"MIT\n", licenseType{name: "MIT", copyleft: false}},
	{"APACHE", licenseType{name: "Apache", copyleft: false}},
	{"BSD", licenseType{name: "BSD", copyleft: false}},
	{"ISC ", licenseType{name: "ISC", copyleft: false}},
	{"ISC\n", licenseType{name: "ISC", copyleft: false}},
	{"MPL", licenseType{name: "MPL", copyleft: false}},
	{"MOZILLA PUBLIC", licenseType{name: "MPL", copyleft: false}},
	{"UNLICENSE", licenseType{name: "Unlicense", copyleft: false}},
}

// licenseFileNames lists the common license file names to look for.
var licenseFileNames = []string{
	"LICENSE",
	"LICENSE.md",
	"LICENSE.txt",
	"LICENCE",
	"LICENCE.md",
	"LICENCE.txt",
	"COPYING",
	"COPYING.md",
	"COPYING.txt",
}

// LicenseScanner detects license compliance issues by identifying copyleft
// licenses, missing license files, and license headers in source files.
type LicenseScanner struct {
	findingCounter int
	mu             sync.Mutex
}

// NewLicenseScanner creates a LicenseScanner.
func NewLicenseScanner() *LicenseScanner {
	return &LicenseScanner{}
}

// Name returns the scanner name.
func (s *LicenseScanner) Name() string {
	return "license"
}

// Scan examines the target directory for license files and headers.
func (s *LicenseScanner) Scan(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("license scanner cancelled: %w", err)
	}

	var findings []security.Finding

	// Check for license files in root.
	licenseFound := false
	for _, name := range licenseFileNames {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("license scanner cancelled: %w", err)
		}

		absPath := filepath.Join(target.RootDir, name)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		licenseFound = true
		lt := identifyLicense(string(data))
		if lt != nil && lt.copyleft {
			findings = append(findings, s.newFinding(
				fmt.Sprintf("Copyleft license detected: %s", lt.name),
				fmt.Sprintf("Project uses %s license which requires derivative works to be open-sourced", lt.name),
				security.SeverityMedium,
				name, 1,
			))
		}
		break // Only check the first license file found.
	}

	if !licenseFound {
		findings = append(findings, s.newFinding(
			"Missing LICENSE file",
			"No LICENSE file found in the project root; licensing terms are unclear",
			security.SeverityLow,
			"", 0,
		))
	}

	// Scan source files for license headers.
	headerFindings, err := s.scanHeaders(ctx, target)
	if err != nil {
		return nil, err
	}
	findings = append(findings, headerFindings...)

	return findings, nil
}

// identifyLicense classifies a license file's contents by keyword matching.
func identifyLicense(content string) *licenseType {
	upper := strings.ToUpper(content)
	for _, kl := range knownLicenses {
		if strings.Contains(upper, kl.keyword) {
			lt := kl.license
			return &lt
		}
	}
	return nil
}

// scanHeaders checks the first 10 lines of source files for license/copyright headers.
func (s *LicenseScanner) scanHeaders(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	files, err := s.collectSourceFiles(target)
	if err != nil {
		return nil, err
	}

	var findings []security.Finding
	for _, relPath := range files {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("license scanner cancelled: %w", err)
		}

		absPath := filepath.Join(target.RootDir, relPath)
		header := s.checkHeader(absPath, relPath)
		if header != nil {
			findings = append(findings, *header)
		}
	}
	return findings, nil
}

// sourceFileExtensions is the list of extensions used for header scanning.
var sourceFileExtensions = []string{
	".go", ".py", ".js", ".ts", ".java", ".rs", ".c", ".cpp", ".h", ".rb", ".swift",
}

// collectSourceFiles returns source files from the target.
func (s *LicenseScanner) collectSourceFiles(target security.ScanTarget) ([]string, error) {
	return security.CollectFiles(target, sourceFileExtensions)
}

// checkHeader reads the first 10 lines of a file looking for Copyright or License text.
func (s *LicenseScanner) checkHeader(absPath, relPath string) *security.Finding {
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() && lineNum < 10 {
		lineNum++
		line := scanner.Text()
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "COPYRIGHT") || strings.Contains(upper, "LICENSE") {
			finding := s.newFinding(
				"License header found in source file",
				fmt.Sprintf("Source file contains a license/copyright header at line %d", lineNum),
				security.SeverityInfo,
				relPath, lineNum,
			)
			return &finding
		}
	}
	return nil
}

func (s *LicenseScanner) newFinding(title, description string, severity security.Severity, file string, line int) security.Finding {
	s.mu.Lock()
	s.findingCounter++
	id := fmt.Sprintf("LIC-%04d", s.findingCounter)
	s.mu.Unlock()

	return security.Finding{
		ID:          id,
		Scanner:     "license",
		Severity:    severity,
		Category:    security.CategoryLicenseCompliance,
		Title:       title,
		Description: description,
		Location: security.Location{
			File:      file,
			StartLine: line,
			EndLine:   line,
		},
		Confidence: security.ConfidenceHigh,
	}
}
