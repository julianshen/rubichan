# Hierarchical Permissions Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `HierarchicalChecker` that evaluates tool permissions against Org → Project → User policy files, slotting into the existing `CompositeApprovalChecker` chain.

**Architecture:** New `internal/permissions/` package with Policy types, TOML loading, and an `ApprovalChecker` implementation. Wired into `cmd/rubichan/main.go` as the first checker in the composite chain. Config extended with `[permissions]` section.

**Tech Stack:** Go stdlib, `github.com/BurntSushi/toml` (existing), `pkg/agentsdk` for `ApprovalChecker` interface.

**Spec:** `docs/superpowers/specs/2026-03-17-hierarchical-permissions-design.md`

---

## File Structure

| File | Package | Responsibility |
|------|---------|---------------|
| `internal/permissions/policy.go` | `permissions` | Policy/ToolPolicy/ShellPolicy/FilePolicy/SkillPolicy types, LoadPolicies, LoadPolicyFile |
| `internal/permissions/policy_test.go` | `permissions_test` | TOML loading tests: valid, missing, malformed |
| `internal/permissions/checker.go` | `permissions` | HierarchicalChecker, CheckApproval, Explain |
| `internal/permissions/checker_test.go` | `permissions_test` | Resolution tests: deny-wins, prompt, allow, shell/file matching |
| `internal/config/config.go` | `config` | Add PermissionsConfig to Config struct |
| `cmd/rubichan/main.go` | `main` | Wire hierarchical checker into composite chain |

---

## Chunk 1: Policy Types and Loading

### Task 1: Policy types and LoadPolicyFile

**Files:**
- Create: `internal/permissions/policy.go`
- Test: `internal/permissions/policy_test.go`

- [ ] **Step 1: Write failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/permissions/ -run TestLoadPolicyFile -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write implementation**

```go
package permissions

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Policy represents a permission policy loaded from a single source.
type Policy struct {
	Level  string // "org", "project", "user"
	Source string // file path this was loaded from
	Tools  ToolPolicy  `toml:"tools"`
	Shell  ShellPolicy `toml:"shell"`
	Files  FilePolicy  `toml:"files"`
	Skills SkillPolicy `toml:"skills"`
}

// ToolPolicy controls which tools are allowed, denied, or require prompting.
type ToolPolicy struct {
	Allow  []string `toml:"allow"`
	Deny   []string `toml:"deny"`
	Prompt []string `toml:"prompt"`
}

// ShellPolicy controls which shell commands are allowed or denied.
type ShellPolicy struct {
	AllowCommands  []string `toml:"allow_commands"`
	DenyCommands   []string `toml:"deny_commands"`
	PromptPatterns []string `toml:"prompt_patterns"`
}

// FilePolicy controls which file paths are allowed or denied via glob patterns.
type FilePolicy struct {
	AllowPatterns  []string `toml:"allow_patterns"`
	DenyPatterns   []string `toml:"deny_patterns"`
	PromptPatterns []string `toml:"prompt_patterns"`
}

// SkillPolicy controls which skills are auto-approved or denied.
type SkillPolicy struct {
	AutoApprove []string `toml:"auto_approve"`
	Deny        []string `toml:"deny"`
}

// LoadPolicyFile reads a TOML policy file and returns a Policy with the given level.
// Returns (nil, nil) if the file does not exist. Returns an error for malformed files.
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/permissions/ -run TestLoadPolicyFile -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add permission Policy types and LoadPolicyFile
```

---

### Task 2: LoadPolicies — multi-source loading

**Files:**
- Modify: `internal/permissions/policy.go`
- Test: `internal/permissions/policy_test.go`

- [ ] **Step 1: Write failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/permissions/ -run TestLoadPolicies -v`
Expected: FAIL — LoadPolicies not defined

- [ ] **Step 3: Write implementation**

```go
// LoadPolicies reads policy files from up to three sources and returns
// them in precedence order (org first, project second, user third).
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/permissions/ -run TestLoadPolicies -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add LoadPolicies for multi-source policy loading
```

---

## Chunk 2: HierarchicalChecker

### Task 3: Basic tool name matching (deny/prompt/allow)

**Files:**
- Create: `internal/permissions/checker.go`
- Test: `internal/permissions/checker_test.go`

- [ ] **Step 1: Write failing tests**

```go
package permissions_test

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/permissions"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
)

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/permissions/ -run TestChecker -v`
Expected: FAIL — NewHierarchicalChecker not defined

- [ ] **Step 3: Write implementation**

```go
package permissions

