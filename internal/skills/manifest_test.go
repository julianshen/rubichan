package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseManifestMinimal(t *testing.T) {
	yaml := []byte(`
name: my-skill
version: 1.0.0
description: "A minimal skill"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
`)
	m, err := ParseManifest(yaml)
	require.NoError(t, err)
	require.NotNil(t, m)

	assert.Equal(t, "my-skill", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Equal(t, "A minimal skill", m.Description)
	assert.Equal(t, []SkillType{SkillTypeTool}, m.Types)
	assert.Equal(t, BackendStarlark, m.Implementation.Backend)
	assert.Equal(t, "skill.star", m.Implementation.Entrypoint)

	// Optional fields should be zero values.
	assert.Empty(t, m.Author)
	assert.Empty(t, m.License)
	assert.Empty(t, m.Homepage)
	assert.Empty(t, m.Triggers.Files)
	assert.Empty(t, m.Triggers.Keywords)
	assert.Empty(t, m.Triggers.Modes)
	assert.Empty(t, m.Triggers.Languages)
	assert.Empty(t, m.Permissions)
	assert.Empty(t, m.Dependencies)
	assert.Empty(t, m.Tools)
}

func TestParseManifestFull(t *testing.T) {
	yaml := []byte(`
name: kubernetes
version: 1.2.0
description: "Kubernetes operations, troubleshooting, and best practices"
author: "community"
license: MIT
homepage: https://github.com/aiagent-skills/kubernetes
types:
  - tool
  - prompt
  - security-rule
triggers:
  files:
    - "*.yaml"
    - "Dockerfile"
  keywords:
    - "kubernetes"
    - "k8s"
  modes:
    - interactive
    - headless
  languages:
    - yaml
    - dockerfile
permissions:
  - shell:exec
  - file:read
  - net:fetch
  - env:read
dependencies:
  - name: docker
    version: ">=1.0.0"
    optional: true
implementation:
  backend: starlark
  entrypoint: skill.star
prompt:
  system_prompt_file: prompts/system.md
  context_files:
    - prompts/troubleshooting.md
    - prompts/best-practices.md
  max_context_tokens: 4000
tools:
  - name: kubectl_get
    description: "Get Kubernetes resources"
    input_schema_file: schemas/kubectl_get.json
  - name: kubectl_apply
    description: "Apply a Kubernetes manifest"
    input_schema_file: schemas/kubectl_apply.json
    requires_approval: true
security_rules:
  sast_rules_file: rules/sast.yaml
  scanners:
    - name: k8s-manifest-scanner
      entrypoint: scanners/manifests.star
  overrides_file: rules/overrides.yaml
wiki:
  sections:
    - title: "Kubernetes Architecture"
      template: wiki/k8s-architecture.md
      analyzer: analyzers/k8s-wiki.star
  diagrams:
    - type: deployment-topology
      template: wiki/deployment-diagram.mermaid.tmpl
compatibility:
  agent_version: ">=1.0.0"
  platforms:
    - linux
    - darwin
`)
	m, err := ParseManifest(yaml)
	require.NoError(t, err)
	require.NotNil(t, m)

	// Core fields.
	assert.Equal(t, "kubernetes", m.Name)
	assert.Equal(t, "1.2.0", m.Version)
	assert.Equal(t, "Kubernetes operations, troubleshooting, and best practices", m.Description)
	assert.Equal(t, "community", m.Author)
	assert.Equal(t, "MIT", m.License)
	assert.Equal(t, "https://github.com/aiagent-skills/kubernetes", m.Homepage)

	// Types.
	assert.Equal(t, []SkillType{SkillTypeTool, SkillTypePrompt, SkillTypeSecurityRule}, m.Types)

	// Triggers.
	assert.Equal(t, []string{"*.yaml", "Dockerfile"}, m.Triggers.Files)
	assert.Equal(t, []string{"kubernetes", "k8s"}, m.Triggers.Keywords)
	assert.Equal(t, []string{"interactive", "headless"}, m.Triggers.Modes)
	assert.Equal(t, []string{"yaml", "dockerfile"}, m.Triggers.Languages)

	// Permissions.
	assert.Equal(t, []Permission{PermShellExec, PermFileRead, PermNetFetch, PermEnvRead}, m.Permissions)

	// Dependencies.
	require.Len(t, m.Dependencies, 1)
	assert.Equal(t, "docker", m.Dependencies[0].Name)
	assert.Equal(t, ">=1.0.0", m.Dependencies[0].Version)
	assert.True(t, m.Dependencies[0].Optional)

	// Implementation.
	assert.Equal(t, BackendStarlark, m.Implementation.Backend)
	assert.Equal(t, "skill.star", m.Implementation.Entrypoint)

	// Prompt.
	assert.Equal(t, "prompts/system.md", m.Prompt.SystemPromptFile)
	assert.Equal(t, []string{"prompts/troubleshooting.md", "prompts/best-practices.md"}, m.Prompt.ContextFiles)
	assert.Equal(t, 4000, m.Prompt.MaxContextTokens)

	// Tools.
	require.Len(t, m.Tools, 2)
	assert.Equal(t, "kubectl_get", m.Tools[0].Name)
	assert.Equal(t, "Get Kubernetes resources", m.Tools[0].Description)
	assert.Equal(t, "schemas/kubectl_get.json", m.Tools[0].InputSchemaFile)
	assert.False(t, m.Tools[0].RequiresApproval)
	assert.Equal(t, "kubectl_apply", m.Tools[1].Name)
	assert.True(t, m.Tools[1].RequiresApproval)

	// Security rules.
	assert.Equal(t, "rules/sast.yaml", m.SecurityRules.SASTRulesFile)
	require.Len(t, m.SecurityRules.Scanners, 1)
	assert.Equal(t, "k8s-manifest-scanner", m.SecurityRules.Scanners[0].Name)
	assert.Equal(t, "scanners/manifests.star", m.SecurityRules.Scanners[0].Entrypoint)
	assert.Equal(t, "rules/overrides.yaml", m.SecurityRules.OverridesFile)

	// Wiki.
	require.Len(t, m.Wiki.Sections, 1)
	assert.Equal(t, "Kubernetes Architecture", m.Wiki.Sections[0].Title)
	assert.Equal(t, "wiki/k8s-architecture.md", m.Wiki.Sections[0].Template)
	assert.Equal(t, "analyzers/k8s-wiki.star", m.Wiki.Sections[0].Analyzer)
	require.Len(t, m.Wiki.Diagrams, 1)
	assert.Equal(t, "deployment-topology", m.Wiki.Diagrams[0].Type)
	assert.Equal(t, "wiki/deployment-diagram.mermaid.tmpl", m.Wiki.Diagrams[0].Template)

	// Compatibility.
	assert.Equal(t, ">=1.0.0", m.Compatibility.AgentVersion)
	assert.Equal(t, []string{"linux", "darwin"}, m.Compatibility.Platforms)
}

func TestParseManifestMissingName(t *testing.T) {
	yaml := []byte(`
version: 1.0.0
description: "A skill without a name"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestParseManifestMissingTypes(t *testing.T) {
	yaml := []byte(`
name: my-skill
version: 1.0.0
description: "A skill without types"
implementation:
  backend: starlark
  entrypoint: skill.star
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "types")
}

func TestParseManifestInvalidPermission(t *testing.T) {
	yaml := []byte(`
name: my-skill
version: 1.0.0
description: "A skill with bad permission"
types:
  - tool
permissions:
  - "invalid:perm"
implementation:
  backend: starlark
  entrypoint: skill.star
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid:perm")
}

func TestParseManifestInvalidBackend(t *testing.T) {
	yaml := []byte(`
name: my-skill
version: 1.0.0
description: "A skill with bad backend"
types:
  - tool
implementation:
  backend: unknown
  entrypoint: skill.star
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}

func TestParseManifestInvalidSkillType(t *testing.T) {
	yaml := []byte(`
name: my-skill
version: 1.0.0
description: "A skill with invalid type"
types:
  - bogus
implementation:
  backend: starlark
  entrypoint: skill.star
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func TestParseManifestInvalidName(t *testing.T) {
	yaml := []byte(`
name: "My Skill!"
version: 1.0.0
description: "A skill with invalid name"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "My Skill!")
}

func TestParseManifestInvalidYAML(t *testing.T) {
	data := []byte(`{{{not valid yaml`)
	_, err := ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse manifest")
}

func TestParseManifestMissingVersion(t *testing.T) {
	yaml := []byte(`
name: my-skill
description: "A skill without a version"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestParseManifestMissingDescription(t *testing.T) {
	yaml := []byte(`
name: my-skill
version: 1.0.0
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "description")
}

func TestParseManifestTrailingHyphenInName(t *testing.T) {
	yaml := []byte(`
name: my-skill-
version: 1.0.0
description: "Trailing hyphen in name"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid name")
}

func TestParseManifestNonPromptWithoutBackend(t *testing.T) {
	yaml := []byte(`
name: my-skill
version: 1.0.0
description: "Tool skill without backend"
types:
  - tool
`)
	_, err := ParseManifest(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "implementation.backend is required")
}

func TestParseManifestPurePromptWithoutBackend(t *testing.T) {
	yaml := []byte(`
name: my-prompt
version: 1.0.0
description: "A pure prompt skill"
types:
  - prompt
prompt:
  system_prompt_file: prompts/system.md
`)
	m, err := ParseManifest(yaml)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, []SkillType{SkillTypePrompt}, m.Types)
	assert.Equal(t, BackendType(""), m.Implementation.Backend)
}

func TestParseManifestWithCommands(t *testing.T) {
	data := []byte(`
name: kubernetes
version: "1.0.0"
description: Kubernetes skill
types: [tool]
implementation:
  backend: starlark
  entrypoint: main.star
commands:
  - name: pods
    description: "List Kubernetes pods"
    arguments:
      - name: namespace
        description: "Target namespace"
        required: false
  - name: deploy
    description: "Deploy a resource"
    arguments:
      - name: resource
        description: "Resource to deploy"
        required: true
`)
	m, err := ParseManifest(data)
	require.NoError(t, err)
	assert.Len(t, m.Commands, 2)
	assert.Equal(t, "pods", m.Commands[0].Name)
	assert.Len(t, m.Commands[0].Arguments, 1)
	assert.False(t, m.Commands[0].Arguments[0].Required)
	assert.Equal(t, "deploy", m.Commands[1].Name)
	assert.True(t, m.Commands[1].Arguments[0].Required)
}

func TestParseManifestWithoutCommands(t *testing.T) {
	data := []byte(`
name: simple
version: "1.0.0"
description: Simple skill
types: [prompt]
`)
	m, err := ParseManifest(data)
	require.NoError(t, err)
	assert.Empty(t, m.Commands)
}

func TestParseManifestMCPBackendNoEntrypoint(t *testing.T) {
	// MCP backends are exempt from the entrypoint requirement since their
	// config comes from MCPServerConfig, not from a file path.
	yaml := []byte(`
name: mcp-filesystem
version: 0.0.0
description: "MCP server"
types:
  - tool
permissions:
  - shell:exec
implementation:
  backend: mcp
`)
	m, err := ParseManifest(yaml)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, BackendMCP, m.Implementation.Backend)
	assert.Empty(t, m.Implementation.Entrypoint)
}
