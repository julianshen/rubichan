package platform

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
)

func newTestGitHubClient(t *testing.T, handler http.Handler) *GitHubClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := github.NewClient(nil).WithAuthToken("test-token")
	c, err := c.WithEnterpriseURLs(srv.URL+"/", srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	return &GitHubClient{client: c}
}

func TestGitHubPostPRComment(t *testing.T) {
	var gotPath, gotBody string
	client := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))

	err := client.PostPRComment(context.Background(), "owner/repo", 42, "Hello PR")
	if err != nil {
		t.Fatalf("PostPRComment() error = %v", err)
	}
	if !strings.HasSuffix(gotPath, "/repos/owner/repo/issues/42/comments") {
		t.Errorf("path = %q, want suffix /repos/owner/repo/issues/42/comments", gotPath)
	}
	if !strings.Contains(gotBody, "Hello PR") {
		t.Errorf("body = %q, want to contain %q", gotBody, "Hello PR")
	}
}

func TestGitHubPostPRReview(t *testing.T) {
	var gotPath string
	var gotReview map[string]interface{}
	client := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotReview)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": 1}`))
	}))

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
	if !strings.HasSuffix(gotPath, "/repos/owner/repo/pulls/5/reviews") {
		t.Errorf("path = %q, want suffix /repos/owner/repo/pulls/5/reviews", gotPath)
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
	client := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "diff") {
			t.Errorf("Accept = %q, want diff media type", r.Header.Get("Accept"))
		}
		w.Write([]byte("diff --git a/file.go b/file.go\n"))
	}))

	diff, err := client.GetPRDiff(context.Background(), "o/r", 1)
	if err != nil {
		t.Fatalf("GetPRDiff() error = %v", err)
	}
	if !strings.HasPrefix(diff, "diff") {
		t.Errorf("diff = %q, want to start with 'diff'", diff)
	}
}

func TestGitHubListPRFiles(t *testing.T) {
	client := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"filename":"a.go","status":"modified","patch":"@@ -1 +1 @@"}]`))
	}))

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
	client := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"id": "scan-1"}`))
	}))

	err := client.UploadSARIF(context.Background(), "owner/repo", "abc123", "refs/heads/main", []byte(`{"runs":[]}`))
	if err != nil {
		t.Fatalf("UploadSARIF() error = %v", err)
	}
	if !strings.HasSuffix(gotPath, "/repos/owner/repo/code-scanning/sarifs") {
		t.Errorf("path = %q, want suffix for code-scanning/sarifs", gotPath)
	}
}

func TestGitHubUploadSARIF_EncodesGzipBase64(t *testing.T) {
	var gotSarif string
	client := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]string
		json.Unmarshal(b, &body)
		gotSarif = body["sarif"]
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"id":"1"}`))
	}))

	original := []byte(`{"$schema":"https://example.com","version":"2.1.0","runs":[]}`)
	err := client.UploadSARIF(context.Background(), "o/r", "abc", "refs/heads/main", original)
	if err != nil {
		t.Fatalf("UploadSARIF() error = %v", err)
	}

	// Verify: base64 decode, then gzip decompress, should equal original.
	compressed, err := base64.StdEncoding.DecodeString(gotSarif)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}
	gz, err := gzip.NewReader(strings.NewReader(string(compressed)))
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	decompressed, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("gzip read error: %v", err)
	}
	if string(decompressed) != string(original) {
		t.Errorf("decompressed = %q, want %q", string(decompressed), string(original))
	}
}

func TestGitHubPostPRComment_HTTPError(t *testing.T) {
	client := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))

	err := client.PostPRComment(context.Background(), "o/r", 1, "test")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestGitHubAuthFromToken(t *testing.T) {
	var gotAuth string
	client := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))

	_ = client.PostPRComment(context.Background(), "o/r", 1, "test")
	if !strings.Contains(gotAuth, "test-token") {
		t.Errorf("Authorization = %q, want to contain test-token", gotAuth)
	}
}

func TestSplitRepo(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{"owner/repo", "owner", "repo", false},
		{"org/sub-repo", "org", "sub-repo", false},
		{"invalid", "", "", true},
		{"/repo", "", "", true},
		{"owner/", "", "", true},
	}
	for _, tt := range tests {
		owner, name, err := splitRepo(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("splitRepo(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if owner != tt.wantOwner || name != tt.wantName {
			t.Errorf("splitRepo(%q) = (%q, %q), want (%q, %q)", tt.input, owner, name, tt.wantOwner, tt.wantName)
		}
	}
}

func TestNewGitHubClientWithURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	client, err := NewGitHubClientWithURL("token", srv.URL+"/")
	if err != nil {
		t.Fatalf("NewGitHubClientWithURL() error = %v", err)
	}
	if client.Name() != "github" {
		t.Errorf("Name() = %q, want %q", client.Name(), "github")
	}
	// Verify it actually works against the custom URL.
	err = client.PostPRComment(context.Background(), "o/r", 1, "test")
	if err != nil {
		t.Fatalf("PostPRComment via custom URL error = %v", err)
	}
}

func TestGitHubInvalidRepo(t *testing.T) {
	client := newTestGitHubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	err := client.PostPRComment(context.Background(), "invalid", 1, "test")
	if err == nil {
		t.Fatal("expected error for invalid repo format")
	}
}

func TestGzipBase64(t *testing.T) {
	data := []byte("hello world")
	encoded, err := gzipBase64(data)
	if err != nil {
		t.Fatalf("gzipBase64() error = %v", err)
	}
	if encoded == "" {
		t.Fatal("encoded is empty")
	}
	// Verify round-trip.
	compressed, _ := base64.StdEncoding.DecodeString(encoded)
	gz, _ := gzip.NewReader(strings.NewReader(string(compressed)))
	got, _ := io.ReadAll(gz)
	if string(got) != "hello world" {
		t.Errorf("round-trip = %q, want %q", string(got), "hello world")
	}
}
