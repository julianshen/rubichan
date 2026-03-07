package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Feature 2: Instruction Skills (SKILL.md) ---

func TestParseInstructionSkill(t *testing.T) {
	input := []byte(`---
name: react-patterns
version: 1.0.0
description: React best practices
triggers:
  files: ["*.tsx", "*.jsx"]
  languages: [typescript, javascript]
---

## Instructions

When working with React components, always use functional components.
`)

	manifest, body, err := ParseInstructionSkill(input)
	require.NoError(t, err)

	assert.Equal(t, "react-patterns", manifest.Name)
	assert.Equal(t, "1.0.0", manifest.Version)
	assert.Equal(t, "React best practices", manifest.Description)
	assert.Equal(t, []SkillType{SkillTypePrompt}, manifest.Types)
	assert.Equal(t, []string{"*.tsx", "*.jsx"}, manifest.Triggers.Files)
	assert.Equal(t, []string{"typescript", "javascript"}, manifest.Triggers.Languages)
	assert.Contains(t, body, "When working with React components")
	assert.Contains(t, body, "functional components")
}

func TestParseInstructionSkillMinimal(t *testing.T) {
	input := []byte(`---
name: minimal
version: 0.1.0
description: Minimal skill
---
`)

	manifest, body, err := ParseInstructionSkill(input)
	require.NoError(t, err)

	assert.Equal(t, "minimal", manifest.Name)
	assert.Equal(t, "0.1.0", manifest.Version)
	assert.Equal(t, "Minimal skill", manifest.Description)
	assert.Equal(t, []SkillType{SkillTypePrompt}, manifest.Types)
	assert.Empty(t, body)
}

func TestParseInstructionSkillInvalidFrontmatter(t *testing.T) {
	t.Run("missing name", func(t *testing.T) {
		input := []byte(`---
version: 1.0.0
description: No name
---
body
`)
		_, _, err := ParseInstructionSkill(input)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("missing version", func(t *testing.T) {
		input := []byte(`---
name: no-version
description: Missing version
---
body
`)
		_, _, err := ParseInstructionSkill(input)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "version is required")
	})

	t.Run("missing description", func(t *testing.T) {
		input := []byte(`---
name: no-desc
version: 1.0.0
---
body
`)
		_, _, err := ParseInstructionSkill(input)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "description is required")
	})
}

