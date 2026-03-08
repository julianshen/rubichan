package toolexec_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultInterceptionRulesNotEmpty(t *testing.T) {
	rules := toolexec.DefaultInterceptionRules()
	assert.NotEmpty(t, rules, "default rules should not be empty")
}

func TestCommandInterceptorUsesDefaultRulesWhenNil(t *testing.T) {
	ci, err := toolexec.NewCommandInterceptor(t.TempDir(), nil)
	require.NoError(t, err)
	assert.NotEmpty(t, ci.Rules(), "should use default rules when nil is passed")
}

func TestCommandInterceptorUsesCustomRules(t *testing.T) {
	custom := []toolexec.InterceptionRule{
		{
			Pattern: regexp.MustCompile(`\bfoo\b`),
			Action:  toolexec.ActionBlock,
			Message: "foo is blocked",
		},
	}
	ci, err := toolexec.NewCommandInterceptor(t.TempDir(), custom)
	require.NoError(t, err)
	assert.Len(t, ci.Rules(), 1)
}

func TestCommandInterceptorRejectsNilPattern(t *testing.T) {
	rules := []toolexec.InterceptionRule{
		{
			Pattern: nil,
			Action:  toolexec.ActionBlock,
			Message: "bad rule",
		},
	}
	ci, err := toolexec.NewCommandInterceptor(t.TempDir(), rules)
	assert.Error(t, err)
	assert.Nil(t, ci)
	assert.Contains(t, err.Error(), "nil Pattern")
}

func TestCommandInterceptorDefensiveCopy(t *testing.T) {
	custom := []toolexec.InterceptionRule{
		{
			Pattern: regexp.MustCompile(`\bfoo\b`),
			Action:  toolexec.ActionBlock,
			Message: "foo is blocked",
		},
	}
	ci, err := toolexec.NewCommandInterceptor(t.TempDir(), custom)
	require.NoError(t, err)

	// Mutating the original slice should not affect the interceptor.
	custom[0].Message = "mutated"
	assert.Equal(t, "foo is blocked", ci.Rules()[0].Message)
}

func TestCommandInterceptorCustomBlockRule(t *testing.T) {
	custom := []toolexec.InterceptionRule{
		{
			Pattern: regexp.MustCompile(`\bdangerous\b`),
			Action:  toolexec.ActionBlock,
			Message: "dangerous command blocked",
		},
	}
	ci, err := toolexec.NewCommandInterceptor(t.TempDir(), custom)
	require.NoError(t, err)

	parts, err := toolexec.ParseCommand("dangerous --force")
	require.NoError(t, err)

	result := ci.Intercept("dangerous --force", parts)
	assert.Equal(t, "dangerous command blocked", result.BlockReason)
}

func TestCommandInterceptorCustomRouteRule(t *testing.T) {
	custom := []toolexec.InterceptionRule{
		{
			Pattern: regexp.MustCompile(`\bcustom_patch\b`),
			Action:  toolexec.ActionRouteToFileTool,
			Message: "custom_patch must be routed",
		},
	}
	ci, err := toolexec.NewCommandInterceptor(t.TempDir(), custom)
	require.NoError(t, err)

	parts, err := toolexec.ParseCommand("custom_patch apply")
	require.NoError(t, err)

	result := ci.Intercept("custom_patch apply", parts)
	assert.Equal(t, "custom_patch must be routed", result.RouteReason)
}

func TestCommandInterceptorWarnsOnRedirect(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("echo hi > output.txt")
	require.NoError(t, err)

	result := ci.Intercept("echo hi > output.txt", parts)
	assert.Contains(t, result.Warnings, "command redirects output to a file")
	assert.Empty(t, result.BlockReason)
}

func TestCommandInterceptorWarnsOnRedirectWithoutSpace(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("echo hi>out.txt")
	require.NoError(t, err)

	result := ci.Intercept("echo hi>out.txt", parts)
	assert.Contains(t, result.Warnings, "command redirects output to a file")
}

func TestCommandInterceptorWarnsOnSedInPlace(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("sed -i 's/foo/bar/' file.txt")
	require.NoError(t, err)

	result := ci.Intercept("sed -i 's/foo/bar/' file.txt", parts)
	assert.Contains(t, result.Warnings, "command uses sed -i for in-place file edits")
}

func TestCommandInterceptorWarnsOnChmod(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("chmod +x script.sh")
	require.NoError(t, err)

	result := ci.Intercept("chmod +x script.sh", parts)
	assert.Contains(t, result.Warnings, "command changes file ownership/permissions")
}

func TestCommandInterceptorWarnsOnChown(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("chown root:root file.txt")
	require.NoError(t, err)

	result := ci.Intercept("chown root:root file.txt", parts)
	assert.Contains(t, result.Warnings, "command changes file ownership/permissions")
}

