// Package platform provides a unified interface for interacting with
// git hosting platforms (GitHub, GitLab) for PR comments, reviews,
// and CI integration.
package platform

import (
	"context"
	"fmt"
)

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
	UploadSARIF(ctx context.Context, repo string, ref string, sarif []byte) error
}

// ReviewComment represents an inline comment on a specific file and line.
type ReviewComment struct {
	Path string
	Line int
	Body string
	Side string // "LEFT" or "RIGHT"
}

// Review represents a pull request review with summary and inline comments.
type Review struct {
	Body     string
	Event    string // "APPROVE", "REQUEST_CHANGES", "COMMENT"
	Comments []ReviewComment
}

// PRFile represents a file changed in a pull request.
type PRFile struct {
	Filename string
	Status   string // "added", "modified", "removed"
	Patch    string
}

// DetectedEnv holds the auto-detected CI environment information.
type DetectedEnv struct {
	PlatformName string // "github" or "gitlab"
	Repo         string // "owner/repo"
	PRNumber     int    // 0 if not in a PR context
	Token        string
}

// New creates a Platform client from the detected environment.
func New(env *DetectedEnv) (Platform, error) {
	switch env.PlatformName {
	case "github":
		return NewGitHubClient(env.Token), nil
	case "gitlab":
		return NewGitLabClient(env.Token)
	default:
		return nil, fmt.Errorf("unsupported platform: %q", env.PlatformName)
	}
}
