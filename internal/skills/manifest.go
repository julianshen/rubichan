package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	BackendMCP      BackendType = "mcp"
)

var validBackends = map[BackendType]bool{
	BackendStarlark: true,
	BackendPlugin:   true,
	BackendProcess:  true,
	BackendMCP:      true,
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

// CommandDef declares a slash command contributed by a skill.
type CommandDef struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Arguments   []CommandArgDef `yaml:"arguments"`
}

// CommandArgDef describes an argument for a skill-contributed command.
type CommandArgDef struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

// AgentDefManifest describes an agent definition contributed by a skill.
type AgentDefManifest struct {
	Name          string   `yaml:"name"`
	Description   string   `yaml:"description"`
	SystemPrompt  string   `yaml:"system_prompt"`
	Tools         []string `yaml:"tools"`
	MaxTurns      int      `yaml:"max_turns"`
	MaxDepth      int      `yaml:"max_depth"`
	Model         string   `yaml:"model"`
	InheritSkills *bool    `yaml:"inherit_skills"`
	ExtraSkills   []string `yaml:"extra_skills"`
	DisableSkills []string `yaml:"disable_skills"`
}

// ReferenceDef describes an auxiliary file a skill may load on demand.
type ReferenceDef struct {
	Path string `yaml:"path"`
	When string `yaml:"when"`
}

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
	Priority       int                  `yaml:"priority"`
	ToolsAllow     []string             `yaml:"tools_allow"`
	ToolsDeny      []string             `yaml:"tools_deny"`
	References     []ReferenceDef       `yaml:"references"`
	Implementation ImplementationConfig `yaml:"implementation"`
	Prompt         PromptConfig         `yaml:"prompt"`
	Tools          []ToolDef            `yaml:"tools"`
	Commands       []CommandDef         `yaml:"commands"`
	Agents         []AgentDefManifest   `yaml:"agents"`
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

	// MCP transport fields — populated programmatically for BackendMCP skills
	// discovered from config.MCPServerConfig. Not set via YAML.
	MCPTransport string   `yaml:"-" json:"-"`
	MCPCommand   string   `yaml:"-" json:"-"`
	MCPArgs      []string `yaml:"-" json:"-"`
	MCPURL       string   `yaml:"-" json:"-"`
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

	for _, cmd := range m.Commands {
		if cmd.Name == "" {
			return fmt.Errorf("manifest validation: command name is required")
		}
	}
	for _, agent := range m.Agents {
		if agent.Name == "" {
			return fmt.Errorf("manifest validation: agent name is required")
		}
	}

	// Backend.
	if m.Implementation.Backend != "" && !validBackends[m.Implementation.Backend] {
		return fmt.Errorf("manifest validation: unknown backend %q", m.Implementation.Backend)
	}

	// Backend requires an entrypoint (except MCP, which gets transport config programmatically).
	if m.Implementation.Backend != "" && m.Implementation.Backend != BackendMCP && m.Implementation.Entrypoint == "" {
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

// instructionFrontmatter is the subset of fields allowed in a SKILL.md
// frontmatter block. Instruction skills are always prompt-only.
type instructionFrontmatter struct {
	Name        string             `yaml:"name"`
	Version     string             `yaml:"version"`
	Description string             `yaml:"description"`
	Types       []SkillType        `yaml:"types"`
	Triggers    TriggerConfig      `yaml:"triggers"`
	Permissions []Permission       `yaml:"permissions"`
	Priority    int                `yaml:"priority"`
	ToolsAllow  []string           `yaml:"tools_allow"`
	ToolsDeny   []string           `yaml:"tools_deny"`
	References  []ReferenceDef     `yaml:"references"`
	Commands    []CommandDef       `yaml:"commands"`
	Agents      []AgentDefManifest `yaml:"agents"`
}

// ParseInstructionSkill parses a SKILL.md file with YAML frontmatter delimited
// by "---" lines. Returns the synthesized manifest (always type prompt), the
// markdown body, and an error if parsing or validation fails.
func ParseInstructionSkill(data []byte) (*SkillManifest, string, error) {
	return parseInstructionSkill(data, false)
}

// ParseInstructionSkillStrict parses an instruction skill and rejects unknown
// frontmatter fields. Intended for linting and authoring workflows.
func ParseInstructionSkillStrict(data []byte) (*SkillManifest, string, error) {
	return parseInstructionSkill(data, true)
}

func parseInstructionSkill(data []byte, strict bool) (*SkillManifest, string, error) {
	// Split frontmatter from body.
	frontmatter, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, "", fmt.Errorf("instruction skill: %w", err)
	}

	var fm instructionFrontmatter
	if strict {
		dec := yaml.NewDecoder(bytes.NewReader(frontmatter))
		dec.KnownFields(true)
		err = dec.Decode(&fm)
	} else {
		err = yaml.Unmarshal(frontmatter, &fm)
	}
	if err != nil {
		return nil, "", fmt.Errorf("instruction skill: parse frontmatter: %w", err)
	}

	// Default types to [prompt] if not specified.
	if len(fm.Types) == 0 {
		fm.Types = []SkillType{SkillTypePrompt}
	}

	// Instruction skills must be prompt-only.
	for _, st := range fm.Types {
		if st != SkillTypePrompt {
			return nil, "", fmt.Errorf("instruction skill: type %q not allowed (only %q is supported)", st, SkillTypePrompt)
		}
	}

	m := &SkillManifest{
		Name:        fm.Name,
		Version:     fm.Version,
		Description: fm.Description,
		Types:       fm.Types,
		Triggers:    fm.Triggers,
		Permissions: fm.Permissions,
		Priority:    fm.Priority,
		ToolsAllow:  fm.ToolsAllow,
		ToolsDeny:   fm.ToolsDeny,
		References:  fm.References,
		Commands:    fm.Commands,
		Agents:      fm.Agents,
	}

	if err := validateManifest(m); err != nil {
		return nil, "", fmt.Errorf("instruction skill: %w", err)
	}

	return m, string(body), nil
}

