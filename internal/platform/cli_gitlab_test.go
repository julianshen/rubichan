package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIGitLabClientImplementsPlatform(t *testing.T) {
	var _ Platform = (*CLIGitLabClient)(nil)
}

func TestCLIGitLabPostMRComment(t *testing.T) {
	var calls []recordedCall
	c := NewCLIGitLabClientWithExec(fakeExec([]byte(`{"id":7}`), nil, &calls))

	body := "@file-read-guard\nlooks good"
	err := c.PostPRComment(context.Background(), "grp/proj", 17, body)
	require.NoError(t, err)

	require.Len(t, calls, 1)
	assert.Equal(t, "glab", calls[0].Name)

	joined := strings.Join(calls[0].Args, " ")
	assert.Contains(t, joined, "api")
	// The GitLab REST API requires the project path to be URL-encoded.
	// group/proj → group%2Fproj.
	assert.Contains(t, joined, "projects/grp%2Fproj/merge_requests/17/notes")
	assert.Contains(t, joined, "-X")
	assert.Contains(t, joined, "POST")
	assert.Contains(t, joined, "--input")
	assert.NotContains(t, joined, "-f",
		"-f is unsafe for bodies starting with @ and can hit ARG_MAX")

	var payload struct {
		Body string `json:"body"`
	}
	require.NoError(t, json.Unmarshal(calls[0].Stdin, &payload))
	assert.Equal(t, body, payload.Body)
}

func TestCLIGitLabGetMRDiff(t *testing.T) {
	diff := "diff --git a/x b/x\n"
	var calls []recordedCall
	c := NewCLIGitLabClientWithExec(fakeExec([]byte(diff), nil, &calls))

	got, err := c.GetPRDiff(context.Background(), "grp/proj", 9)
	require.NoError(t, err)
	assert.Equal(t, diff, got)

	require.Len(t, calls, 1)
	assert.Equal(t, "glab", calls[0].Name)
	joined := strings.Join(calls[0].Args, " ")
	assert.Contains(t, joined, "mr")
	assert.Contains(t, joined, "diff")
	assert.Contains(t, joined, "9")
	assert.Contains(t, joined, "--repo")
	assert.Contains(t, joined, "grp/proj")
}

func TestCLIGitLabListMRFiles(t *testing.T) {
	body := `{
		"changes": [
			{"new_path":"a.go","new_file":false,"deleted_file":false,"renamed_file":false,"diff":"@@ -1 +1 @@\n-a\n+A"},
			{"new_path":"b.go","new_file":true,"diff":""},
			{"new_path":"c.go","deleted_file":true,"diff":""}
		]
	}`
	var calls []recordedCall
	c := NewCLIGitLabClientWithExec(fakeExec([]byte(body), nil, &calls))

	files, err := c.ListPRFiles(context.Background(), "grp/proj", 4)
	require.NoError(t, err)
	require.Len(t, files, 3)
	assert.Equal(t, "a.go", files[0].Filename)
	assert.Equal(t, FileStatusModified, files[0].Status)
	assert.Equal(t, FileStatusAdded, files[1].Status)
	assert.Equal(t, FileStatusRemoved, files[2].Status)

	require.Len(t, calls, 1)
	joined := strings.Join(calls[0].Args, " ")
	assert.Contains(t, joined, "projects/grp%2Fproj/merge_requests/4/changes")
}

// PostPRReview on GitLab collapses the review body + inline comments
// into a single MR note, so the note body travelling via stdin must
// contain the summary plus every inline fragment.
func TestCLIGitLabPostPRReviewFallsBackToSingleNote(t *testing.T) {
	var calls []recordedCall
	c := NewCLIGitLabClientWithExec(fakeExec([]byte(`{"id":42}`), nil, &calls))

	review := Review{
		Body: "Summary",
		Comments: []ReviewComment{
			{Path: "a.go", Line: 10, Body: "fix"},
			{Path: "b.go", Line: 20, Body: "rename"},
		},
	}
	require.NoError(t, c.PostPRReview(context.Background(), "grp/proj", 6, review))

	require.Len(t, calls, 1)
	joined := strings.Join(calls[0].Args, " ")
	assert.Contains(t, joined, "projects/grp%2Fproj/merge_requests/6/notes")
	assert.Contains(t, joined, "--input")

	var payload struct {
		Body string `json:"body"`
	}
	require.NoError(t, json.Unmarshal(calls[0].Stdin, &payload))
	assert.Contains(t, payload.Body, "Summary")
	assert.Contains(t, payload.Body, "a.go:10")
	assert.Contains(t, payload.Body, "b.go:20")
}

func TestCLIGitLabUploadSARIFIsNoOp(t *testing.T) {
	// GitLab ingests SARIF as a CI artifact, not via API. The CLI
	// fallback must mirror the SDK's no-op behavior so downstream
	// callers can use either interchangeably.
	var calls []recordedCall
	c := NewCLIGitLabClientWithExec(fakeExec(nil, nil, &calls))

	err := c.UploadSARIF(context.Background(), "grp/proj", "sha", "ref", []byte(`{}`))
	require.NoError(t, err)
	assert.Len(t, calls, 0, "UploadSARIF must not shell out on GitLab")
}
