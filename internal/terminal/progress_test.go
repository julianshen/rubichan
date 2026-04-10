package terminal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetProgress_Normal(t *testing.T) {
	var buf strings.Builder
	SetProgress(&buf, ProgressNormal, 75)
	assert.Equal(t, "\x1b]9;4;1;75\x07", buf.String())
}

func TestSetProgress_Error(t *testing.T) {
	var buf strings.Builder
	SetProgress(&buf, ProgressError, 100)
	assert.Equal(t, "\x1b]9;4;2;100\x07", buf.String())
}

func TestSetProgress_Indeterminate(t *testing.T) {
	var buf strings.Builder
	SetProgress(&buf, ProgressIndeterminate, 0)
	assert.Equal(t, "\x1b]9;4;3;0\x07", buf.String())
}

func TestSetProgress_Warning(t *testing.T) {
	var buf strings.Builder
	SetProgress(&buf, ProgressWarning, 50)
	assert.Equal(t, "\x1b]9;4;4;50\x07", buf.String())
}

func TestClearProgress(t *testing.T) {
	var buf strings.Builder
	ClearProgress(&buf)
	assert.Equal(t, "\x1b]9;4;0;0\x07", buf.String())
}

func TestSetProgress_ClampHigh(t *testing.T) {
	var buf strings.Builder
	SetProgress(&buf, ProgressNormal, 150)
	assert.Equal(t, "\x1b]9;4;1;100\x07", buf.String())
}

func TestSetProgress_ClampLow(t *testing.T) {
	var buf strings.Builder
	SetProgress(&buf, ProgressNormal, -10)
	assert.Equal(t, "\x1b]9;4;1;0\x07", buf.String())
}
