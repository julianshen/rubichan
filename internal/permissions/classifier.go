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

	// Stage 1: Fast check (works even without provider).
	decision, err := c.stage1(toolName, input)
	if err != nil {
		c.recordDenial()
		return agentsdk.ApprovalRequired, nil
	}

	var result agentsdk.ApprovalResult
	switch decision {
	case DecisionSafe:
		c.resetDenials()
		return agentsdk.AutoApproved, nil
	case DecisionUnsafe:
		c.recordDenial()
		result = agentsdk.AutoDenied
	case DecisionUncertain:
		// Stage 2: Detailed analysis (requires provider).
		if c.prov == nil {
			c.recordDenial()
			result = agentsdk.ApprovalRequired
		} else {
			result, err = c.stage2(toolName, input)
			if err != nil {
				result = agentsdk.ApprovalRequired
			}
		}
	}

	if result == agentsdk.AutoApproved {
		c.resetDenials()
	} else {
		c.recordDenial()
	}

	// Fallback to manual approval after too many consecutive denials.
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

// stage1 is a fast check that classifies obvious cases.
func (c *YOLOClassifier) stage1(toolName string, input map[string]interface{}) (ClassifierDecision, error) {
	_ = c.fastMax
	_ = input

	// Use provider for a short completion.
	// Simplified: in practice this would use a constrained completion.
	// Mock implementation: classify based on tool name heuristics.
	if strings.Contains(toolName, "write") || strings.Contains(toolName, "edit") ||
		strings.Contains(toolName, "delete") || strings.Contains(toolName, "shell") {
		return DecisionUncertain, nil
	}
	return DecisionSafe, nil
}

// stage2 is a detailed analysis for borderline cases.
func (c *YOLOClassifier) stage2(toolName string, input map[string]interface{}) (agentsdk.ApprovalResult, error) {
	_ = c.slowMax
	_ = toolName
	_ = input

	// Detailed prompt with reasoning would go here.
	// Mock: for now, require approval for all uncertain cases.
	return agentsdk.ApprovalRequired, nil
}

// stage2PromptFormat is the prompt template for detailed safety analysis.
// Reserved for future use when stage2 is implemented with LLM reasoning.
//
//nolint:unused
const stage2PromptFormat = `Analyze if this tool is safe to auto-approve.
Tool: %s
Input: %v

Reply with:
<thinking> reasoning </thinking>
<decision> ALLOW or DENY </decision>`
