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

func TestGitHubPostPRComment(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	client := NewGitHubClient("test-token")
	client.baseURL = srv.URL

	err := client.PostPRComment(context.Background(), "owner/repo", 42, "Hello PR")
	if err != nil {
		t.Fatalf("PostPRComment() error = %v", err)
	}
	if gotPath != "/repos/owner/repo/issues/42/comments" {
		t.Errorf("path = %q, want %q", gotPath, "/repos/owner/repo/issues/42/comments")
	}
	if !strings.Contains(gotBody, "Hello PR") {
		t.Errorf("body = %q, want to contain %q", gotBody, "Hello PR")
	}
}

func TestGitHubPostPRComment_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	client := NewGitHubClient("ghp_secret")
	client.baseURL = srv.URL

	_ = client.PostPRComment(context.Background(), "o/r", 1, "test")
	if gotAuth != "Bearer ghp_secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer ghp_secret")
	}
}

func TestGitHubPostPRReview(t *testing.T) {
	var gotPath string
	var gotReview map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotReview)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	client := NewGitHubClient("token")
	client.baseURL = srv.URL

	review := Review{
		Body:  "Looks good",
		Event: "COMMENT",
		Comments: []ReviewComment{
			{Path: "main.go", Line: 10, Body: "fix this", Side: "RIGHT"},
		},
	}
	err := client.PostPRReview(context.Background(), "owner/repo", 5, review)
	if err != nil {
		t.Fatalf("PostPRReview() error = %v", err)
	}
	if gotPath != "/repos/owner/repo/pulls/5/reviews" {
		t.Errorf("path = %q, want %q", gotPath, "/repos/owner/repo/pulls/5/reviews")
	}
	if gotReview["event"] != "COMMENT" {
		t.Errorf("event = %v, want COMMENT", gotReview["event"])
	}
	comments, ok := gotReview["comments"].([]interface{})
	if !ok || len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %v", gotReview["comments"])
	}
}

func TestGitHubGetPRDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github.v3.diff" {
			t.Errorf("Accept = %q, want diff media type", r.Header.Get("Accept"))
		}
		w.Write([]byte("diff --git a/file.go b/file.go\n"))
	}))
	defer srv.Close()

	client := NewGitHubClient("token")
	client.baseURL = srv.URL

	diff, err := client.GetPRDiff(context.Background(), "o/r", 1)
	if err != nil {
		t.Fatalf("GetPRDiff() error = %v", err)
	}
	if !strings.HasPrefix(diff, "diff") {
		t.Errorf("diff = %q, want to start with 'diff'", diff)
	}
}

func TestGitHubListPRFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"filename":"a.go","status":"modified","patch":"@@ -1 +1 @@"}]`))
	}))
	defer srv.Close()

	client := NewGitHubClient("token")
	client.baseURL = srv.URL

	files, err := client.ListPRFiles(context.Background(), "o/r", 1)
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

func TestGitHubUploadSARIF(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"id": "scan-1"}`))
	}))
	defer srv.Close()

	client := NewGitHubClient("token")
	client.baseURL = srv.URL

	err := client.UploadSARIF(context.Background(), "owner/repo", "refs/heads/main", []byte(`{"runs":[]}`))
	if err != nil {
		t.Fatalf("UploadSARIF() error = %v", err)
	}
	if gotPath != "/repos/owner/repo/code-scanning/sarifs" {
		t.Errorf("path = %q, want %q", gotPath, "/repos/owner/repo/code-scanning/sarifs")
	}
}

func TestGitHubPostPRComment_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	client := NewGitHubClient("bad-token")
	client.baseURL = srv.URL

	err := client.PostPRComment(context.Background(), "o/r", 1, "test")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %q, want to contain '401'", err.Error())
	}
}
