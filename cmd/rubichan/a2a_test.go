package main

import (
	"testing"

	"github.com/spf13/cobra"
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

func TestA2AServeCmdInheritsToolsFlag(t *testing.T) {
	cmd := a2aServeCmd()
	// --tools is defined as a persistent flag on rootCmd; a2aServeCmd must be
	// wired under a parent that has it for --tools to actually restrict what
	// runA2AServe registers. Confirm it isn't redefined locally (which would
	// shadow the inherited persistent flag) and is resolvable once attached
	// to a parent that has it, mirroring how rootCmd wires it in main.go.
	assert.Nil(t, cmd.Flags().Lookup("tools"), "a2a serve must not redefine --tools locally; it should inherit rootCmd's persistent flag")

	root := &cobra.Command{Use: "rubichan"}
	root.PersistentFlags().StringVar(&toolsFlag, "tools", "", "comma-separated tool whitelist (empty = all)")
	root.AddCommand(cmd)

	require.NotNil(t, cmd.InheritedFlags().Lookup("tools"), "a2a serve should inherit --tools once attached under rootCmd")
}

func TestA2AServeAddrDidNotChange_UsesConfigValue(t *testing.T) {
	// Simulates: config.toml has a2a.serve_addr set, user did not pass --addr.
	got := resolveA2AServeAddr(false, "127.0.0.1:9910", "0.0.0.0:8080")
	assert.Equal(t, "0.0.0.0:8080", got, "config.toml's serve_addr must win when --addr wasn't passed")
}

func TestA2AServeAddrChanged_FlagWins(t *testing.T) {
	// Simulates: user explicitly passed --addr, overriding config.toml.
	got := resolveA2AServeAddr(true, "127.0.0.1:1234", "0.0.0.0:8080")
	assert.Equal(t, "127.0.0.1:1234", got, "--addr must win when the user explicitly passed it")
}

func TestA2AServeAddrDidNotChange_NoConfigValue_KeepsDefault(t *testing.T) {
	// Simulates: neither --addr nor config.toml override the config
	// package's own built-in default.
	got := resolveA2AServeAddr(false, "127.0.0.1:9910", "127.0.0.1:9910")
	assert.Equal(t, "127.0.0.1:9910", got)
}
