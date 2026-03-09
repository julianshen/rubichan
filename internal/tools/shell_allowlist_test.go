package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandAllowlistAllow(t *testing.T) {
	al := NewCommandAllowlist()
	al.Allow("go test ./...")

	assert.True(t, al.IsAllowed("go test ./..."))
	assert.False(t, al.IsAllowed("go build ./..."))
}

func TestCommandAllowlistAllowPattern(t *testing.T) {
	al := NewCommandAllowlist()
	al.AllowPattern("go test")

	assert.True(t, al.IsAllowed("go test ./..."))
	assert.True(t, al.IsAllowed("go test -v -count=1"))
	assert.False(t, al.IsAllowed("go build ./..."))
}

func TestCommandAllowlistReadOnlyBypass(t *testing.T) {
	al := NewCommandAllowlist()
	// Read-only commands should be auto-allowed without explicit Allow
	assert.True(t, al.IsAllowed("ls -la"))
	assert.True(t, al.IsAllowed("git status"))
	assert.True(t, al.IsAllowed("cat file.go"))

	// Write commands should not be auto-allowed
	assert.False(t, al.IsAllowed("rm file.go"))
	assert.False(t, al.IsAllowed("git push"))
}

func TestCommandAllowlistClear(t *testing.T) {
	al := NewCommandAllowlist()
	al.Allow("rm -rf tempdir")
	assert.True(t, al.IsAllowed("rm -rf tempdir"))

	al.Clear()
	assert.False(t, al.IsAllowed("rm -rf tempdir"))
	// Read-only commands still bypass
	assert.True(t, al.IsAllowed("ls"))
}

func TestCommandAllowlistConcurrentAccess(t *testing.T) {
	al := NewCommandAllowlist()
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			al.Allow("command-a")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			al.IsAllowed("command-a")
		}
		done <- true
	}()

	<-done
	<-done
}

func TestCommandAllowlistMultiplePatterns(t *testing.T) {
	al := NewCommandAllowlist()
	al.AllowPattern("npm run")
	al.AllowPattern("yarn")

	assert.True(t, al.IsAllowed("npm run build"))
	assert.True(t, al.IsAllowed("npm run test"))
	assert.True(t, al.IsAllowed("yarn install"))
	assert.False(t, al.IsAllowed("pip install"))
}

func TestCommandAllowlistExactMatchOverPattern(t *testing.T) {
	al := NewCommandAllowlist()
	// Exact match should work even without a pattern
	al.Allow("rm -rf /tmp/test")
	assert.True(t, al.IsAllowed("rm -rf /tmp/test"))
	assert.False(t, al.IsAllowed("rm -rf /tmp/other"))
}
