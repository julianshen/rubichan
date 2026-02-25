package xcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRealPlatformChecker_IsDarwin(t *testing.T) {
	pc := NewRealPlatformChecker()
	// We can't assert a specific value since tests run on different OSes,
	// but we can verify the method returns without panic.
	_ = pc.IsDarwin()
}

func TestMockPlatformChecker_Darwin(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/Applications/Xcode.app/Contents/Developer"}
	assert.True(t, pc.IsDarwin())
	path, err := pc.XcodePath()
	assert.NoError(t, err)
	assert.Equal(t, "/Applications/Xcode.app/Contents/Developer", path)
}

func TestMockPlatformChecker_NotDarwin(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: false}
	assert.False(t, pc.IsDarwin())
	_, err := pc.XcodePath()
	assert.Error(t, err)
}
