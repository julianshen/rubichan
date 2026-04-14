package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIGitHubClientImplementsPlatform(t *testing.T) {
	var _ Platform = (*CLIGitHubClient)(nil)
}

// recordedCall captures the arguments of a single exec invocation so
// tests can inspect how the CLI client built the shell-out.
type recordedCall struct {
	Name  string
	Args  []string
	Stdin []byte
}

// fakeExec returns an ExecFunc that records every call it sees and
// replies with the given stdout/err pair.
func fakeExec(out []byte, err error, calls *[]recordedCall) ExecFunc {
	return func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, recordedCall{Name: name, Args: append([]string(nil), args...), Stdin: append([]byte(nil), stdin...)})
		return out, err
	}
}

func TestCLIGitHubPostPRComment(t *testing.T) {
	var calls []recordedCall
	c := NewCLIGitHubClientWithExec(fakeExec([]byte(`{"id":1}`), nil, &calls))

	err := c.PostPRComment(context.Background(), "octo/hello", 42, "looks good")
	require.NoError(t, err)

	require.Len(t, calls, 1)
	assert.Equal(t, "gh", calls[0].Name)

	// Must invoke `gh api repos/octo/hello/issues/42/comments` with
	// method POST and the body as a form field.
	joined := strings.Join(calls[0].Args, " ")
	assert.Contains(t, joined, "api")
	assert.Contains(t, joined, "repos/octo/hello/issues/42/comments")
	assert.Contains(t, joined, "-X")
	assert.Contains(t, joined, "POST")
	assert.Contains(t, joined, "-f")
	assert.Contains(t, joined, "body=looks good")
}

func TestCLIGitHubPostPRReview(t *testing.T) {
	var calls []recordedCall
	c := NewCLIGitHubClientWithExec(fakeExec([]byte(`{"id":100}`), nil, &calls))

	review := Review{
		Body:  "Found 2 issues",
		Event: EventComment,
		Comments: []ReviewComment{
			{Path: "main.go", Line: 42, Body: "nil check missing", Side: SideRight},
			{Path: "util.go", Line: 7, Body: "extract helper", Side: SideRight},
		},
	}
	err := c.PostPRReview(context.Background(), "octo/hello", 5, review)
	require.NoError(t, err)

	require.Len(t, calls, 1)
	assert.Equal(t, "gh", calls[0].Name)
	joined := strings.Join(calls[0].Args, " ")
	assert.Contains(t, joined, "api")
	assert.Contains(t, joined, "repos/octo/hello/pulls/5/reviews")
	assert.Contains(t, joined, "--input")
	assert.Contains(t, joined, "-") // stdin marker

	// The review body must arrive as JSON on stdin with fields the GitHub
	// reviews API expects: body, event, comments[].{path,line,body,side}.
	require.NotEmpty(t, calls[0].Stdin, "review JSON must be passed via stdin")
	var payload struct {
		Body     string `json:"body"`
		Event    string `json:"event"`
		Comments []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Body string `json:"body"`
			Side string `json:"side"`
		} `json:"comments"`
	}
	require.NoError(t, json.Unmarshal(calls[0].Stdin, &payload))
	assert.Equal(t, "Found 2 issues", payload.Body)
	assert.Equal(t, EventComment, payload.Event)
	require.Len(t, payload.Comments, 2)
	assert.Equal(t, "main.go", payload.Comments[0].Path)
	assert.Equal(t, 42, payload.Comments[0].Line)
	assert.Equal(t, "nil check missing", payload.Comments[0].Body)
	assert.Equal(t, SideRight, payload.Comments[0].Side)
}

func TestCLIGitHubGetPRDiff(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index abc..def 100644
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-old
+new
`
	var calls []recordedCall
	c := NewCLIGitHubClientWithExec(fakeExec([]byte(diff), nil, &calls))

	got, err := c.GetPRDiff(context.Background(), "octo/hello", 17)
	require.NoError(t, err)
	assert.Equal(t, diff, got)

	require.Len(t, calls, 1)
	assert.Equal(t, "gh", calls[0].Name)
	joined := strings.Join(calls[0].Args, " ")
	// `gh pr diff 17 --repo octo/hello` is the documented invocation.
	assert.Contains(t, joined, "pr")
	assert.Contains(t, joined, "diff")
	assert.Contains(t, joined, "17")
	assert.Contains(t, joined, "--repo")
	assert.Contains(t, joined, "octo/hello")
}

func TestCLIGitHubListPRFiles(t *testing.T) {
	jsonBody := `[
		{"filename":"a.go","status":"modified","patch":"@@ -1 +1 @@\n-a\n+A"},
		{"filename":"b.go","status":"added","patch":""}
	]`
	var calls []recordedCall
	c := NewCLIGitHubClientWithExec(fakeExec([]byte(jsonBody), nil, &calls))

	files, err := c.ListPRFiles(context.Background(), "octo/hello", 3)
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "a.go", files[0].Filename)
	assert.Equal(t, "modified", files[0].Status)
	assert.Equal(t, "b.go", files[1].Filename)
	assert.Equal(t, "added", files[1].Status)

	require.Len(t, calls, 1)
	joined := strings.Join(calls[0].Args, " ")
	assert.Contains(t, joined, "repos/octo/hello/pulls/3/files")
	assert.Contains(t, joined, "--paginate",
		"CLI client must request pagination so large PRs return fully")
}

func TestCLIGitHubUploadSARIF(t *testing.T) {
	var calls []recordedCall
	c := NewCLIGitHubClientWithExec(fakeExec([]byte(`{"id":"abc"}`), nil, &calls))

	sarif := []byte(`{"version":"2.1.0","runs":[]}`)
	err := c.UploadSARIF(context.Background(), "octo/hello", "deadbeef", "refs/heads/main", sarif)
	require.NoError(t, err)

	require.Len(t, calls, 1)
	joined := strings.Join(calls[0].Args, " ")
	assert.Contains(t, joined, "repos/octo/hello/code-scanning/sarifs")
	assert.Contains(t, joined, "--input")

	// Stdin must be a JSON object with commit_sha, ref and a
	// gzip+base64 encoded sarif body.
	var payload struct {
		CommitSHA string `json:"commit_sha"`
		Ref       string `json:"ref"`
		Sarif     string `json:"sarif"`
	}
	require.NoError(t, json.Unmarshal(calls[0].Stdin, &payload))
	assert.Equal(t, "deadbeef", payload.CommitSHA)
	assert.Equal(t, "refs/heads/main", payload.Ref)
	assert.NotEmpty(t, payload.Sarif, "SARIF body must be encoded and sent")
	assert.NotEqual(t, string(sarif), payload.Sarif,
		"SARIF body must be gzip+base64 encoded, not raw")
}

// Exec errors from the underlying CLI (nonzero exit, parse failures,
// auth failures) must propagate to the caller so the operator sees the
// actual reason instead of a generic wrap.
func TestCLIGitHubExecFailurePropagates(t *testing.T) {
	execErr := errors.New("HTTP 403: Not Found (stderr: gh: 403 Forbidden)")
	var calls []recordedCall
	c := NewCLIGitHubClientWithExec(fakeExec(nil, execErr, &calls))

	err := c.PostPRComment(context.Background(), "octo/hello", 1, "body")
	require.Error(t, err)
	assert.ErrorIs(t, err, execErr,
		"exec error must be wrapped with %%w so errors.Is finds it")
	assert.Contains(t, err.Error(), "403 Forbidden",
		"stderr context must be preserved end-to-end for operator diagnosis")
	assert.Len(t, calls, 1, "client must attempt the exec even on failure")
}
