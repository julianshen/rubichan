package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchDomainExact(t *testing.T) {
	assert.True(t, MatchDomain("github.com", []string{"github.com"}))
	assert.False(t, MatchDomain("evil.com", []string{"github.com"}))
	assert.False(t, MatchDomain("github.com.evil.com", []string{"github.com"}))
}

func TestMatchDomainWildcard(t *testing.T) {
	assert.True(t, MatchDomain("registry.npmjs.org", []string{"*.npmjs.org"}))
	assert.False(t, MatchDomain("npmjs.org", []string{"*.npmjs.org"}))
	assert.False(t, MatchDomain("evil.npmjs.org.bad.com", []string{"*.npmjs.org"}))
}

func TestMatchDomainEdgeCases(t *testing.T) {
	// Empty domain
	assert.False(t, MatchDomain("", []string{"github.com"}))

	// Nil patterns
	assert.False(t, MatchDomain("github.com", nil))

	// Empty patterns slice
	assert.False(t, MatchDomain("github.com", []string{}))

	// Case insensitive
	assert.True(t, MatchDomain("GitHub.COM", []string{"github.com"}))
	assert.True(t, MatchDomain("Registry.NPMJS.org", []string{"*.npmjs.org"}))

	// Multiple patterns — first match wins
	assert.True(t, MatchDomain("api.github.com", []string{"evil.com", "*.github.com"}))
}
