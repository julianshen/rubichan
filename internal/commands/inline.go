package commands

import (
	"encoding/json"
	"fmt"
	"strings"
)

type inlineSkillDirective struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

type InlineSkillDirectiveResult struct {
	// Command is a display-friendly rendering of Args.
	Command string
	Args    []string
	Name    string
	Action  string
}

func formatCommandDisplay(args []string) string {
	if len(args) == 0 {
		return ""
	}

	rendered := make([]string, len(args))
	for i, arg := range args {
		if arg != "" && !strings.ContainsAny(arg, " \t\n\"\\") {
			rendered[i] = arg
			continue
		}

		escaped := strings.ReplaceAll(arg, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		rendered[i] = `"` + escaped + `"`
	}
	return strings.Join(rendered, " ")
}

// RewriteInlineSkillDirective converts __skill({...}) into an equivalent
// slash command so existing command handling can execute it.
func RewriteInlineSkillDirective(line string) (InlineSkillDirectiveResult, bool, error) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "__skill(") || !strings.HasSuffix(trimmed, ")") {
		return InlineSkillDirectiveResult{}, false, nil
	}

	payload := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "__skill("), ")"))
	if payload == "" {
		return InlineSkillDirectiveResult{}, true, fmt.Errorf("skill directive payload is required")
	}

	var directive inlineSkillDirective
	if err := json.Unmarshal([]byte(payload), &directive); err != nil {
		return InlineSkillDirectiveResult{}, true, fmt.Errorf("parse skill directive: %w", err)
	}

	name := strings.TrimSpace(directive.Name)
	if name == "" {
		return InlineSkillDirectiveResult{}, true, fmt.Errorf("skill directive name is required")
	}

	action := strings.ToLower(strings.TrimSpace(directive.Action))
	if action == "" {
		action = "activate"
	}
	switch action {
	case "activate", "deactivate":
	default:
		return InlineSkillDirectiveResult{}, true, fmt.Errorf("unsupported skill directive action %q", directive.Action)
	}

	args := []string{"/skill", action, name}
	return InlineSkillDirectiveResult{
		Command: formatCommandDisplay(args),
		Args:    args,
		Name:    name,
		Action:  action,
	}, true, nil
}
