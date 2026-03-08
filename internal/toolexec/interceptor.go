package toolexec

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// InterceptionAction defines the response when a command matches an
// interception rule.
type InterceptionAction int

const (
	// ActionWarn allows execution but prefixes a warning in the output.
	ActionWarn InterceptionAction = iota
	// ActionBlock denies execution entirely.
	ActionBlock
	// ActionRouteToFileTool redirects the command to the file tool.
	ActionRouteToFileTool
)

// InterceptionRule pairs a compiled regex pattern with an action and a
// human-readable message that is surfaced when the pattern matches.
type InterceptionRule struct {
	// Pattern is the compiled regex used to match against the raw command
	// string. Must not be nil.
	Pattern *regexp.Regexp
	// Action determines how a matching command is handled (warn, block,
	// or route to the file tool).
	Action InterceptionAction
	// Message is the human-readable explanation shown when the rule fires.
	Message string
}

// CommandInterceptor evaluates shell commands against configurable rules
// to detect file-modifying patterns. It produces block, warn, and route
// decisions as a defense-in-depth safety net.
//
// Rules are evaluated in order. For ActionBlock and ActionRouteToFileTool,
// only the first matching rule's message is recorded (subsequent matches
// for the same action kind are ignored). For ActionWarn, every matching
// rule appends a warning. Callers that supply custom rules should order
// them from highest to lowest priority.
type CommandInterceptor struct {
	rules   []InterceptionRule
	workDir string
}

// NewCommandInterceptor creates a CommandInterceptor with the given rules.
// If rules is nil, DefaultInterceptionRules() is used. The rules slice is
// defensively copied so that later mutations by the caller do not affect
// the interceptor. Rules with a nil Pattern are rejected and cause an error.
func NewCommandInterceptor(workDir string, rules []InterceptionRule) (*CommandInterceptor, error) {
	if rules == nil {
		rules = DefaultInterceptionRules()
	}
	copied := make([]InterceptionRule, len(rules))
	for i, r := range rules {
		if r.Pattern == nil {
			return nil, errors.New("interception rule has nil Pattern")
		}
		copied[i] = r
	}
	return &CommandInterceptor{
		rules:   copied,
		workDir: workDir,
	}, nil
}

// MustNewCommandInterceptor is like NewCommandInterceptor but panics on
// error. It is intended for use with known-good rule sets such as
// DefaultInterceptionRules.
func MustNewCommandInterceptor(workDir string, rules []InterceptionRule) *CommandInterceptor {
	ci, err := NewCommandInterceptor(workDir, rules)
	if err != nil {
		panic(fmt.Sprintf("toolexec: NewCommandInterceptor: %v", err))
	}
	return ci
}

// DefaultInterceptionRules returns the standard set of interception rules
// for detecting file-modifying shell patterns.
func DefaultInterceptionRules() []InterceptionRule {
	return []InterceptionRule{
		{
			Pattern: regexp.MustCompile(`(?i)\b(?:echo|cat)\b[^;\n]*\s*(?:>>?)\s*[^\s;]+`),
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
			Pattern: regexp.MustCompile(`(?i)\b(?:mv|cp)\b[^;\n]*(?:\s/\S*|\s\.\./\S*)`),
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
// AST-parsed command parts. It checks for apply_patch routing, recursive
// rm outside the working directory, and all regex-based interception rules.
//
// Rules are evaluated in declaration order. For ActionBlock and
// ActionRouteToFileTool only the first match is recorded; for ActionWarn
// every match appends a warning. See [CommandInterceptor] for details on
// rule ordering.
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

	// Warn on any recursive rm (even inside the working directory).
	for _, part := range parts {
		if part.Prefix == "rm" && isRecursiveRM(part.Full) {
			interception.Warnings = append(interception.Warnings, "command uses recursive rm")
			break
		}
	}

	// Apply regex-based rules in order.
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

// Rules returns the current interception rules. The returned slice is a
// copy so that callers cannot mutate the interceptor's internal state.
func (ci *CommandInterceptor) Rules() []InterceptionRule {
	out := make([]InterceptionRule, len(ci.rules))
	copy(out, ci.rules)
	return out
}
