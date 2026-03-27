package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPromptRenderer_BasicCWD(t *testing.T) {
	t.Parallel()
	r := NewPromptRenderer("/home/user")

	prompt := r.Render("/home/user/project", "")

	assert.Equal(t, "~/project ai$ ", prompt)
}

func TestPromptRenderer_HomeDirShortening(t *testing.T) {
	t.Parallel()
	r := NewPromptRenderer("/home/user")

	prompt := r.Render("/home/user/project/src", "")

	assert.Equal(t, "~/project/src ai$ ", prompt)
}

func TestPromptRenderer_GitBranch(t *testing.T) {
	t.Parallel()
	r := NewPromptRenderer("/home/user")

	prompt := r.Render("/home/user/project", "main")

	assert.Equal(t, "~/project (main) ai$ ", prompt)
}

func TestPromptRenderer_NoGitBranch(t *testing.T) {
	t.Parallel()
	r := NewPromptRenderer("/home/user")

	prompt := r.Render("/tmp", "")

	assert.Equal(t, "/tmp ai$ ", prompt)
}

func TestPromptRenderer_RootDirectory(t *testing.T) {
	t.Parallel()
	r := NewPromptRenderer("/home/user")

	prompt := r.Render("/", "")

	assert.Equal(t, "/ ai$ ", prompt)
}

func TestPromptRenderer_DetachedHEAD(t *testing.T) {
	t.Parallel()
	r := NewPromptRenderer("/home/user")

	prompt := r.Render("/home/user/project", "abc1234")

	assert.Equal(t, "~/project (abc1234) ai$ ", prompt)
}
