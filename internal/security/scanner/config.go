package scanner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/julianshen/rubichan/internal/security"
)

// ConfigScanner detects security misconfigurations in Dockerfiles, Kubernetes
// manifests, CI configs, and general configuration files.
type ConfigScanner struct {
	findingCounter int
	mu             sync.Mutex
}

// NewConfigScanner creates a ConfigScanner.
func NewConfigScanner() *ConfigScanner {
	return &ConfigScanner{}
}

// Name returns the scanner name.
func (s *ConfigScanner) Name() string {
	return "config"
}

// Scan walks the target files and returns findings for detected misconfigurations.
func (s *ConfigScanner) Scan(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("config scanner cancelled: %w", err)
	}

	files, err := s.collectFiles(target)
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	var findings []security.Finding
	for _, relPath := range files {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("config scanner cancelled: %w", err)
		}

		absPath := filepath.Join(target.RootDir, relPath)
		fileFindings := s.scanFile(absPath, relPath)
		findings = append(findings, fileFindings...)
	}

	return findings, nil
}

// collectFiles builds the list of relative file paths to scan.
func (s *ConfigScanner) collectFiles(target security.ScanTarget) ([]string, error) {
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

		relPath, err := filepath.Rel(target.RootDir, path)
		if err != nil {
			return nil
		}
		files = append(files, relPath)
		return nil
	})
	return files, err
}

// scanFile dispatches to the appropriate checker based on file type.
func (s *ConfigScanner) scanFile(absPath, relPath string) []security.Finding {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	var findings []security.Finding

	baseName := filepath.Base(relPath)

	// Dockerfile checks
	if strings.HasPrefix(baseName, "Dockerfile") {
		findings = append(findings, s.checkDockerfile(data, relPath)...)
	}

	// Kubernetes YAML checks
	if isKubernetesYAML(data, relPath) {
		findings = append(findings, s.checkKubernetes(data, relPath)...)
	}

	// CI config checks
	if isCIConfig(relPath) {
		findings = append(findings, s.checkCIConfig(data, relPath)...)
	}

	// General config checks (YAML, TOML, env, JSON, properties, INI)
	if isConfigFile(relPath) {
		findings = append(findings, s.checkGeneralConfig(data, relPath)...)
	}

	return findings
}

// ─── Dockerfile checks ──────────────────────────────────────────────────────

var (
	dockerUserRootPat = regexp.MustCompile(`(?im)^\s*USER\s+root\s*$`)
	dockerUserPat     = regexp.MustCompile(`(?im)^\s*USER\s+\S+`)
	dockerAddURLPat   = regexp.MustCompile(`(?im)^\s*ADD\s+https?://`)
)

func (s *ConfigScanner) checkDockerfile(data []byte, relPath string) []security.Finding {
	var findings []security.Finding
	content := string(data)

	// Check for USER root
	if dockerUserRootPat.MatchString(content) {
		line := findLineNumber(content, dockerUserRootPat)
		findings = append(findings, s.newFinding(
			"Container runs as root user",
			"Dockerfile explicitly sets USER root, which violates least privilege",
			security.SeverityMedium,
			security.CategoryMisconfiguration,
			"CWE-250",
			relPath, line,
		))
	} else if !dockerUserPat.MatchString(content) {
		// No USER directive at all
		findings = append(findings, s.newFinding(
			"Container runs as root user",
			"Dockerfile has no USER directive; container will run as root by default",
			security.SeverityMedium,
			security.CategoryMisconfiguration,
			"CWE-250",
			relPath, 1,
		))
	}

	// Check for ADD with URL
	if dockerAddURLPat.MatchString(content) {
		line := findLineNumber(content, dockerAddURLPat)
		findings = append(findings, s.newFinding(
			"Prefer COPY over ADD with URLs",
			"ADD with URLs is less transparent than using RUN curl/wget + COPY",
			security.SeverityLow,
			security.CategoryMisconfiguration,
			"",
			relPath, line,
		))
	}

	return findings
}

// ─── Kubernetes checks ──────────────────────────────────────────────────────

var (
	k8sPrivilegedPat  = regexp.MustCompile(`(?m)privileged\s*:\s*true`)
	k8sHostNetworkPat = regexp.MustCompile(`(?m)hostNetwork\s*:\s*true`)
	k8sHostPIDPat     = regexp.MustCompile(`(?m)hostPID\s*:\s*true`)
	k8sRunAsRootPat   = regexp.MustCompile(`(?m)runAsUser\s*:\s*0`)
)

func isKubernetesYAML(data []byte, relPath string) bool {
	ext := filepath.Ext(relPath)
	if ext != ".yaml" && ext != ".yml" {
		return false
	}
	content := string(data)
	return strings.Contains(content, "apiVersion:") || strings.Contains(content, "kind:")
}

