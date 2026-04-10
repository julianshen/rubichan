package terminal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBeginSync(t *testing.T) {
	var buf strings.Builder
	BeginSync(&buf)
	assert.Equal(t, "\x1b[?2026h", buf.String())
}

func TestEndSync(t *testing.T) {
	var buf strings.Builder
	EndSync(&buf)
	assert.Equal(t, "\x1b[?2026l", buf.String())
}