func TestCommandInterceptorWarnsOnMvOutside(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("mv file.txt ../outside/")
	require.NoError(t, err)

	result := ci.Intercept("mv file.txt ../outside/", parts)
	assert.Contains(t, result.Warnings, "command may move/copy files outside the working directory")
}

func TestCommandInterceptorWarnsOnMvToRoot(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("mv file /")
	require.NoError(t, err)

	result := ci.Intercept("mv file /", parts)
	assert.Contains(t, result.Warnings, "command may move/copy files outside the working directory")
}

func TestCommandInterceptorWarnsOnMvToDotDot(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("mv file ..")
	require.NoError(t, err)

	result := ci.Intercept("mv file ..", parts)
	assert.Contains(t, result.Warnings, "command may move/copy files outside the working directory")
}

func TestCommandInterceptorWarnsOnTee(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("echo hello | tee output.txt")
	require.NoError(t, err)

	result := ci.Intercept("echo hello | tee output.txt", parts)
	assert.Contains(t, result.Warnings, "command uses tee to write to a file")
}

func TestCommandInterceptorWarnsOnDd(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("dd if=/dev/zero of=output.bin bs=1024 count=1")
	require.NoError(t, err)

	result := ci.Intercept("dd if=/dev/zero of=output.bin bs=1024 count=1", parts)
	assert.Contains(t, result.Warnings, "command uses dd to write to a file")
}

func TestCommandInterceptorDoesNotWarnOnDdWithoutOf(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("dd if=/dev/zero bs=1024 count=1")
	require.NoError(t, err)

	result := ci.Intercept("dd if=/dev/zero bs=1024 count=1", parts)
	for _, w := range result.Warnings {
		assert.NotContains(t, w, "dd")
	}
}

func TestCommandInterceptorWarnsOnTruncate(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("truncate -s 0 logfile.log")
	require.NoError(t, err)

	result := ci.Intercept("truncate -s 0 logfile.log", parts)
	assert.Contains(t, result.Warnings, "command uses truncate to modify a file")
}

func TestCommandInterceptorRoutesApplyPatch(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("apply_patch foo")
	require.NoError(t, err)

	result := ci.Intercept("apply_patch foo", parts)
	assert.Equal(t, "apply_patch shell commands must be routed through the file tool", result.RouteReason)
}

func TestCommandInterceptorBlocksRecursiveRMOutsideWorkdir(t *testing.T) {
	workDir := t.TempDir()
	ci := toolexec.MustNewCommandInterceptor(workDir, nil)

	parts, err := toolexec.ParseCommand("rm -rf ../outside")
	require.NoError(t, err)

	result := ci.Intercept("rm -rf ../outside", parts)
	assert.Contains(t, result.BlockReason, "../outside")
}

func TestCommandInterceptorAllowsRecursiveRMInsideWorkdir(t *testing.T) {
	workDir := t.TempDir()
	ci := toolexec.MustNewCommandInterceptor(workDir, nil)

	parts, err := toolexec.ParseCommand("rm -rf subdir")
	require.NoError(t, err)

	result := ci.Intercept("rm -rf subdir", parts)
	assert.Empty(t, result.BlockReason)
}

func TestCommandInterceptorMultipleWarnings(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	cmd := "echo hi > output.txt; chmod +x output.txt"
	parts, err := toolexec.ParseCommand(cmd)
	require.NoError(t, err)

	result := ci.Intercept(cmd, parts)
	assert.GreaterOrEqual(t, len(result.Warnings), 2, "should collect multiple warnings")
	assert.Contains(t, result.Warnings, "command redirects output to a file")
	assert.Contains(t, result.Warnings, "command changes file ownership/permissions")
}

func TestCommandInterceptorNoMatchReturnsEmpty(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)

	parts, err := toolexec.ParseCommand("ls -la")
	require.NoError(t, err)

	result := ci.Intercept("ls -la", parts)
	assert.Empty(t, result.BlockReason)
	assert.Empty(t, result.RouteReason)
	assert.Empty(t, result.Warnings)
}

func TestCommandInterceptorRulesCopyIsolation(t *testing.T) {
	ci := toolexec.MustNewCommandInterceptor(t.TempDir(), nil)
	rules1 := ci.Rules()
	rules2 := ci.Rules()

	// Mutating the returned slice should not affect the interceptor.
	rules1[0].Message = "mutated"
	assert.NotEqual(t, rules1[0].Message, rules2[0].Message)
}

func TestNewShellValidatorWithNilInterceptor(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil)
	// Passing nil interceptor should not panic; falls back to defaults.
	validator := toolexec.NewShellValidatorWithInterceptor(engine, t.TempDir(), nil)

	interception, err := validator.Inspect(context.Background(), "echo hi > output.txt")
	require.NoError(t, err)
	assert.Contains(t, interception.Warnings, "command redirects output to a file")
}
