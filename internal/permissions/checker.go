package permissions

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// HierarchicalChecker implements agentsdk.ApprovalChecker by evaluating
// tool calls against a cascade of permission policies.
type HierarchicalChecker struct {
	policies []Policy
}

func NewHierarchicalChecker(policies []Policy) *HierarchicalChecker {
	return &HierarchicalChecker{policies: policies}
}

// CheckApproval evaluates policies: Deny > Prompt > Allow.
func (h *HierarchicalChecker) CheckApproval(tool string, input json.RawMessage) agentsdk.ApprovalResult {
	if len(h.policies) == 0 {
		return agentsdk.ApprovalRequired
	}

	for _, p := range h.policies {
		if h.matchesDeny(p, tool, input) {
			return agentsdk.AutoDenied
		}
	}
	for _, p := range h.policies {
		if h.matchesPrompt(p, tool, input) {
			return agentsdk.ApprovalRequired
		}
	}
	for _, p := range h.policies {
		if h.matchesAllow(p, tool, input) {
			return agentsdk.TrustRuleApproved
		}
	}
	return agentsdk.ApprovalRequired
}

// Explain re-evaluates and returns a human-readable reason.
func (h *HierarchicalChecker) Explain(tool string, input json.RawMessage) string {
	for _, p := range h.policies {
		if h.matchesDeny(p, tool, input) {
			return fmt.Sprintf("denied by %s policy (%s): tool '%s'", p.Level, p.Source, tool)
		}
	}
	for _, p := range h.policies {
		if h.matchesPrompt(p, tool, input) {
			return fmt.Sprintf("requires approval per %s policy (%s): tool '%s'", p.Level, p.Source, tool)
		}
	}
	for _, p := range h.policies {
		if h.matchesAllow(p, tool, input) {
			return fmt.Sprintf("allowed by %s policy (%s): tool '%s'", p.Level, p.Source, tool)
		}
	}
	return ""
}

func (h *HierarchicalChecker) matchesDeny(p Policy, tool string, input json.RawMessage) bool {
	if slices.Contains(p.Tools.Deny, tool) {
		return true
	}
	if tool == "shell" {
		cmd := extractShellCommand(input)
		for _, pattern := range p.Shell.DenyCommands {
			if containsCommandPattern(cmd, pattern) {
				return true
			}
		}
	}
	if tool == "file" {
		op, fp := extractFileInfo(input)
		if op == "write" || op == "patch" {
			for _, pattern := range p.Files.DenyPatterns {
				if matchesFilePattern(fp, pattern) {
					return true
				}
			}
		}
	}
	return false
}

func (h *HierarchicalChecker) matchesPrompt(p Policy, tool string, input json.RawMessage) bool {
	if slices.Contains(p.Tools.Prompt, tool) {
		return true
	}
	if tool == "shell" {
		cmd := extractShellCommand(input)
		for _, pattern := range p.Shell.PromptPatterns {
			if matchesCommandPrefix(cmd, pattern) {
				return true
			}
		}
	}
	if tool == "file" {
		op, fp := extractFileInfo(input)
		if op == "write" || op == "patch" {
			for _, pattern := range p.Files.PromptPatterns {
				if matchesFilePattern(fp, pattern) {
					return true
				}
			}
		}
	}
	return false
}

func (h *HierarchicalChecker) matchesAllow(p Policy, tool string, input json.RawMessage) bool {
	if slices.Contains(p.Tools.Allow, tool) {
		return true
	}
	if tool == "shell" {
		cmd := extractShellCommand(input)
		for _, pattern := range p.Shell.AllowCommands {
			if matchesCommandPrefix(cmd, pattern) {
				return true
			}
		}
	}
	if tool == "file" {
		op, fp := extractFileInfo(input)
		if op == "write" || op == "patch" {
			for _, pattern := range p.Files.AllowPatterns {
				if matchesFilePattern(fp, pattern) {
					return true
				}
			}
		}
	}
	return false
}

// --- Shell helpers ---

func extractShellCommand(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var parsed struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return ""
	}
	return parsed.Command
}

// matchesCommandPrefix matches using first-token boundary.
// "go test" matches "go test ./..." but NOT "gorilla-tool".
func matchesCommandPrefix(command, pattern string) bool {
	patternParts := strings.Fields(pattern)
	commandParts := strings.Fields(command)
	if len(commandParts) < len(patternParts) {
		return false
	}
	for i, pp := range patternParts {
		if commandParts[i] != pp {
			return false
		}
	}
	return true
}

// containsCommandPattern checks for pattern anywhere in the command
// (after splitting on shell separators &&, ||, ;, |).
func containsCommandPattern(fullCommand, pattern string) bool {
	if matchesCommandPrefix(fullCommand, pattern) {
		return true
	}
	for _, sep := range []string{"&&", "||", ";", "|"} {
		parts := strings.Split(fullCommand, sep)
		for _, part := range parts {
			if matchesCommandPrefix(strings.TrimSpace(part), pattern) {
				return true
			}
		}
	}
	return false
}

// --- File helpers ---

func extractFileInfo(input json.RawMessage) (operation, filePath string) {
	if len(input) == 0 {
		return "", ""
	}
	var parsed struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return "", ""
	}
	return parsed.Operation, parsed.Path
}

func matchesFilePattern(filePath, pattern string) bool {
	if matched, _ := path.Match(pattern, filePath); matched {
		return true
	}
	if matched, _ := path.Match(pattern, path.Base(filePath)); matched {
		return true
	}
	return false
}
