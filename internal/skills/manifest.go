package skills

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// SkillType represents the type of skill (tool, prompt, workflow, security-rule, transform).
type SkillType string

const (
	SkillTypeTool         SkillType = "tool"
	SkillTypePrompt       SkillType = "prompt"
	SkillTypeWorkflow     SkillType = "workflow"
	SkillTypeSecurityRule SkillType = "security-rule"
	SkillTypeTransform    SkillType = "transform"
)

var validSkillTypes = map[SkillType]bool{
	SkillTypeTool:         true,
	SkillTypePrompt:       true,
	SkillTypeWorkflow:     true,
	SkillTypeSecurityRule: true,
	SkillTypeTransform:    true,
}

// BackendType represents the implementation backend for a skill.
type BackendType string

const (
	BackendStarlark BackendType = "starlark"
	BackendPlugin   BackendType = "plugin"
	BackendProcess  BackendType = "process"
)

var validBackends = map[BackendType]bool{
	BackendStarlark: true,
	BackendPlugin:   true,
	BackendProcess:  true,
}

// Permission represents a declared permission required by a skill.
type Permission string

const (
	PermFileRead    Permission = "file:read"
	PermFileWrite   Permission = "file:write"
	PermShellExec   Permission = "shell:exec"
	PermNetFetch    Permission = "net:fetch"
	PermLLMCall     Permission = "llm:call"
	PermGitRead     Permission = "git:read"
	PermGitWrite    Permission = "git:write"
	PermEnvRead     Permission = "env:read"
	PermEnvWrite    Permission = "env:write"
	PermSkillInvoke Permission = "skill:invoke"
)

var validPermissions = map[Permission]bool{
	PermFileRead:    true,
	PermFileWrite:   true,
	PermShellExec:   true,
	PermNetFetch:    true,
	PermLLMCall:     true,
	PermGitRead:     true,
	PermGitWrite:    true,
	PermEnvRead:     true,
	PermEnvWrite:    true,
	PermSkillInvoke: true,
}

// nameRegex enforces lowercase letters, digits, and hyphens, starting with a letter.
// Hyphens must be followed by at least one alphanumeric character (no trailing hyphens).
var nameRegex = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// SkillManifest represents a parsed SKILL.yaml file.
type SkillManifest struct {
	Name           string               `yaml:"name"`
	Version        string               `yaml:"version"`
	Description    string               `yaml:"description"`
	Types          []SkillType          `yaml:"types"`
	Author         string               `yaml:"author"`
	License        string               `yaml:"license"`
	Homepage       string               `yaml:"homepage"`
	Triggers       TriggerConfig        `yaml:"triggers"`
	Permissions    []Permission         `yaml:"permissions"`
	Dependencies   []Dependency         `yaml:"dependencies"`
	Implementation ImplementationConfig `yaml:"implementation"`
	Prompt         PromptConfig         `yaml:"prompt"`
	Tools          []ToolDef            `yaml:"tools"`
	SecurityRules  SecurityRuleConfig   `yaml:"security_rules"`
	Wiki           WikiConfig           `yaml:"wiki"`
	Compatibility  CompatibilityConfig  `yaml:"compatibility"`
}

// TriggerConfig defines when a skill should auto-activate.
type TriggerConfig struct {
	Files     []string `yaml:"files"`
	Keywords  []string `yaml:"keywords"`
	Modes     []string `yaml:"modes"`
	Languages []string `yaml:"languages"`
}

// ImplementationConfig specifies how the skill is executed.
type ImplementationConfig struct {
	Backend    BackendType `yaml:"backend"`
	Entrypoint string      `yaml:"entrypoint"`
}

// Dependency represents a dependency on another skill.
type Dependency struct {
	Name     string `yaml:"name"`
	Version  string `yaml:"version"`
	Optional bool   `yaml:"optional"`
}

// PromptConfig holds prompt skill configuration.
type PromptConfig struct {
	SystemPromptFile string   `yaml:"system_prompt_file"`
	ContextFiles     []string `yaml:"context_files"`
	MaxContextTokens int      `yaml:"max_context_tokens"`
}

