package permissions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	order     int // insertion order for deterministic LRU eviction
}

// YOLOClassifier is a two-stage LLM-based safety classifier for auto-approval.
type YOLOClassifier struct {
	prov                  agentsdk.LLMProvider
	fastMax               int
	slowMax               int
	consecutiveDenials    int
	maxConsecutiveDenials int
	mu                    sync.Mutex

	cache      map[string]classificationCacheEntry
	cacheMu    sync.RWMutex
	cacheLimit int
	cacheOrder int // monotonic counter for LRU

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

func (c *YOLOClassifier) recordCacheHit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.telemetry.CacheHits++
}

func (c *YOLOClassifier) recordStage1(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.telemetry.Stage1Count++
	c.telemetry.Stage1Latency += d
}

func (c *YOLOClassifier) recordStage2(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.telemetry.Stage2Count++
	c.telemetry.Stage2Latency += d
}

func hashToolInput(toolName string, input map[string]interface{}) string {
	h := sha256.New()
	h.Write([]byte(toolName))
	data, err := json.Marshal(input)
	if err != nil {
		// Fallback: use fmt.Sprintf to avoid cache key collisions on marshal failure.
		data = []byte(fmt.Sprintf("%v", input))
	}
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (c *YOLOClassifier) getCached(key string) (agentsdk.ApprovalResult, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	entry, ok := c.cache[key]
	if !ok {
		return 0, false
	}
	if time.Since(entry.timestamp) > 5*time.Minute {
		return 0, false
	}
	return entry.result, true
}

func (c *YOLOClassifier) setCached(key string, result agentsdk.ApprovalResult) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if len(c.cache) >= c.cacheLimit {
		// Deterministic LRU: evict oldest entries by order field.
		type kv struct {
			key   string
			order int
		}
		entries := make([]kv, 0, len(c.cache))
		for k, v := range c.cache {
			entries = append(entries, kv{key: k, order: v.order})
		}
		// Sort by order ascending (oldest first).
		for i := 0; i < len(entries)-1; i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[j].order < entries[i].order {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}
		newCache := make(map[string]classificationCacheEntry, c.cacheLimit/2)
		for i := len(entries) / 2; i < len(entries); i++ {
			newCache[entries[i].key] = c.cache[entries[i].key]
		}
		c.cache = newCache
	}
	c.cacheOrder++
	c.cache[key] = classificationCacheEntry{result: result, timestamp: time.Now(), order: c.cacheOrder}
}

// Classify evaluates a tool call and returns an approval decision.
func (c *YOLOClassifier) Classify(toolName string, input map[string]interface{}) (agentsdk.ApprovalResult, error) {
	if isReadOnlyTool(toolName) {
		c.resetDenials()
		return agentsdk.AutoApproved, nil
	}

	cacheKey := hashToolInput(toolName, input)
	if cached, ok := c.getCached(cacheKey); ok {
		c.recordCacheHit()
		if cached == agentsdk.AutoApproved {
			c.resetDenials()
		} else {
			c.recordDenial()
		}
		return c.fallbackIfNeeded(cached)
	}

	start := time.Now()
	decision := c.stage1(toolName, input)
	c.recordStage1(time.Since(start))

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
			c.recordStage2(time.Since(start2))
			if stage2Err != nil {
				result = agentsdk.ApprovalRequired
			}
		}
	}

	c.setCached(cacheKey, result)

	if result == agentsdk.AutoApproved {
		c.resetDenials()
	} else {
		c.recordDenial()
	}

	return c.fallbackIfNeeded(result)
}

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

// stage1 is a fast heuristic check with severity scoring.
func (c *YOLOClassifier) stage1(toolName string, input map[string]interface{}) ClassifierDecision {
	score := 0

	if path, ok := getStringInput(input, "path", "file_path", "target"); ok {
		score += scorePath(path)
	}

	if cmd, ok := getStringInput(input, "command", "cmd", "shell"); ok {
		score += scoreCommand(cmd)
	}

	if content, ok := getStringInput(input, "content", "text", "code"); ok {
		score += scoreContent(content)
	}

	score += scoreToolName(toolName)

	if score <= 0 {
		return DecisionSafe
	}
	if score >= 3 {
		return DecisionUnsafe
	}
	return DecisionUncertain
}

func getStringInput(input map[string]interface{}, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := input[k].(string); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

func scorePath(path string) int {
	safePrefixes := []string{"/usr/", "/opt/"}
	for _, prefix := range safePrefixes {
		if strings.HasPrefix(path, prefix) && !strings.Contains(path, "..") {
			return -1
		}
	}

	dangerous := []string{"/dev/null", "/dev/zero", "/proc/", "/sys/"}
	for _, d := range dangerous {
		if strings.Contains(path, d) {
			return 2
		}
	}

	if strings.Contains(path, "..") || strings.Contains(path, "~/") {
		return 1
	}

	return 0
}

func scoreCommand(cmd string) int {
	score := 0
	cmdLower := strings.ToLower(cmd)

	blocklist := []string{
		"rm -rf /", "mkfs", "dd if=",
		":(){ :|: & };:", "curl | sh", "wget | sh",
	}
	for _, pattern := range blocklist {
		if strings.Contains(cmdLower, pattern) {
			score += 3
		}
	}

	moderate := []string{"rm ", "mv ", "cp -r", "chmod ", "chown "}
	for _, pattern := range moderate {
		if strings.Contains(cmdLower, pattern) {
			score += 1
		}
	}

	return score
}

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

// stage2 performs LLM-based reasoning for borderline cases.
func (c *YOLOClassifier) stage2(toolName string, input map[string]interface{}) (agentsdk.ApprovalResult, error) {
	if c.prov == nil {
		return agentsdk.ApprovalRequired, nil
	}

	prompt := buildClassificationPrompt(toolName, input)

	req := agentsdk.CompletionRequest{
		System:    "You are a safety classifier. Respond with exactly one word: safe, unsafe, or uncertain.",
		Messages:  []agentsdk.Message{{Role: "user", Content: []agentsdk.ContentBlock{{Type: agentsdk.BlockTypeText, Text: prompt}}}},
		MaxTokens: c.slowMax,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := c.prov.Stream(ctx, req)
	if err != nil {
		return agentsdk.ApprovalRequired, fmt.Errorf("stage2 stream: %w", err)
	}

	var response strings.Builder
	for evt := range stream {
		if evt.Type == agentsdk.EventTextDelta {
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
	b.WriteString("Input:\n")
	for k, v := range input {
		fmt.Fprintf(&b, "  %s: %v\n", k, v)
	}
	b.WriteString("\nIs this tool call safe to execute without user approval?\n")
	b.WriteString("Consider: does it read-only, write files, execute shell commands, or delete data?\n")
	b.WriteString("Respond with exactly one word: safe, unsafe, or uncertain.")
	return b.String()
}
