package platform

import (
	"strings"
	"testing"
)

// Compile-time interface compliance checks.
var _ Platform = (*GitHubClient)(nil)
var _ Platform = (*GitLabClient)(nil)

func TestReviewCommentFields(t *testing.T) {
	rc := ReviewComment{
		Path: "main.go",
		Line: 42,
		Body: "potential nil pointer",
		Side: "RIGHT",
	}
	if rc.Path != "main.go" {
		t.Errorf("Path = %q, want %q", rc.Path, "main.go")
	}
	if rc.Line != 42 {
		t.Errorf("Line = %d, want %d", rc.Line, 42)
	}
	if rc.Body != "potential nil pointer" {
		t.Errorf("Body = %q, want %q", rc.Body, "potential nil pointer")
	}
	if rc.Side != "RIGHT" {
		t.Errorf("Side = %q, want %q", rc.Side, "RIGHT")
	}
}

func TestReviewFields(t *testing.T) {
	review := Review{
		Body:  "LGTM with minor issues",
		Event: "COMMENT",
		Comments: []ReviewComment{
			{Path: "a.go", Line: 1, Body: "fix this"},
		},
	}
	if review.Body != "LGTM with minor issues" {
		t.Errorf("Body = %q, want %q", review.Body, "LGTM with minor issues")
	}
	if review.Event != "COMMENT" {
		t.Errorf("Event = %q, want %q", review.Event, "COMMENT")
	}
	if len(review.Comments) != 1 {
		t.Fatalf("len(Comments) = %d, want 1", len(review.Comments))
	}
}

func TestPRFileFields(t *testing.T) {
	f := PRFile{
		Filename: "internal/foo.go",
		Status:   "modified",
		Patch:    "@@ -1,3 +1,4 @@",
	}
	if f.Filename != "internal/foo.go" {
		t.Errorf("Filename = %q, want %q", f.Filename, "internal/foo.go")
	}
	if f.Status != "modified" {
		t.Errorf("Status = %q, want %q", f.Status, "modified")
	}
}

func TestDetectedEnvFields(t *testing.T) {
	env := DetectedEnv{
		PlatformName: "github",
		Repo:         "owner/repo",
		PRNumber:     42,
		Token:        "ghp_test",
	}
	if env.PlatformName != "github" {
		t.Errorf("PlatformName = %q, want %q", env.PlatformName, "github")
	}
	if env.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want %q", env.Repo, "owner/repo")
	}
	if env.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want %d", env.PRNumber, 42)
	}
}

func TestNewReturnsGitHubForGitHubEnv(t *testing.T) {
	env := &DetectedEnv{
		PlatformName: "github",
		Token:        "ghp_test",
	}
	p, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if p.Name() != "github" {
		t.Errorf("Name() = %q, want %q", p.Name(), "github")
	}
}

func TestNewReturnsGitLabForGitLabEnv(t *testing.T) {
	env := &DetectedEnv{
		PlatformName: "gitlab",
		Token:        "glpat-test",
	}
	p, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if p.Name() != "gitlab" {
		t.Errorf("Name() = %q, want %q", p.Name(), "gitlab")
	}
}

func TestNewReturnsErrorForUnknownPlatform(t *testing.T) {
	env := &DetectedEnv{
		PlatformName: "bitbucket",
		Token:        "some-token",
	}
	_, err := New(env)
	if err == nil {
		t.Fatal("New() expected error for unknown platform")
	}
}

func TestNewReturnsErrorForNilEnv(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("New(nil) expected error")
	}
}

func TestNewReturnsErrorForEmptyToken(t *testing.T) {
	env := &DetectedEnv{
		PlatformName: "github",
		Token:        "",
	}
	_, err := New(env)
	if err == nil {
		t.Fatal("New() expected error for empty token")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("error = %q, want to mention GITHUB_TOKEN", err.Error())
	}
}
