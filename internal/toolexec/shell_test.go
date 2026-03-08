package toolexec_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Parser tests ---

func TestParseCommandSimple(t *testing.T) {
	parts, err := toolexec.ParseCommand("git push origin main")
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "git", parts[0].Prefix)
	assert.Equal(t, "git push origin main", parts[0].Full)
}

func TestParseCommandCompound(t *testing.T) {
	parts, err := toolexec.ParseCommand("go test ./... && rm -rf /tmp")
	require.NoError(t, err)
	require.Len(t, parts, 2)

	assert.Equal(t, "go", parts[0].Prefix)
	assert.Equal(t, "go test ./...", parts[0].Full)

	assert.Equal(t, "rm", parts[1].Prefix)
	assert.Equal(t, "rm -rf /tmp", parts[1].Full)
}

func TestParseCommandPipeline(t *testing.T) {
	parts, err := toolexec.ParseCommand("cat file | grep error | wc -l")
	require.NoError(t, err)
	require.Len(t, parts, 3)

	assert.Equal(t, "cat", parts[0].Prefix)
	assert.Equal(t, "cat file", parts[0].Full)

	assert.Equal(t, "grep", parts[1].Prefix)
	assert.Equal(t, "grep error", parts[1].Full)

	assert.Equal(t, "wc", parts[2].Prefix)
	assert.Equal(t, "wc -l", parts[2].Full)
}

func TestParseCommandSubshell(t *testing.T) {
	parts, err := toolexec.ParseCommand("echo $(rm -rf /)")
	require.Error(t, err)
	assert.Nil(t, parts)
	assert.Contains(t, err.Error(), "unsupported shell word part")
}

func TestParseCommandRejectsParameterExpansion(t *testing.T) {
	parts, err := toolexec.ParseCommand("echo $HOME")
	require.Error(t, err)
	assert.Nil(t, parts)
	assert.Contains(t, err.Error(), "unsupported shell word part")
}

func TestParseCommandEnvPrefix(t *testing.T) {
	parts, err := toolexec.ParseCommand("RAILS_ENV=prod rails db:migrate")
	require.NoError(t, err)
	require.Len(t, parts, 1)

	assert.Equal(t, "rails", parts[0].Prefix)
	assert.Equal(t, "rails db:migrate", parts[0].Full)
}

func TestParseCommandBashDashC(t *testing.T) {
	parts, err := toolexec.ParseCommand(`bash -c "npm install"`)
	require.NoError(t, err)
	// Should find npm from the re-parsed -c argument.
	found := false
	for _, p := range parts {
		if p.Prefix == "npm" {
			found = true
			assert.Equal(t, "npm install", p.Full)
		}
	}
	assert.True(t, found, "should find npm command from bash -c argument")
}

func TestParseCommandBashDashCPropagatesInnerParseErrors(t *testing.T) {
	parts, err := toolexec.ParseCommand(`bash -c 'echo $HOME'`)
	require.Error(t, err)
	assert.Nil(t, parts)
	assert.Contains(t, err.Error(), "parse bash -c payload")
}

func TestParseCommandQuotedArgs(t *testing.T) {
	parts, err := toolexec.ParseCommand("rm '-rf' /")
	require.NoError(t, err)
	require.Len(t, parts, 1)

	assert.Equal(t, "rm", parts[0].Prefix)
	assert.Equal(t, "rm -rf /", parts[0].Full)
}

func TestParseCommandEmpty(t *testing.T) {
	parts, err := toolexec.ParseCommand("")
	require.NoError(t, err)
	assert.Nil(t, parts)
}

func TestParseCommandSemicolon(t *testing.T) {
	parts, err := toolexec.ParseCommand("echo hello; rm -rf /")
	require.NoError(t, err)
	require.Len(t, parts, 2)

	assert.Equal(t, "echo", parts[0].Prefix)
	assert.Equal(t, "echo hello", parts[0].Full)

	assert.Equal(t, "rm", parts[1].Prefix)
	assert.Equal(t, "rm -rf /", parts[1].Full)
}

// --- Validator tests ---

func TestShellValidatorDeniesSubCommand(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "rm *",
			Action:   toolexec.ActionDeny,
		},
	}
	engine := toolexec.NewRuleEngine(rules)
	validator := toolexec.NewShellValidator(engine, t.TempDir())

	// Compound command where second part is denied.
	err := validator.Validate(context.Background(), "ls -la && rm -rf /")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rm")
}

func TestShellValidatorAllowsCleanCommand(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "rm *",
			Action:   toolexec.ActionDeny,
		},
	}
	engine := toolexec.NewRuleEngine(rules)
	validator := toolexec.NewShellValidator(engine, t.TempDir())

	err := validator.Validate(context.Background(), "ls -la")
	assert.NoError(t, err)
}

