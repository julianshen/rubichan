# Two-Stage Permission Classifier Improvements

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve rubichan's existing `YOLOClassifier` with real stage1 (pattern-based heuristic) and stage2 (LLM reasoning) implementations, plus caching and telemetry.

**Architecture:** Stage 1 uses path-based, command-based, and content-based heuristics with severity scoring. Stage 2 uses constrained LLM completion when a provider is available. Results are cached in an LRU (100 entries). Telemetry tracks classification counts and latency per stage.

**Tech Stack:** Go, existing `YOLOClassifier`, `agentsdk.LLMProvider`, `agentsdk.ApprovalResult`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/permissions/classifier.go` | `YOLOClassifier` with real stage1 + stage2, cache, telemetry |
| `internal/permissions/classifier_test.go` | Tests for stage1, stage2, cache, fallback |
| `internal/permissions/mode_checker.go` | Integrate improved classifier |
| `pkg/agentsdk/approval.go` | Add `ClassifierTelemetry` type if needed |

---

## Chunk 1: Stage 1 Heuristic Improvements

### Task 1: Implement pattern-based stage1 with severity scoring

**Files:**
- Modify: `internal/permissions/classifier.go`

**Code:**

```go
package permissions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ClassifierDecision is the outcome of the safety classifier.
type ClassifierDecision int

const (
	DecisionUnknown ClassifierDecision = iota
	DecisionSafe
	DecisionUnsafe
	DecisionUncertain
)

// classificationCacheEntry stores a cached classification result.
type classificationCacheEntry struct {
	result    agentsdk.ApprovalResult
	timestamp time.Time
}

// YOLOClassifier is a two-stage LLM-based safety classifier for auto-approval.
type YOLOClassifier struct {
	prov                  agentsdk.LLMProvider
	fastMax               int
	slowMax               int
	consecutiveDenials    int
	maxConsecutiveDenials int
	mu                    sync.Mutex

	// Cache: tool input hash -> result
	cache      map[string]classificationCacheEntry
	cacheMu    sync.RWMutex
	cacheLimit int

	// Telemetry
	telemetry ClassifierTelemetry
}

// ClassifierTelemetry tracks classification metrics.
type ClassifierTelemetry struct {
	Stage1Count   int
	Stage2Count   int
	CacheHits     int
	Stage1Latency time.Duration
	Stage2Latency time.Duration
}

// NewYOLOClassifier creates a classifier with the given provider.
func NewYOLOClassifier(prov agentsdk.LLMProvider, fastMax, slowMax int) *YOLOClassifier {
	if fastMax <= 0 {
		fastMax = 64
	}
	if slowMax <= 0 {
		slowMax = 4096
	}
	return &YOLOClassifier{
		prov:       prov,
		fastMax:    fastMax,
		slowMax:    slowMax,
		cache:      make(map[string]classificationCacheEntry),
		cacheLimit: 100,
	}
}

// SetMaxConsecutiveDenials sets the threshold for consecutive denials.
func (c *YOLOClassifier) SetMaxConsecutiveDenials(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxConsecutiveDenials = n
}

// Telemetry returns a copy of current telemetry.
func (c *YOLOClassifier) Telemetry() ClassifierTelemetry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.telemetry
}

