package platform

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// DetectFromEnv auto-detects the git hosting platform from CI environment
// variables. Returns nil if no known CI environment is detected.
func DetectFromEnv() (*DetectedEnv, error) {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return detectGitHub(), nil
	}
	if os.Getenv("GITLAB_CI") == "true" {
		return detectGitLab(), nil
	}
	return nil, nil
}

func detectGitHub() *DetectedEnv {
	return &DetectedEnv{
		PlatformName: "github",
		Repo:         os.Getenv("GITHUB_REPOSITORY"),
		PRNumber:     parseGitHubPRNumber(os.Getenv("GITHUB_REF")),
		Token:        os.Getenv("GITHUB_TOKEN"),
		CommitSHA:    os.Getenv("GITHUB_SHA"),
		Ref:          os.Getenv("GITHUB_REF"),
	}
}

func detectGitLab() *DetectedEnv {
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		token = os.Getenv("CI_JOB_TOKEN")
	}
	prNum, _ := strconv.Atoi(os.Getenv("CI_MERGE_REQUEST_IID"))
	return &DetectedEnv{
		PlatformName: "gitlab",
		Repo:         os.Getenv("CI_PROJECT_PATH"),
		PRNumber:     prNum,
		Token:        token,
		CommitSHA:    os.Getenv("CI_COMMIT_SHA"),
		Ref:          os.Getenv("CI_COMMIT_REF_NAME"),
	}
}

// parseGitHubPRNumber extracts the PR number from a GITHUB_REF like
// "refs/pull/42/merge" or "refs/pull/42/head". Returns 0 if not a PR ref.
func parseGitHubPRNumber(ref string) int {
	// Format: refs/pull/<number>/merge or refs/pull/<number>/head
	if !strings.HasPrefix(ref, "refs/pull/") {
		return 0
	}
	parts := strings.Split(ref, "/")
	if len(parts) < 4 {
		return 0
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0
	}
	return n
}

// ParseRemoteURL parses a git remote URL (SSH or HTTPS) and returns
// the host and repository path (e.g., "owner/repo").
func ParseRemoteURL(remote string) (host, repo string, err error) {
	// SSH format: git@host:path.git
	if strings.HasPrefix(remote, "git@") {
		return parseSSHRemote(remote)
	}

	// HTTPS format: https://host/path.git
	u, parseErr := url.Parse(remote)
	if parseErr != nil || u.Host == "" {
		return "", "", fmt.Errorf("cannot parse remote URL: %q", remote)
	}
	host = u.Host
	repo = strings.TrimPrefix(u.Path, "/")
	repo = strings.TrimSuffix(repo, ".git")
	return host, repo, nil
}

func parseSSHRemote(remote string) (host, repo string, err error) {
	// git@host:path.git
	withoutPrefix := strings.TrimPrefix(remote, "git@")
	colonIdx := strings.Index(withoutPrefix, ":")
	if colonIdx < 0 {
		return "", "", fmt.Errorf("cannot parse SSH remote URL: %q", remote)
	}
	host = withoutPrefix[:colonIdx]
	repo = withoutPrefix[colonIdx+1:]
	repo = strings.TrimSuffix(repo, ".git")
	return host, repo, nil
}

// hostToPlatformName maps a hostname to a platform identifier.
// Returns empty string for unknown hosts.
func hostToPlatformName(host string) string {
	switch {
	case strings.Contains(host, "github"):
		return "github"
	case strings.Contains(host, "gitlab"):
		return "gitlab"
	default:
		return ""
	}
}
