package platform

import (
	"testing"
)

func TestParseRemoteURL_SSHFormat(t *testing.T) {
	host, repo, err := ParseRemoteURL("git@github.com:owner/repo.git")
	if err != nil {
		t.Fatalf("ParseRemoteURL() error = %v", err)
	}
	if host != "github.com" {
		t.Errorf("host = %q, want %q", host, "github.com")
	}
	if repo != "owner/repo" {
		t.Errorf("repo = %q, want %q", repo, "owner/repo")
	}
}

func TestParseRemoteURL_HTTPSFormat(t *testing.T) {
	host, repo, err := ParseRemoteURL("https://github.com/owner/repo.git")
	if err != nil {
		t.Fatalf("ParseRemoteURL() error = %v", err)
	}
	if host != "github.com" {
		t.Errorf("host = %q, want %q", host, "github.com")
	}
	if repo != "owner/repo" {
		t.Errorf("repo = %q, want %q", repo, "owner/repo")
	}
}

func TestParseRemoteURL_HTTPSNoGitSuffix(t *testing.T) {
	host, repo, err := ParseRemoteURL("https://gitlab.com/group/repo")
	if err != nil {
		t.Fatalf("ParseRemoteURL() error = %v", err)
	}
	if host != "gitlab.com" {
		t.Errorf("host = %q, want %q", host, "gitlab.com")
	}
	if repo != "group/repo" {
		t.Errorf("repo = %q, want %q", repo, "group/repo")
	}
}

func TestParseRemoteURL_SSHGitLab(t *testing.T) {
	host, repo, err := ParseRemoteURL("git@gitlab.com:group/subgroup/repo.git")
	if err != nil {
		t.Fatalf("ParseRemoteURL() error = %v", err)
	}
	if host != "gitlab.com" {
		t.Errorf("host = %q, want %q", host, "gitlab.com")
	}
	if repo != "group/subgroup/repo" {
		t.Errorf("repo = %q, want %q", repo, "group/subgroup/repo")
	}
}

func TestParseRemoteURL_Invalid(t *testing.T) {
	_, _, err := ParseRemoteURL("not-a-url")
	if err == nil {
		t.Fatal("ParseRemoteURL() expected error for invalid URL")
	}
}

func TestHostToPlatformName_GitHub(t *testing.T) {
	name := hostToPlatformName("github.com")
	if name != "github" {
		t.Errorf("hostToPlatformName(%q) = %q, want %q", "github.com", name, "github")
	}
}

func TestHostToPlatformName_GitLab(t *testing.T) {
	name := hostToPlatformName("gitlab.com")
	if name != "gitlab" {
		t.Errorf("hostToPlatformName(%q) = %q, want %q", "gitlab.com", name, "gitlab")
	}
}

func TestHostToPlatformName_Unknown(t *testing.T) {
	name := hostToPlatformName("bitbucket.org")
	if name != "" {
		t.Errorf("hostToPlatformName(%q) = %q, want empty", "bitbucket.org", name)
	}
}

func TestDetectFromGitHubActions(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_REF", "refs/pull/42/merge")
	t.Setenv("GITHUB_TOKEN", "ghp_test123")

	env, err := DetectFromEnv()
	if err != nil {
		t.Fatalf("DetectFromEnv() error = %v", err)
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
	if env.Token != "ghp_test123" {
		t.Errorf("Token = %q, want %q", env.Token, "ghp_test123")
	}
}

func TestDetectFromGitLabCI(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_PROJECT_PATH", "group/repo")
	t.Setenv("CI_MERGE_REQUEST_IID", "7")
	t.Setenv("GITLAB_TOKEN", "glpat-test")

	env, err := DetectFromEnv()
	if err != nil {
		t.Fatalf("DetectFromEnv() error = %v", err)
	}
	if env.PlatformName != "gitlab" {
		t.Errorf("PlatformName = %q, want %q", env.PlatformName, "gitlab")
	}
	if env.Repo != "group/repo" {
		t.Errorf("Repo = %q, want %q", env.Repo, "group/repo")
	}
	if env.PRNumber != 7 {
		t.Errorf("PRNumber = %d, want %d", env.PRNumber, 7)
	}
	if env.Token != "glpat-test" {
		t.Errorf("Token = %q, want %q", env.Token, "glpat-test")
	}
}

func TestDetectFromGitLabCI_UsesJobToken(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_PROJECT_PATH", "group/repo")
	t.Setenv("CI_JOB_TOKEN", "job-token-123")
	// No GITLAB_TOKEN set — should fall back to CI_JOB_TOKEN.

	env, err := DetectFromEnv()
	if err != nil {
		t.Fatalf("DetectFromEnv() error = %v", err)
	}
	if env.Token != "job-token-123" {
		t.Errorf("Token = %q, want %q", env.Token, "job-token-123")
	}
}

func TestDetectFromEnv_NoCIVars(t *testing.T) {
	// No CI env vars set.
	env, err := DetectFromEnv()
	if err != nil {
		t.Fatalf("DetectFromEnv() error = %v", err)
	}
	if env != nil {
		t.Errorf("expected nil env when no CI vars set, got %+v", env)
	}
}

func TestParseGitHubPRNumber(t *testing.T) {
	tests := []struct {
		ref  string
		want int
	}{
		{"refs/pull/42/merge", 42},
		{"refs/pull/1/head", 1},
		{"refs/heads/main", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseGitHubPRNumber(tt.ref)
		if got != tt.want {
			t.Errorf("parseGitHubPRNumber(%q) = %d, want %d", tt.ref, got, tt.want)
		}
	}
}
