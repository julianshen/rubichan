//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteDiagnosticDumpIncludesSignalAndSessionLog(t *testing.T) {
	cfgDir := t.TempDir()
	dumpPath, err := writeDiagnosticDump(cfgDir, syscall.SIGQUIT, "/tmp/session.log")
	require.NoError(t, err)
	require.FileExists(t, dumpPath)

	data, err := os.ReadFile(dumpPath)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "signal: quit")
	assert.Contains(t, text, "session_log: /tmp/session.log")
	assert.Contains(t, text, "goroutine")
	info, err := os.Stat(dumpPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Contains(t, filepath.Base(dumpPath), strconv.Itoa(os.Getpid()))
}
