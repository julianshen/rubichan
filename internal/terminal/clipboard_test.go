package terminal

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyToClipboard_BasicContent(t *testing.T) {
	var buf strings.Builder
	CopyToClipboard(&buf, "hello")
	encoded := base64.StdEncoding.EncodeToString([]byte("hello"))
	assert.Equal(t, "\x1b]52;c;"+encoded+"\x1b\\", buf.String())
}

func TestCopyToClipboard_EmptyContent(t *testing.T) {
	var buf strings.Builder
	CopyToClipboard(&buf, "")
	encoded := base64.StdEncoding.EncodeToString([]byte(""))
	assert.Equal(t, "\x1b]52;c;"+encoded+"\x1b\\", buf.String())
}
