package folderaccess_test

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// failingStore lets the error paths be exercised: a real store only fails on
// conditions (a corrupt database, a full disk) that a test cannot stage.
type failingStore struct {
	checkErr   error
	approveErr error
	approved   bool
}

func (f *failingStore) IsFolderApproved(string) (bool, error) { return f.approved, f.checkErr }
func (f *failingStore) ApproveFolderAccess(string) error      { return f.approveErr }

// failingWriter refuses every write, standing in for a closed or broken
// terminal.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("terminal closed") }

// failingReader refuses every read with something other than EOF, which the
// prompt must distinguish from a user simply pressing enter.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("stdin broken") }

func TestPromptReportsWriteFailure(t *testing.T) {
	t.Parallel()

	allowed, err := folderaccess.Prompt("/tmp/project", strings.NewReader("yes\n"), failingWriter{})
	require.Error(t, err)
	assert.False(t, allowed, "a prompt the user never saw cannot be an approval")
	assert.Contains(t, err.Error(), "writing folder access prompt")
}

func TestPromptReportsReadFailure(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	allowed, err := folderaccess.Prompt("/tmp/project", failingReader{}, &out)
	require.Error(t, err)
	assert.False(t, allowed)
	assert.Contains(t, err.Error(), "reading folder access response")
}

func TestPromptTreatsEOFAsDeclined(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	allowed, err := folderaccess.Prompt("/tmp/project", strings.NewReader("yes"), &out)
	require.NoError(t, err, "input without a trailing newline is not a failure")
	assert.True(t, allowed)

	allowed, err = folderaccess.Prompt("/tmp/project", strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.False(t, allowed, "silence is not approval")
}

func TestEnsureApprovedReportsCheckFailure(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := folderaccess.EnsureApproved(&failingStore{checkErr: errors.New("db gone")},
		"/tmp/project", strings.NewReader("yes\n"), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checking folder access approval")
	assert.Empty(t, out.String(), "an unreadable store must not fall through to the prompt")
}

func TestEnsureApprovedPropagatesPromptFailure(t *testing.T) {
	t.Parallel()

	err := folderaccess.EnsureApproved(&failingStore{}, "/tmp/project",
		strings.NewReader("yes\n"), failingWriter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing folder access prompt")
}

func TestEnsureApprovedReportsSaveFailure(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := folderaccess.EnsureApproved(&failingStore{approveErr: errors.New("disk full")},
		"/tmp/project", strings.NewReader("yes\n"), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "saving folder access approval")
}

func TestEnsureApprovedNonInteractiveReportsCheckFailure(t *testing.T) {
	t.Parallel()

	err := folderaccess.EnsureApprovedNonInteractive(&failingStore{checkErr: errors.New("db gone")},
		"/tmp/project", true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checking folder access approval")
}

func TestEnsureApprovedNonInteractiveReportsSaveFailure(t *testing.T) {
	t.Parallel()

	err := folderaccess.EnsureApprovedNonInteractive(&failingStore{approveErr: errors.New("disk full")},
		"/tmp/project", true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "saving folder access approval")
}

func TestEnsureApprovedNonInteractiveSkipsSaveWhenAlreadyApproved(t *testing.T) {
	t.Parallel()

	err := folderaccess.EnsureApprovedNonInteractive(
		&failingStore{approved: true, approveErr: errors.New("must not be called")},
		"/tmp/project", false, false)
	assert.NoError(t, err, "an already-approved folder needs neither a flag nor a second write")
}

func TestEnsureApprovedInteractivePromptsWithoutFlags(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := folderaccess.EnsureApprovedInteractive(&failingStore{}, "/tmp/project",
		strings.NewReader("yes\n"), &out, false, false)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Allow rubichan to access this folder?",
		"with neither flag set the user must still be asked")
}

// TestPromptLeavesLaterInputForTheNextReader pins the contract that makes
// Prompt safe on a stream it does not own: the interactive path hands the same
// os.Stdin to the TUI once the prompt is answered, so consuming past the
// response line would swallow the user's first keystrokes.
func TestPromptLeavesLaterInputForTheNextReader(t *testing.T) {
	t.Parallel()

	in := strings.NewReader("yes\nhello from the TUI\n")
	var out bytes.Buffer

	allowed, err := folderaccess.Prompt("/tmp/project", in, &out)
	require.NoError(t, err)
	require.True(t, allowed)

	rest, err := io.ReadAll(in)
	require.NoError(t, err)
	assert.Equal(t, "hello from the TUI\n", string(rest),
		"Prompt must consume its line and no more")
}

// stalledReader always reports zero bytes and no error. io.Reader permits this
// and tells callers to treat it as a no-op, so a reader that never progresses
// must be given up on rather than spun on forever — bufio.Reader, which this
// package used to rely on, bails after 100 such reads.
type stalledReader struct{}

func (stalledReader) Read([]byte) (int, error) { return 0, nil }

func TestPromptGivesUpOnAReaderThatNeverProgresses(t *testing.T) {
	t.Parallel()

	type result struct {
		allowed bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		var out bytes.Buffer
		allowed, err := folderaccess.Prompt("/tmp/project", stalledReader{}, &out)
		done <- result{allowed, err}
	}()

	select {
	case got := <-done:
		require.Error(t, got.err, "a reader that never progresses is not an approval")
		assert.False(t, got.allowed)
		assert.Contains(t, got.err.Error(), "reading folder access response")
	case <-time.After(5 * time.Second):
		t.Fatal("Prompt hung on a reader that never progresses")
	}
}
