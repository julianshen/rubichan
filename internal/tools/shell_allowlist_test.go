package tools

import (
	"sync"
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
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			al.Allow("command-a")
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			al.IsAllowed("command-a")
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			al.AllowPattern("pattern-")
		}
	}()

	wg.Wait()
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

func TestCommandAllowlistPatternNotExploitableViaSeparators(t *testing.T) {
	al := NewCommandAllowlist()
	al.AllowPattern("git status")

	// "git status; rm -rf /" should NOT match the "git status" pattern
	assert.False(t, al.IsAllowed("git status; rm -rf /"))
	assert.False(t, al.IsAllowed("git status&& rm -rf /"))
}

func TestCommandAllowlistDeduplicatesPatterns(t *testing.T) {
	al := NewCommandAllowlist()
	al.AllowPattern("go test")
	al.AllowPattern("go test")
	al.AllowPattern("go test")

	al.mu.RLock()
	count := len(al.patterns)
	al.mu.RUnlock()

	assert.Equal(t, 1, count, "duplicate patterns should be deduplicated")
}
