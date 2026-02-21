package scanner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepScannerName(t *testing.T) {
	s := NewDepScanner(nil)
	assert.Equal(t, "dependency-audit", s.Name())
}

func TestDepScannerInterface(t *testing.T) {
	var _ security.StaticScanner = NewDepScanner(nil)
}

func TestDepScannerParsesGoSum(t *testing.T) {
	// Mock OSV server that returns a vulnerability for "golang.org/x/text".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req osvQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Package.Name == "golang.org/x/text" {
			resp := osvQueryResponse{
				Vulns: []osvVuln{
					{
						ID:      "GO-2022-1059",
						Summary: "Denial of service via crafted Accept-Language header",
						Severity: []osvSeverity{
							{Type: "CVSS_V3", Score: "7.5"},
						},
						Affected: []osvAffected{
							{
								Package: osvAffectedPackage{
									Name:      "golang.org/x/text",
									Ecosystem: "Go",
								},
								Ranges: []osvRange{
									{
										Type: "SEMVER",
										Events: []osvEvent{
											{Introduced: "0"},
											{Fixed: "0.3.8"},
										},
									},
								},
							},
						},
						References: []osvReference{
							{Type: "ADVISORY", URL: "https://nvd.nist.gov/vuln/detail/CVE-2022-32149"},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		// No vulns for other packages.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(osvQueryResponse{})
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "go.sum", `golang.org/x/text v0.3.7 h1:olpwvP2KacW1ZWvsR7uQhoyTYvKAupfQrRGBFM352Gk=
golang.org/x/text v0.3.7/go.mod h1:u+2+/6zg+i71rQMx5EYifcz6MCKuco9NR6JIITiCfzQ=
golang.org/x/net v0.0.0-20220722155237-a158d28d115b h1:PxfKdU9lEEDYjdIzOtC4qFWgkU2rGHdKlKowJSMN9Y=
golang.org/x/net v0.0.0-20220722155237-a158d28d115b/go.mod h1:XRhObCWvk6IyKnWLug+ECip1KBveYUHfp+8e9klMJ9c=
`)

	s := NewDepScanner(srv.Client())
	s.OSVBaseURL = srv.URL

	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	// Should find the vulnerability for golang.org/x/text.
	found := false
	for _, f := range findings {
		if f.Category == security.CategoryVulnerableDep && f.Metadata["vuln_id"] == "GO-2022-1059" {
			found = true
			assert.Equal(t, "dependency-audit", f.Scanner)
			assert.Equal(t, "CWE-1035", f.CWE)
			assert.Contains(t, f.Title, "golang.org/x/text")
		}
	}
	assert.True(t, found, "expected a finding for golang.org/x/text vulnerability")
}

func TestDepScannerParsesPackageLock(t *testing.T) {
	// Mock OSV server that returns no vulnerabilities.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(osvQueryResponse{})
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "package-lock.json", `{
  "name": "my-app",
  "version": "1.0.0",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "name": "my-app",
      "version": "1.0.0"
    },
    "node_modules/express": {
      "version": "4.18.2",
      "resolved": "https://registry.npmjs.org/express/-/express-4.18.2.tgz"
    },
    "node_modules/lodash": {
      "version": "4.17.21",
      "resolved": "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"
    }
  }
}`)

	s := NewDepScanner(srv.Client())
	s.OSVBaseURL = srv.URL

	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings, "expected no findings when OSV returns no vulns")
}

func TestDepScannerHandlesOSVUnavailable(t *testing.T) {
	// Mock server that returns 503.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "go.sum", `golang.org/x/text v0.3.7 h1:olpwvP2KacW1ZWvsR7uQhoyTYvKAupfQrRGBFM352Gk=
golang.org/x/text v0.3.7/go.mod h1:u+2+/6zg+i71rQMx5EYifcz6MCKuco9NR6JIITiCfzQ=
`)

	s := NewDepScanner(srv.Client())
	s.OSVBaseURL = srv.URL

	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	// Should return an Info-severity finding about OSV being unavailable.
	f := findings[0]
	assert.Equal(t, security.SeverityInfo, f.Severity)
	assert.Contains(t, f.Title, "OSV API unavailable")
}

func TestDepScannerNoLockfiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main
func main() {}
`)

	s := NewDepScanner(http.DefaultClient)
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDepScannerNilClient(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.sum", `golang.org/x/text v0.3.7 h1:olpwvP2KacW1ZWvsR7uQhoyTYvKAupfQrRGBFM352Gk=
golang.org/x/text v0.3.7/go.mod h1:u+2+/6zg+i71rQMx5EYifcz6MCKuco9NR6JIITiCfzQ=
`)

	// nil client means no OSV queries — just parse lockfiles.
	s := NewDepScanner(nil)
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings, "nil client should skip OSV queries, producing no vulnerability findings")
}

func TestDepScannerParsesRequirementsTxt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req osvQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Package.Name == "django" {
			resp := osvQueryResponse{
				Vulns: []osvVuln{
					{
						ID:      "PYSEC-2023-100",
						Summary: "SQL injection in Django ORM",
						Severity: []osvSeverity{
							{Type: "CVSS_V3", Score: "9.8"},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(osvQueryResponse{})
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", `django==3.2.0
requests==2.28.1
# this is a comment
flask==2.3.0
`)

	s := NewDepScanner(srv.Client())
	s.OSVBaseURL = srv.URL

	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Metadata["vuln_id"] == "PYSEC-2023-100" {
			found = true
			assert.Contains(t, f.Title, "django")
		}
	}
	assert.True(t, found, "expected a finding for django vulnerability")
}

func TestDepScannerParsesGemfileLock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(osvQueryResponse{})
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "Gemfile.lock", `GEM
  remote: https://rubygems.org/
  specs:
    actioncable (7.0.4)
      actionpack (= 7.0.4)
    actionpack (7.0.4)
      rack (~> 2.2)
    rack (2.2.6)

PLATFORMS
  ruby

DEPENDENCIES
  actioncable (~> 7.0)
`)

	s := NewDepScanner(srv.Client())
	s.OSVBaseURL = srv.URL

	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// No vulns returned by mock, so no findings.
	assert.Empty(t, findings)
}

func TestDepScannerParsesCargoLock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(osvQueryResponse{})
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "Cargo.lock", `# This file is automatically generated by Cargo.
[[package]]
name = "serde"
version = "1.0.152"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "tokio"
version = "1.25.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
`)

	s := NewDepScanner(srv.Client())
	s.OSVBaseURL = srv.URL

	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDepScannerParsesPodfileLock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(osvQueryResponse{})
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "Podfile.lock", `PODS:
  - Alamofire (5.6.4)
  - SwiftyJSON (5.0.1)

DEPENDENCIES:
  - Alamofire (~> 5.6)
  - SwiftyJSON (~> 5.0)

SPEC REPOS:
  trunk:
    - Alamofire
    - SwiftyJSON
`)

	s := NewDepScanner(srv.Client())
	s.OSVBaseURL = srv.URL

	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDepScannerPackageLockV1Dependencies(t *testing.T) {
	// Test parsing the older lockfileVersion 1 format with "dependencies" field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(osvQueryResponse{})
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "package-lock.json", `{
  "name": "my-app",
  "version": "1.0.0",
  "lockfileVersion": 1,
  "dependencies": {
    "express": {
      "version": "4.18.2",
      "resolved": "https://registry.npmjs.org/express/-/express-4.18.2.tgz"
    },
    "lodash": {
      "version": "4.17.21",
      "resolved": "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"
    }
  }
}`)

	s := NewDepScanner(srv.Client())
	s.OSVBaseURL = srv.URL

	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDepScannerContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.sum", `golang.org/x/text v0.3.7 h1:olpwvP2KacW1ZWvsR7uQhoyTYvKAupfQrRGBFM352Gk=
`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewDepScanner(http.DefaultClient)
	_, err := s.Scan(ctx, security.ScanTarget{RootDir: dir})
	assert.Error(t, err)
}

func TestDepScannerClassifyOSVSeverityAllRanges(t *testing.T) {
	tests := []struct {
		name     string
		score    string
		expected security.Severity
	}{
		{"critical", "9.8", security.SeverityCritical},
		{"critical_exact", "9.0", security.SeverityCritical},
		{"high", "7.5", security.SeverityHigh},
		{"high_exact", "7.0", security.SeverityHigh},
		{"medium", "5.0", security.SeverityMedium},
		{"medium_exact", "4.0", security.SeverityMedium},
		{"low", "3.9", security.SeverityLow},
		{"low_zero", "0.0", security.SeverityLow},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vuln := osvVuln{
				Severity: []osvSeverity{{Type: "CVSS_V3", Score: tc.score}},
			}
			assert.Equal(t, tc.expected, classifyOSVSeverity(vuln))
		})
	}
}

func TestDepScannerClassifyOSVSeverityNoCVSS(t *testing.T) {
	// No severity entries at all -> default to Medium.
	vuln := osvVuln{}
	assert.Equal(t, security.SeverityMedium, classifyOSVSeverity(vuln))

	// Non-CVSS_V3 severity type -> should not match, default to Medium.
	vuln2 := osvVuln{
		Severity: []osvSeverity{{Type: "CVSS_V2", Score: "9.0"}},
	}
	assert.Equal(t, security.SeverityMedium, classifyOSVSeverity(vuln2))
}

func TestDepScannerUnquoteTOMLVariants(t *testing.T) {
	// Quoted value.
	assert.Equal(t, "serde", unquoteTOML(`"serde"`))
	// Unquoted value (no surrounding quotes).
	assert.Equal(t, "serde", unquoteTOML("serde"))
	// With whitespace.
	assert.Equal(t, "tokio", unquoteTOML(`  "tokio"  `))
	// Empty string.
	assert.Equal(t, "", unquoteTOML(`""`))
	// Single char (not enough for quotes).
	assert.Equal(t, "x", unquoteTOML("x"))
}

func TestDepScannerGoSumEdgeCases(t *testing.T) {
	// Test with empty lines, short lines, duplicates.
	data := []byte(`
golang.org/x/text v0.3.7 h1:abc=

short
golang.org/x/text v0.3.7 h1:abc=
golang.org/x/net v0.1.0 h1:xyz=
`)
	deps, err := parseGoSum(data)
	require.NoError(t, err)
	// Should have 2 unique deps (text and net), with text deduplicated.
	assert.Len(t, deps, 2)
}

func TestDepScannerQueryOSVInvalidJSON(t *testing.T) {
	// Mock server that returns invalid JSON body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "go.sum", `golang.org/x/text v0.3.7 h1:abc=
`)

	s := NewDepScanner(srv.Client())
	s.OSVBaseURL = srv.URL

	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// Should get an info finding about OSV API error.
	require.NotEmpty(t, findings)
	assert.Contains(t, findings[0].Title, "OSV API unavailable")
}

func TestDepScannerPackageLockEmptyVersion(t *testing.T) {
	// Test that packages with empty version are skipped.
	data := []byte(`{
		"packages": {
			"": {"version": ""},
			"node_modules/express": {"version": "4.18.2"},
			"node_modules/empty": {"version": ""}
		}
	}`)
	deps, err := parsePackageLock(data)
	require.NoError(t, err)
	assert.Len(t, deps, 1)
	assert.Equal(t, "express", deps[0].Name)
}
