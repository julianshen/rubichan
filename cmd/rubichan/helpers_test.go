package main

import (
	"context"
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

func TestParseSkillsFlagEmpty(t *testing.T) {
	result := parseSkillsFlag("")
	assert.Nil(t, result)
}

func TestParseSkillsFlagWhitespace(t *testing.T) {
	result := parseSkillsFlag("   ")
	assert.Nil(t, result)
}

func TestParseSkillsFlagSingle(t *testing.T) {
	result := parseSkillsFlag("my-skill")
	assert.Equal(t, []string{"my-skill"}, result)
}

func TestParseSkillsFlagMultiple(t *testing.T) {
	result := parseSkillsFlag("skill-a,skill-b")
	assert.Equal(t, []string{"skill-a", "skill-b"}, result)
}

func TestParseSkillsFlagWithSpaces(t *testing.T) {
	result := parseSkillsFlag(" skill-a , skill-b ")
	assert.Equal(t, []string{"skill-a", "skill-b"}, result)
}

func TestCreateSkillRuntimeNilConfig(t *testing.T) {
	// When config is nil, createSkillRuntime returns an error.
	oldFlag := skillsFlag
	skillsFlag = ""
	defer func() { skillsFlag = oldFlag }()

	rt, closer, err := createSkillRuntime(context.Background(), nil, nil, nil, "interactive")
	assert.Error(t, err)
	assert.Nil(t, rt)
	assert.Nil(t, closer)
}
