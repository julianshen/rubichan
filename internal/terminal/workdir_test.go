package terminal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetWorkingDirectory_BasicPath(t *testing.T) {
	var buf strings.Builder
	SetWorkingDirectory(&buf, "/home/user/project")
	assert.Equal(t, "\x1b]7;file:///home/user/project\x07", buf.String())
}

func TestSetWorkingDirectory_PathWithSpaces(t *testing.T) {
	var buf strings.Builder
	SetWorkingDirectory(&buf, "/home/user/my project")
	assert.Equal(t, "\x1b]7;file:///home/user/my%20project\x07", buf.String())
}
