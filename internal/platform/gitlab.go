package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultGitLabAPIURL = "https://gitlab.com"

// GitLabClient implements Platform using the GitLab REST API.
type GitLabClient struct {
	token   string
	baseURL string
	client  *http.Client
}

// NewGitLabClient creates a GitLab platform client.
func NewGitLabClient(token string) *GitLabClient {
	return &GitLabClient{
		token:   token,
		baseURL: defaultGitLabAPIURL,
		client:  &http.Client{},
	}
}

func (g *GitLabClient) Name() string { return "gitlab" }

func (g *GitLabClient) PostPRComment(ctx context.Context, repo string, prNum int, body string) error {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes", encodeProject(repo), prNum)
	payload := map[string]string{"body": body}
	_, err := g.doJSON(ctx, http.MethodPost, path, payload, http.StatusCreated)
	return err
}

func (g *GitLabClient) PostPRReview(ctx context.Context, repo string, prNum int, review Review) error {
	// GitLab doesn't have a native "review" concept. We post:
	// 1. A summary MR note
	// 2. One discussion per inline comment

	if review.Body != "" {
		if err := g.PostPRComment(ctx, repo, prNum, review.Body); err != nil {
			return fmt.Errorf("posting review summary: %w", err)
		}
	}

	for _, c := range review.Comments {
		if err := g.postDiscussion(ctx, repo, prNum, c); err != nil {
			return fmt.Errorf("posting discussion on %s:%d: %w", c.Path, c.Line, err)
		}
	}
	return nil
}

func (g *GitLabClient) postDiscussion(ctx context.Context, repo string, prNum int, c ReviewComment) error {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/discussions", encodeProject(repo), prNum)

	type position struct {
		BaseSHA      string `json:"base_sha"`
		StartSHA     string `json:"start_sha"`
		HeadSHA      string `json:"head_sha"`
		PositionType string `json:"position_type"`
		NewPath      string `json:"new_path"`
		NewLine      int    `json:"new_line"`
	}

	payload := map[string]interface{}{
		"body": c.Body,
		"position": position{
			PositionType: "text",
			NewPath:      c.Path,
			NewLine:      c.Line,
		},
	}
	_, err := g.doJSON(ctx, http.MethodPost, path, payload, http.StatusCreated)
	return err
}

func (g *GitLabClient) GetPRDiff(ctx context.Context, repo string, prNum int) (string, error) {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/diffs", encodeProject(repo), prNum)
	respBody, err := g.doJSON(ctx, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		return "", err
	}

	var diffs []struct {
		Diff    string `json:"diff"`
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := json.Unmarshal(respBody, &diffs); err != nil {
		return "", fmt.Errorf("gitlab: parsing diffs: %w", err)
	}

	var b strings.Builder
	for _, d := range diffs {
		fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n%s\n", d.OldPath, d.NewPath, d.Diff)
	}
	return b.String(), nil
}

func (g *GitLabClient) ListPRFiles(ctx context.Context, repo string, prNum int) ([]PRFile, error) {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/diffs", encodeProject(repo), prNum)
	respBody, err := g.doJSON(ctx, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}

	var glDiffs []struct {
		OldPath     string `json:"old_path"`
		NewPath     string `json:"new_path"`
		Diff        string `json:"diff"`
		NewFile     bool   `json:"new_file"`
		RenamedFile bool   `json:"renamed_file"`
		DeletedFile bool   `json:"deleted_file"`
	}
	if err := json.Unmarshal(respBody, &glDiffs); err != nil {
		return nil, fmt.Errorf("gitlab: parsing diffs: %w", err)
	}

	files := make([]PRFile, len(glDiffs))
	for i, d := range glDiffs {
		status := "modified"
		if d.NewFile {
			status = "added"
		} else if d.DeletedFile {
			status = "removed"
		}
		files[i] = PRFile{
			Filename: d.NewPath,
			Status:   status,
			Patch:    d.Diff,
		}
	}
	return files, nil
}

func (g *GitLabClient) UploadSARIF(_ context.Context, _ string, _ string, _ []byte) error {
	// GitLab uses artifact-based SAST reports rather than API upload.
	// Users should output SARIF to a file and configure it as a CI artifact.
	return nil
}

// doJSON sends a JSON request and returns the response body.
func (g *GitLabClient) doJSON(ctx context.Context, method, path string, payload interface{}, expectStatus int) ([]byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("gitlab: marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	reqURL := g.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("gitlab: creating request: %w", err)
	}
	if g.token != "" {
		req.Header.Set("PRIVATE-TOKEN", g.token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab: reading response: %w", err)
	}

	if resp.StatusCode >= 400 || (expectStatus > 0 && resp.StatusCode != expectStatus) {
		return nil, fmt.Errorf("gitlab: %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// encodeProject URL-encodes the project path for GitLab API (e.g., "group/repo" → "group%2Frepo").
func encodeProject(project string) string {
	return url.PathEscape(project)
}
