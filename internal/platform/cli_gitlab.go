package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// glProjectPath URL-encodes a "group/project" path for the GitLab REST
// API, which requires the project to appear as a single encoded segment.
func glProjectPath(repo string) string {
	return url.PathEscape(repo)
}

// CLIGitLabClient implements Platform by shelling out to the `glab` CLI.
// Used as a fallback when no GITLAB_TOKEN is present but `glab` is
// installed locally (typically authenticated via `glab auth login`).
type CLIGitLabClient struct {
	execFn ExecFunc
}

// NewCLIGitLabClient returns a CLI-backed GitLab platform client that
// invokes the real `glab` binary. For tests, use NewCLIGitLabClientWithExec.
func NewCLIGitLabClient() *CLIGitLabClient {
	return &CLIGitLabClient{execFn: defaultExec}
}

// NewCLIGitLabClientWithExec returns a client that calls the provided exec
// function instead of spawning a real process. Intended for tests.
func NewCLIGitLabClientWithExec(execFn ExecFunc) *CLIGitLabClient {
	return &CLIGitLabClient{execFn: execFn}
}

func (c *CLIGitLabClient) Name() string { return "gitlab" }

func (c *CLIGitLabClient) PostPRComment(ctx context.Context, repo string, prNum int, body string) error {
	path := fmt.Sprintf("projects/%s/merge_requests/%d/notes", glProjectPath(repo), prNum)
	_, err := c.execFn(ctx, nil,
		"glab", "api", path, "-X", "POST", "-f", "body="+body)
	if err != nil {
		return fmt.Errorf("glab api: posting MR note: %w", err)
	}
	return nil
}

// PostPRReview emits the review body as a single MR note and appends each
// inline comment as an additional note, prefixed with its file/line
// location. GitLab's Discussions API requires a PositionOptions object
// with diff SHAs that the CLI cannot easily supply without an additional
// round-trip; the fallback keeps the information visible without
// attempting to map it onto diff hunks.
func (c *CLIGitLabClient) PostPRReview(ctx context.Context, repo string, prNum int, review Review) error {
	var sb strings.Builder
	sb.WriteString(review.Body)
	for _, cmt := range review.Comments {
		sb.WriteString(fmt.Sprintf("\n\n**%s:%d**: %s", cmt.Path, cmt.Line, cmt.Body))
	}
	return c.PostPRComment(ctx, repo, prNum, sb.String())
}

func (c *CLIGitLabClient) GetPRDiff(ctx context.Context, repo string, prNum int) (string, error) {
	out, err := c.execFn(ctx, nil,
		"glab", "mr", "diff", fmt.Sprintf("%d", prNum), "--repo", repo)
	if err != nil {
		return "", fmt.Errorf("glab mr diff: %w", err)
	}
	return string(out), nil
}

func (c *CLIGitLabClient) ListPRFiles(ctx context.Context, repo string, prNum int) ([]PRFile, error) {
	// The `changes` endpoint returns the MR with a "changes" array whose
	// entries carry new_path, new_file/deleted_file/renamed_file flags,
	// and the raw "diff" text.
	path := fmt.Sprintf("projects/%s/merge_requests/%d/changes", glProjectPath(repo), prNum)
	out, err := c.execFn(ctx, nil, "glab", "api", path)
	if err != nil {
		return nil, fmt.Errorf("glab api: listing MR changes: %w", err)
	}

	var resp struct {
		Changes []struct {
			NewPath     string `json:"new_path"`
			NewFile     bool   `json:"new_file"`
			DeletedFile bool   `json:"deleted_file"`
			RenamedFile bool   `json:"renamed_file"`
			Diff        string `json:"diff"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("glab api: parsing MR changes: %w", err)
	}
	files := make([]PRFile, 0, len(resp.Changes))
	for _, ch := range resp.Changes {
		status := FileStatusModified
		switch {
		case ch.NewFile:
			status = FileStatusAdded
		case ch.DeletedFile:
			status = FileStatusRemoved
		}
		files = append(files, PRFile{
			Filename: ch.NewPath,
			Status:   status,
			Patch:    ch.Diff,
		})
	}
	return files, nil
}

func (c *CLIGitLabClient) UploadSARIF(ctx context.Context, repo string, commitSHA, ref string, sarif []byte) error {
	// GitLab ingests SARIF via CI artifact, not API. No-op.
	return nil
}