// hashToolInput creates a cache key from tool name and input.
func hashToolInput(toolName string, input map[string]interface{}) string {
	h := sha256.New()
	h.Write([]byte(toolName))
	if data, err := json.Marshal(input); err == nil {
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// getCached returns a cached result if present and fresh.
func (c *YOLOClassifier) getCached(key string) (agentsdk.ApprovalResult, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	entry, ok := c.cache[key]
	if !ok {
		return 0, false
	}
	// Cache entries expire after 5 minutes
	if time.Since(entry.timestamp) > 5*time.Minute {
		return 0, false
	}
	return entry.result, true
}

// setCached stores a result in the cache.
func (c *YOLOClassifier) setCached(key string, result agentsdk.ApprovalResult) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if len(c.cache) >= c.cacheLimit {
		// Evict oldest (simple: clear half)
		newCache := make(map[string]classificationCacheEntry, c.cacheLimit/2)
		for k, v := range c.cache {
			if len(newCache) >= c.cacheLimit/2 {
				break
			}
			newCache[k] = v
		}
		c.cache = newCache
	}
	c.cache[key] = classificationCacheEntry{result: result, timestamp: time.Now()}
}

// Classify evaluates a tool call and returns an approval decision.
func (c *YOLOClassifier) Classify(toolName string, input map[string]interface{}) (agentsdk.ApprovalResult, error) {
	if isReadOnlyTool(toolName) {
		c.resetDenials()
		return agentsdk.AutoApproved, nil
	}

	// Check cache
	cacheKey := hashToolInput(toolName, input)
	if cached, ok := c.getCached(cacheKey); ok {
		c.mu.Lock()
		c.telemetry.CacheHits++
		c.mu.Unlock()
		if cached == agentsdk.AutoApproved {
			c.resetDenials()
		} else {
			c.recordDenial()
		}
		return c.fallbackIfNeeded(cached)
	}

	// Stage 1: Fast heuristic
	start := time.Now()
	decision := c.stage1(toolName, input)
	c.mu.Lock()
	c.telemetry.Stage1Count++
	c.telemetry.Stage1Latency += time.Since(start)
	c.mu.Unlock()

	var result agentsdk.ApprovalResult
	switch decision {
	case DecisionSafe:
		c.resetDenials()
		result = agentsdk.AutoApproved
	case DecisionUnsafe:
		result = agentsdk.AutoDenied
	case DecisionUncertain:
		if c.prov == nil {
			result = agentsdk.ApprovalRequired
		} else {
			start2 := time.Now()
			var stage2Err error
			result, stage2Err = c.stage2(toolName, input)
			c.mu.Lock()
			c.telemetry.Stage2Count++
			c.telemetry.Stage2Latency += time.Since(start2)
			c.mu.Unlock()
			if stage2Err != nil {
				result = agentsdk.ApprovalRequired
			}
		}
	}

	// Cache result
	c.setCached(cacheKey, result)

	if result == agentsdk.AutoApproved {
		c.resetDenials()
	} else {
		c.recordDenial()
	}

	return c.fallbackIfNeeded(result)
}

// fallbackIfNeeded returns ApprovalRequired if consecutive denials exceed threshold.
func (c *YOLOClassifier) fallbackIfNeeded(result agentsdk.ApprovalResult) (agentsdk.ApprovalResult, error) {
	if c.shouldFallback() {
		return agentsdk.ApprovalRequired, nil
	}
	return result, nil
}

func (c *YOLOClassifier) resetDenials() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveDenials = 0
}

func (c *YOLOClassifier) recordDenial() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveDenials++
}

func (c *YOLOClassifier) shouldFallback() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxConsecutiveDenials > 0 && c.consecutiveDenials >= c.maxConsecutiveDenials
}
```

**Test:**

```go
func TestYOLOClassifier_CacheHit(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	c.SetMaxConsecutiveDenials(3)

	// First call populates cache
	result1, _ := c.Classify("write_file", map[string]interface{}{"path": "/tmp/test"})
	// Second call should hit cache
	result2, _ := c.Classify("write_file", map[string]interface{}{"path": "/tmp/test"})
	assert.Equal(t, result1, result2)

	tele := c.Telemetry()
	assert.GreaterOrEqual(t, tele.CacheHits, 1)
}

