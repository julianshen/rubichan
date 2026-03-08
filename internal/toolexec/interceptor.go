package toolexec

import (
	"fmt"
	"regexp"
	"strings"
)

// InterceptionAction defines the response when a command matches an interception rule.
type InterceptionAction int

const (
	// ActionWarn allows execution but prefixes a warning in the output.
	ActionWarn InterceptionAction = iota
	// ActionBlock denies execution entirely.
	ActionBlock
	// ActionRouteToFileTool redirects the command to the file tool.
	ActionRouteToFileTool
)

// InterceptionRule pairs a regex pattern with an action and message.
type InterceptionRule struct {
	Pattern *regexp.Regexp
	Action  InterceptionAction
	Message string
}

// CommandInterceptor evaluates shell commands against configurable rules
// to detect file-modifying patterns. It produces block, warn, and route
// decisions as a defense-in-depth safety net.
type CommandInterceptor struct {
	rules   []InterceptionRule
	workDir string
}

// NewCommandInterceptor creates a CommandInterceptor with the given rules.
// If rules is nil, DefaultInterceptionRules() is used.
func NewCommandInterceptor(workDir string, rules []InterceptionRule) *CommandInterceptor {
	if rules == nil {
		rules = DefaultInterceptionRules()
	}
	return &CommandInterceptor{
		rules:   rules,
		workDir: workDir,
	}
}

// DefaultInterceptionRules returns the standard set of interception rules for
// detecting file-modifying shell patterns.
func DefaultInterceptionRules() []InterceptionRule {
	return []InterceptionRule{
		{
			Pattern: regexp.MustCompile(`(?i)\b(?:echo|cat)\b[^;\n]*\s(?:>>?)\s*[^\s;]+`),
			Action:  ActionWarn,
			Message: "command redirects output to a file",
		},
		{
			Pattern: regexp.MustCompile(`(?i)\bsed\b[^;\n]*\s-i(?:\s|$)`),
			Action:  ActionWarn,
			Message: "command uses sed -i for in-place file edits",
		},
		{
			Pattern: regexp.MustCompile(`(?i)\b(?:chmod|chown)\b`),
			Action:  ActionWarn,
			Message: "command changes file ownership/permissions",
		},
		{
			Pattern: regexp.MustCompile(`(?i)\b(?:mv|cp)\b[^;\n]*(?:\s/\S+|\s\.\./\S+)`),
			Action:  ActionWarn,
			Message: "command may move/copy files outside the working directory",
		},
		{
			Pattern: regexp.MustCompile(`(?i)\btee\b`),
			Action:  ActionWarn,
			Message: "command uses tee to write to a file",
		},
		{
			Pattern: regexp.MustCompile(`(?i)\bdd\b[^;\n]*\bof=`),
			Action:  ActionWarn,
			Message: "command uses dd to write to a file",
		},
		{
			Pattern: regexp.MustCompile(`(?i)\btruncate\b`),
			Action:  ActionWarn,
			Message: "command uses truncate to modify a file",
		},
	}
}

// Intercept evaluates a shell command against the interceptor's rules and
// AST-parsed command parts. It checks for apply_patch routing, recursive rm
// outside the working directory, and all regex-based interception rules.
func (ci *CommandInterceptor) Intercept(command string, parts []CommandPart) ShellInterception {
	var interception ShellInterception

	// Check for apply_patch routing.
	for _, part := range parts {
		if part.Prefix == "apply_patch" {
			interception.RouteReason = "apply_patch shell commands must be routed through the file tool"
			break
		}
	}

	// Check for recursive rm outside working directory.
	if interception.BlockReason == "" {
		if outsideTargets := findRecursiveRMOutsideWorkdir(parts, ci.workDir); len(outsideTargets) > 0 {
			interception.BlockReason = fmt.Sprintf(
				"recursive rm target(s) escape working directory: %s",
				strings.Join(outsideTargets, ", "),
			)
		}
	}

	// Apply regex-based rules.
	for _, rule := range ci.rules {
		if !rule.Pattern.MatchString(command) {
			continue
		}
		switch rule.Action {
		case ActionWarn:
			interception.Warnings = append(interception.Warnings, rule.Message)
		case ActionBlock:
			if interception.BlockReason == "" {
				interception.BlockReason = rule.Message
			}
		case ActionRouteToFileTool:
			if interception.RouteReason == "" {
				interception.RouteReason = rule.Message
			}
		}
	}

	return interception
}

// Rules returns the current interception rules. The returned slice is a copy.
func (ci *CommandInterceptor) Rules() []InterceptionRule {
	out := make([]InterceptionRule, len(ci.rules))
	copy(out, ci.rules)
	return out
}
