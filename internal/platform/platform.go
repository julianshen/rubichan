// Package platform provides a unified interface for interacting with
// git hosting platforms (GitHub, GitLab) for PR comments, reviews,
// and CI integration.
package platform

import (
	"context"
	"fmt"
	"os/exec"
)

// cliAvailable reports whether the given CLI binary is installed and
// executable on PATH. Overridable by tests that must not depend on the
// host having `gh` or `glab` present.
var cliAvailable = func(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Platform abstracts git hosting platform operations.
type Platform interface {
	// Name returns the platform identifier ("github" or "gitlab").
	Name() string

	// PostPRComment posts a top-level comment on a pull/merge request.
	PostPRComment(ctx context.Context, repo string, prNum int, body string) error

	// PostPRReview submits a review with optional inline comments.
	PostPRReview(ctx context.Context, repo string, prNum int, review Review) error

	// GetPRDiff retrieves the diff for a pull/merge request.
	GetPRDiff(ctx context.Context, repo string, prNum int) (string, error)

	// ListPRFiles lists files changed in a pull/merge request.
	ListPRFiles(ctx context.Context, repo string, prNum int) ([]PRFile, error)

	// UploadSARIF uploads a SARIF report for code scanning integration.
	// commitSHA is the commit hash; ref is the git ref (e.g., "refs/heads/main").
	UploadSARIF(ctx context.Context, repo string, commitSHA, ref string, sarif []byte) error
}

// Review side constants.
const (
	SideRight = "RIGHT"
	SideLeft  = "LEFT"
)

// Review event constants.
const (
	EventComment        = "COMMENT"
	EventApprove        = "APPROVE"
	EventRequestChanges = "REQUEST_CHANGES"
)

// PR file status constants.
const (
	FileStatusAdded    = "added"
	FileStatusModified = "modified"
	FileStatusRemoved  = "removed"
)

// ReviewComment represents an inline comment on a specific file and line.
type ReviewComment struct {
	Path string
	Line int
	Body string
	Side string
}

// Review represents a pull request review with summary and inline comments.
type Review struct {
	Body     string
	Event    string
	Comments []ReviewComment
}

// PRFile represents a file changed in a pull request.
type PRFile struct {
	Filename string
	Status   string
	Patch    string
}

// DetectedEnv holds the auto-detected CI environment information.
type DetectedEnv struct {
	PlatformName string // "github" or "gitlab"
	Repo         string // "owner/repo"
	PRNumber     int    // 0 if not in a PR context
	Token        string
	CommitSHA    string // commit hash for SARIF upload
	Ref          string // git ref (e.g., "refs/heads/main")
}

// New creates a Platform client from the detected environment.
//
// Selection order:
//  1. SDK-backed client when env.Token is set (preferred: type-safe,
//     pagination, rate-limit handling).
//  2. CLI-backed fallback when no token is present but the platform's
//     CLI (`gh` or `glab`) is installed — those binaries authenticate
//     via their own token caches (e.g. `gh auth login`).
//  3. Descriptive error pointing at both options.
//
// Returns an error if env is nil or the platform is unsupported.
func New(env *DetectedEnv) (Platform, error) {
	if env == nil {
		return nil, fmt.Errorf("no platform environment detected")
	}

	if env.Token != "" {
		switch env.PlatformName {
		case "github":
			return NewGitHubClient(env.Token), nil
		case "gitlab":
			return NewGitLabClient(env.Token)
		default:
			return nil, fmt.Errorf("unsupported platform: %q", env.PlatformName)
		}
	}

	// CLI fallback path: no token but the local binary may be able
	// to authenticate on our behalf via its own token cache.
	switch env.PlatformName {
	case "github":
		if cliAvailable("gh") {
			return NewCLIGitHubClient(), nil
		}
		return nil, fmt.Errorf(
			"GITHUB_TOKEN not set and 'gh' CLI not installed; " +
				"set GITHUB_TOKEN or install gh (https://cli.github.com)")
	case "gitlab":
		if cliAvailable("glab") {
			return NewCLIGitLabClient(), nil
		}
		return nil, fmt.Errorf(
			"GITLAB_TOKEN not set and 'glab' CLI not installed; " +
				"set GITLAB_TOKEN or install glab (https://gitlab.com/gitlab-org/cli)")
	default:
		return nil, fmt.Errorf("unsupported platform: %q", env.PlatformName)
	}
}
