package permissions_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/permissions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPolicyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.toml")
	os.WriteFile(path, []byte(`
[tools]
allow = ["file", "code_search"]
deny = ["dangerous_tool"]
prompt = ["shell"]

[shell]
allow_commands = ["go test", "go build"]
deny_commands = ["rm -rf /"]

[files]
allow_patterns = ["*.go"]
deny_patterns = [".env"]

[skills]
auto_approve = ["core-tools"]
`), 0644)

	policy, err := permissions.LoadPolicyFile(path, "project")
	require.NoError(t, err)
	assert.Equal(t, "project", policy.Level)
	assert.Equal(t, path, policy.Source)
	assert.Equal(t, []string{"file", "code_search"}, policy.Tools.Allow)
	assert.Equal(t, []string{"dangerous_tool"}, policy.Tools.Deny)
	assert.Equal(t, []string{"shell"}, policy.Tools.Prompt)
	assert.Equal(t, []string{"go test", "go build"}, policy.Shell.AllowCommands)
	assert.Equal(t, []string{"rm -rf /"}, policy.Shell.DenyCommands)
	assert.Equal(t, []string{"*.go"}, policy.Files.AllowPatterns)
	assert.Equal(t, []string{".env"}, policy.Files.DenyPatterns)
	assert.Equal(t, []string{"core-tools"}, policy.Skills.AutoApprove)
}

func TestLoadPolicyFileMissing(t *testing.T) {
	policy, err := permissions.LoadPolicyFile("/nonexistent/path.toml", "org")
	require.NoError(t, err)
	assert.Nil(t, policy, "missing file should return nil policy, no error")
}

func TestLoadPolicyFileMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	os.WriteFile(path, []byte("not valid toml {{{"), 0644)

	_, err := permissions.LoadPolicyFile(path, "org")
	assert.Error(t, err)
}

func TestLoadPolicyFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.toml")
	os.WriteFile(path, []byte(""), 0644)

	policy, err := permissions.LoadPolicyFile(path, "user")
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, "user", policy.Level)
	assert.Empty(t, policy.Tools.Allow)
}

func TestLoadPolicies(t *testing.T) {
	dir := t.TempDir()
	orgPath := filepath.Join(dir, "org-policy.toml")
	projectPath := filepath.Join(dir, "permissions.toml")

	os.WriteFile(orgPath, []byte(`
[shell]
deny_commands = ["rm -rf /"]
`), 0644)

	os.WriteFile(projectPath, []byte(`
[tools]
allow = ["file", "shell"]
`), 0644)

	userPerms := &permissions.Policy{
		Level: "user",
		Tools: permissions.ToolPolicy{Allow: []string{"code_search"}},
	}

	policies, err := permissions.LoadPolicies(orgPath, projectPath, userPerms)
	require.NoError(t, err)
	require.Len(t, policies, 3)
	assert.Equal(t, "org", policies[0].Level)
	assert.Equal(t, "project", policies[1].Level)
	assert.Equal(t, "user", policies[2].Level)
}

func TestLoadPoliciesMissingFiles(t *testing.T) {
	policies, err := permissions.LoadPolicies("/no/org.toml", "/no/project.toml", nil)
	require.NoError(t, err)
	assert.Empty(t, policies, "all missing files should produce empty list")
}

func TestLoadPoliciesPartial(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "permissions.toml")
	os.WriteFile(projectPath, []byte(`[tools]
allow = ["file"]
`), 0644)

	policies, err := permissions.LoadPolicies("/no/org.toml", projectPath, nil)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, "project", policies[0].Level)
}
