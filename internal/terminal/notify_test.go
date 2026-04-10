package terminal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNotify_BasicMessage(t *testing.T) {
	var buf strings.Builder
	Notify(&buf, "Hello, World!")
	assert.Equal(t, "\x1b]9;Hello, World!\x07", buf.String())
}

func TestNotify_EmptyMessage(t *testing.T) {
	var buf strings.Builder
	Notify(&buf, "")
	assert.Equal(t, "\x1b]9;\x07", buf.String())
}

func TestNotify_SpecialChars(t *testing.T) {
	var buf strings.Builder
	Notify(&buf, "Build failed: exit code 1 (error!)")
	assert.Equal(t, "\x1b]9;Build failed: exit code 1 (error!)\x07", buf.String())
}
