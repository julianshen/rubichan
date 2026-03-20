package hooks_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckTrustNotApproved(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	hks := []hooks.UserHookConfig{{Event: "post_edit", Command: "gofmt -w {file}"}}
	trusted, err := hooks.CheckTrust(s, "/project", hks)
	require.NoError(t, err)
	assert.False(t, trusted)
}

func TestApproveTrustAndCheck(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	hks := []hooks.UserHookConfig{{Event: "post_edit", Command: "gofmt -w {file}"}}
	err = hooks.ApproveTrust(s, "/project", hks)
	require.NoError(t, err)

	trusted, err := hooks.CheckTrust(s, "/project", hks)
	require.NoError(t, err)
	assert.True(t, trusted)
}

func TestTrustInvalidatedOnChange(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	hks := []hooks.UserHookConfig{{Event: "post_edit", Command: "gofmt -w {file}"}}
	hooks.ApproveTrust(s, "/project", hks) //nolint:errcheck

	hks[0].Command = "golangci-lint run"
	trusted, err := hooks.CheckTrust(s, "/project", hks)
	require.NoError(t, err)
	assert.False(t, trusted, "trust should be invalidated when hooks change")
}

func TestTrustInvalidatedOnPatternChange(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	hks := []hooks.UserHookConfig{{Event: "pre_edit", Pattern: "*.go", Command: "gofmt -w {file}"}}
	hooks.ApproveTrust(s, "/project", hks) //nolint:errcheck

	// Widen the pattern scope — must invalidate approval
	hks[0].Pattern = "*"
	trusted, err := hooks.CheckTrust(s, "/project", hks)
	require.NoError(t, err)
	assert.False(t, trusted, "trust should be invalidated when pattern changes")
}
