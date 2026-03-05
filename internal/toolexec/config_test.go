package toolexec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSecurityYAMLRules(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
rules:
  - id: custom-001
    pattern: "test"
    severity: high
    title: "Test rule"

tool_rules:
  - category: bash
    tool: shell
    pattern: "rm -rf *"
    action: deny
  - category: file_read
    action: allow
  - tool: git-push
    pattern: "*/main"
    action: ask
`
	yamlPath := filepath.Join(dir, ".security.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(yamlContent), 0o644))

	rules, err := toolexec.LoadSecurityYAMLRules(yamlPath)
	require.NoError(t, err)
	require.Len(t, rules, 3)

	// First rule: bash + shell + deny with pattern.
	assert.Equal(t, toolexec.Category("bash"), rules[0].Category)
	assert.Equal(t, "shell", rules[0].Tool)
	assert.Equal(t, "rm -rf *", rules[0].Pattern)
	assert.Equal(t, toolexec.ActionDeny, rules[0].Action)
	assert.Equal(t, toolexec.SourceProject, rules[0].Source)

	// Second rule: file_read + allow, no tool or pattern.
	assert.Equal(t, toolexec.Category("file_read"), rules[1].Category)
	assert.Equal(t, "", rules[1].Tool)
	assert.Equal(t, "", rules[1].Pattern)
	assert.Equal(t, toolexec.ActionAllow, rules[1].Action)
	assert.Equal(t, toolexec.SourceProject, rules[1].Source)

	// Third rule: tool only + ask with pattern.
	assert.Equal(t, toolexec.Category(""), rules[2].Category)
	assert.Equal(t, "git-push", rules[2].Tool)
	assert.Equal(t, "*/main", rules[2].Pattern)
	assert.Equal(t, toolexec.ActionAsk, rules[2].Action)
	assert.Equal(t, toolexec.SourceProject, rules[2].Source)
}

func TestLoadSecurityYAMLRulesMissingFile(t *testing.T) {
	rules, err := toolexec.LoadSecurityYAMLRules("/nonexistent/path/.security.yaml")
	assert.NoError(t, err)
	assert.Nil(t, rules)
}

func TestTOMLRulesToPermissionRules(t *testing.T) {
	confs := []toolexec.ToolRuleConf{
		{
			Category: "bash",
			Tool:     "shell",
			Pattern:  "go test *",
			Action:   "allow",
		},
		{
			Category: "net",
			Action:   "deny",
		},
		{
			Tool:    "mcp-server",
			Pattern: "*/secrets/*",
			Action:  "ask",
		},
	}

	rules := toolexec.TOMLRulesToPermissionRules(confs, toolexec.SourceUser)
	require.Len(t, rules, 3)

	assert.Equal(t, toolexec.Category("bash"), rules[0].Category)
	assert.Equal(t, "shell", rules[0].Tool)
	assert.Equal(t, "go test *", rules[0].Pattern)
	assert.Equal(t, toolexec.ActionAllow, rules[0].Action)
	assert.Equal(t, toolexec.SourceUser, rules[0].Source)

	assert.Equal(t, toolexec.Category("net"), rules[1].Category)
	assert.Equal(t, "", rules[1].Tool)
	assert.Equal(t, "", rules[1].Pattern)
	assert.Equal(t, toolexec.ActionDeny, rules[1].Action)
	assert.Equal(t, toolexec.SourceUser, rules[1].Source)

	assert.Equal(t, toolexec.Category(""), rules[2].Category)
	assert.Equal(t, "mcp-server", rules[2].Tool)
	assert.Equal(t, "*/secrets/*", rules[2].Pattern)
	assert.Equal(t, toolexec.ActionAsk, rules[2].Action)
	assert.Equal(t, toolexec.SourceUser, rules[2].Source)
}

func TestTOMLRulesToPermissionRulesEmpty(t *testing.T) {
	rules := toolexec.TOMLRulesToPermissionRules(nil, toolexec.SourceUser)
	assert.Nil(t, rules)
}

func TestMergeRules(t *testing.T) {
	userRules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Action:   toolexec.ActionAllow,
			Source:   toolexec.SourceUser,
		},
	}
	projectRules := []toolexec.PermissionRule{
		{
			Tool:   "shell",
			Action: toolexec.ActionDeny,
			Source: toolexec.SourceProject,
		},
		{
			Category: toolexec.CategoryNet,
			Action:   toolexec.ActionAsk,
			Source:   toolexec.SourceProject,
		},
	}

	merged := toolexec.MergeRules(userRules, projectRules)
	require.Len(t, merged, 3)

	// Verify order: user rules first, then project rules.
	assert.Equal(t, toolexec.SourceUser, merged[0].Source)
	assert.Equal(t, toolexec.CategoryBash, merged[0].Category)

	assert.Equal(t, toolexec.SourceProject, merged[1].Source)
	assert.Equal(t, "shell", merged[1].Tool)

	assert.Equal(t, toolexec.SourceProject, merged[2].Source)
	assert.Equal(t, toolexec.CategoryNet, merged[2].Category)
}

func TestMergeRulesEmpty(t *testing.T) {
	// No sources.
	merged := toolexec.MergeRules()
	assert.Nil(t, merged)

	// All nil slices.
	merged = toolexec.MergeRules(nil, nil)
	assert.Nil(t, merged)

	// Mix of nil and populated.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Action: toolexec.ActionAllow},
	}
	merged = toolexec.MergeRules(nil, rules, nil)
	require.Len(t, merged, 1)
	assert.Equal(t, toolexec.CategoryBash, merged[0].Category)
}
