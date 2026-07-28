package folderaccess_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/folderaccess"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustOpenStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPromptYesResponse(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer

	allowed, err := folderaccess.Prompt("/tmp/project", strings.NewReader("yes\n"), &out)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Contains(t, out.String(), "Allow rubichan to access this folder?")
}

func TestPromptNoResponse(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer

	allowed, err := folderaccess.Prompt("/tmp/project", strings.NewReader("no\n"), &out)
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestPromptCaseInsensitive(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer

	allowed, err := folderaccess.Prompt("/tmp/project", strings.NewReader("YES\n"), &out)
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestEnsureApprovedFirstTimeApprove(t *testing.T) {
	s := mustOpenStore(t)

	var out bytes.Buffer
	err := folderaccess.EnsureApproved(s, "/tmp/project", strings.NewReader("yes\n"), &out)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Allow rubichan to access this folder?")

	approved, err := s.IsFolderApproved("/tmp/project")
	require.NoError(t, err)
	assert.True(t, approved)
}

func TestEnsureApprovedDenied(t *testing.T) {
	s := mustOpenStore(t)

	var out bytes.Buffer
	err := folderaccess.EnsureApproved(s, "/tmp/project", strings.NewReader("no\n"), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "folder access denied")
}

func TestEnsureApprovedAlreadyApprovedSkipsPrompt(t *testing.T) {
	s := mustOpenStore(t)
	require.NoError(t, s.ApproveFolderAccess("/tmp/project"))

	var out bytes.Buffer
	err := folderaccess.EnsureApproved(s, "/tmp/project", strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Empty(t, out.String())
}

func TestEnsureApprovedNonInteractiveDeniedWithoutAutoApprove(t *testing.T) {
	s := mustOpenStore(t)

	err := folderaccess.EnsureApprovedNonInteractive(s, "/tmp/project", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not approved")
}

func TestEnsureApprovedNonInteractiveAutoApproves(t *testing.T) {
	s := mustOpenStore(t)

	err := folderaccess.EnsureApprovedNonInteractive(s, "/tmp/project", true, false)
	require.NoError(t, err)

	approved, err := s.IsFolderApproved("/tmp/project")
	require.NoError(t, err)
	assert.True(t, approved)
}

func TestEnsureApprovedNonInteractiveRequiresExplicitApproval(t *testing.T) {
	s := mustOpenStore(t)

	err := folderaccess.EnsureApprovedNonInteractive(s, filepath.Join(t.TempDir(), "repo"), false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--approve-cwd/--auto-approve")
}

func TestEnsureApprovedNonInteractiveApproveCwd(t *testing.T) {
	s := mustOpenStore(t)

	repoDir := filepath.Join(t.TempDir(), "repo")
	err := folderaccess.EnsureApprovedNonInteractive(s, repoDir, false, true)
	require.NoError(t, err)

	approved, err := s.IsFolderApproved(repoDir)
	require.NoError(t, err)
	assert.True(t, approved)
}

func TestEnsureApprovedInteractiveUsesAutoApprove(t *testing.T) {
	s := mustOpenStore(t)

	var out bytes.Buffer
	err := folderaccess.EnsureApprovedInteractive(s, "/tmp/project", strings.NewReader(""), &out, true, false)
	require.NoError(t, err)
	assert.Empty(t, out.String())

	approved, err := s.IsFolderApproved("/tmp/project")
	require.NoError(t, err)
	assert.True(t, approved)
}

func TestEnsureApprovedInteractiveUsesApproveCwd(t *testing.T) {
	s := mustOpenStore(t)

	var out bytes.Buffer
	err := folderaccess.EnsureApprovedInteractive(s, "/tmp/project", strings.NewReader(""), &out, false, true)
	require.NoError(t, err)
	assert.Empty(t, out.String())

	approved, err := s.IsFolderApproved("/tmp/project")
	require.NoError(t, err)
	assert.True(t, approved)
}
