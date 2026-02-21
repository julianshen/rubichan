package security

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/julianshen/rubichan/internal/parser"
)

// riskSignal defines a keyword pattern and its associated risk score for prioritization.
type riskSignal struct {
	pattern *regexp.Regexp
	score   int
}

// riskSignals defines the risk scoring rules for file content analysis.
// Each signal represents a security-relevant code pattern and its risk score.
var riskSignals = []riskSignal{
	{pattern: regexp.MustCompile(`(?i)(auth|login|jwt|session|password|credential)`), score: 10},
	{pattern: regexp.MustCompile(`(?i)(os/exec|exec\.Command|os\.system|subprocess)`), score: 9},
	{pattern: regexp.MustCompile(`(?i)(http\.Handler|http\.Request|gin\.Context|echo\.Context|request\.form|request\.args)`), score: 8},
	{pattern: regexp.MustCompile(`(?i)(database/sql|db\.Query|db\.Exec|\.execute\(|orm|gorm)`), score: 7},
	{pattern: regexp.MustCompile(`(?i)(crypto/|cipher|encrypt|decrypt|hmac|hash)`), score: 7},
	{pattern: regexp.MustCompile(`(?i)(keychain|SecItem|security\.framework)`), score: 6},
	{pattern: regexp.MustCompile(`(?i)(os\.Open|os\.ReadFile|os\.WriteFile|ioutil\.ReadFile)`), score: 5},
	{pattern: regexp.MustCompile(`(?i)(net/http|URLSession|WKWebView|fetch\(|axios)`), score: 5},
}

// staticFindingBoost is the score added when a file already appears in static findings.
const staticFindingBoost = 3

// PrioritizerConfig controls the behavior of the Prioritizer.
type PrioritizerConfig struct {
	MinRiskScore int
	MaxChunks    int
}

// Prioritizer scores source files by risk signals and produces prioritized
// AnalysisChunks for LLM analysis. Files with higher risk scores (auth code,
// exec, SQL, etc.) are prioritized. The output is capped by MaxChunks and
// filtered by MinRiskScore.
type Prioritizer struct {
	config PrioritizerConfig
}

// NewPrioritizer creates a new Prioritizer with the given configuration.
func NewPrioritizer(config PrioritizerConfig) *Prioritizer {
	return &Prioritizer{config: config}
}

// Prioritize walks the scan target, scores files by risk signals, splits them
// into function-level chunks using tree-sitter, and returns the highest-risk
// chunks up to the configured budget.
func (p *Prioritizer) Prioritize(ctx context.Context, target ScanTarget, staticFindings []Finding) ([]AnalysisChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("prioritizer cancelled: %w", err)
	}

	files, err := CollectFiles(target, nil)
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	flaggedFiles := buildFlaggedFileSet(staticFindings)
	codeParser := parser.NewParser()

	var chunks []AnalysisChunk
	for _, relPath := range files {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prioritizer cancelled: %w", err)
		}

		absPath := filepath.Join(target.RootDir, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue // skip unreadable files
		}

		score := p.scoreFile(string(data))
		if flaggedFiles[relPath] {
			score += staticFindingBoost
		}

		language := extensionToLanguage(relPath)
		fileChunks := p.splitIntoChunks(codeParser, relPath, data, score, language)
		chunks = append(chunks, fileChunks...)
	}

	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].RiskScore > chunks[j].RiskScore
	})

	chunks = p.filterByMinScore(chunks)
	chunks = p.capChunks(chunks)

	return chunks, nil
}

// scoreFile calculates the aggregate risk score for a file based on keyword signals.
func (p *Prioritizer) scoreFile(content string) int {
	score := 0
	for _, signal := range riskSignals {
		if signal.pattern.MatchString(content) {
			score += signal.score
		}
	}
	return score
}

// splitIntoChunks splits a file into function-level AnalysisChunks using tree-sitter.
// Falls back to a single whole-file chunk when parsing is unsupported or fails.
func (p *Prioritizer) splitIntoChunks(codeParser *parser.Parser, relPath string, data []byte, score int, language string) []AnalysisChunk {
	tree, err := codeParser.Parse(relPath, data)
	if err != nil {
		return []AnalysisChunk{p.wholeFileChunk(relPath, data, score, language)}
	}
	defer tree.Close()

	funcs := tree.Functions()
	if len(funcs) == 0 {
		return []AnalysisChunk{p.wholeFileChunk(relPath, data, score, language)}
	}

	lines := strings.Split(string(data), "\n")
	var chunks []AnalysisChunk
	for _, fn := range funcs {
		start := fn.StartLine
		end := fn.EndLine
		if start < 1 {
			start = 1
		}
		if end > len(lines) {
			end = len(lines)
		}

		content := strings.Join(lines[start-1:end], "\n")
		chunks = append(chunks, AnalysisChunk{
			File:      relPath,
			StartLine: start,
			EndLine:   end,
			Content:   content,
			Language:  language,
			RiskScore: score,
		})
	}
	return chunks
}

// wholeFileChunk creates a single AnalysisChunk spanning the entire file.
func (p *Prioritizer) wholeFileChunk(relPath string, data []byte, score int, language string) AnalysisChunk {
	lineCount := strings.Count(string(data), "\n") + 1
	return AnalysisChunk{
		File:      relPath,
		StartLine: 1,
		EndLine:   lineCount,
		Content:   string(data),
		Language:  language,
		RiskScore: score,
	}
}

// filterByMinScore removes chunks below the minimum risk score threshold.
func (p *Prioritizer) filterByMinScore(chunks []AnalysisChunk) []AnalysisChunk {
	if p.config.MinRiskScore <= 0 {
		return chunks
	}
	var filtered []AnalysisChunk
	for _, c := range chunks {
		if c.RiskScore >= p.config.MinRiskScore {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// capChunks limits the number of chunks to the configured maximum.
func (p *Prioritizer) capChunks(chunks []AnalysisChunk) []AnalysisChunk {
	if p.config.MaxChunks > 0 && len(chunks) > p.config.MaxChunks {
		return chunks[:p.config.MaxChunks]
	}
	return chunks
}

// buildFlaggedFileSet creates a set of file paths that appear in static findings.
func buildFlaggedFileSet(findings []Finding) map[string]bool {
	flagged := make(map[string]bool, len(findings))
	for _, f := range findings {
		if f.Location.File != "" {
			flagged[f.Location.File] = true
		}
	}
	return flagged
}

// extensionToLanguage maps a file path's extension to a language name.
func extensionToLanguage(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescript"
	case ".jsx":
		return "javascript"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp":
		return "cpp"
	default:
		return ""
	}
}