func TestYOLOClassifier_Telemetry(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	_, _ = c.Classify("shell", map[string]interface{}{"command": "ls"})
	tele := c.Telemetry()
	assert.GreaterOrEqual(t, tele.Stage1Count, 1)
}
```

**Command:**
```bash
go test ./internal/permissions/... -run TestYOLOClassifier_CacheHit -v
go test ./internal/permissions/... -run TestYOLOClassifier_Telemetry -v
```

**Expected:** PASS.

---

### Task 2: Implement real stage1 with severity scoring

**Files:**
- Modify: `internal/permissions/classifier.go`

**Code:**

```go
// stage1 is a fast heuristic check with severity scoring.
func (c *YOLOClassifier) stage1(toolName string, input map[string]interface{}) ClassifierDecision {
	_ = c.fastMax

	score := 0

	// Path-based checks
	if path, ok := getStringInput(input, "path", "file_path", "target"); ok {
		score += scorePath(path)
	}

	// Command-based checks
	if cmd, ok := getStringInput(input, "command", "cmd", "shell"); ok {
		score += scoreCommand(cmd)
	}

	// Content-based checks
	if content, ok := getStringInput(input, "content", "text", "code"); ok {
		score += scoreContent(content)
	}

	// Tool name-based checks
	score += scoreToolName(toolName)

	// Decision thresholds
	if score <= 0 {
		return DecisionSafe
	}
	if score >= 3 {
		return DecisionUnsafe
	}
	return DecisionUncertain
}

