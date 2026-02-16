package sandbox

import (
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSandbox creates a Sandbox backed by an in-memory SQLite store.
// The skill name and declared permissions are passed in. The caller is
// responsible for closing the returned store.
func newTestSandbox(t *testing.T, skillName string, declared []skills.Permission) (*Sandbox, *store.Store) {
	t.Helper()
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	sb := New(s, skillName, declared, DefaultPolicy())
	return sb, s
}

func TestCheckPermissionAllowed(t *testing.T) {
	sb, st := newTestSandbox(t, "my-skill", []skills.Permission{skills.PermFileRead})
	defer st.Close()

	// Pre-approve the permission in the store.
	err := st.Approve("my-skill", string(skills.PermFileRead), "always")
	require.NoError(t, err)

	// Check should pass.
	err = sb.CheckPermission(skills.PermFileRead)
	assert.NoError(t, err)
}

func TestCheckPermissionDenied(t *testing.T) {
	sb, st := newTestSandbox(t, "my-skill", []skills.Permission{skills.PermShellExec})
	defer st.Close()

	// No approval in the store.
	err := sb.CheckPermission(skills.PermShellExec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not approved")
}

func TestCheckPermissionNotDeclared(t *testing.T) {
	sb, st := newTestSandbox(t, "my-skill", []skills.Permission{skills.PermFileRead})
	defer st.Close()

	// shell:exec was not declared in the manifest.
	err := sb.CheckPermission(skills.PermShellExec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not declared")
}

func TestRateLimitShellExec(t *testing.T) {
	policy := DefaultPolicy()
	policy.MaxShellExecPerTurn = 2

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sb := New(s, "my-skill", []skills.Permission{skills.PermShellExec}, policy)

	// First two calls should succeed.
	err = sb.CheckRateLimit("shell:exec")
	assert.NoError(t, err)
	err = sb.CheckRateLimit("shell:exec")
	assert.NoError(t, err)

	// Third call should fail.
	err = sb.CheckRateLimit("shell:exec")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}

func TestResetRateLimits(t *testing.T) {
	policy := DefaultPolicy()
	policy.MaxShellExecPerTurn = 2

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sb := New(s, "my-skill", []skills.Permission{skills.PermShellExec}, policy)

	// Use up the limit.
	require.NoError(t, sb.CheckRateLimit("shell:exec"))
	require.NoError(t, sb.CheckRateLimit("shell:exec"))
	require.Error(t, sb.CheckRateLimit("shell:exec"))

	// Reset counters.
	sb.ResetTurnLimits()

	// Should be allowed again.
	err = sb.CheckRateLimit("shell:exec")
	assert.NoError(t, err)
}

func TestAutoApproveMode(t *testing.T) {
	sb, st := newTestSandbox(t, "my-skill", []skills.Permission{skills.PermFileRead, skills.PermNetFetch})
	defer st.Close()

	// No approval in store for file:read, but set auto-approve.
	sb.SetAutoApprove([]string{"my-skill"})

	// Auto-approved skill should bypass store check.
	err := sb.CheckPermission(skills.PermFileRead)
	assert.NoError(t, err)

	// Non-declared permission should still fail, even with auto-approve.
	err = sb.CheckPermission(skills.PermShellExec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not declared")
}

func TestRateLimitLLMCall(t *testing.T) {
	policy := DefaultPolicy()
	policy.MaxLLMCallsPerTurn = 1

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sb := New(s, "my-skill", nil, policy)

	err = sb.CheckRateLimit("llm:call")
	assert.NoError(t, err)

	err = sb.CheckRateLimit("llm:call")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}

func TestRateLimitNetFetch(t *testing.T) {
	policy := DefaultPolicy()
	policy.MaxNetFetchPerTurn = 1

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sb := New(s, "my-skill", nil, policy)

	err = sb.CheckRateLimit("net:fetch")
	assert.NoError(t, err)

	err = sb.CheckRateLimit("net:fetch")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}

func TestRateLimitUnknownResourceNoLimit(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sb := New(s, "my-skill", nil, DefaultPolicy())

	// Unknown resource should have no limit and always succeed.
	err = sb.CheckRateLimit("unknown:resource")
	assert.NoError(t, err)
}

func TestDefaultPolicyValues(t *testing.T) {
	p := DefaultPolicy()

	assert.Equal(t, 10, p.MaxLLMCallsPerTurn)
	assert.Equal(t, 20, p.MaxShellExecPerTurn)
	assert.Equal(t, 10, p.MaxNetFetchPerTurn)
	assert.NotZero(t, p.ShellExecTimeout)
	assert.NotZero(t, p.NetFetchTimeout)
}