import (
	"encoding/json"
	"slices"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// HierarchicalChecker implements agentsdk.ApprovalChecker by evaluating
// tool calls against a cascade of permission policies.
type HierarchicalChecker struct {
	policies []Policy
}

// NewHierarchicalChecker creates a checker from ordered policies (org first).
func NewHierarchicalChecker(policies []Policy) *HierarchicalChecker {
	return &HierarchicalChecker{policies: policies}
}

// CheckApproval evaluates the tool/input against all policies.
// Resolution: Deny > Prompt > Allow. First match at any level wins.
func (h *HierarchicalChecker) CheckApproval(tool string, input json.RawMessage) agentsdk.ApprovalResult {
	if len(h.policies) == 0 {
		return agentsdk.ApprovalRequired
	}

	// Pass 1: Deny (any level)
	for _, p := range h.policies {
		if h.matchesDeny(p, tool, input) {
			return agentsdk.AutoDenied
		}
	}

	// Pass 2: Prompt (any level)
	for _, p := range h.policies {
		if h.matchesPrompt(p, tool, input) {
			return agentsdk.ApprovalRequired
		}
	}

	// Pass 3: Allow (any level)
	for _, p := range h.policies {
		if h.matchesAllow(p, tool, input) {
			return agentsdk.TrustRuleApproved
		}
	}

	return agentsdk.ApprovalRequired
}

func (h *HierarchicalChecker) matchesDeny(p Policy, tool string, input json.RawMessage) bool {
	return slices.Contains(p.Tools.Deny, tool)
}

func (h *HierarchicalChecker) matchesPrompt(p Policy, tool string, input json.RawMessage) bool {
	return slices.Contains(p.Tools.Prompt, tool)
}

func (h *HierarchicalChecker) matchesAllow(p Policy, tool string, input json.RawMessage) bool {
	return slices.Contains(p.Tools.Allow, tool)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/permissions/ -run TestChecker -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add HierarchicalChecker with tool name matching
```

---

### Task 4: Shell command matching

**Files:**
- Modify: `internal/permissions/checker.go`
- Test: `internal/permissions/checker_test.go`

- [ ] **Step 1: Write failing tests**

```go
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

	// "go" should not match "gorilla-tool"
	input, _ := json.Marshal(map[string]string{"command": "gorilla-tool exploit"})
	result := checker.CheckApproval("shell", input)
	assert.Equal(t, agentsdk.ApprovalRequired, result, "go should not match gorilla-tool")
}

func TestCheckerShellDenyFullCommandInjection(t *testing.T) {
	checker := permissions.NewHierarchicalChecker([]permissions.Policy{
		{Level: "org", Shell: permissions.ShellPolicy{DenyCommands: []string{"rm -rf"}}},
	})

	// Deny patterns should match anywhere in the full command
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/permissions/ -run "TestCheckerShell" -v`
Expected: FAIL — shell matching not implemented

- [ ] **Step 3: Write implementation**

Add shell matching helpers and update matchesDeny/matchesPrompt/matchesAllow:

```go
import "strings"

// extractShellCommand parses the command string from shell tool input JSON.
func extractShellCommand(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var parsed struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return ""
	}
	return parsed.Command
}

// matchesCommandPrefix checks if a command matches a pattern using first-token
// matching. Pattern "go test" matches "go test ./..." but not "gorilla-tool".
func matchesCommandPrefix(command, pattern string) bool {
	patternParts := strings.Fields(pattern)
	commandParts := strings.Fields(command)
	if len(commandParts) < len(patternParts) {
		return false
	}
	for i, pp := range patternParts {
		if commandParts[i] != pp {
			return false
		}
	}
	return true
}

// containsCommandPattern checks if the full command string contains the pattern
// as a command anywhere (for injection detection in deny rules).
func containsCommandPattern(fullCommand, pattern string) bool {
	// Check direct prefix match
	if matchesCommandPrefix(fullCommand, pattern) {
		return true
	}
	// Check after command separators (&&, ||, ;, |)
	for _, sep := range []string{"&&", "||", ";", "|"} {
		parts := strings.Split(fullCommand, sep)
		for _, part := range parts {
			if matchesCommandPrefix(strings.TrimSpace(part), pattern) {
				return true
			}
		}
	}
	return false
}
```

Update the three match methods to handle shell:

```go
func (h *HierarchicalChecker) matchesDeny(p Policy, tool string, input json.RawMessage) bool {
	if slices.Contains(p.Tools.Deny, tool) {
		return true
	}
	if tool == "shell" {
		cmd := extractShellCommand(input)
		for _, pattern := range p.Shell.DenyCommands {
			if containsCommandPattern(cmd, pattern) {
				return true
			}
		}
	}
	return false
}

func (h *HierarchicalChecker) matchesPrompt(p Policy, tool string, input json.RawMessage) bool {
	if slices.Contains(p.Tools.Prompt, tool) {
		return true
	}
	if tool == "shell" {
		cmd := extractShellCommand(input)
		for _, pattern := range p.Shell.PromptPatterns {
			if matchesCommandPrefix(cmd, pattern) {
				return true
			}
		}
	}
	return false
}

func (h *HierarchicalChecker) matchesAllow(p Policy, tool string, input json.RawMessage) bool {
	if slices.Contains(p.Tools.Allow, tool) {
		return true
	}
	if tool == "shell" {
		cmd := extractShellCommand(input)
		for _, pattern := range p.Shell.AllowCommands {
			if matchesCommandPrefix(cmd, pattern) {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/permissions/ -run "TestCheckerShell" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add shell command matching with injection detection
```

---

### Task 5: File path glob matching

**Files:**
- Modify: `internal/permissions/checker.go`
- Test: `internal/permissions/checker_test.go`

- [ ] **Step 1: Write failing tests**

```go
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

	// Read operations should not be checked against file policies
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/permissions/ -run "TestCheckerFile" -v`
Expected: FAIL — file matching not implemented

- [ ] **Step 3: Write implementation**

Add file matching helpers:

```go
import "path"

// extractFileInfo parses operation and path from file tool input JSON.
func extractFileInfo(input json.RawMessage) (operation, filePath string) {
	if len(input) == 0 {
		return "", ""
	}
	var parsed struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return "", ""
	}
	return parsed.Operation, parsed.Path
}

// matchesFilePattern checks if a file path matches a glob pattern.
// Uses path.Match which supports *, ?, and [] patterns (no ** recursive).
func matchesFilePattern(filePath, pattern string) bool {
	// Match against the full path
	if matched, _ := path.Match(pattern, filePath); matched {
		return true
	}
	// Also match against just the filename (for patterns like "*.go", ".env")
	if matched, _ := path.Match(pattern, path.Base(filePath)); matched {
		return true
	}
	return false
}
```

Update the three match methods to handle file tool:

```go
// In matchesDeny, add after shell check:
if tool == "file" {
	op, fp := extractFileInfo(input)
	if op == "write" || op == "patch" {
		for _, pattern := range p.Files.DenyPatterns {
			if matchesFilePattern(fp, pattern) {
				return true
			}
		}
	}
}

// In matchesPrompt, add after shell check:
if tool == "file" {
	op, fp := extractFileInfo(input)
	if op == "write" || op == "patch" {
		for _, pattern := range p.Files.PromptPatterns {
			if matchesFilePattern(fp, pattern) {
				return true
			}
		}
	}
}

// In matchesAllow, add after shell check:
if tool == "file" {
	op, fp := extractFileInfo(input)
	if op == "write" || op == "patch" {
		for _, pattern := range p.Files.AllowPatterns {
			if matchesFilePattern(fp, pattern) {
				return true
			}
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/permissions/ -run "TestCheckerFile" -v`
Expected: PASS

- [ ] **Step 5: Run all permissions tests**

Run: `go test ./internal/permissions/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add file path glob matching for permissions
```

---

### Task 6: Explain method

**Files:**
- Modify: `internal/permissions/checker.go`
- Test: `internal/permissions/checker_test.go`

- [ ] **Step 1: Write failing test**

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/permissions/ -run "TestCheckerExplain" -v`
Expected: FAIL — Explain not defined

- [ ] **Step 3: Write implementation**

```go
import "fmt"

// Explain re-evaluates the tool/input against policies and returns a
// human-readable reason. Returns empty string if no policy matched.
func (h *HierarchicalChecker) Explain(tool string, input json.RawMessage) string {
	for _, p := range h.policies {
		if h.matchesDeny(p, tool, input) {
			return fmt.Sprintf("denied by %s policy (%s): tool '%s'", p.Level, p.Source, tool)
		}
	}
	for _, p := range h.policies {
		if h.matchesPrompt(p, tool, input) {
			return fmt.Sprintf("requires approval per %s policy (%s): tool '%s'", p.Level, p.Source, tool)
		}
	}
	for _, p := range h.policies {
		if h.matchesAllow(p, tool, input) {
			return fmt.Sprintf("allowed by %s policy (%s): tool '%s'", p.Level, p.Source, tool)
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/permissions/ -run "TestCheckerExplain" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add Explain method for permission decisions
```

---

## Chunk 3: Config + Wiring

### Task 7: Add PermissionsConfig to config

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestConfigPermissionsSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"

[permissions.tools]
allow = ["file", "code_search"]
deny = ["dangerous"]

[permissions.shell]
allow_commands = ["go test"]
deny_commands = ["rm -rf /"]

[permissions.files]
allow_patterns = ["*.go"]
deny_patterns = [".env"]
`), 0644)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"file", "code_search"}, cfg.Permissions.Tools.Allow)
	assert.Equal(t, []string{"dangerous"}, cfg.Permissions.Tools.Deny)
	assert.Equal(t, []string{"go test"}, cfg.Permissions.Shell.AllowCommands)
	assert.Equal(t, []string{".env"}, cfg.Permissions.Files.DenyPatterns)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestConfigPermissionsSection -v`
Expected: FAIL — `cfg.Permissions` doesn't exist

- [ ] **Step 3: Write implementation**

Add to `internal/config/config.go`:

```go
// In the Config struct, add:
Permissions PermissionsConfig `toml:"permissions"`

// New type:
type PermissionsConfig struct {
	Tools  PermToolPolicy  `toml:"tools"`
	Shell  PermShellPolicy `toml:"shell"`
	Files  PermFilePolicy  `toml:"files"`
	Skills PermSkillPolicy `toml:"skills"`
}

type PermToolPolicy struct {
	Allow  []string `toml:"allow"`
	Deny   []string `toml:"deny"`
	Prompt []string `toml:"prompt"`
}

type PermShellPolicy struct {
	AllowCommands  []string `toml:"allow_commands"`
	DenyCommands   []string `toml:"deny_commands"`
	PromptPatterns []string `toml:"prompt_patterns"`
}

type PermFilePolicy struct {
	AllowPatterns  []string `toml:"allow_patterns"`
	DenyPatterns   []string `toml:"deny_patterns"`
	PromptPatterns []string `toml:"prompt_patterns"`
}

type PermSkillPolicy struct {
	AutoApprove []string `toml:"auto_approve"`
	Deny        []string `toml:"deny"`
}
```

Note: These types mirror `permissions.ToolPolicy` etc. but are in the config package to avoid a dependency from config → permissions. The wiring code in main.go will convert between them.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestConfigPermissionsSection -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add PermissionsConfig to config for user-level policies
```

---

### Task 8: Wire into CompositeApprovalChecker in main.go

**Files:**
- Modify: `cmd/rubichan/main.go:1363-1384`

- [ ] **Step 1: Write implementation**

In `cmd/rubichan/main.go`, at line ~1367 (where `var checkers []agent.ApprovalChecker` is built), add the hierarchical checker before existing checkers:

```go
import "github.com/julianshen/rubichan/internal/permissions"

// Before the existing checker chain, load hierarchical policies:
{
	configDir, _ := os.UserConfigDir()
	orgPath := filepath.Join(configDir, "aiagent", "org-policy.toml")
	projectPath := filepath.Join(cwd, ".agent", "permissions.toml")

	// Convert user config permissions to a Policy
	var userPolicy *permissions.Policy
	cp := cfg.Permissions
	if len(cp.Tools.Allow) > 0 || len(cp.Tools.Deny) > 0 || len(cp.Tools.Prompt) > 0 ||
		len(cp.Shell.AllowCommands) > 0 || len(cp.Shell.DenyCommands) > 0 ||
		len(cp.Files.AllowPatterns) > 0 || len(cp.Files.DenyPatterns) > 0 {
		userPolicy = &permissions.Policy{
			Level: "user",
			Source: configPath,
			Tools: permissions.ToolPolicy{
				Allow: cp.Tools.Allow, Deny: cp.Tools.Deny, Prompt: cp.Tools.Prompt,
			},
			Shell: permissions.ShellPolicy{
				AllowCommands: cp.Shell.AllowCommands, DenyCommands: cp.Shell.DenyCommands,
				PromptPatterns: cp.Shell.PromptPatterns,
			},
			Files: permissions.FilePolicy{
				AllowPatterns: cp.Files.AllowPatterns, DenyPatterns: cp.Files.DenyPatterns,
				PromptPatterns: cp.Files.PromptPatterns,
			},
			Skills: permissions.SkillPolicy{
				AutoApprove: cp.Skills.AutoApprove, Deny: cp.Skills.Deny,
			},
		}
	}

	policies, err := permissions.LoadPolicies(orgPath, projectPath, userPolicy)
	if err != nil {
		log.Printf("warning: failed to load permission policies: %v", err)
	}
	if len(policies) > 0 {
		checkers = append(checkers, permissions.NewHierarchicalChecker(policies))
	}
}
```

**Important:** This block goes right after `var checkers []agent.ApprovalChecker` (line 1367) and BEFORE the session cache checker is appended (line 1369-1371). The hierarchical checker must be first in the chain.

- [ ] **Step 2: Verify build**

Run: `go build ./cmd/rubichan/`
Expected: BUILD OK

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```
[BEHAVIORAL] wire hierarchical permissions into composite approval chain
```

---

### Task 9: Final integration — tests + lint + coverage

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Check formatting**

Run: `gofmt -l .`
Expected: No files listed

- [ ] **Step 3: Check coverage**

Run: `go test -cover ./internal/permissions/`
Expected: >90%

- [ ] **Step 4: Commit any fixes**

```
[STRUCTURAL] fix lint and formatting issues
```
