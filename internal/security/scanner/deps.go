package scanner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/julianshen/rubichan/internal/security"
)

const defaultOSVBaseURL = "https://api.osv.dev"

// dependency represents a parsed package with its version.
type dependency struct {
	Name    string
	Version string
}

// lockfileParser defines how to parse a specific lockfile format.
type lockfileParser struct {
	filename  string
	ecosystem string
	parse     func(data []byte) ([]dependency, error)
}

// DepScanner audits project dependencies for known vulnerabilities
// by parsing lockfiles and querying the OSV API.
type DepScanner struct {
	client         *http.Client
	OSVBaseURL     string
	parsers        []lockfileParser
	findingCounter int
	mu             sync.Mutex
}

// NewDepScanner creates a DepScanner. If client is nil, OSV queries are skipped.
func NewDepScanner(client *http.Client) *DepScanner {
	s := &DepScanner{
		client:     client,
		OSVBaseURL: defaultOSVBaseURL,
	}
	s.parsers = []lockfileParser{
		{filename: "go.sum", ecosystem: "Go", parse: parseGoSum},
		{filename: "package-lock.json", ecosystem: "npm", parse: parsePackageLock},
		{filename: "requirements.txt", ecosystem: "PyPI", parse: parseRequirementsTxt},
		{filename: "Gemfile.lock", ecosystem: "RubyGems", parse: parseGemfileLock},
		{filename: "Cargo.lock", ecosystem: "crates.io", parse: parseCargoLock},
		{filename: "Podfile.lock", ecosystem: "CocoaPods", parse: parsePodfileLock},
	}
	return s
}

// Name returns the scanner name.
func (s *DepScanner) Name() string {
	return "dependency-audit"
}

// Scan finds lockfiles in the target directory, parses them, and queries OSV
// for known vulnerabilities.
func (s *DepScanner) Scan(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("dependency auditor cancelled: %w", err)
	}

	var findings []security.Finding

	for _, parser := range s.parsers {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("dependency auditor cancelled: %w", err)
		}

		lockfilePath := filepath.Join(target.RootDir, parser.filename)
		data, err := os.ReadFile(lockfilePath)
		if err != nil {
			continue // lockfile not present — skip silently
		}

		deps, err := parser.parse(data)
		if err != nil {
			continue // parse error — skip silently
		}

		if s.client == nil {
			continue // no HTTP client — skip OSV queries
		}

		for _, dep := range deps {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("dependency auditor cancelled: %w", err)
			}

			vulnFindings, err := s.queryOSV(ctx, dep, parser.ecosystem, parser.filename)
			if err != nil {
				// OSV unavailable — add an info finding and stop querying this lockfile.
				findings = append(findings, s.newInfoFinding(
					"OSV API unavailable",
					fmt.Sprintf("Could not query OSV API for %s: %s", dep.Name, err),
					parser.filename,
				))
				break
			}
			findings = append(findings, vulnFindings...)
		}
	}

	return findings, nil
}

// queryOSV queries the OSV API for vulnerabilities affecting the given dependency.
func (s *DepScanner) queryOSV(ctx context.Context, dep dependency, ecosystem, lockfile string) ([]security.Finding, error) {
	reqBody := osvQueryRequest{
		Package: osvPackage{
			Name:      dep.Name,
			Ecosystem: ecosystem,
		},
		Version: dep.Version,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling OSV request: %w", err)
	}

	url := s.OSVBaseURL + "/v1/query"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating OSV request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OSV API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSV API returned status %d", resp.StatusCode)
	}

	var osvResp osvQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&osvResp); err != nil {
		return nil, fmt.Errorf("decoding OSV response: %w", err)
	}

	var findings []security.Finding
	for _, vuln := range osvResp.Vulns {
		sev := classifyOSVSeverity(vuln)
		findings = append(findings, s.newVulnFinding(dep, vuln, sev, lockfile))
	}
	return findings, nil
}

