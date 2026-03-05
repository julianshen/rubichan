package toolexec

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ToolRuleConf mirrors config.ToolRuleConf to avoid circular imports.
// Callers convert from their config types to this intermediate form.
type ToolRuleConf struct {
	Category string
	Tool     string
	Pattern  string
	Action   string
}

// yamlToolRules is a partial representation of .security.yaml used to
// extract only the tool_rules section without importing internal/security.
type yamlToolRules struct {
	ToolRules []struct {
		Category string `yaml:"category"`
		Tool     string `yaml:"tool"`
		Pattern  string `yaml:"pattern"`
		Action   string `yaml:"action"`
	} `yaml:"tool_rules"`
}

// LoadSecurityYAMLRules loads tool rules from a .security.yaml file path.
// Returns nil, nil if file doesn't exist.
func LoadSecurityYAMLRules(path string) ([]PermissionRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading security YAML: %w", err)
	}

	var partial yamlToolRules
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return nil, fmt.Errorf("parsing security YAML: %w", err)
	}

	if len(partial.ToolRules) == 0 {
		return nil, nil
	}

	rules := make([]PermissionRule, len(partial.ToolRules))
	for i, r := range partial.ToolRules {
		rules[i] = PermissionRule{
			Category: Category(r.Category),
			Tool:     r.Tool,
			Pattern:  r.Pattern,
			Action:   RuleAction(r.Action),
			Source:   SourceProject,
		}
	}
	return rules, nil
}

// TOMLRulesToPermissionRules converts config-level rules to PermissionRules.
func TOMLRulesToPermissionRules(confs []ToolRuleConf, source ConfigSource) []PermissionRule {
	if len(confs) == 0 {
		return nil
	}

	rules := make([]PermissionRule, len(confs))
	for i, c := range confs {
		rules[i] = PermissionRule{
			Category: Category(c.Category),
			Tool:     c.Tool,
			Pattern:  c.Pattern,
			Action:   RuleAction(c.Action),
			Source:   source,
		}
	}
	return rules
}

// MergeRules concatenates rule slices from multiple sources.
func MergeRules(sources ...[]PermissionRule) []PermissionRule {
	var total int
	for _, s := range sources {
		total += len(s)
	}
	if total == 0 {
		return nil
	}

	merged := make([]PermissionRule, 0, total)
	for _, s := range sources {
		merged = append(merged, s...)
	}
	return merged
}
