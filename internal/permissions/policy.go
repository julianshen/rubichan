package permissions

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Policy represents a permission policy loaded from a single source.
type Policy struct {
	Level  string      // "org", "project", "user"
	Source string      // file path this was loaded from
	Tools  ToolPolicy  `toml:"tools"`
	Shell  ShellPolicy `toml:"shell"`
	Files  FilePolicy  `toml:"files"`
	Skills SkillPolicy `toml:"skills"`
}

type ToolPolicy struct {
	Allow  []string `toml:"allow"`
	Deny   []string `toml:"deny"`
	Prompt []string `toml:"prompt"`
}

type ShellPolicy struct {
	AllowCommands  []string `toml:"allow_commands"`
	DenyCommands   []string `toml:"deny_commands"`
	PromptPatterns []string `toml:"prompt_patterns"`
}

type FilePolicy struct {
	AllowPatterns  []string `toml:"allow_patterns"`
	DenyPatterns   []string `toml:"deny_patterns"`
	PromptPatterns []string `toml:"prompt_patterns"`
}

type SkillPolicy struct {
	AutoApprove []string `toml:"auto_approve"`
	Deny        []string `toml:"deny"`
}

// LoadPolicyFile reads a TOML policy file. Returns (nil, nil) if file doesn't exist.
func LoadPolicyFile(path, level string) (*Policy, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat policy file %s: %w", path, err)
	}

	var p Policy
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return nil, fmt.Errorf("parse policy file %s: %w", path, err)
	}
	p.Level = level
	p.Source = path
	return &p, nil
}

// LoadPolicies reads policies from up to three sources in precedence order.
// Missing files are silently skipped. userPolicy may be nil.
func LoadPolicies(orgPath, projectPath string, userPolicy *Policy) ([]Policy, error) {
	var policies []Policy

	org, err := LoadPolicyFile(orgPath, "org")
	if err != nil {
		return nil, fmt.Errorf("org policy: %w", err)
	}
	if org != nil {
		policies = append(policies, *org)
	}

	project, err := LoadPolicyFile(projectPath, "project")
	if err != nil {
		return nil, fmt.Errorf("project policy: %w", err)
	}
	if project != nil {
		policies = append(policies, *project)
	}

	if userPolicy != nil {
		policies = append(policies, *userPolicy)
	}

	return policies, nil
}