// newVulnFinding creates a Finding for a discovered vulnerability.
func (s *DepScanner) newVulnFinding(dep dependency, vuln osvVuln, sev security.Severity, lockfile string) security.Finding {
	s.mu.Lock()
	s.findingCounter++
	id := fmt.Sprintf("DEP-%04d", s.findingCounter)
	s.mu.Unlock()

	var refs []string
	for _, r := range vuln.References {
		refs = append(refs, r.URL)
	}

	return security.Finding{
		ID:          id,
		Scanner:     "dependency-audit",
		Severity:    sev,
		Category:    security.CategoryVulnerableDep,
		Title:       fmt.Sprintf("Vulnerable dependency: %s@%s (%s)", dep.Name, dep.Version, vuln.ID),
		Description: vuln.Summary,
		Location: security.Location{
			File: lockfile,
		},
		CWE:        "CWE-1035",
		Evidence:   fmt.Sprintf("Package %s@%s has known vulnerability %s", dep.Name, dep.Version, vuln.ID),
		Confidence: security.ConfidenceHigh,
		References: refs,
		Metadata: map[string]string{
			"vuln_id":   vuln.ID,
			"package":   dep.Name,
			"version":   dep.Version,
			"ecosystem": lockfile,
		},
	}
}

// newInfoFinding creates an Info-severity finding.
func (s *DepScanner) newInfoFinding(title, description, lockfile string) security.Finding {
	s.mu.Lock()
	s.findingCounter++
	id := fmt.Sprintf("DEP-%04d", s.findingCounter)
	s.mu.Unlock()

	return security.Finding{
		ID:          id,
		Scanner:     "dependency-audit",
		Severity:    security.SeverityInfo,
		Category:    security.CategoryVulnerableDep,
		Title:       title,
		Description: description,
		Location: security.Location{
			File: lockfile,
		},
		CWE:        "CWE-1035",
		Confidence: security.ConfidenceLow,
	}
}

// classifyOSVSeverity maps an OSV vulnerability's CVSS score to a Severity.
func classifyOSVSeverity(vuln osvVuln) security.Severity {
	for _, sev := range vuln.Severity {
		if sev.Type == "CVSS_V3" {
			score := parseCVSSScore(sev.Score)
			switch {
			case score >= 9.0:
				return security.SeverityCritical
			case score >= 7.0:
				return security.SeverityHigh
			case score >= 4.0:
				return security.SeverityMedium
			default:
				return security.SeverityLow
			}
		}
	}
	// No CVSS score available — default to Medium.
	return security.SeverityMedium
}

// parseCVSSScore extracts a numeric CVSS score from a string.
// The string may be a plain number ("7.5") or a full CVSS vector string.
func parseCVSSScore(s string) float64 {
	var score float64
	fmt.Sscanf(s, "%f", &score)
	return score
}

// ─── OSV API types ──────────────────────────────────────────────────────────

type osvQueryRequest struct {
	Package osvPackage `json:"package"`
	Version string     `json:"version"`
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvQueryResponse struct {
	Vulns []osvVuln `json:"vulns"`
}

type osvVuln struct {
	ID         string         `json:"id"`
	Summary    string         `json:"summary"`
	Severity   []osvSeverity  `json:"severity"`
	Affected   []osvAffected  `json:"affected"`
	References []osvReference `json:"references"`
}

type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type osvAffected struct {
	Package osvAffectedPackage `json:"package"`
	Ranges  []osvRange         `json:"ranges"`
}

type osvAffectedPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvRange struct {
	Type   string     `json:"type"`
	Events []osvEvent `json:"events"`
}

type osvEvent struct {
	Introduced string `json:"introduced,omitempty"`
	Fixed      string `json:"fixed,omitempty"`
}

type osvReference struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// ─── Lockfile parsers ───────────────────────────────────────────────────────

// parseGoSum extracts unique module@version pairs from a go.sum file,
// skipping /go.mod lines.
func parseGoSum(data []byte) ([]dependency, error) {
	seen := make(map[string]bool)
	var deps []dependency

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		name := parts[0]
		version := parts[1]

		// Skip /go.mod hash lines.
		if strings.HasSuffix(version, "/go.mod") {
			continue
		}

		// Remove the h1: hash suffix from version if present.
		// go.sum format: module version h1:hash
		// version may have a /go.mod suffix (already handled above).
		key := name + "@" + version
		if seen[key] {
			continue
		}
		seen[key] = true

		deps = append(deps, dependency{Name: name, Version: strings.TrimPrefix(version, "v")})
	}
	return deps, scanner.Err()
}

