package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaCmd_Structure(t *testing.T) {
	cmd := ollamaCmd()
	assert.Equal(t, "ollama", cmd.Use)

	subcommands := map[string]bool{}
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}

	assert.True(t, subcommands["list"], "should have list subcommand")
	assert.True(t, subcommands["pull"], "should have pull subcommand")
	assert.True(t, subcommands["rm"], "should have rm subcommand")
	assert.True(t, subcommands["status"], "should have status subcommand")
}

func TestOllamaCmd_BaseURLFlag(t *testing.T) {
	cmd := ollamaCmd()
	flag := cmd.PersistentFlags().Lookup("base-url")
	require.NotNil(t, flag)
	assert.Equal(t, "", flag.DefValue)
}
