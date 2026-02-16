package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionString(t *testing.T) {
	s := versionString()
	assert.Contains(t, s, "rubichan")
	assert.Contains(t, s, version)
	assert.Contains(t, s, commit)
	assert.Contains(t, s, date)
}

func TestVersionStringDefaults(t *testing.T) {
	s := versionString()
	assert.Contains(t, s, "dev")
	assert.Contains(t, s, "none")
	assert.Contains(t, s, "unknown")
}

func TestAutoApproveDefaultsFalse(t *testing.T) {
	// autoApprove is a package-level var; verify it defaults to false
	assert.False(t, autoApprove, "auto-approve must default to false to prevent RCE")
}
