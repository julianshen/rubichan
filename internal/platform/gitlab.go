package platform

import (
	"context"
	"fmt"
	"strings"

	gitlab "github.com/xanzy/go-gitlab"
)

// GitLabClient implements Platform using the go-gitlab SDK.
type GitLabClient struct {
	client *gitlab.Client
}

// NewGitLabClient creates a GitLab platform client authenticated with the given token.
func NewGitLabClient(token string) (*GitLabClient, error) {
	c, err := gitlab.NewClient(token)
	if err != nil {
		return nil, fmt.Errorf("gitlab: creating client: %w", err)
	}
	return &GitLabClient{client: c}, nil
}

// NewGitLabClientWithURL creates a GitLab client pointing to a self-hosted instance.
func NewGitLabClientWithURL(token, baseURL string) (*GitLabClient, error) {
	c, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("gitlab: configuring base URL: %w", err)
	}
	return &GitLabClient{client: c}, nil
}

func (g *GitLabClient) Name() string { return "gitlab" }

func (g *GitLabClient) PostPRComment(ctx context.Context, repo string, prNum int, body string) error {
	opts := &gitlab.CreateMergeRequestNoteOptions{Body: gitlab.Ptr(body)}
	_, _, err := g.client.Notes.CreateMergeRequestNote(repo, prNum, opts, gitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("gitlab: posting MR comment: %w", err)
	}
	return nil
}

func (g *GitLabClient) PostPRReview(ctx context.Context, repo string, prNum int, review Review) error {
	// GitLab doesn't have a native "review" concept.
	// Post summary as MR note + one discussion per inline comment.

	if review.Body != "" {
		if err := g.PostPRComment(ctx, repo, prNum, review.Body); err != nil {
			return fmt.Errorf("posting review summary: %w", err)
		}
	}

	if len(review.Comments) == 0 {
		return nil
	}

	// Fetch diff version SHAs required for inline discussion positions.
	var baseSHA, headSHA, startSHA string
	versions, _, err := g.client.MergeRequests.GetMergeRequestDiffVersions(repo, prNum, nil, gitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("gitlab: fetching MR diff versions: %w", err)
	}
	if len(versions) > 0 {
		baseSHA = versions[0].BaseCommitSHA
		headSHA = versions[0].HeadCommitSHA
		startSHA = versions[0].StartCommitSHA
	}

	for _, c := range review.Comments {
		if err := g.postDiscussion(ctx, repo, prNum, c, baseSHA, headSHA, startSHA); err != nil {
			return fmt.Errorf("posting discussion on %s:%d: %w", c.Path, c.Line, err)
		}
	}
	return nil
}

func (g *GitLabClient) postDiscussion(ctx context.Context, repo string, prNum int, c ReviewComment, baseSHA, headSHA, startSHA string) error {
	opts := &gitlab.CreateMergeRequestDiscussionOptions{
		Body: gitlab.Ptr(c.Body),
		Position: &gitlab.PositionOptions{
			BaseSHA:      gitlab.Ptr(baseSHA),
			HeadSHA:      gitlab.Ptr(headSHA),
			StartSHA:     gitlab.Ptr(startSHA),
			PositionType: gitlab.Ptr("text"),
			NewPath:      gitlab.Ptr(c.Path),
			NewLine:      gitlab.Ptr(c.Line),
		},
	}
	_, _, err := g.client.Discussions.CreateMergeRequestDiscussion(repo, prNum, opts, gitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("gitlab: creating discussion: %w", err)
	}
	return nil
}

func (g *GitLabClient) GetPRDiff(ctx context.Context, repo string, prNum int) (string, error) {
	versions, _, err := g.client.MergeRequests.GetMergeRequestDiffVersions(repo, prNum, nil, gitlab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("gitlab: getting MR diff versions: %w", err)
	}
	if len(versions) == 0 {
		return "", nil
	}

	// Use the latest diff version.
	latest := versions[0]
	version, _, err := g.client.MergeRequests.GetSingleMergeRequestDiffVersion(repo, prNum, latest.ID, nil, gitlab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("gitlab: getting MR diff: %w", err)
	}

	var b strings.Builder
	for _, d := range version.Diffs {
		fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n%s\n", d.OldPath, d.NewPath, d.Diff)
	}
	return b.String(), nil
}

func (g *GitLabClient) ListPRFiles(ctx context.Context, repo string, prNum int) ([]PRFile, error) {
	changes, _, err := g.client.MergeRequests.GetMergeRequestChanges(repo, prNum, nil, gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab: listing MR files: %w", err)
	}

	files := make([]PRFile, len(changes.Changes))
	for i, c := range changes.Changes {
		status := "modified"
		if c.NewFile {
			status = "added"
		} else if c.DeletedFile {
			status = "removed"
		}
		files[i] = PRFile{
			Filename: c.NewPath,
			Status:   status,
			Patch:    c.Diff,
		}
	}
	return files, nil
}

func (g *GitLabClient) UploadSARIF(_ context.Context, _ string, _, _ string, _ []byte) error {
	return fmt.Errorf("gitlab: SARIF upload is not supported via API; configure SARIF output as a CI artifact instead")
}
