package platform

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/go-github/v68/github"
)

// GitHubClient implements Platform using the go-github SDK.
type GitHubClient struct {
	client *github.Client
}

// NewGitHubClient creates a GitHub platform client authenticated with the given token.
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		client: github.NewClient(nil).WithAuthToken(token),
	}
}

// NewGitHubClientWithURL creates a GitHub client pointing to a custom base URL
// (e.g., GitHub Enterprise). The URL should include the /api/v3/ suffix.
func NewGitHubClientWithURL(token, baseURL string) (*GitHubClient, error) {
	c := github.NewClient(nil).WithAuthToken(token)
	var err error
	c, err = c.WithEnterpriseURLs(baseURL, baseURL)
	if err != nil {
		return nil, fmt.Errorf("github: configuring enterprise URL: %w", err)
	}
	return &GitHubClient{client: c}, nil
}

func (g *GitHubClient) Name() string { return "github" }

func (g *GitHubClient) PostPRComment(ctx context.Context, repo string, prNum int, body string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	comment := &github.IssueComment{Body: github.Ptr(body)}
	_, _, err = g.client.Issues.CreateComment(ctx, owner, name, prNum, comment)
	if err != nil {
		return fmt.Errorf("github: posting PR comment: %w", err)
	}
	return nil
}

func (g *GitHubClient) PostPRReview(ctx context.Context, repo string, prNum int, review Review) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}

	comments := make([]*github.DraftReviewComment, len(review.Comments))
	for i, c := range review.Comments {
		comments[i] = &github.DraftReviewComment{
			Path: github.Ptr(c.Path),
			Line: github.Ptr(c.Line),
			Body: github.Ptr(c.Body),
			Side: github.Ptr(c.Side),
		}
	}

	ghReview := &github.PullRequestReviewRequest{
		Body:     github.Ptr(review.Body),
		Event:    github.Ptr(review.Event),
		Comments: comments,
	}
	_, _, err = g.client.PullRequests.CreateReview(ctx, owner, name, prNum, ghReview)
	if err != nil {
		return fmt.Errorf("github: posting PR review: %w", err)
	}
	return nil
}

func (g *GitHubClient) GetPRDiff(ctx context.Context, repo string, prNum int) (string, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return "", err
	}
	diff, _, err := g.client.PullRequests.GetRaw(ctx, owner, name, prNum, github.RawOptions{Type: github.Diff})
	if err != nil {
		return "", fmt.Errorf("github: getting PR diff: %w", err)
	}
	return diff, nil
}

func (g *GitHubClient) ListPRFiles(ctx context.Context, repo string, prNum int) ([]PRFile, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	var allFiles []PRFile
	opts := &github.ListOptions{PerPage: 100}
	for {
		ghFiles, resp, err := g.client.PullRequests.ListFiles(ctx, owner, name, prNum, opts)
		if err != nil {
			return nil, fmt.Errorf("github: listing PR files: %w", err)
		}
		for _, f := range ghFiles {
			allFiles = append(allFiles, PRFile{
				Filename: f.GetFilename(),
				Status:   f.GetStatus(),
				Patch:    f.GetPatch(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return allFiles, nil
}

func (g *GitHubClient) UploadSARIF(ctx context.Context, repo string, commitSHA, ref string, sarif []byte) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}

	// GitHub requires gzip + base64 encoding for SARIF uploads.
	encoded, err := gzipBase64(sarif)
	if err != nil {
		return fmt.Errorf("github: encoding SARIF: %w", err)
	}

	analysis := &github.SarifAnalysis{
		CommitSHA: github.Ptr(commitSHA),
		Ref:       github.Ptr(ref),
		Sarif:     github.Ptr(encoded),
	}
	_, _, err = g.client.CodeScanning.UploadSarif(ctx, owner, name, analysis)
	if err != nil {
		return fmt.Errorf("github: uploading SARIF: %w", err)
	}
	return nil
}

// splitRepo splits "owner/repo" into owner and repo name.
func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format %q: expected owner/repo", repo)
	}
	return parts[0], parts[1], nil
}

// gzipBase64 compresses data with gzip then base64-encodes it.
func gzipBase64(data []byte) (string, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		return "", fmt.Errorf("gzip write: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("gzip close: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
