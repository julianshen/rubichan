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
		cmd, err := extractShellCommand(input)
		if err != nil {
			return true // fail-closed: malformed input is treated as denied
		}
		for _, pattern := range p.Shell.DenyCommands {
			if containsCommandPattern(cmd, pattern) {
				return true
			}
		}
	}
	if tool == "file" {
		op, fp, err := extractFileInfo(input)
		if err != nil {
			return true // fail-closed: malformed input is treated as denied
		}
		if isDestructiveFileOp(op) {
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
		cmd, err := extractShellCommand(input)
		if err != nil {
			return true // fail-closed: malformed input requires approval
		}
		for _, pattern := range p.Shell.PromptPatterns {
			if containsCommandPattern(cmd, pattern) {
				return true
			}
		}
	}
	if tool == "file" {
		op, fp, err := extractFileInfo(input)
		if err != nil {
			return true // fail-closed: malformed input requires approval
		}
		if isDestructiveFileOp(op) {
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
		cmd, err := extractShellCommand(input)
		if err != nil {
			return false // fail-closed: malformed input is never auto-allowed
		}
		if cmd != "" && allSubCommandsAllowed(cmd, p.Shell.AllowCommands) {
			return true
		}
	}
	if tool == "file" {
		op, fp, err := extractFileInfo(input)
		if err != nil {
			return false // fail-closed: malformed input is never auto-allowed
		}
		if isDestructiveFileOp(op) {
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

func extractShellCommand(input json.RawMessage) (string, error) {
	if len(input) == 0 {
		return "", nil
	}
	var parsed struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return "", fmt.Errorf("malformed shell input: %w", err)
	}
	return parsed.Command, nil
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

// containsCommandPattern checks for pattern anywhere in the command,
// splitting on shell separators (&&, ||, ;, |, newlines) and also
// extracting content from subshell constructs ($() and backticks).
func containsCommandPattern(fullCommand, pattern string) bool {
	// Collect all sub-commands to check
	subCommands := splitShellCommands(fullCommand)
	for _, sub := range subCommands {
		if matchesCommandPrefix(strings.TrimSpace(sub), pattern) {
			return true
		}
	}
	return false
}

// allSubCommandsAllowed returns true only if every sub-command in a compound
// command matches at least one allow pattern. This prevents "go test && rm -rf /"
// from being auto-approved when only "go test" is in the allow list.
func allSubCommandsAllowed(fullCommand string, allowPatterns []string) bool {
	subs := splitShellCommands(fullCommand)
	if len(subs) == 0 {
		return false
	}
	// Check each unique sub-command (skip the full command entry at index 0
	// if there are sub-parts, to avoid matching the compound command as a whole)
	checked := 0
	for _, sub := range subs {
		sub = strings.TrimSpace(sub)
		if sub == "" || sub == fullCommand {
			continue
		}
		matched := false
		for _, pattern := range allowPatterns {
			if matchesCommandPrefix(sub, pattern) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
		checked++
	}
	// If no sub-parts found (simple command), check the full command directly
	if checked == 0 {
		for _, pattern := range allowPatterns {
			if matchesCommandPrefix(fullCommand, pattern) {
				return true
			}
		}
		return false
	}
	return true
}

// splitShellCommands splits a command on separators (&&, ||, ;, |, \n)
// and also extracts content from $() and backtick subshells.
func splitShellCommands(cmd string) []string {
	var parts []string
	parts = append(parts, cmd) // always check the full command

	// Split on newlines first
	for _, line := range strings.Split(cmd, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Split on shell separators
		for _, sep := range []string{"&&", "||", ";", "|"} {
			for _, part := range strings.Split(line, sep) {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					parts = append(parts, trimmed)
				}
			}
		}
	}

	// Extract $() subshell content
	for i := 0; i < len(cmd); i++ {
		if i+1 < len(cmd) && cmd[i] == '$' && cmd[i+1] == '(' {
			depth := 1
			start := i + 2
			for j := start; j < len(cmd) && depth > 0; j++ {
				if cmd[j] == '(' {
					depth++
				} else if cmd[j] == ')' {
					depth--
					if depth == 0 {
						inner := cmd[start:j]
						parts = append(parts, strings.TrimSpace(inner))
						parts = append(parts, splitShellCommands(inner)...)
					}
				}
			}
		}
	}

	// Extract backtick content
	inBacktick := false
	start := 0
	for i := 0; i < len(cmd); i++ {
		if cmd[i] == '`' {
			if inBacktick {
				inner := cmd[start:i]
				parts = append(parts, strings.TrimSpace(inner))
				parts = append(parts, splitShellCommands(inner)...)
				inBacktick = false
			} else {
				inBacktick = true
				start = i + 1
			}
		}
	}

	return parts
}

// --- File helpers ---

// isDestructiveFileOp returns true for file operations that modify or remove
// files and therefore must be checked against deny/prompt/allow patterns.
func isDestructiveFileOp(op string) bool {
	return op == "write" || op == "patch" || op == "delete"
}

func extractFileInfo(input json.RawMessage) (operation, filePath string, err error) {
	if len(input) == 0 {
		return "", "", nil
	}
	var parsed struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return "", "", fmt.Errorf("malformed file input: %w", err)
	}
	return parsed.Operation, parsed.Path, nil
}

func matchesFilePattern(filePath, pattern string) bool {
	filePath = path.Clean(filePath)
	if matched, _ := path.Match(pattern, filePath); matched {
		return true
	}
	if matched, _ := path.Match(pattern, path.Base(filePath)); matched {
		return true
	}
	return false
}
