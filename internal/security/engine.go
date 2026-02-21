package security

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sourcegraph/conc/pool"
)

// Engine orchestrates the two-phase security pipeline: static scanners
// followed by LLM-powered analyzers on prioritized code segments.
type Engine struct {
	config    EngineConfig
	scanners  []StaticScanner
	analyzers []LLMAnalyzer
}

// NewEngine creates a new Engine with the given configuration.
func NewEngine(config EngineConfig) *Engine {
	if config.Concurrency <= 0 {
		config.Concurrency = 1
	}
	return &Engine{config: config}
}

// AddScanner registers a static scanner for phase 1.
func (e *Engine) AddScanner(s StaticScanner) {
	e.scanners = append(e.scanners, s)
}

// AddAnalyzer registers an LLM analyzer for phase 2.
func (e *Engine) AddAnalyzer(a LLMAnalyzer) {
	e.analyzers = append(e.analyzers, a)
}

// Run executes the two-phase security pipeline:
//  1. Run all static scanners concurrently, collecting findings and errors.
//  2. Prioritize code into chunks using static findings as hints.
//  3. Run all LLM analyzers concurrently on the prioritized chunks.
//  4. Correlate all findings to deduplicate and detect attack chains.
//  5. Return the final report.
func (e *Engine) Run(ctx context.Context, target ScanTarget) (*Report, error) {
	start := time.Now()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("engine cancelled before start: %w", err)
	}

	// Merge engine-level exclude patterns into the target.
	mergedTarget := target
	if len(e.config.ExcludePatterns) > 0 {
		mergedTarget.ExcludePatterns = append(
			append([]string{}, target.ExcludePatterns...),
			e.config.ExcludePatterns...,
		)
	}

	// Phase 1: static scanners.
	staticFindings, scanErrors := e.runScanners(ctx, mergedTarget)

	// Check context between phases.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("engine cancelled after phase 1: %w", err)
	}

	// Prioritize code segments for LLM analysis.
	prioritizer := NewPrioritizer(PrioritizerConfig{
		MinRiskScore: e.config.MinRiskScore,
		MaxChunks:    e.config.MaxLLMChunks,
	})

	chunks, err := prioritizer.Prioritize(ctx, mergedTarget, staticFindings)
	if err != nil {
		return nil, fmt.Errorf("prioritization failed: %w", err)
	}

	// Phase 2: LLM analyzers on prioritized chunks.
	var analyzerFindings []Finding
	var analyzerErrors []ScanError

	if len(chunks) > 0 && len(e.analyzers) > 0 {
		analyzerFindings, analyzerErrors = e.runAnalyzers(ctx, chunks)
	}

	// Combine all findings and errors.
	allFindings := append(staticFindings, analyzerFindings...)
	allErrors := append(scanErrors, analyzerErrors...)

	// Correlate: deduplicate and detect attack chains.
	correlator := NewCorrelator()
	chains, deduped := correlator.Correlate(allFindings)

	report := &Report{
		Findings:     deduped,
		AttackChains: chains,
		Stats: ScanStats{
			Duration:       time.Since(start),
			FilesScanned:   countFiles(mergedTarget),
			ChunksAnalyzed: len(chunks),
			FindingsCount:  len(deduped),
			ChainCount:     len(chains),
		},
		Errors: allErrors,
	}

	return report, nil
}

// runScanners executes all static scanners concurrently and returns their
// combined findings and any non-fatal errors.
func (e *Engine) runScanners(ctx context.Context, target ScanTarget) ([]Finding, []ScanError) {
	if len(e.scanners) == 0 {
		return nil, nil
	}

	p := pool.New().WithMaxGoroutines(e.config.Concurrency)
	var mu sync.Mutex
	var findings []Finding
	var errors []ScanError

	for _, s := range e.scanners {
		s := s // capture loop variable
		p.Go(func() {
			result, err := s.Scan(ctx, target)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, ScanError{
					Scanner: s.Name(),
					Err:     err,
					Fatal:   false,
				})
				return
			}
			findings = append(findings, result...)
		})
	}

	p.Wait()
	return findings, errors
}

// runAnalyzers executes all LLM analyzers concurrently on the given chunks
// and returns their combined findings and any non-fatal errors.
func (e *Engine) runAnalyzers(ctx context.Context, chunks []AnalysisChunk) ([]Finding, []ScanError) {
	if len(e.analyzers) == 0 {
		return nil, nil
	}

	p := pool.New().WithMaxGoroutines(e.config.Concurrency)
	var mu sync.Mutex
	var findings []Finding
	var errors []ScanError

	for _, a := range e.analyzers {
		a := a // capture loop variable
		p.Go(func() {
			result, err := a.Analyze(ctx, chunks)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, ScanError{
					Scanner: a.Name(),
					Err:     err,
					Fatal:   false,
				})
				return
			}
			findings = append(findings, result...)
		})
	}

	p.Wait()
	return findings, errors
}

// countFiles returns a rough count of files in the scan target for stats.
func countFiles(target ScanTarget) int {
	if len(target.Files) > 0 {
		return len(target.Files)
	}
	// Use the prioritizer's file collection to count files.
	p := NewPrioritizer(PrioritizerConfig{})
	files, err := p.collectFiles(target)
	if err != nil {
		return 0
	}
	return len(files)
}
