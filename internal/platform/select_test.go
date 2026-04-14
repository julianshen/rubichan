package platform

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withCLIAvailable temporarily overrides the package-level cliAvailable
// hook so selection tests don't depend on whether gh/glab are installed.
// The override is reverted when the test completes.
func withCLIAvailable(t *testing.T, present map[string]bool) {
	t.Helper()
	orig := cliAvailable
	cliAvailable = func(name string) bool {
		p, ok := present[name]
		return ok && p
	}
	t.Cleanup(func() { cliAvailable = orig })
}

func TestCLIClientDetection_TokenAvailable(t *testing.T) {
	// Even if the CLI is installed, an explicit token must prefer the SDK.
	withCLIAvailable(t, map[string]bool{"gh": true})

	env := &DetectedEnv{PlatformName: "github", Token: "ghp_test"}
	p, err := New(env)
	require.NoError(t, err)
	require.NotNil(t, p)

	_, ok := p.(*GitHubClient)
	assert.True(t, ok, "SDK client must be preferred when token is present; got %T", p)
}

func TestCLIClientDetection_CLIAvailable_GitHub(t *testing.T) {
	withCLIAvailable(t, map[string]bool{"gh": true})

	env := &DetectedEnv{PlatformName: "github", Token: ""}
	p, err := New(env)
	require.NoError(t, err)
	require.NotNil(t, p)

	_, ok := p.(*CLIGitHubClient)
	assert.True(t, ok, "CLI client must be selected when no token and gh is installed; got %T", p)
}

func TestCLIClientDetection_CLIAvailable_GitLab(t *testing.T) {
	withCLIAvailable(t, map[string]bool{"glab": true})

	env := &DetectedEnv{PlatformName: "gitlab", Token: ""}
	p, err := New(env)
	require.NoError(t, err)
	require.NotNil(t, p)

	_, ok := p.(*CLIGitLabClient)
	assert.True(t, ok, "CLI client must be selected when no token and glab is installed; got %T", p)
}

func TestCLIClientDetection_NeitherAvailable_GitHub(t *testing.T) {
	withCLIAvailable(t, map[string]bool{"gh": false})

	env := &DetectedEnv{PlatformName: "github", Token: ""}
	_, err := New(env)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "GITHUB_TOKEN",
		"error must mention the missing token")
	assert.Contains(t, msg, "gh",
		"error must mention the fallback CLI by name")
	assert.True(t, strings.Contains(msg, "install") || strings.Contains(msg, "cli.github.com"),
		"error must direct user to install instructions; got: %q", msg)
}

func TestCLIClientDetection_NeitherAvailable_GitLab(t *testing.T) {
	withCLIAvailable(t, map[string]bool{"glab": false})

	env := &DetectedEnv{PlatformName: "gitlab", Token: ""}
	_, err := New(env)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "GITLAB_TOKEN")
	assert.Contains(t, msg, "glab")
}
