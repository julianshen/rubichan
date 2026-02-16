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
