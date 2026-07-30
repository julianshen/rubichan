// Package folderaccess gates rubichan on the user having approved the
// directory it was started in. Approval is per working directory and
// persisted, so the question is asked once per folder rather than once per
// session.
//
// It lives outside cmd/ because the policy — when a prompt is allowed, what
// the CLI flags mean, what an unapproved folder costs — is product behaviour
// that main.go should call rather than contain. Extracted from
// cmd/rubichan/main.go as a slice of the redesign's Phase 3.
package folderaccess

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Store persists folder-access decisions. It is the subset of
// internal/store.Store this package needs, declared here so the policy does
// not depend on the persistence layer's package.
type Store interface {
	IsFolderApproved(workingDir string) (bool, error)
	ApproveFolderAccess(workingDir string) error
}

// Prompt asks the user to approve workingDir and reports their answer. Only
// "yes" (in any case) approves; anything else — including EOF from a closed
// stdin — declines, because access to a folder should never be granted by
// silence.
func Prompt(workingDir string, in io.Reader, out io.Writer) (bool, error) {
	if _, err := fmt.Fprintf(out, "Allow rubichan to access this folder?\n  %s\nType 'yes' to continue: ", workingDir); err != nil {
		return false, fmt.Errorf("writing folder access prompt: %w", err)
	}

	line, err := readLine(in)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("reading folder access response: %w", err)
	}
	return strings.EqualFold(strings.TrimSpace(line), "yes"), nil
}

// readLine consumes one line from in and not a byte more, returning it without
// the trailing newline. A buffered reader cannot be used here: it fills its
// buffer from a single underlying read and is then discarded, so anything the
// stream offered after the response line would be lost. Prompt does not own
// its reader — the interactive path hands the same os.Stdin to the TUI once
// the folder is approved — so over-reading would swallow the user's first
// keystrokes.
//
// Reaching the end of the stream without a newline returns what was read
// along with io.EOF, leaving the caller to decide whether that counts as an
// answer.
func readLine(in io.Reader) (string, error) {
	var line []byte
	var buf [1]byte
	stalled := 0
	for {
		n, err := in.Read(buf[:])
		if n > 0 {
			stalled = 0
			if buf[0] == '\n' {
				return string(line), nil
			}
			line = append(line, buf[0])
		}
		if err != nil {
			return string(line), err
		}
		// io.Reader permits (0, nil) and asks callers to treat it as a
		// no-op, so a reader that never progresses would spin here
		// forever. bufio.Reader gives up after this many attempts; a
		// prompt that hangs is worse than one that errors.
		if n == 0 {
			stalled++
			if stalled >= maxStalledReads {
				return string(line), io.ErrNoProgress
			}
		}
	}
}

// maxStalledReads matches bufio.Reader's tolerance for readers that return no
// bytes and no error, so replacing it here does not lose its guard.
const maxStalledReads = 100

// EnsureApproved returns nil once workingDir is approved, prompting the user
// when it is not already on record. A denial is an error: the caller cannot
// proceed without the folder.
func EnsureApproved(s Store, workingDir string, in io.Reader, out io.Writer) error {
	approved, err := s.IsFolderApproved(workingDir)
	if err != nil {
		return fmt.Errorf("checking folder access approval: %w", err)
	}
	if approved {
		return nil
	}

	allow, err := Prompt(workingDir, in, out)
	if err != nil {
		return err
	}
	if !allow {
		return fmt.Errorf("folder access denied by user")
	}

	if err := s.ApproveFolderAccess(workingDir); err != nil {
		return fmt.Errorf("saving folder access approval: %w", err)
	}
	return nil
}

// EnsureApprovedInteractive is the interactive mode's entry point. Either
// approval flag turns the prompt off, so a user who already said yes on the
// command line is not asked again.
func EnsureApprovedInteractive(s Store, workingDir string, in io.Reader, out io.Writer, autoApprove, approveCwd bool) error {
	if autoApprove || approveCwd {
		return EnsureApprovedNonInteractive(s, workingDir, autoApprove, approveCwd)
	}
	return EnsureApproved(s, workingDir, in, out)
}

// EnsureApprovedNonInteractive is the headless entry point: with no user to
// ask, an unapproved folder is an error unless a flag granted approval up
// front.
func EnsureApprovedNonInteractive(s Store, workingDir string, autoApprove, approveCwd bool) error {
	approved, err := s.IsFolderApproved(workingDir)
	if err != nil {
		return fmt.Errorf("checking folder access approval: %w", err)
	}
	if approved {
		return nil
	}

	if !autoApprove && !approveCwd {
		return fmt.Errorf("folder access for %q is not approved; rerun interactively to approve or use --approve-cwd/--auto-approve", workingDir)
	}

	if err := s.ApproveFolderAccess(workingDir); err != nil {
		return fmt.Errorf("saving folder access approval: %w", err)
	}
	return nil
}
