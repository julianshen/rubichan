package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/julianshen/rubichan/internal/parser"
	"github.com/julianshen/rubichan/internal/security"
)

// sastPattern defines a single SAST detection pattern for a specific language.
type sastPattern struct {
	name       string
	language   string
	pattern    *regexp.Regexp
	severity   security.Severity
	category   security.Category
	cwe        string
	title      string
	confidence security.Confidence
}

// SASTScanner detects common security vulnerabilities in source code using
// tree-sitter AST parsing combined with regex pattern matching on function bodies.
type SASTScanner struct {
	parser         *parser.Parser
	patterns       []sastPattern
	findingCounter int
	mu             sync.Mutex
}

// NewSASTScanner creates a SASTScanner with the standard detection patterns.
func NewSASTScanner() *SASTScanner {
	return &SASTScanner{
		parser:   parser.NewParser(),
		patterns: defaultSASTPatterns(),
	}
}

// Name returns the scanner name.
func (s *SASTScanner) Name() string {
	return "sast"
}

// Scan walks the target files, parses supported source files with tree-sitter,
// and applies regex-based pattern matching on function bodies to detect vulnerabilities.
func (s *SASTScanner) Scan(ctx context.Context, target security.ScanTarget) ([]security.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sast scanner cancelled: %w", err)
	}

	files, err := security.CollectFiles(target, []string{".go", ".py", ".js", ".ts", ".tsx", ".jsx"})
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	var findings []security.Finding
	for _, relPath := range files {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("sast scanner cancelled: %w", err)
		}

		absPath := filepath.Join(target.RootDir, relPath)
		fileFindings := s.scanFile(absPath, relPath)
		findings = append(findings, fileFindings...)
	}

	return findings, nil
}

// supportedExtensions lists file extensions the SAST scanner can analyze.
var supportedExtensions = map[string]string{
	".go":  "go",
	".py":  "python",
	".js":  "javascript",
	".ts":  "typescript",
	".tsx": "typescript",
	".jsx": "javascript",
}

// scanFile parses a single source file and applies SAST patterns to its function bodies.
func (s *SASTScanner) scanFile(absPath, relPath string) []security.Finding {
	source, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	ext := filepath.Ext(relPath)
	lang, ok := supportedExtensions[ext]
	if !ok {
		return nil
	}

	tree, err := s.parser.Parse(relPath, source)
	if err != nil {
		return nil
	}
	defer tree.Close()

	// Check import-level patterns (e.g., weak crypto imports in Go).
	var findings []security.Finding
	findings = append(findings, s.checkImports(tree, source, relPath, lang)...)

	// Get function boundaries and check each function body.
	funcs := tree.Functions()
	lines := strings.Split(string(source), "\n")

	for _, fn := range funcs {
		// Extract function body text (0-indexed lines).
		startIdx := fn.StartLine - 1
		endIdx := fn.EndLine
		if startIdx < 0 {
			startIdx = 0
		}
		if endIdx > len(lines) {
			endIdx = len(lines)
		}
		body := strings.Join(lines[startIdx:endIdx], "\n")

		for _, pat := range s.patterns {
			if pat.language != lang {
				continue
			}
			// Skip import-level patterns handled separately.
			if pat.name == "go-weak-crypto" {
				continue
			}

			if pat.pattern.MatchString(body) {
				// Find the specific line within the function body.
				matchLine := s.findMatchLine(lines, startIdx, endIdx, pat.pattern)
				findings = append(findings, s.newFinding(pat, relPath, matchLine, fn.Name))
			}
		}
	}

	return findings
}

// checkImports checks for import-level patterns like weak crypto imports.
func (s *SASTScanner) checkImports(tree *parser.Tree, source []byte, relPath, lang string) []security.Finding {
	var findings []security.Finding
	imports := tree.Imports()
	lines := strings.Split(string(source), "\n")

	for _, imp := range imports {
		for _, pat := range s.patterns {
			if pat.language != lang || pat.name != "go-weak-crypto" {
				continue
			}
			if pat.pattern.MatchString(imp) {
				// Find the line number of this import.
				lineNum := 1
				for i, line := range lines {
					if strings.Contains(line, imp) {
						lineNum = i + 1
						break
					}
				}
				findings = append(findings, s.newFinding(pat, relPath, lineNum, ""))
			}
		}
	}
	return findings
}

// findMatchLine locates the first line within a range that matches the pattern.
func (s *SASTScanner) findMatchLine(lines []string, startIdx, endIdx int, pattern *regexp.Regexp) int {
	for i := startIdx; i < endIdx && i < len(lines); i++ {
		if pattern.MatchString(lines[i]) {
			return i + 1 // 1-indexed
		}
	}
	return startIdx + 1 // fallback to function start
}

// newFinding creates a properly formatted Finding from a matched SAST pattern.
func (s *SASTScanner) newFinding(pat sastPattern, file string, line int, funcName string) security.Finding {
	s.mu.Lock()
	s.findingCounter++
	id := fmt.Sprintf("SAST-%04d", s.findingCounter)
	s.mu.Unlock()

	return security.Finding{
		ID:       id,
		Scanner:  "sast",
		Severity: pat.severity,
		Category: pat.category,
		Title:    pat.title,
		Description: fmt.Sprintf(
			"%s found at %s:%d",
			pat.title, file, line,
		),
		Location: security.Location{
			File:      file,
			StartLine: line,
			EndLine:   line,
			Function:  funcName,
		},
		CWE:        pat.cwe,
		Confidence: pat.confidence,
	}
}

