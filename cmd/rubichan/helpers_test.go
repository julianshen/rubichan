package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseToolsFlagEmpty(t *testing.T) {
	result := parseToolsFlag("")
	assert.Nil(t, result)
}

func TestParseToolsFlagWhitespace(t *testing.T) {
	result := parseToolsFlag("   ")
	assert.Nil(t, result)
}

func TestParseToolsFlagSingle(t *testing.T) {
	result := parseToolsFlag("file")
	assert.True(t, result["file"])
	assert.False(t, result["shell"])
}

func TestParseToolsFlagMultiple(t *testing.T) {
	result := parseToolsFlag("file,shell")
	assert.True(t, result["file"])
	assert.True(t, result["shell"])
}

func TestParseToolsFlagWithSpaces(t *testing.T) {
	result := parseToolsFlag(" file , shell ")
	assert.True(t, result["file"])
	assert.True(t, result["shell"])
}

func TestShouldRegisterAllAllowed(t *testing.T) {
	assert.True(t, shouldRegister("file", nil))
	assert.True(t, shouldRegister("shell", nil))
}

func TestShouldRegisterFiltered(t *testing.T) {
	allowed := map[string]bool{"file": true}
	assert.True(t, shouldRegister("file", allowed))
	assert.False(t, shouldRegister("shell", allowed))
}
