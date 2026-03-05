package toolexec

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// RuleAction represents the permission decision for a tool call.
type RuleAction string

const (
	ActionAllow RuleAction = "allow"
	ActionAsk   RuleAction = "ask"
	ActionDeny  RuleAction = "deny"
)

// ConfigSource indicates where a permission rule originated.
// It is informational only and does not affect evaluation precedence.
type ConfigSource int

const (
	SourceDefault ConfigSource = iota
	SourceUser
	SourceProject
	SourceLocal
)

// PermissionRule defines a single permission policy for tool execution.
// A rule targets tools by Category, Tool name, or both. The Pattern field
// is a glob matched against all string values in the tool input JSON.
type PermissionRule struct {
	Category Category // target by category (e.g. "bash")
	Tool     string   // or target by tool name (e.g. "shell")
	Pattern  string   // glob pattern matched against input string values
	Action   RuleAction
	Source   ConfigSource // informational only, does not affect precedence
}

// ruleActionKey is the context key for storing RuleAction values.
type ruleActionKey struct{}

// WithRuleAction returns a new context carrying the given RuleAction.
func WithRuleAction(ctx context.Context, action RuleAction) context.Context {
	return context.WithValue(ctx, ruleActionKey{}, action)
}

// RuleActionFromContext extracts the RuleAction from the context.
// Returns the zero value ("") when no action has been set.
func RuleActionFromContext(ctx context.Context) RuleAction {
	action, _ := ctx.Value(ruleActionKey{}).(RuleAction)
	return action
}

// categoryDefaults maps categories to their default actions when no rule matches.
var categoryDefaults = map[Category]RuleAction{
	CategoryFileRead: ActionAllow,
	CategorySearch:   ActionAllow,
	CategoryAgent:    ActionAllow,
	CategoryBash:     ActionAsk,
	CategoryGit:      ActionAsk,
	CategoryNet:      ActionAsk,
	CategoryMCP:      ActionAsk,
	CategoryPlatform: ActionAsk,
	CategorySkill:    ActionAsk,
}

// RuleEngine evaluates permission rules against tool calls.
// Deny rules are checked first and always win, then ask rules,
// then allow rules. If no rule matches, category defaults apply.
type RuleEngine struct {
	rules []PermissionRule
}

// NewRuleEngine creates a RuleEngine with the given rules.
func NewRuleEngine(rules []PermissionRule) *RuleEngine {
	return &RuleEngine{rules: rules}
}

// Evaluate checks the rule set against the given category, tool name,
// and input. It returns the highest-priority matching action:
// deny > ask > allow > category default.
func (e *RuleEngine) Evaluate(cat Category, toolName string, input json.RawMessage) RuleAction {
	inputStrings := extractStringValues(input)

	var hasDeny, hasAsk, hasAllow bool

	for _, rule := range e.rules {
		if !ruleTargetMatches(rule, cat, toolName) {
			continue
		}
		if !patternMatchesInput(rule.Pattern, inputStrings) {
			continue
		}

		switch rule.Action {
		case ActionDeny:
			hasDeny = true
		case ActionAsk:
			hasAsk = true
		case ActionAllow:
			hasAllow = true
		}
	}

	// Deny always wins.
	if hasDeny {
		return ActionDeny
	}
	if hasAsk {
		return ActionAsk
	}
	if hasAllow {
		return ActionAllow
	}

	// No rule matched — fall back to category default.
	if action, ok := categoryDefaults[cat]; ok {
		return action
	}
	return ActionAsk
}

// ruleTargetMatches checks whether a rule's category or tool name
// matches the given values. If both fields are empty, the rule is skipped.
func ruleTargetMatches(rule PermissionRule, cat Category, toolName string) bool {
	if rule.Category == "" && rule.Tool == "" {
		return false
	}

	if rule.Category != "" && rule.Category == cat {
		return true
	}
	if rule.Tool != "" && rule.Tool == toolName {
		return true
	}
	return false
}

// patternMatchesInput checks whether the glob pattern matches any of the
// input strings. An empty pattern matches everything.
func patternMatchesInput(pattern string, inputStrings []string) bool {
	if pattern == "" {
		return true
	}
	re, err := regexp.Compile(globToRegex(pattern))
	if err != nil {
		return false
	}
	for _, s := range inputStrings {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// globToRegex converts a glob pattern (*, ?, [abc]) to a regex pattern string.
func globToRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")

	i := 0
	for i < len(pattern) {
		ch := pattern[i]
		switch ch {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			// Find the closing bracket.
			j := i + 1
			for j < len(pattern) && pattern[j] != ']' {
				j++
			}
			if j < len(pattern) {
				b.WriteString(pattern[i : j+1])
				i = j
			} else {
				// No closing bracket, escape the opening bracket.
				b.WriteString("\\[")
			}
		case '.', '+', '^', '$', '(', ')', '{', '}', '|', '\\':
			b.WriteByte('\\')
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
		i++
	}
	b.WriteString("$")
	return b.String()
}

// extractStringValues recursively extracts all string values from a
// JSON message, traversing objects and arrays.
func extractStringValues(data json.RawMessage) []string {
	if len(data) == 0 {
		return nil
	}

	var result []string

	// Try to unmarshal as a string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return []string{s}
	}

	// Try as an object.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		for _, v := range obj {
			result = append(result, extractStringValues(v)...)
		}
		return result
	}

	// Try as an array.
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err == nil {
		for _, v := range arr {
			result = append(result, extractStringValues(v)...)
		}
		return result
	}

	// Numbers, booleans, null — no strings to extract.
	return nil
}

// RuleEngineMiddleware returns a Middleware that evaluates the rule engine
// before calling the next handler. If the engine returns ActionDeny, the
// middleware short-circuits with an error result. Otherwise, the action
// is stored in the context for downstream handlers.
func RuleEngineMiddleware(engine *RuleEngine) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			cat := CategoryFromContext(ctx)
			action := engine.Evaluate(cat, tc.Name, tc.Input)

			if action == ActionDeny {
				return Result{
					Content: fmt.Sprintf("tool %q blocked by deny rule", tc.Name),
					IsError: true,
				}
			}

			ctx = WithRuleAction(ctx, action)
			return next(ctx, tc)
		}
	}
}