func TestShellValidatorAllowsCleanCommandWithNilRuleEngine(t *testing.T) {
	validator := toolexec.NewShellValidator(nil, t.TempDir())

	err := validator.Validate(context.Background(), "ls -la")
	assert.NoError(t, err)
}

func TestShellValidatorRoutesApplyPatch(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil)
	validator := toolexec.NewShellValidator(engine, t.TempDir())

	interception, err := validator.Inspect(context.Background(), "sh -c 'apply_patch <<\"PATCH\"\n*** Begin Patch\n*** End Patch\nPATCH'")
	require.NoError(t, err)
	assert.Equal(t, "apply_patch shell commands must be routed through the file tool", interception.RouteReason)
}

func TestShellValidatorWarnsOnRedirect(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil)
	validator := toolexec.NewShellValidator(engine, t.TempDir())

	interception, err := validator.Inspect(context.Background(), "echo hi > redirected.txt")
	require.NoError(t, err)
	assert.Contains(t, interception.Warnings, "command redirects output to a file")
}

func TestShellValidatorBlocksRecursiveRMOutsideWorkdir(t *testing.T) {
	workDir := t.TempDir()
	engine := toolexec.NewRuleEngine(nil)
	validator := toolexec.NewShellValidator(engine, workDir)

	interception, err := validator.Inspect(context.Background(), "rm -rf ../outside")
	require.NoError(t, err)
	assert.Contains(t, interception.BlockReason, "../outside")
}

func TestShellValidatorBlocksRecursiveRMOutsideWorkdirWithUppercaseFlag(t *testing.T) {
	workDir := t.TempDir()
	engine := toolexec.NewRuleEngine(nil)
	validator := toolexec.NewShellValidator(engine, workDir)

	interception, err := validator.Inspect(context.Background(), "rm -Rf ../outside")
	require.NoError(t, err)
	assert.Contains(t, interception.BlockReason, "../outside")
}

func TestShellValidatorAllowsPathsInsideRootWorkdir(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil)
	validator := toolexec.NewShellValidator(engine, string(filepath.Separator))

	interception, err := validator.Inspect(context.Background(), "rm -rf /tmp")
	require.NoError(t, err)
	assert.Empty(t, interception.BlockReason)
}

// --- Middleware tests ---

func TestShellSafetyMiddlewareBlocksDangerousCommand(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "rm *",
			Action:   toolexec.ActionDeny,
		},
	}
	engine := toolexec.NewRuleEngine(rules)
	validator := toolexec.NewShellValidator(engine, t.TempDir())
	mw := toolexec.ShellSafetyMiddleware(validator)

	baseCalled := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "executed"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryBash)
	result := handler(ctx, toolexec.ToolCall{
		ID:    "call-1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"echo ok && rm -rf /"}`),
	})

	assert.False(t, baseCalled, "base should not be called when command is blocked")
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "shell command blocked")
}

func TestShellSafetyMiddlewareSkipsNonBash(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "rm *",
			Action:   toolexec.ActionDeny,
		},
	}
	engine := toolexec.NewRuleEngine(rules)
	validator := toolexec.NewShellValidator(engine, t.TempDir())
	mw := toolexec.ShellSafetyMiddleware(validator)

	baseCalled := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "file contents"}
	}

	handler := mw(base)
	// Use CategoryFileRead — middleware should pass through.
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryFileRead)
	result := handler(ctx, toolexec.ToolCall{
		ID:    "call-2",
		Name:  "file",
		Input: json.RawMessage(`{"path":"/tmp/test.go"}`),
	})

	assert.True(t, baseCalled, "base should be called for non-bash categories")
	assert.False(t, result.IsError)
	assert.Equal(t, "file contents", result.Content)
}

func TestShellSafetyMiddlewareRoutesApplyPatch(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil)
	validator := toolexec.NewShellValidator(engine, t.TempDir())
	mw := toolexec.ShellSafetyMiddleware(validator)

	baseCalled := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "executed"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryBash)
	result := handler(ctx, toolexec.ToolCall{
		ID:    "call-route-1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"apply_patch <<'PATCH'\n*** Begin Patch\n*** End Patch\nPATCH"}`),
	})

	assert.False(t, baseCalled, "base should not be called when command must be routed")
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "requires routing")
}

func TestShellSafetyMiddlewarePrefixesWarnings(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil)
	validator := toolexec.NewShellValidator(engine, t.TempDir())
	mw := toolexec.ShellSafetyMiddleware(validator)

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryBash)
	result := handler(ctx, toolexec.ToolCall{
		ID:    "call-warn-1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"echo hi > redirected.txt"}`),
	})

	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "warning: shell safety interceptor")
	assert.Contains(t, result.Content, "redirects output to a file")
	assert.Contains(t, result.Content, "ok")
}
