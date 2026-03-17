package permissions_test

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/permissions"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
)

// Task 3: Tool name matching
func TestCheckerDenyWins(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "org", Tools: permissions.ToolPolicy{Deny: []string{"dangerous"}}},
		{Level: "user", Tools: permissions.ToolPolicy{Allow: []string{"dangerous"}}},
	})
	result := checker.CheckApproval("dangerous", nil)
	assert.Equal(t, agentsdk.AutoDenied, result, "org deny should override user allow")
}

func TestCheckerPromptOverridesAllow(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "org", Tools: permissions.ToolPolicy{Prompt: []string{"shell"}}},
		{Level: "user", Tools: permissions.ToolPolicy{Allow: []string{"shell"}}},
	})
	result := checker.CheckApproval("shell", nil)
	assert.Equal(t, agentsdk.ApprovalRequired, result, "prompt should override allow")
}

func TestCheckerAllowReturnsApproved(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "project", Tools: permissions.ToolPolicy{Allow: []string{"file"}}},
	})
	result := checker.CheckApproval("file", nil)
	assert.Equal(t, agentsdk.TrustRuleApproved, result)
}

func TestCheckerNoMatchFallsThrough(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "project", Tools: permissions.ToolPolicy{Allow: []string{"file"}}},
	})
	result := checker.CheckApproval("unknown_tool", nil)
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}

func TestCheckerEmptyPolicies(t *testing.T) {
	checker := permissions.NewHierarchicalChecker(nil)
	result := checker.CheckApproval("file", nil)
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}

// Task 4: Shell command matching
func TestCheckerShellDeny(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "org", Shell: permissions.ShellPolicy{DenyCommands: []string{"rm -rf"}}},
	})
	input, _ := json.Marshal(map[string]string{"command": "rm -rf /"})
	result := checker.CheckApproval("shell", input)
	assert.Equal(t, agentsdk.AutoDenied, result)
}

func TestCheckerShellAllow(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "project", Shell: permissions.ShellPolicy{AllowCommands: []string{"go test"}}},
	})
	input, _ := json.Marshal(map[string]string{"command": "go test ./..."})
	result := checker.CheckApproval("shell", input)
	assert.Equal(t, agentsdk.TrustRuleApproved, result)
}

func TestCheckerShellNoWordBoundaryBypass(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "project", Shell: permissions.ShellPolicy{AllowCommands: []string{"go"}}},
	})
	input, _ := json.Marshal(map[string]string{"command": "gorilla-tool exploit"})
	result := checker.CheckApproval("shell", input)
	assert.Equal(t, agentsdk.ApprovalRequired, result, "go should not match gorilla-tool")
}

func TestCheckerShellDenyFullCommandInjection(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "org", Shell: permissions.ShellPolicy{DenyCommands: []string{"rm -rf"}}},
	})
	input, _ := json.Marshal(map[string]string{"command": "go test && rm -rf /"})
	result := checker.CheckApproval("shell", input)
	assert.Equal(t, agentsdk.AutoDenied, result, "deny should catch injection in full command")
}

func TestCheckerShellPrompt(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "project", Shell: permissions.ShellPolicy{PromptPatterns: []string{"curl"}}},
	})
	input, _ := json.Marshal(map[string]string{"command": "curl https://example.com"})
	result := checker.CheckApproval("shell", input)
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}

// Task 5: File glob matching
func TestCheckerFileDeny(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "org", Files: permissions.FilePolicy{DenyPatterns: []string{".env", "*.pem"}}},
	})
	input, _ := json.Marshal(map[string]string{"operation": "write", "path": ".env"})
	result := checker.CheckApproval("file", input)
	assert.Equal(t, agentsdk.AutoDenied, result)
}

func TestCheckerFileAllow(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "project", Files: permissions.FilePolicy{AllowPatterns: []string{"*.go"}}},
	})
	input, _ := json.Marshal(map[string]string{"operation": "write", "path": "main.go"})
	result := checker.CheckApproval("file", input)
	assert.Equal(t, agentsdk.TrustRuleApproved, result)
}

func TestCheckerFileReadSkipped(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "org", Files: permissions.FilePolicy{DenyPatterns: []string{".env"}}},
	})
	input, _ := json.Marshal(map[string]string{"operation": "read", "path": ".env"})
	result := checker.CheckApproval("file", input)
	assert.Equal(t, agentsdk.ApprovalRequired, result, "read should not trigger file deny")
}

func TestCheckerFilePrompt(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "project", Files: permissions.FilePolicy{PromptPatterns: []string{"go.mod"}}},
	})
	input, _ := json.Marshal(map[string]string{"operation": "write", "path": "go.mod"})
	result := checker.CheckApproval("file", input)
	assert.Equal(t, agentsdk.ApprovalRequired, result)
}

func TestCheckerFileGlobPattern(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "org", Files: permissions.FilePolicy{DenyPatterns: []string{"*.key"}}},
	})
	input, _ := json.Marshal(map[string]string{"operation": "write", "path": "server.key"})
	result := checker.CheckApproval("file", input)
	assert.Equal(t, agentsdk.AutoDenied, result)
}

// Task 6: Explain
func TestCheckerExplainDeny(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "org", Source: "/etc/org-policy.toml", Tools: permissions.ToolPolicy{Deny: []string{"shell"}}},
	})
	reason := checker.Explain("shell", nil)
	assert.Contains(t, reason, "denied")
	assert.Contains(t, reason, "org")
	assert.Contains(t, reason, "shell")
}

func TestCheckerExplainAllow(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "project", Source: ".agent/permissions.toml", Tools: permissions.ToolPolicy{Allow: []string{"file"}}},
	})
	reason := checker.Explain("file", nil)
	assert.Contains(t, reason, "allowed")
	assert.Contains(t, reason, "project")
}

func TestCheckerExplainNoMatch(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "project", Tools: permissions.ToolPolicy{Allow: []string{"file"}}},
	})
	reason := checker.Explain("unknown", nil)
	assert.Empty(t, reason)
}
