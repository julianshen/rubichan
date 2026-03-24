package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const defaultGitHubAPIURL = "https://api.github.com"

// GitHubClient implements Platform using the GitHub REST API.
type GitHubClient struct {
	token   string
	baseURL string
	client  *http.Client
}

// NewGitHubClient creates a GitHub platform client.
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		token:   token,
		baseURL: defaultGitHubAPIURL,
		client:  &http.Client{},
	}
}

func (g *GitHubClient) Name() string { return "github" }

func (g *GitHubClient) PostPRComment(ctx context.Context, repo string, prNum int, body string) error {
	// GitHub uses the issues API for PR comments.
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", repo, prNum)
	payload := map[string]string{"body": body}
	_, err := g.doJSON(ctx, http.MethodPost, path, payload, http.StatusCreated)
	return err
}

func (g *GitHubClient) PostPRReview(ctx context.Context, repo string, prNum int, review Review) error {
	path := fmt.Sprintf("/repos/%s/pulls/%d/reviews", repo, prNum)

	type ghComment struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Body string `json:"body"`
		Side string `json:"side"`
	}
	type ghReview struct {
		Body     string      `json:"body"`
		Event    string      `json:"event"`
		Comments []ghComment `json:"comments"`
	}

	payload := ghReview{
		Body:     review.Body,
		Event:    review.Event,
		Comments: make([]ghComment, len(review.Comments)),
	}
	for i, c := range review.Comments {
		payload.Comments[i] = ghComment{
			Path: c.Path,
			Line: c.Line,
			Body: c.Body,
			Side: c.Side,
		}
	}

	_, err := g.doJSON(ctx, http.MethodPost, path, payload, http.StatusOK)
	return err
}

func (g *GitHubClient) GetPRDiff(ctx context.Context, repo string, prNum int) (string, error) {
	path := fmt.Sprintf("/repos/%s/pulls/%d", repo, prNum)
	req, err := g.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3.diff")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("github: reading response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("github: GET %s returned %d: %s", path, resp.StatusCode, string(b))
	}
	return string(b), nil
}

func (g *GitHubClient) ListPRFiles(ctx context.Context, repo string, prNum int) ([]PRFile, error) {
	path := fmt.Sprintf("/repos/%s/pulls/%d/files", repo, prNum)
	respBody, err := g.doJSON(ctx, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}

	var ghFiles []struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
		Patch    string `json:"patch"`
	}
	if err := json.Unmarshal(respBody, &ghFiles); err != nil {
		return nil, fmt.Errorf("github: parsing files response: %w", err)
	}

	files := make([]PRFile, len(ghFiles))
	for i, f := range ghFiles {
		files[i] = PRFile{
			Filename: f.Filename,
			Status:   f.Status,
			Patch:    f.Patch,
		}
	}
	return files, nil
}

func (g *GitHubClient) UploadSARIF(ctx context.Context, repo string, ref string, sarif []byte) error {
	path := fmt.Sprintf("/repos/%s/code-scanning/sarifs", repo)
	payload := map[string]string{
		"commit_sha": ref,
		"ref":        ref,
		"sarif":      base64.StdEncoding.EncodeToString(sarif),
	}
	_, err := g.doJSON(ctx, http.MethodPost, path, payload, http.StatusAccepted)
	return err
}

// doJSON sends a JSON request and returns the response body.
func (g *GitHubClient) doJSON(ctx context.Context, method, path string, payload interface{}, expectStatus int) ([]byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("github: marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := g.newRequest(ctx, method, path, bodyReader)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: reading response: %w", err)
	}

	if resp.StatusCode >= 400 || (expectStatus > 0 && resp.StatusCode != expectStatus) {
		return nil, fmt.Errorf("github: %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func (g *GitHubClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	url := g.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("github: creating request: %w", err)
	}
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	return req, nil
}