func TestParseInstructionSkillNoFrontmatter(t *testing.T) {
	input := []byte(`# Just a regular markdown file

No frontmatter delimiters here.
`)

	_, _, err := ParseInstructionSkill(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing frontmatter delimiter")
}

func TestParseInstructionSkillTypesDefaultToPrompt(t *testing.T) {
	input := []byte(`---
name: default-type
version: 1.0.0
description: No types specified
---
Body text.
`)

	manifest, _, err := ParseInstructionSkill(input)
	require.NoError(t, err)
	assert.Equal(t, []SkillType{SkillTypePrompt}, manifest.Types)
}

func TestParseInstructionSkillRejectsNonPromptTypes(t *testing.T) {
	input := []byte(`---
name: bad-type
version: 1.0.0
description: Has tool type
types:
  - tool
---
Body text.
`)

	_, _, err := ParseInstructionSkill(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
	assert.Contains(t, err.Error(), "tool")
}

func TestParseInstructionSkillExtendedFrontmatter(t *testing.T) {
	input := []byte(`---
name: review-helper
version: 1.2.0
description: Review instructions
priority: 7
tools_allow: [read_file, search]
tools_deny: [shell]
references:
  - path: references/checklist.md
    when: review requested
commands:
  - name: review-plan
    description: Draft a review plan
    arguments:
      - name: scope
        description: Review scope
        required: true
agents:
  - name: review-subagent
    description: Focused reviewer
    system_prompt: Review carefully.
    tools: [read_file]
    max_turns: 4
    max_depth: 2
    model: test-model
---
Review with discipline.
`)

	manifest, body, err := ParseInstructionSkill(input)
	require.NoError(t, err)

	assert.Equal(t, 7, manifest.Priority)
	assert.Equal(t, []string{"read_file", "search"}, manifest.ToolsAllow)
	assert.Equal(t, []string{"shell"}, manifest.ToolsDeny)
	require.Len(t, manifest.References, 1)
	assert.Equal(t, "references/checklist.md", manifest.References[0].Path)
	assert.Equal(t, "review requested", manifest.References[0].When)
	require.Len(t, manifest.Commands, 1)
	assert.Equal(t, "review-plan", manifest.Commands[0].Name)
	require.Len(t, manifest.Commands[0].Arguments, 1)
	assert.True(t, manifest.Commands[0].Arguments[0].Required)
	require.Len(t, manifest.Agents, 1)
	assert.Equal(t, "review-subagent", manifest.Agents[0].Name)
	assert.Equal(t, 4, manifest.Agents[0].MaxTurns)
	assert.Equal(t, 2, manifest.Agents[0].MaxDepth)
	assert.Equal(t, "test-model", manifest.Agents[0].Model)
	assert.Contains(t, body, "Review with discipline.")
}

func TestParseInstructionSkillStrictRejectsUnknownFields(t *testing.T) {
	input := []byte(`---
name: strict-skill
version: 1.0.0
description: Strict parsing
unknown_field: nope
---
Body.
`)

	_, _, err := ParseInstructionSkillStrict(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown_field")
}

func TestScanDirDiscoversInstructionSkills(t *testing.T) {
	dir := t.TempDir()

	// Create a SKILL.yaml skill.
	writeSkillYAML(t, dir, "yaml-skill", minimalManifestYAML("yaml-skill"))

	// Create a SKILL.md instruction skill.
	mdDir := filepath.Join(dir, "md-skill")
	require.NoError(t, os.MkdirAll(mdDir, 0o755))
	mdContent := `---
name: md-skill
version: 1.0.0
description: Markdown instruction skill
---

Be helpful.
`
	require.NoError(t, os.WriteFile(filepath.Join(mdDir, "SKILL.md"), []byte(mdContent), 0o644))

	results, err := scanDir(dir, SourceUser)
	require.NoError(t, err)
	require.Len(t, results, 2)

	byName := make(map[string]DiscoveredSkill)
	for _, ds := range results {
		byName[ds.Manifest.Name] = ds
	}

	// YAML skill has no instruction body.
	yamlSkill := byName["yaml-skill"]
	assert.Empty(t, yamlSkill.InstructionBody)

	// MD skill has the instruction body.
	mdSkill := byName["md-skill"]
	assert.Equal(t, "Markdown instruction skill", mdSkill.Manifest.Description)
	assert.Contains(t, mdSkill.InstructionBody, "Be helpful.")
	assert.Equal(t, []SkillType{SkillTypePrompt}, mdSkill.Manifest.Types)
}

func TestScanDirSkillYAMLTakesPrecedence(t *testing.T) {
	dir := t.TempDir()

	// Create a directory with both SKILL.yaml and SKILL.md.
	skillDir := filepath.Join(dir, "both-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	yamlContent := `name: both-skill
version: 2.0.0
description: "YAML version"
types:
  - prompt
`
	mdContent := `---
name: both-skill
version: 1.0.0
description: MD version
---
Body from MD.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(mdContent), 0o644))

	results, err := scanDir(dir, SourceUser)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// SKILL.yaml should win.
	assert.Equal(t, "2.0.0", results[0].Manifest.Version)
	assert.Equal(t, "YAML version", results[0].Manifest.Description)
	assert.Empty(t, results[0].InstructionBody)
}

func TestDiscoverIntegrationWithInstructionSkills(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Place an instruction skill in the user dir.
	mdDir := filepath.Join(userDir, "instruct-skill")
	require.NoError(t, os.MkdirAll(mdDir, 0o755))
	mdContent := `---
name: instruct-skill
version: 1.0.0
description: An instruction skill
triggers:
  keywords: [review]
---

Always review code thoroughly.
`
	require.NoError(t, os.WriteFile(filepath.Join(mdDir, "SKILL.md"), []byte(mdContent), 0o644))

	loader := NewLoader(userDir, projectDir)
	discovered, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, discovered, 1)

	ds := discovered[0]
	assert.Equal(t, "instruct-skill", ds.Manifest.Name)
	assert.Equal(t, SourceUser, ds.Source)
	assert.Contains(t, ds.InstructionBody, "review code thoroughly")
	assert.Equal(t, filepath.Join(userDir, "instruct-skill"), ds.Dir)
}

func TestRuntimeActivateInstructionSkill(t *testing.T) {
	bf := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{
			tools: nil,
			hooks: map[HookPhase]HookHandler{},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"instruct-test"}, bf)

	m := &SkillManifest{
		Name:        "instruct-test",
		Version:     "1.0.0",
		Description: "An instruction skill",
		Types:       []SkillType{SkillTypePrompt},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Manually set the InstructionBody (simulating what Discover does for SKILL.md).
	rt.skills["instruct-test"].InstructionBody = "Always follow best practices."

	require.NoError(t, rt.Activate("instruct-test"))

	fragments := rt.GetPromptFragments()
	require.Len(t, fragments, 1)
	assert.Equal(t, "instruct-test", fragments[0].SkillName)
	assert.Equal(t, "Always follow best practices.", fragments[0].ResolvedPrompt)
}

func TestInstructionSkillPromptFragmentContent(t *testing.T) {
	bf := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{
			tools: nil,
			hooks: map[HookPhase]HookHandler{},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"md-prompt"}, bf)

	m := &SkillManifest{
		Name:        "md-prompt",
		Version:     "1.0.0",
		Description: "Markdown prompt",
		Types:       []SkillType{SkillTypePrompt},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	expectedBody := "## Guidelines\n\nUse TypeScript for all new code."
	rt.skills["md-prompt"].InstructionBody = expectedBody

	require.NoError(t, rt.Activate("md-prompt"))

	// Verify via hook dispatch.
	result, err := rt.lifecycle.Dispatch(HookEvent{
		Phase: HookOnBeforePromptBuild,
		Data:  map[string]any{},
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	fragment, ok := result.Modified["prompt_fragment"].(string)
	require.True(t, ok)
	assert.Equal(t, expectedBody, fragment)
}

func TestLintInstructionSkillDir(t *testing.T) {
	skillDir := t.TempDir()
	repeated := strings.Repeat("word ", 501)
	content := `---
name: lint-skill
version: 1.0.0
description: Lint me
references:
  - path: references/missing.md
commands:
  - name: repeat
    description: once
  - name: repeat
    description: twice
agents:
  - name: helper
    description: one
  - name: helper
    description: two
---
` + repeated

	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))

	issues := LintSkillDir(skillDir)
	assert.Contains(t, issues, `instruction skill body exceeds 500 words`)
	assert.Contains(t, issues, `duplicate command name "repeat"`)
	assert.Contains(t, issues, `duplicate agent name "helper"`)
	assert.Contains(t, issues, `missing reference file "references/missing.md"`)
}
