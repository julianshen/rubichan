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

func TestNotify_SanitizesControlChars(t *testing.T) {
	var buf strings.Builder
	Notify(&buf, "bad\x07message\x1bhere")
	// BEL (\x07) and ESC (\x1b) should be stripped from the message.
	assert.Equal(t, "\x1b]9;badmessagehere\x07", buf.String())
}