func (s *ConfigScanner) checkKubernetes(data []byte, relPath string) []security.Finding {
	var findings []security.Finding
	content := string(data)

	if k8sPrivilegedPat.MatchString(content) {
		line := findLineNumber(content, k8sPrivilegedPat)
		findings = append(findings, s.newFinding(
			"Kubernetes container running in privileged mode",
			"privileged: true grants the container nearly all host capabilities",
			security.SeverityHigh,
			security.CategoryMisconfiguration,
			"CWE-250",
			relPath, line,
		))
	}

	if k8sHostNetworkPat.MatchString(content) {
		line := findLineNumber(content, k8sHostNetworkPat)
		findings = append(findings, s.newFinding(
			"Kubernetes pod uses host network",
			"hostNetwork: true exposes the pod to the host network stack",
			security.SeverityMedium,
			security.CategoryMisconfiguration,
			"",
			relPath, line,
		))
	}

	if k8sHostPIDPat.MatchString(content) {
		line := findLineNumber(content, k8sHostPIDPat)
		findings = append(findings, s.newFinding(
			"Kubernetes pod uses host PID namespace",
			"hostPID: true allows the pod to see host processes",
			security.SeverityMedium,
			security.CategoryMisconfiguration,
			"",
			relPath, line,
		))
	}

	if k8sRunAsRootPat.MatchString(content) {
		line := findLineNumber(content, k8sRunAsRootPat)
		findings = append(findings, s.newFinding(
			"Kubernetes container runs as root",
			"runAsUser: 0 runs the container process as root",
			security.SeverityMedium,
			security.CategoryMisconfiguration,
			"CWE-250",
			relPath, line,
		))
	}

	return findings
}

// ─── CI config checks ───────────────────────────────────────────────────────

var ciSecretPat = regexp.MustCompile(`(?im)(?:password|secret|token|api_key|apikey)\s*[:=]\s*["']?[^\s"'${}]+["']?`)

// ciSecretExcludePat matches values that are variable references or placeholders,
// not actual secrets.
var ciSecretExcludePat = regexp.MustCompile(`(?i)(\$\{|\$\(|secrets\.|<|YOUR_|CHANGE_?ME|example|placeholder)`)

func isCIConfig(relPath string) bool {
	normalized := filepath.ToSlash(relPath)
	if strings.HasPrefix(normalized, ".github/workflows/") && strings.HasSuffix(normalized, ".yml") {
		return true
	}
	if normalized == ".gitlab-ci.yml" {
		return true
	}
	if normalized == ".circleci/config.yml" {
		return true
	}
	return false
}

func (s *ConfigScanner) checkCIConfig(data []byte, relPath string) []security.Finding {
	var findings []security.Finding

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if ciSecretPat.MatchString(line) {
			// Extract the value part to check if it's a variable reference.
			value := extractConfigValue(line)
			if value != "" && !ciSecretExcludePat.MatchString(value) {
				findings = append(findings, s.newFinding(
					"Potential secret in CI configuration",
					fmt.Sprintf("Plain text secret/token/password found in CI config at line %d", lineNum),
					security.SeverityHigh,
					security.CategorySecretsExposure,
					"CWE-798",
					relPath, lineNum,
				))
			}
		}
	}

	return findings
}

// extractConfigValue extracts the value portion from a key=value or key: value line.
func extractConfigValue(line string) string {
	line = strings.TrimSpace(line)

	// Try key: value
	if idx := strings.Index(line, ":"); idx >= 0 {
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, "\"'")
		return val
	}

	// Try key=value
	if idx := strings.Index(line, "="); idx >= 0 {
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, "\"'")
		return val
	}

	return ""
}

// ─── General config checks ──────────────────────────────────────────────────

var (
	debugModePat   = regexp.MustCompile(`(?im)(debug\s*[:=]\s*true|DEBUG\s*=\s*true)`)
	permissiveCORS = regexp.MustCompile(`(?im)(Access-Control-Allow-Origin\s*[:=]\s*["']?\*|cors\s*[:=]\s*["']?\*)`)
)

func isConfigFile(relPath string) bool {
	ext := filepath.Ext(relPath)
	switch ext {
	case ".yaml", ".yml", ".toml", ".json", ".env", ".ini", ".conf", ".cfg", ".properties":
		return true
	}
	baseName := filepath.Base(relPath)
	if strings.HasPrefix(baseName, ".env") {
		return true
	}
	return false
}

func (s *ConfigScanner) checkGeneralConfig(data []byte, relPath string) []security.Finding {
	var findings []security.Finding
	content := string(data)

	if debugModePat.MatchString(content) {
		line := findLineNumber(content, debugModePat)
		findings = append(findings, s.newFinding(
			"Debug mode enabled in configuration",
			"Debug mode may expose sensitive information in production",
			security.SeverityLow,
			security.CategoryMisconfiguration,
			"",
			relPath, line,
		))
	}

	if permissiveCORS.MatchString(content) {
		line := findLineNumber(content, permissiveCORS)
		findings = append(findings, s.newFinding(
			"Permissive CORS configuration",
			"Access-Control-Allow-Origin: * allows any domain to make requests",
			security.SeverityMedium,
			security.CategoryMisconfiguration,
			"",
			relPath, line,
		))
	}

	return findings
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// findLineNumber returns the 1-indexed line number of the first regex match.
func findLineNumber(content string, pat *regexp.Regexp) int {
	loc := pat.FindStringIndex(content)
	if loc == nil {
		return 1
	}
	return strings.Count(content[:loc[0]], "\n") + 1
}

func (s *ConfigScanner) newFinding(title, description string, severity security.Severity, category security.Category, cwe, file string, line int) security.Finding {
	s.mu.Lock()
	s.findingCounter++
	id := fmt.Sprintf("CFG-%04d", s.findingCounter)
	s.mu.Unlock()

	return security.Finding{
		ID:          id,
		Scanner:     "config",
		Severity:    severity,
		Category:    category,
		Title:       title,
		Description: description,
		Location: security.Location{
			File:      file,
			StartLine: line,
			EndLine:   line,
		},
		CWE:        cwe,
		Confidence: security.ConfidenceHigh,
	}
}
