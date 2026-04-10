package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectWithEnv_Ghostty(t *testing.T) {
	caps := DetectWithEnv("ghostty", nil)
	assert.True(t, caps.Hyperlinks)
	assert.True(t, caps.KittyGraphics)
	assert.True(t, caps.KittyKeyboard)
	assert.True(t, caps.ProgressBar)
	assert.True(t, caps.Notifications)
	assert.True(t, caps.SyncRendering)
	assert.True(t, caps.ClipboardAccess)
	assert.True(t, caps.FocusEvents)
}

func TestDetectWithEnv_Kitty(t *testing.T) {
	caps := DetectWithEnv("kitty", nil)
	assert.True(t, caps.Hyperlinks)
	assert.True(t, caps.KittyGraphics)
	assert.True(t, caps.KittyKeyboard)
	assert.False(t, caps.ProgressBar)
	assert.True(t, caps.Notifications)
	assert.True(t, caps.SyncRendering)
	assert.True(t, caps.ClipboardAccess)
	assert.True(t, caps.FocusEvents)
}

func TestDetectWithEnv_AppleTerminal(t *testing.T) {
	caps := DetectWithEnv("Apple_Terminal", nil)
	assert.True(t, caps.Hyperlinks)
	assert.False(t, caps.KittyGraphics)
	assert.False(t, caps.KittyKeyboard)
	assert.False(t, caps.ProgressBar)
	assert.False(t, caps.Notifications)
	assert.False(t, caps.SyncRendering)
	assert.False(t, caps.ClipboardAccess)
	assert.False(t, caps.FocusEvents)
}

func TestDetectWithEnv_UnknownTerminal(t *testing.T) {
	caps := DetectWithEnv("unknown-terminal", nil)
	assert.False(t, caps.Hyperlinks)
	assert.False(t, caps.KittyGraphics)
	assert.False(t, caps.ProgressBar)
}

func TestDetectWithEnv_EmptyTermProgram(t *testing.T) {
	caps := DetectWithEnv("", nil)
	assert.False(t, caps.Hyperlinks)
	assert.False(t, caps.KittyGraphics)
}
