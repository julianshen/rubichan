package permissions

import (
	"strings"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ClassifierDecision is the outcome of the LLM safety classifier.
type ClassifierDecision int

const (
	DecisionUnknown ClassifierDecision = iota
	DecisionSafe
	DecisionUnsafe
	DecisionUncertain
)

// YOLOClassifier is a two-stage LLM-based safety classifier for auto-approval
// mode. It evaluates whether a tool operation is safe to execute without
// user confirmation.
type YOLOClassifier struct {
	prov                  agentsdk.LLMProvider
	fastMax               int // max tokens for stage 1 (default 64)
	slowMax               int // max tokens for stage 2 (default 4096)
	consecutiveDenials    int
	maxConsecutiveDenials int
	mu                    sync.Mutex
}

// NewYOLOClassifier creates a classifier with the given provider.
// If provider is nil, all non-allowlist tools return ApprovalRequired.
func NewYOLOClassifier(prov agentsdk.LLMProvider, fastMax, slowMax int) *YOLOClassifier {
	if fastMax <= 0 {
		fastMax = 64
	}
	if slowMax <= 0 {
		slowMax = 4096
	}
	return &YOLOClassifier{
		prov:    prov,
		fastMax: fastMax,
		slowMax: slowMax,
	}
}

// SetMaxConsecutiveDenials sets the threshold for consecutive denials before
// falling back to manual approval. Zero disables the fallback.
func (c *YOLOClassifier) SetMaxConsecutiveDenials(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxConsecutiveDenials = n
}

// Classify evaluates a tool call and returns an approval decision.
// Safe-tool allowlist bypasses classification entirely.
func (c *YOLOClassifier) Classify(toolName string, input map[string]interface{}) (agentsdk.ApprovalResult, error) {
	if isReadOnlyTool(toolName) {
		c.resetDenials()
		return agentsdk.AutoApproved, nil
	}

	// Stage 1: Fast heuristic check (works even without provider).
	decision := c.stage1(toolName, input)

	var result agentsdk.ApprovalResult
	switch decision {
	case DecisionSafe:
		c.resetDenials()
		return agentsdk.AutoApproved, nil
	case DecisionUnsafe:
		result = agentsdk.AutoDenied
	case DecisionUncertain:
		// Stage 2: Detailed analysis (requires provider).
		if c.prov == nil {
			result = agentsdk.ApprovalRequired
		} else {
			var stage2Err error
			result, stage2Err = c.stage2(toolName, input)
			if stage2Err != nil {
				result = agentsdk.ApprovalRequired
			}
		}
	}

	if result == agentsdk.AutoApproved {
		c.resetDenials()
	} else {
		c.recordDenial()
	}

	return c.fallbackIfNeeded(result)
}

// fallbackIfNeeded returns ApprovalRequired if consecutive denials exceed
// the threshold, otherwise returns the given result.
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

// stage1 is a fast heuristic check that classifies obvious cases.
// When a provider is available, this would use a constrained completion
// with a small token budget (fastMax). Without a provider, it falls back
// to tool name substring heuristics.
func (c *YOLOClassifier) stage1(toolName string, input map[string]interface{}) ClassifierDecision {
	_ = c.fastMax
	_ = input

	// Heuristic fallback: tools with dangerous keywords need stage 2.
	if strings.Contains(toolName, "write") || strings.Contains(toolName, "edit") ||
		strings.Contains(toolName, "delete") || strings.Contains(toolName, "shell") {
		return DecisionUncertain
	}
	return DecisionSafe
}

// stage2 performs detailed analysis for borderline cases.
// Currently a placeholder — when a provider is available, this will send
// a structured prompt (see stage2PromptFormat) and parse the response.
func (c *YOLOClassifier) stage2(toolName string, input map[string]interface{}) (agentsdk.ApprovalResult, error) {
	_ = c.slowMax
	_ = toolName
	_ = input

	// Placeholder: always require approval until LLM integration is wired.
	return agentsdk.ApprovalRequired, nil
}
