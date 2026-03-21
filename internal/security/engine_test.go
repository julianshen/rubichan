package security

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockScanner implements StaticScanner for testing.
type mockScanner struct {
	name     string
	findings []Finding
	err      error
}

func (m *mockScanner) Name() string { return m.name }
func (m *mockScanner) Scan(_ context.Context, _ ScanTarget) ([]Finding, error) {
	return m.findings, m.err
}

// mockAnalyzer implements LLMAnalyzer for testing.
type mockAnalyzer struct {
	name     string
	category Category
	findings []Finding
	err      error
}

func (m *mockAnalyzer) Name() string       { return m.name }
func (m *mockAnalyzer) Category() Category { return m.category }
func (m *mockAnalyzer) Analyze(_ context.Context, _ []AnalysisChunk) ([]Finding, error) {
	return m.findings, m.err
}

func TestEngineRunBothPhases(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineConfig{
		MaxLLMChunks: 100,
		MinRiskScore: 0,
		Concurrency:  2,
	})

	e.AddScanner(&mockScanner{
		name: "test-scanner",
		findings: []Finding{
			{ID: "S-1", Severity: SeverityHigh, Category: CategorySecretsExposure, Title: "Secret found",
				CWE: "CWE-798", Location: Location{File: "main.go", StartLine: 1}},
		},
	})
	e.AddAnalyzer(&mockAnalyzer{
		name:     "test-analyzer",
		category: CategoryAuthentication,
		findings: []Finding{
			{ID: "A-1", Severity: SeverityMedium, Category: CategoryAuthentication, Title: "Auth issue",
				CWE: "CWE-306", Location: Location{File: "main.go", StartLine: 3}},
		},
	})

	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", `package main
import "os/exec"
func main() { exec.Command("sh").Run() }
`)

	report, err := e.Run(context.Background(), ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.GreaterOrEqual(t, len(report.Findings), 2, "should have findings from both phases")
	assert.Greater(t, report.Stats.Duration, time.Duration(0))
}

func TestEngineHandlesScannerError(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineConfig{Concurrency: 1})
	e.AddScanner(&mockScanner{
		name: "failing-scanner",
		err:  fmt.Errorf("scanner crashed"),
	})

	report, err := e.Run(context.Background(), ScanTarget{RootDir: t.TempDir()})
	require.NoError(t, err, "engine should not fail for individual scanner errors")
	require.NotEmpty(t, report.Errors)
	assert.Equal(t, "failing-scanner", report.Errors[0].Scanner)
}

func TestEngineHandlesAnalyzerError(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineConfig{
		MaxLLMChunks: 100,
		MinRiskScore: 0,
		Concurrency:  1,
	})
	e.AddAnalyzer(&mockAnalyzer{
		name:     "failing-analyzer",
		category: CategoryInjection,
		err:      fmt.Errorf("LLM timeout"),
	})

	dir := t.TempDir()
	writeTestFile(t, dir, "auth.go", `package auth
import "os/exec"
func Login() { exec.Command("sh").Run() }
`)

	report, err := e.Run(context.Background(), ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, report.Errors)
}

func TestEngineContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	e := NewEngine(EngineConfig{Concurrency: 1})
	e.AddScanner(&mockScanner{name: "slow"})

	report, err := e.Run(ctx, ScanTarget{RootDir: t.TempDir()})
	// Should return quickly with context error
	assert.Error(t, err)
	_ = report
}

// slowScanner blocks until context is cancelled, simulating a hanging scanner.
type slowScanner struct{ name string }

func (s *slowScanner) Name() string { return s.name }
func (s *slowScanner) Scan(ctx context.Context, _ ScanTarget) ([]Finding, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestEngineScannerTimeout(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineConfig{
		Concurrency:    1,
		ScannerTimeout: 100 * time.Millisecond,
	})
	e.AddScanner(&slowScanner{name: "slow-scanner"})

	start := time.Now()
	report, err := e.Run(context.Background(), ScanTarget{RootDir: t.TempDir()})
	elapsed := time.Since(start)

	require.NoError(t, err, "engine should not fail for timed-out scanners")
	require.NotEmpty(t, report.Errors, "timed-out scanner should produce an error")
	assert.Equal(t, "slow-scanner", report.Errors[0].Scanner)
	assert.Less(t, elapsed, 5*time.Second, "should not hang — timeout should kick in")
}

func TestEngineEmptyTarget(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineConfig{Concurrency: 1})
	report, err := e.Run(context.Background(), ScanTarget{RootDir: t.TempDir()})
	require.NoError(t, err)
	assert.Empty(t, report.Findings)
}