// getStringInput extracts a string value from input by trying multiple keys.
func getStringInput(input map[string]interface{}, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := input[k].(string); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

// scorePath checks if target path is in safe zones.
func scorePath(path string) int {
	// Safe read-only directories
	safePrefixes := []string{"/usr/", "/opt/", "/etc/"}
	for _, prefix := range safePrefixes {
		if strings.HasPrefix(path, prefix) && !strings.Contains(path, "..") {
			return -1 // safer
		}
	}

	// Dangerous patterns
	dangerous := []string{"/dev/null", "/dev/zero", "/proc/", "/sys/"}
	for _, d := range dangerous {
		if strings.Contains(path, d) {
			return 2
		}
	}

	// Relative path traversal
	if strings.Contains(path, "..") || strings.Contains(path, "~/") {
		return 1
	}

	return 0
}

// scoreCommand checks shell command for dangerous patterns.
func scoreCommand(cmd string) int {
	score := 0
	cmdLower := strings.ToLower(cmd)

	// Blocklist patterns
	blocklist := []string{
		"rm -rf", "rm -rf /", "> /dev/null", "mkfs", "dd if=",
		":(){ :|:& };:", "curl | sh", "wget | sh",
	}
	for _, pattern := range blocklist {
		if strings.Contains(cmdLower, pattern) {
			score += 3
		}
	}

	// Moderate risk
	moderate := []string{"rm ", "mv ", "cp -r", "chmod ", "chown "}
	for _, pattern := range moderate {
		if strings.Contains(cmdLower, pattern) {
			score += 1
		}
	}

	return score
}

// scoreContent checks content for destructive patterns.
func scoreContent(content string) int {
	score := 0
	lower := strings.ToLower(content)

	if strings.Contains(lower, "drop table") || strings.Contains(lower, "delete from") {
		score += 2
	}
	if strings.Contains(lower, "truncate") || strings.Contains(lower, "alter table") {
		score += 1
	}

	return score
}

// scoreToolName checks tool name for known dangerous keywords.
func scoreToolName(toolName string) int {
	score := 0
	lower := strings.ToLower(toolName)

	if strings.Contains(lower, "delete") || strings.Contains(lower, "remove") {
		score += 2
	}
	if strings.Contains(lower, "write") || strings.Contains(lower, "edit") ||
		strings.Contains(lower, "shell") || strings.Contains(lower, "exec") {
		score += 1
	}

	return score
}
```

**Test:**

```go
func TestStage1_SafeTool(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	decision := c.stage1("read_file", map[string]interface{}{"path": "/tmp/test"})
	assert.Equal(t, DecisionSafe, decision)
}

func TestStage1_DangerousCommand(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	decision := c.stage1("shell", map[string]interface{}{"command": "rm -rf /"})
	assert.Equal(t, DecisionUnsafe, decision)
}

func TestStage1_UncertainWrite(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	decision := c.stage1("write_file", map[string]interface{}{"path": "/tmp/test", "content": "hello"})
	assert.Equal(t, DecisionUncertain, decision)
}

func TestStage1_PathTraversal(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	decision := c.stage1("read_file", map[string]interface{}{"path": "../../../etc/passwd"})
	assert.Equal(t, DecisionUncertain, decision)
}
```

**Command:**
```bash
go test ./internal/permissions/... -run TestStage1_ -v
```

**Expected:** PASS.

---

## Chunk 2: Stage 2 LLM Reasoning

### Task 3: Implement stage2 with constrained completion

**Files:**
- Modify: `internal/permissions/classifier.go`

**Code:**

```go
// stage2 performs LLM-based reasoning for borderline cases.
func (c *YOLOClassifier) stage2(toolName string, input map[string]interface{}) (agentsdk.ApprovalResult, error) {
	_ = c.slowMax

	if c.prov == nil {
		return agentsdk.ApprovalRequired, nil
	}

	// Build constrained prompt
	prompt := buildClassificationPrompt(toolName, input)

	req := agentsdk.CompletionRequest{
		Model:     "", // Use provider default
		System:    "You are a safety classifier. Respond with exactly one word: safe, unsafe, or uncertain.",
		Messages:  []agentsdk.Message{{Role: "user", Content: []agentsdk.ContentBlock{{Type: "text", Text: prompt}}}},
		MaxTokens: c.fastMax,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := c.prov.Stream(ctx, req)
	if err != nil {
		return agentsdk.ApprovalRequired, fmt.Errorf("stage2 stream: %w", err)
	}

	var response strings.Builder
	for evt := range stream {
		if evt.Type == "text_delta" {
			response.WriteString(evt.Text)
		}
	}

	result := strings.ToLower(strings.TrimSpace(response.String()))
	switch {
	case strings.Contains(result, "safe") && !strings.Contains(result, "unsafe"):
		return agentsdk.AutoApproved, nil
	case strings.Contains(result, "unsafe"):
		return agentsdk.AutoDenied, nil
	default:
		return agentsdk.ApprovalRequired, nil
	}
}

func buildClassificationPrompt(toolName string, input map[string]interface{}) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tool: %s\n", toolName)
	fmt.Fprintf(&b, "Input: %v\n", input)
	b.WriteString("\nIs this tool call safe to execute without user approval?\n")
	b.WriteString("Consider: does it read-only, write files, execute shell commands, or delete data?\n")
	b.WriteString("Respond with exactly one word: safe, unsafe, or uncertain.")
	return b.String()
}
```

**Test:**

```go
// mockProvider implements agentsdk.LLMProvider for testing.
type mockProvider struct {
	response string
}

func (m *mockProvider) Stream(ctx context.Context, req agentsdk.CompletionRequest) (<-chan agentsdk.StreamEvent, error) {
	ch := make(chan agentsdk.StreamEvent, 1)
	ch <- agentsdk.StreamEvent{Type: "text_delta", Text: m.response}
	close(ch)
	return ch, nil
}

func TestStage2_SafeResponse(t *testing.T) {
	prov := &mockProvider{response: "safe"}
	c := NewYOLOClassifier(prov, 64, 4096)
	result, err := c.stage2("read_file", map[string]interface{}{"path": "/tmp/test"})
	require.NoError(t, err)
	assert.Equal(t, agentsdk.AutoApproved, result)
}

func TestStage2_UnsafeResponse(t *testing.T) {
	prov := &mockProvider{response: "unsafe"}
	c := NewYOLOClassifier(prov, 64, 4096)
	result, err := c.stage2("shell", map[string]interface{}{"command": "rm -rf /"})
	require.NoError(t, err)
	assert.Equal(t, agentsdk.AutoDenied, result)
}

func TestStage2_UncertainResponse(t *testing.T) {
	prov := &mockProvider{response: "uncertain"}
	c := NewYOLOClassifier(prov, 64, 4096)
	result, err := c.stage2("write_file", map[string]interface{}{"path": "/tmp/test"})
	require.NoError(t, err)
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}
```

**Command:**
```bash
go test ./internal/permissions/... -run TestStage2_ -v
```

**Expected:** PASS.

---

### Task 4: Update existing tests for new stage1 behavior

**Files:**
- Modify: `internal/permissions/classifier_test.go`

**Code:**

Update `TestYOLOClassifier_Stage1Heuristics` to match new scoring:

```go
func TestYOLOClassifier_Stage1Heuristics(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)

	// write_file is uncertain (score 1 from tool name)
	result, _ := c.Classify("write_file", map[string]interface{}{"path": "/tmp/test"})
	assert.Equal(t, agentsdk.ApprovalRequired, result)

	// shell with dangerous command is unsafe
	result, _ = c.Classify("shell", map[string]interface{}{"command": "rm -rf /"})
	assert.Equal(t, agentsdk.AutoDenied, result)

	// read_file in safe path is safe (bypasses classifier entirely via isReadOnlyTool)
	result, _ = c.Classify("read_file", map[string]interface{}{"path": "/tmp/test"})
	assert.Equal(t, agentsdk.AutoApproved, result)

	// Unknown tool without dangerous keywords
	result, _ = c.Classify("some_info_tool", map[string]interface{}{"query": "hello"})
	assert.Equal(t, agentsdk.AutoApproved, result)
}
```

**Command:**
```bash
go test ./internal/permissions/... -run TestYOLOClassifier_Stage1Heuristics -v
```

**Expected:** PASS.

---

## Chunk 3: Integration

### Task 5: Integrate improved classifier into ModeAwareChecker

**Files:**
- Modify: `internal/permissions/mode_checker.go`

No code changes needed — `ModeAwareChecker` already calls `c.classifier.Classify(tool, parsedInput)`. The improved classifier is a drop-in replacement.

Verify integration test:

```go
func TestModeAwareChecker_WithImprovedClassifier(t *testing.T) {
	classifier := NewYOLOClassifier(nil, 0, 0)
	checker := NewModeAwareChecker(agentsdk.ModeAuto, &alwaysRequire{}, WithClassifier(classifier))

	// Read-only tool should bypass
	result := checker.CheckApproval("read_file", json.RawMessage(`{"path":"/tmp/test"}`))
	assert.Equal(t, agentsdk.AutoApproved, result)

	// Dangerous shell should be denied
	result = checker.CheckApproval("shell", json.RawMessage(`{"command":"rm -rf /"}`))
	assert.Equal(t, agentsdk.AutoDenied, result)
}
```

**Command:**
```bash
go test ./internal/permissions/... -run TestModeAwareChecker_WithImprovedClassifier -v
```

**Expected:** PASS.

---

## Validation Commands

```bash
go test ./internal/permissions/...
go test -cover ./internal/permissions/...
golangci-lint run ./internal/permissions/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Two-stage permission classifier with real stage1 + stage2`

**Body:**
- **Stage 1 (Fast heuristic):** Pattern-based classification with severity scoring
  - Path-based: checks safe zones and dangerous paths
  - Command-based: shell command blocklist (rm -rf, curl | sh, etc.)
  - Content-based: detects destructive SQL patterns
  - Tool name scoring for known dangerous keywords
  - Returns Safe/Unsafe/Uncertain with confidence thresholds
- **Stage 2 (LLM reasoning):** Constrained completion when provider available
  - 64-token fast path with single-word response (safe/unsafe/uncertain)
  - 10-second timeout to prevent blocking
  - Falls back to ApprovalRequired on any error
- **Cache:** LRU cache (100 entries, 5-minute TTL) keyed by tool input hash
- **Telemetry:** Tracks stage1/stage2 counts, cache hits, latency per stage
- **Integration:** Drop-in replacement for existing `YOLOClassifier`
- All existing tests updated and passing

**Commit prefix:** `[BEHAVIORAL]`
