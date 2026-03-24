package platform

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gitlab "github.com/xanzy/go-gitlab"
)

func newTestGitLabClient(t *testing.T, handler http.Handler) *GitLabClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c, err := gitlab.NewClient("test-token", gitlab.WithBaseURL(srv.URL+"/api/v4"))
	if err != nil {
		t.Fatal(err)
	}
	return &GitLabClient{client: c}
}

func TestGitLabPostMRComment(t *testing.T) {
	var gotPath, gotBody string
	client := newTestGitLabClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))

	err := client.PostPRComment(context.Background(), "group/repo", 7, "MR comment")
	if err != nil {
		t.Fatalf("PostPRComment() error = %v", err)
	}
	wantSuffix := "/merge_requests/7/notes"
	if !strings.HasSuffix(gotPath, wantSuffix) {
		t.Errorf("path = %q, want suffix %q", gotPath, wantSuffix)
	}
	if !strings.Contains(gotBody, "MR comment") {
		t.Errorf("body = %q, want to contain %q", gotBody, "MR comment")
	}
}

func TestGitLabPostMRComment_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("PRIVATE-TOKEN")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	c, _ := gitlab.NewClient("glpat-secret", gitlab.WithBaseURL(srv.URL+"/api/v4"))
	client := &GitLabClient{client: c}

	_ = client.PostPRComment(context.Background(), "g/r", 1, "test")
	if gotAuth != "glpat-secret" {
		t.Errorf("PRIVATE-TOKEN = %q, want %q", gotAuth, "glpat-secret")
	}
}

func TestGitLabPostMRReview(t *testing.T) {
	var gotPath string
	client := newTestGitLabClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		if strings.Contains(r.URL.Path, "discussions") {
			w.Write([]byte(`{"id": "abc123"}`))
		} else {
			w.Write([]byte(`{"id": 1}`))
		}
	}))

	review := Review{
		Body:  "Review body",
		Event: "COMMENT",
		Comments: []ReviewComment{
			{Path: "main.go", Line: 10, Body: "issue here"},
		},
	}
	err := client.PostPRReview(context.Background(), "g/r", 3, review)
	if err != nil {
		t.Fatalf("PostPRReview() error = %v", err)
	}
	// The last request should be a discussion (inline comment).
	if !strings.Contains(gotPath, "discussions") {
		t.Errorf("path = %q, want to contain 'discussions'", gotPath)
	}
}

func TestGitLabGetMRDiff(t *testing.T) {
	callCount := 0
	client := newTestGitLabClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if strings.HasSuffix(r.URL.Path, "/versions") {
			// First call: list diff versions
			w.Write([]byte(`[{"id": 1, "head_commit_sha": "abc", "base_commit_sha": "def", "start_commit_sha": "ghi"}]`))
		} else {
			// Second call: get specific version
			w.Write([]byte(`{"id": 1, "diffs": [{"diff":"@@ -1 +1 @@\n-old\n+new","old_path":"a.go","new_path":"a.go"}]}`))
		}
	}))

	diff, err := client.GetPRDiff(context.Background(), "g/r", 1)
	if err != nil {
		t.Fatalf("GetPRDiff() error = %v", err)
	}
	if diff == "" {
		t.Error("diff is empty")
	}
	if !strings.Contains(diff, "a.go") {
		t.Errorf("diff = %q, want to contain file path", diff)
	}
}

func TestGitLabListMRFiles(t *testing.T) {
	client := newTestGitLabClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"changes":[{"old_path":"a.go","new_path":"a.go","diff":"@@","new_file":false,"renamed_file":false,"deleted_file":false}]}`))
	}))

	files, err := client.ListPRFiles(context.Background(), "g/r", 1)
	if err != nil {
		t.Fatalf("ListPRFiles() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	if files[0].Filename != "a.go" {
		t.Errorf("Filename = %q, want %q", files[0].Filename, "a.go")
	}
}

func TestNewGitLabClientWithURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	client, err := NewGitLabClientWithURL("token", srv.URL+"/api/v4")
	if err != nil {
		t.Fatalf("NewGitLabClientWithURL() error = %v", err)
	}
	if client.Name() != "gitlab" {
		t.Errorf("Name() = %q, want %q", client.Name(), "gitlab")
	}
}

func TestNewGitLabClient_ReturnsError(t *testing.T) {
	// NewGitLabClient with a valid token should not error.
	client, err := NewGitLabClient("test-token")
	if err != nil {
		t.Fatalf("NewGitLabClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestGitLabUploadSARIF_ReturnsNil(t *testing.T) {
	client, cErr := NewGitLabClient("token")
	if cErr != nil {
		t.Fatalf("NewGitLabClient() error = %v", cErr)
	}
	err := client.UploadSARIF(context.Background(), "g/r", "abc123", "refs/heads/main", []byte("sarif"))
	if err != nil {
		t.Errorf("UploadSARIF() error = %v, want nil", err)
	}
}

func TestGitLabPostMRComment_HTTPError(t *testing.T) {
	client := newTestGitLabClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"403 Forbidden"}`))
	}))

	err := client.PostPRComment(context.Background(), "g/r", 1, "test")
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}
