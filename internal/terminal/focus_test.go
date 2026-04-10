package terminal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnableFocusEvents(t *testing.T) {
	var buf strings.Builder
	EnableFocusEvents(&buf)
	assert.Equal(t, "\x1b[?1004h", buf.String())
}

func TestDisableFocusEvents(t *testing.T) {
	var buf strings.Builder
	DisableFocusEvents(&buf)
	assert.Equal(t, "\x1b[?1004l", buf.String())
}
