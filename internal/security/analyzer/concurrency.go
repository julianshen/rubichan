package analyzer

import (
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/security"
)

const concurrencySystemPrompt = `You are a security analyzer specializing in concurrency vulnerabilities.

Analyze the provided code for:
- Race conditions in shared mutable state (CWE-362)
- Deadlock potential from lock ordering violations (CWE-833)
- Concurrent map access without synchronization (Go-specific: fatal at runtime)
- Time-of-check-to-time-of-use (TOCTOU) vulnerabilities
- Missing synchronization on shared variables accessed from goroutines
- Improper channel usage leading to goroutine leaks or panics
- Double-checked locking anti-patterns
- Atomic operation misuse
- File system race conditions (symlink races, tmp file races)

Return your findings as a JSON array. Each finding should have this structure:
[{
  "id": "RACE-001",
  "title": "Brief description",
  "severity": "critical|high|medium|low|info",
  "category": "race-condition",
  "description": "Detailed explanation of the concurrency issue",
  "location": {"file": "path", "start_line": 1, "end_line": 10, "function": "name"},
  "cwe": "CWE-362",
  "confidence": "high|medium|low",
  "remediation": "How to fix"
}]

If no issues are found, return an empty array: []`

// NewConcurrencyAnalyzer creates an LLM analyzer focused on concurrency and
// race condition vulnerabilities.
func NewConcurrencyAnalyzer(p provider.LLMProvider) *baseAnalyzer {
	return &baseAnalyzer{
		name:         "concurrency",
		category:     security.CategoryRaceCondition,
		systemPrompt: concurrencySystemPrompt,
		provider:     p,
	}
}
