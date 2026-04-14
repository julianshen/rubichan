package platform

import (
	"context"
	"encoding/json"
	"fmt"
)

// ghReviewComment mirrors the GitHub reviews API comment shape.
// Exported via JSON struct tags only — consumers build Review values.
type ghReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
	Side string `json:"side"`
}

type ghReviewPayload struct {
	Body     string            `json:"body"`
	Event    string            `json:"event"`
	Comments []ghReviewComment `json:"comments"`
}

// CLIGitHubClient implements Platform by shelling out to the `gh` CLI.
// Used as a fallback when no GITHUB_TOKEN is present but `gh` is installed
// locally (typically authenticated via `gh auth login`).
type CLIGitHubClient struct {
	execFn ExecFunc
}

// NewCLIGitHubClient returns a CLI-backed GitHub platform client that invokes
// the real `gh` binary. For tests, use NewCLIGitHubClientWithExec to inject
// a fake exec function.
func NewCLIGitHubClient() *CLIGitHubClient {
	return &CLIGitHubClient{execFn: defaultExec}
}

// NewCLIGitHubClientWithExec returns a client that calls the provided exec
// function instead of spawning a real process. Intended for tests.
func NewCLIGitHubClientWithExec(execFn ExecFunc) *CLIGitHubClient {
	return &CLIGitHubClient{execFn: execFn}
}

func (c *CLIGitHubClient) Name() string { return "github" }

func (c *CLIGitHubClient) PostPRComment(ctx context.Context, repo string, prNum int, body string) error {
	path := fmt.Sprintf("repos/%s/issues/%d/comments", repo, prNum)
	_, err := c.execFn(ctx, nil,
		"gh", "api", path, "-X", "POST", "-f", "body="+body)
	if err != nil {
		return fmt.Errorf("gh api: posting PR comment: %w", err)
	}
	return nil
}

func (c *CLIGitHubClient) PostPRReview(ctx context.Context, repo string, prNum int, review Review) error {
	payload := ghReviewPayload{
		Body:     review.Body,
		Event:    review.Event,
		Comments: make([]ghReviewComment, len(review.Comments)),
	}
	for i, cmt := range review.Comments {
		payload.Comments[i] = ghReviewComment{
			Path: cmt.Path,
			Line: cmt.Line,
			Body: cmt.Body,
			Side: cmt.Side,
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("gh api: encoding review: %w", err)
	}

	path := fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, prNum)
	_, err = c.execFn(ctx, body,
		"gh", "api", path, "-X", "POST", "--input", "-")
	if err != nil {
		return fmt.Errorf("gh api: posting PR review: %w", err)
	}
	return nil
}

func (c *CLIGitHubClient) GetPRDiff(ctx context.Context, repo string, prNum int) (string, error) {
	out, err := c.execFn(ctx, nil,
		"gh", "pr", "diff", fmt.Sprintf("%d", prNum), "--repo", repo)
	if err != nil {
		return "", fmt.Errorf("gh pr diff: %w", err)
	}
	return string(out), nil
}

func (c *CLIGitHubClient) ListPRFiles(ctx context.Context, repo string, prNum int) ([]PRFile, error) {
	// --paginate flattens all pages into a single JSON array, matching
	// what the SDK client returns after its loop-and-append.
	path := fmt.Sprintf("repos/%s/pulls/%d/files", repo, prNum)
	out, err := c.execFn(ctx, nil,
		"gh", "api", path, "--paginate")
	if err != nil {
		return nil, fmt.Errorf("gh api: listing PR files: %w", err)
	}

	var raw []struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
		Patch    string `json:"patch"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh api: parsing PR files JSON: %w", err)
	}
	files := make([]PRFile, len(raw))
	for i, r := range raw {
		files[i] = PRFile{Filename: r.Filename, Status: r.Status, Patch: r.Patch}
	}
	return files, nil
}

func (c *CLIGitHubClient) UploadSARIF(ctx context.Context, repo string, commitSHA, ref string, sarif []byte) error {
	// GitHub's code-scanning/sarifs endpoint expects the SARIF to arrive
	// gzip-compressed and base64-encoded under the "sarif" JSON field.
	encoded, err := gzipBase64(sarif)
	if err != nil {
		return fmt.Errorf("gh api: encoding SARIF: %w", err)
	}
	payload, err := json.Marshal(map[string]string{
		"commit_sha": commitSHA,
		"ref":        ref,
		"sarif":      encoded,
	})
	if err != nil {
		return fmt.Errorf("gh api: encoding SARIF payload: %w", err)
	}
	path := fmt.Sprintf("repos/%s/code-scanning/sarifs", repo)
	_, err = c.execFn(ctx, payload,
		"gh", "api", path, "-X", "POST", "--input", "-")
	if err != nil {
		return fmt.Errorf("gh api: uploading SARIF: %w", err)
	}
	return nil
}