// parsePackageLock extracts packages from a package-lock.json file.
// Supports both lockfileVersion 3 ("packages") and v1 ("dependencies").
func parsePackageLock(data []byte) ([]dependency, error) {
	var lockfile struct {
		Packages     map[string]packageLockEntry `json:"packages"`
		Dependencies map[string]packageLockEntry `json:"dependencies"`
	}

	if err := json.Unmarshal(data, &lockfile); err != nil {
		return nil, fmt.Errorf("parsing package-lock.json: %w", err)
	}

	var deps []dependency

	// lockfileVersion 2/3: uses "packages" with "node_modules/" prefixes.
	for key, entry := range lockfile.Packages {
		if entry.Version == "" {
			continue
		}
		// Extract package name from "node_modules/name" path.
		name := key
		if idx := strings.LastIndex(key, "node_modules/"); idx >= 0 {
			name = key[idx+len("node_modules/"):]
		}
		if name == "" {
			continue
		}
		deps = append(deps, dependency{Name: name, Version: entry.Version})
	}

	// lockfileVersion 1: uses "dependencies" with flat names.
	if len(deps) == 0 {
		for name, entry := range lockfile.Dependencies {
			if entry.Version == "" {
				continue
			}
			deps = append(deps, dependency{Name: name, Version: entry.Version})
		}
	}

	return deps, nil
}

type packageLockEntry struct {
	Version  string `json:"version"`
	Resolved string `json:"resolved"`
}

// parseRequirementsTxt extracts package==version from requirements.txt.
func parseRequirementsTxt(data []byte) ([]dependency, error) {
	var deps []dependency
	re := regexp.MustCompile(`^([a-zA-Z0-9._-]+)==([^\s#]+)`)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if matches != nil {
			deps = append(deps, dependency{Name: matches[1], Version: matches[2]})
		}
	}
	return deps, scanner.Err()
}

// parseGemfileLock extracts gems from the GEM/specs section of a Gemfile.lock.
func parseGemfileLock(data []byte) ([]dependency, error) {
	var deps []dependency
	re := regexp.MustCompile(`^\s{4}(\S+)\s+\(([^)]+)\)`)

	inSpecs := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.TrimSpace(line) == "specs:" {
			inSpecs = true
			continue
		}

		// End of specs section when we hit a non-indented line (except blank).
		if inSpecs && len(line) > 0 && line[0] != ' ' {
			inSpecs = false
			continue
		}

		if inSpecs {
			matches := re.FindStringSubmatch(line)
			if matches != nil {
				deps = append(deps, dependency{Name: matches[1], Version: matches[2]})
			}
		}
	}
	return deps, scanner.Err()
}

// parseCargoLock extracts [[package]] blocks from Cargo.lock.
func parseCargoLock(data []byte) ([]dependency, error) {
	var deps []dependency
	var currentName, currentVersion string

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "[[package]]" {
			if currentName != "" && currentVersion != "" {
				deps = append(deps, dependency{Name: currentName, Version: currentVersion})
			}
			currentName = ""
			currentVersion = ""
			continue
		}

		if strings.HasPrefix(line, "name = ") {
			currentName = unquoteTOML(strings.TrimPrefix(line, "name = "))
		} else if strings.HasPrefix(line, "version = ") {
			currentVersion = unquoteTOML(strings.TrimPrefix(line, "version = "))
		}
	}

	// Don't forget the last package block.
	if currentName != "" && currentVersion != "" {
		deps = append(deps, dependency{Name: currentName, Version: currentVersion})
	}

	return deps, scanner.Err()
}

// parsePodfileLock extracts pods from the PODS section of Podfile.lock.
func parsePodfileLock(data []byte) ([]dependency, error) {
	var deps []dependency
	re := regexp.MustCompile(`^\s+-\s+(\S+)\s+\(([^)]+)\)`)

	inPods := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.TrimSpace(line) == "PODS:" {
			inPods = true
			continue
		}

		// End of PODS section at next top-level key.
		if inPods && len(line) > 0 && line[0] != ' ' {
			break
		}

		if inPods {
			matches := re.FindStringSubmatch(line)
			if matches != nil {
				deps = append(deps, dependency{Name: matches[1], Version: matches[2]})
			}
		}
	}
	return deps, scanner.Err()
}

// unquoteTOML removes surrounding double quotes from a TOML value.
func unquoteTOML(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
