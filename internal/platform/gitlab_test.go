package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitLabPostMRComment(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	client := NewGitLabClient("glpat-test")
	client.baseURL = srv.URL

	err := client.PostPRComment(context.Background(), "group/repo", 7, "MR comment")
	if err != nil {
		t.Fatalf("PostPRComment() error = %v", err)
	}
	// The test server decodes the URL-encoded path, so we check the raw request URL.
	// Either encoded or decoded form is acceptable.
	wantSuffix := "/merge_requests/7/notes"
	if !strings.HasSuffix(gotPath, wantSuffix) {
		t.Errorf("path = %q, want suffix %q", gotPath, wantSuffix)
	}
	if !strings.Contains(gotPath, "group") || !strings.Contains(gotPath, "repo") {
		t.Errorf("path = %q, want to contain project path", gotPath)
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

	client := NewGitLabClient("glpat-secret")
	client.baseURL = srv.URL

	_ = client.PostPRComment(context.Background(), "g/r", 1, "test")
	if gotAuth != "glpat-secret" {
		t.Errorf("PRIVATE-TOKEN = %q, want %q", gotAuth, "glpat-secret")
	}
}

func TestGitLabPostMRReview(t *testing.T) {
	var gotPath string
	var gotDiscussion map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotDiscussion)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	client := NewGitLabClient("token")
	client.baseURL = srv.URL

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
	// First call posts the summary comment.
	// The implementation posts summary + one discussion per comment.
	// We verify the last request was a discussion.
	if !strings.Contains(gotPath, "discussions") {
		t.Errorf("path = %q, want to contain 'discussions'", gotPath)
	}
}

func TestGitLabGetMRDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"diff":"@@ -1 +1 @@\n-old\n+new","old_path":"a.go","new_path":"a.go"}]`))
	}))
	defer srv.Close()

	client := NewGitLabClient("token")
	client.baseURL = srv.URL

	diff, err := client.GetPRDiff(context.Background(), "g/r", 1)
	if err != nil {
		t.Fatalf("GetPRDiff() error = %v", err)
	}
	if diff == "" {
		t.Error("diff is empty")
	}
}

func TestGitLabListMRFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"old_path":"a.go","new_path":"a.go","diff":"@@","new_file":false,"renamed_file":false,"deleted_file":false}]`))
	}))
	defer srv.Close()

	client := NewGitLabClient("token")
	client.baseURL = srv.URL

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

func TestGitLabUploadSARIF_ReturnsNil(t *testing.T) {
	// GitLab uses artifact-based SAST, not API upload.
	client := NewGitLabClient("token")
	err := client.UploadSARIF(context.Background(), "g/r", "ref", []byte("sarif"))
	if err != nil {
		t.Errorf("UploadSARIF() error = %v, want nil", err)
	}
}

func TestGitLabPostMRComment_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"403 Forbidden"}`))
	}))
	defer srv.Close()

	client := NewGitLabClient("bad")
	client.baseURL = srv.URL

	err := client.PostPRComment(context.Background(), "g/r", 1, "test")
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %q, want to contain '403'", err.Error())
	}
}