// LintSkillDir performs author-facing validation for a skill directory.
// It returns a slice of issues; an empty slice means the skill passed linting.
func LintSkillDir(skillDir string) []string {
	manifest, kind, body, err := loadManifestForLint(skillDir)
	if err != nil {
		return []string{err.Error()}
	}

	var issues []string
	if kind == "instruction" {
		if len(strings.Fields(body)) > 500 {
			issues = append(issues, "instruction skill body exceeds 500 words")
		}
	}

	seenCmds := make(map[string]bool)
	for _, cmd := range manifest.Commands {
		if cmd.Name == "" {
			issues = append(issues, "command name is required")
			continue
		}
		if seenCmds[cmd.Name] {
			issues = append(issues, fmt.Sprintf("duplicate command name %q", cmd.Name))
		}
		seenCmds[cmd.Name] = true
	}

	seenAgents := make(map[string]bool)
	for _, agent := range manifest.Agents {
		if agent.Name == "" {
			issues = append(issues, "agent name is required")
			continue
		}
		if seenAgents[agent.Name] {
			issues = append(issues, fmt.Sprintf("duplicate agent name %q", agent.Name))
		}
		seenAgents[agent.Name] = true
	}

	for _, ref := range manifest.References {
		if ref.Path == "" {
			issues = append(issues, "reference path is required")
			continue
		}
		target := filepath.Join(skillDir, ref.Path)
		if _, err := os.Stat(target); err != nil {
			issues = append(issues, fmt.Sprintf("missing reference file %q", ref.Path))
		}
	}

	return issues
}

func loadManifestForLint(skillDir string) (*SkillManifest, string, string, error) {
	yamlPath := filepath.Join(skillDir, "SKILL.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		manifest, parseErr := ParseManifest(data)
		if parseErr != nil {
			return nil, "", "", fmt.Errorf("invalid manifest: %w", parseErr)
		}
		return manifest, "yaml", "", nil
	} else if !os.IsNotExist(err) {
		return nil, "", "", fmt.Errorf("reading skill manifest from %s: %w", skillDir, err)
	}

	mdPath := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", "", fmt.Errorf("reading skill manifest from %s: open %s: no such file or directory", skillDir, yamlPath)
		}
		return nil, "", "", fmt.Errorf("reading skill manifest from %s: %w", skillDir, err)
	}
	manifest, body, parseErr := ParseInstructionSkillStrict(data)
	if parseErr != nil {
		return nil, "", "", parseErr
	}
	return manifest, "instruction", body, nil
}

// splitFrontmatter extracts YAML frontmatter from markdown content.
// The frontmatter must be delimited by "---" lines. The closing delimiter
// must appear at the start of a line to avoid matching "---" as a substring
// inside YAML values.
func splitFrontmatter(data []byte) ([]byte, []byte, error) {
	sep := []byte("---")

	// Must start with "---".
	trimmed := bytes.TrimLeft(data, " \t")
	if !bytes.HasPrefix(trimmed, sep) {
		return nil, nil, fmt.Errorf("missing frontmatter delimiter (---)")
	}

	// Find opening delimiter and skip to next line.
	start := bytes.Index(data, sep)
	rest := data[start+len(sep):]

	// Find closing delimiter at the start of a line ("\n---").
	closingMarker := []byte("\n---")
	end := bytes.Index(rest, closingMarker)
	if end < 0 {
		return nil, nil, fmt.Errorf("missing closing frontmatter delimiter (---)")
	}

	frontmatter := bytes.TrimSpace(rest[:end])
	body := bytes.TrimSpace(rest[end+len(closingMarker):])

	return frontmatter, body, nil
}
