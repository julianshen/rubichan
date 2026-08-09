package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestA2ACmdHasServeSubcommand(t *testing.T) {
	cmd := a2aCmd()
	assert.Equal(t, "a2a", cmd.Use)

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "serve" {
			found = true
		}
	}
	assert.True(t, found, "a2a command must have a serve subcommand")
}

func TestA2AServeCmdFlags(t *testing.T) {
	cmd := a2aServeCmd()
	addrFlag := cmd.Flags().Lookup("addr")
	require.NotNil(t, addrFlag)
	assert.Equal(t, "127.0.0.1:9910", addrFlag.DefValue)

	noAuthFlag := cmd.Flags().Lookup("no-auth")
	require.NotNil(t, noAuthFlag)
}