// ToolDef defines a tool provided by the skill.
type ToolDef struct {
	Name             string `yaml:"name"`
	Description      string `yaml:"description"`
	InputSchemaFile  string `yaml:"input_schema_file"`
	RequiresApproval bool   `yaml:"requires_approval"`
}

// SecurityRuleConfig holds security rule skill configuration.
type SecurityRuleConfig struct {
	SASTRulesFile string       `yaml:"sast_rules_file"`
	Scanners      []ScannerDef `yaml:"scanners"`
	OverridesFile string       `yaml:"overrides_file"`
}

// ScannerDef defines a custom scanner provided by the skill.
type ScannerDef struct {
	Name       string `yaml:"name"`
	Entrypoint string `yaml:"entrypoint"`
}

// WikiConfig holds wiki contribution configuration.
type WikiConfig struct {
	Sections []WikiSection `yaml:"sections"`
	Diagrams []WikiDiagram `yaml:"diagrams"`
}

// WikiSection defines a wiki section contributed by the skill.
type WikiSection struct {
	Title    string `yaml:"title"`
	Template string `yaml:"template"`
	Analyzer string `yaml:"analyzer"`
}

// WikiDiagram defines a wiki diagram contributed by the skill.
type WikiDiagram struct {
	Type     string `yaml:"type"`
	Template string `yaml:"template"`
}

// CompatibilityConfig holds version and platform compatibility constraints.
type CompatibilityConfig struct {
	AgentVersion string   `yaml:"agent_version"`
	Platforms    []string `yaml:"platforms"`
}

// ParseManifest parses raw YAML bytes into a validated SkillManifest.
func ParseManifest(data []byte) (*SkillManifest, error) {
	var m SkillManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if err := validateManifest(&m); err != nil {
		return nil, err
	}

	return &m, nil
}

func validateManifest(m *SkillManifest) error {
	// Required fields.
	if m.Name == "" {
		return fmt.Errorf("manifest validation: name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest validation: version is required")
	}
	if m.Description == "" {
		return fmt.Errorf("manifest validation: description is required")
	}
	if len(m.Types) == 0 {
		return fmt.Errorf("manifest validation: types is required (at least one skill type)")
	}

	// Name format and length.
	const maxNameLength = 128
	if len(m.Name) > maxNameLength {
		return fmt.Errorf("manifest validation: name exceeds maximum length (%d > %d)", len(m.Name), maxNameLength)
	}
	if !nameRegex.MatchString(m.Name) {
		return fmt.Errorf("manifest validation: invalid name %q (must match %s)", m.Name, nameRegex.String())
	}

	// Skill types.
	for _, st := range m.Types {
		if !validSkillTypes[st] {
			return fmt.Errorf("manifest validation: unknown skill type %q", st)
		}
	}

	// Permissions.
	for _, p := range m.Permissions {
		if !validPermissions[p] {
			return fmt.Errorf("manifest validation: unknown permission %q", p)
		}
	}

	// Dependency names.
	for _, dep := range m.Dependencies {
		if !nameRegex.MatchString(dep.Name) {
			return fmt.Errorf("manifest validation: invalid dependency name %q (must match %s)", dep.Name, nameRegex.String())
		}
	}

	// Backend.
	if m.Implementation.Backend != "" && !validBackends[m.Implementation.Backend] {
		return fmt.Errorf("manifest validation: unknown backend %q", m.Implementation.Backend)
	}

	// Backend requires an entrypoint.
	if m.Implementation.Backend != "" && m.Implementation.Entrypoint == "" {
		return fmt.Errorf("manifest validation: implementation.entrypoint is required when backend is specified")
	}

	// Non-prompt skill types require a backend.
	if m.Implementation.Backend == "" {
		for _, st := range m.Types {
			if st != SkillTypePrompt {
				return fmt.Errorf("manifest validation: implementation.backend is required for non-prompt skill type %q", st)
			}
		}
	}

	return nil
}
