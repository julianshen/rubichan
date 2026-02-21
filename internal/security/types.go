package security

import (
	"context"
	"fmt"
	"time"
)

// Severity represents the severity level of a security finding.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// SeverityRank returns a numeric rank for ordering severities.
// Critical=5, High=4, Medium=3, Low=2, Info=1. Unknown severities return 0.
func SeverityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// Confidence represents the confidence level of a security finding.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Category represents a security finding category.
type Category string

const (
	CategoryInjection         Category = "injection"
	CategoryAuthentication    Category = "authentication"
	CategoryAuthorization     Category = "authorization"
	CategoryCryptography      Category = "cryptography"
	CategorySecretsExposure   Category = "secrets-exposure"
	CategoryVulnerableDep     Category = "vulnerable-dependency"
	CategoryMisconfiguration  Category = "misconfiguration"
	CategoryDataExposure      Category = "data-exposure"
	CategoryRaceCondition     Category = "race-condition"
	CategoryInputValidation   Category = "input-validation"
	CategoryLoggingMonitoring Category = "logging-monitoring"
	CategorySupplyChain       Category = "supply-chain"
	CategoryLicenseCompliance Category = "license-compliance"
)

// AllCategories returns all 13 defined security finding categories.
func AllCategories() []Category {
	return []Category{
		CategoryInjection,
		CategoryAuthentication,
		CategoryAuthorization,
		CategoryCryptography,
		CategorySecretsExposure,
		CategoryVulnerableDep,
		CategoryMisconfiguration,
		CategoryDataExposure,
		CategoryRaceCondition,
		CategoryInputValidation,
		CategoryLoggingMonitoring,
		CategorySupplyChain,
		CategoryLicenseCompliance,
	}
}

// Location identifies a specific position in source code.
type Location struct {
	File      string
	StartLine int
	EndLine   int
	Function  string
}

// Finding represents a single security finding produced by a scanner or analyzer.
type Finding struct {
	ID          string
	Scanner     string
	Severity    Severity
	Category    Category
	Title       string
	Description string
	Location    Location
	CWE         string
	OWASP       string
	Evidence    string
	Remediation string
	Confidence  Confidence
	References  []string
	Metadata    map[string]string
	SkillSource string
}

// AttackChain represents a sequence of findings that together form a
// multi-step attack path.
type AttackChain struct {
	ID         string
	Title      string
	Severity   Severity
	Steps      []Finding
	Impact     string
	Likelihood string
}

// ScanTarget describes what should be scanned.
type ScanTarget struct {
	RootDir         string
	Files           []string
	ExcludePatterns []string
}

// AnalysisChunk is a segment of source code sent to an LLM analyzer.
type AnalysisChunk struct {
	File      string
	StartLine int
	EndLine   int
	Content   string
	Language  string
	RiskScore int
}

// ScanError records an error encountered during scanning.
type ScanError struct {
	Scanner string
	Err     error
	Fatal   bool
}

// Error implements the error interface for ScanError.
func (e ScanError) Error() string {
	if e.Fatal {
		return fmt.Sprintf("fatal error in %s: %s", e.Scanner, e.Err)
	}
	return fmt.Sprintf("error in %s: %s", e.Scanner, e.Err)
}

// ScanStats holds timing and count metrics for a completed scan.
type ScanStats struct {
	Duration       time.Duration
	FilesScanned   int
	ChunksAnalyzed int
	FindingsCount  int
	ChainCount     int
}

// ReportSummary provides aggregate counts of findings by severity.
type ReportSummary struct {
	Critical int
	High     int
	Medium   int
	Low      int
	Info     int
	Chains   int
	Total    int
}

// Report is the top-level result of a security scan.
type Report struct {
	Findings     []Finding
	AttackChains []AttackChain
	Stats        ScanStats
	Errors       []ScanError
}

// Summary computes aggregate counts from the report's findings and chains.
func (r *Report) Summary() ReportSummary {
	var s ReportSummary
	for _, f := range r.Findings {
		switch f.Severity {
		case SeverityCritical:
			s.Critical++
		case SeverityHigh:
			s.High++
		case SeverityMedium:
			s.Medium++
		case SeverityLow:
			s.Low++
		case SeverityInfo:
			s.Info++
		}
	}
	s.Chains = len(r.AttackChains)
	s.Total = len(r.Findings)
	return s
}

// StaticScanner is the interface for fast, pattern-based security scanners
// that run in the first phase of the two-phase security engine.
type StaticScanner interface {
	Name() string
	Scan(ctx context.Context, target ScanTarget) ([]Finding, error)
}

// LLMAnalyzer is the interface for LLM-powered security analyzers that run
// in the second phase on prioritized code segments.
type LLMAnalyzer interface {
	Name() string
	Category() Category
	Analyze(ctx context.Context, chunks []AnalysisChunk) ([]Finding, error)
}

// EngineConfig controls the behavior of the security engine.
type EngineConfig struct {
	MaxLLMChunks    int      // maximum number of chunks to send to LLM analyzers
	MinRiskScore    int      // minimum risk score for a chunk to be analyzed
	ExcludePatterns []string // file patterns to exclude from scanning
	Concurrency     int      // maximum concurrent scanner/analyzer goroutines
}

// OutputFormatter is the interface for rendering a security report into a
// specific output format (e.g., JSON, SARIF, Markdown).
type OutputFormatter interface {
	Name() string
	Format(report *Report) ([]byte, error)
}