// defaultSASTPatterns returns the standard set of detection patterns.
func defaultSASTPatterns() []sastPattern {
	return []sastPattern{
		// Go: SQL injection - string concatenation in db.Query/db.Exec/db.QueryRow
		{
			name:       "go-sql-injection",
			language:   "go",
			pattern:    regexp.MustCompile(`db\.(Query|Exec|QueryRow)\s*\([^)]*\+`),
			severity:   security.SeverityHigh,
			category:   security.CategoryInjection,
			cwe:        "CWE-89",
			title:      "Potential SQL injection via string concatenation",
			confidence: security.ConfidenceMedium,
		},
		// Go: Command injection - exec.Command with shell invocation
		{
			name:       "go-command-injection",
			language:   "go",
			pattern:    regexp.MustCompile(`exec\.Command\s*\(\s*"sh"\s*,\s*"-c"\s*,`),
			severity:   security.SeverityHigh,
			category:   security.CategoryInjection,
			cwe:        "CWE-78",
			title:      "Potential command injection via exec.Command with shell",
			confidence: security.ConfidenceMedium,
		},
		// Go: Weak crypto - import of known weak algorithms
		{
			name:       "go-weak-crypto",
			language:   "go",
			pattern:    regexp.MustCompile(`crypto/(md5|sha1|des|rc4)`),
			severity:   security.SeverityMedium,
			category:   security.CategoryCryptography,
			cwe:        "CWE-327",
			title:      "Use of weak cryptographic algorithm",
			confidence: security.ConfidenceHigh,
		},
		// Go: Path traversal - os.Open/os.ReadFile with variable
		{
			name:       "go-path-traversal",
			language:   "go",
			pattern:    regexp.MustCompile(`os\.(Open|ReadFile)\s*\(\s*[a-z]`),
			severity:   security.SeverityMedium,
			category:   security.CategoryInputValidation,
			cwe:        "CWE-22",
			title:      "Potential path traversal via user-controlled file path",
			confidence: security.ConfidenceLow,
		},
		// Python: SQL injection - string concatenation with .execute(
		{
			name:       "python-sql-injection",
			language:   "python",
			pattern:    regexp.MustCompile(`\.execute\s*\([^)]*\+`),
			severity:   security.SeverityHigh,
			category:   security.CategoryInjection,
			cwe:        "CWE-89",
			title:      "Potential SQL injection via string concatenation",
			confidence: security.ConfidenceMedium,
		},
		// Python: Command injection - os.system(variable) or subprocess with shell=True
		{
			name:       "python-command-injection",
			language:   "python",
			pattern:    regexp.MustCompile(`(os\.system\s*\(\s*[a-z]|subprocess\.\w+\s*\([^)]*shell\s*=\s*True)`),
			severity:   security.SeverityHigh,
			category:   security.CategoryInjection,
			cwe:        "CWE-78",
			title:      "Potential command injection via shell execution",
			confidence: security.ConfidenceMedium,
		},
		// JavaScript: XSS - innerHTML or document.write with variable
		{
			name:       "js-xss",
			language:   "javascript",
			pattern:    regexp.MustCompile(`(\.innerHTML\s*=\s*[a-z]|document\.write\s*\(\s*[a-z])`),
			severity:   security.SeverityHigh,
			category:   security.CategoryInjection,
			cwe:        "CWE-79",
			title:      "Potential XSS via unsafe DOM manipulation",
			confidence: security.ConfidenceMedium,
		},
		// TypeScript: XSS - same pattern
		{
			name:       "ts-xss",
			language:   "typescript",
			pattern:    regexp.MustCompile(`(\.innerHTML\s*=\s*[a-z]|document\.write\s*\(\s*[a-z])`),
			severity:   security.SeverityHigh,
			category:   security.CategoryInjection,
			cwe:        "CWE-79",
			title:      "Potential XSS via unsafe DOM manipulation",
			confidence: security.ConfidenceMedium,
		},
		// JavaScript: SQL injection - string concatenation with .query(
		{
			name:       "js-sql-injection",
			language:   "javascript",
			pattern:    regexp.MustCompile(`\.query\s*\([^)]*\+`),
			severity:   security.SeverityHigh,
			category:   security.CategoryInjection,
			cwe:        "CWE-89",
			title:      "Potential SQL injection via string concatenation",
			confidence: security.ConfidenceMedium,
		},
		// TypeScript: SQL injection - same pattern
		{
			name:       "ts-sql-injection",
			language:   "typescript",
			pattern:    regexp.MustCompile(`\.query\s*\([^)]*\+`),
			severity:   security.SeverityHigh,
			category:   security.CategoryInjection,
			cwe:        "CWE-89",
			title:      "Potential SQL injection via string concatenation",
			confidence: security.ConfidenceMedium,
		},
	}
}
