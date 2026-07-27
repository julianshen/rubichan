package diag

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveSessionLogPath_RoundTrip(t *testing.T) {
	// Save and restore since this modifies global state.
	prev := ActiveSessionLogPath()
	defer SetActiveSessionLogPath(prev)

	SetActiveSessionLogPath("/tmp/test-session.log")
	assert.Equal(t, "/tmp/test-session.log", ActiveSessionLogPath())
}

// storeMemoryAdapter

func TestCaptureAllStacks_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()
	stacks := CaptureAllStacks()
	assert.NotEmpty(t, stacks)
	assert.Contains(t, string(stacks), "goroutine")
}

// writeStackDump

func TestWriteStackDump_CreatesFile(t *testing.T) {
	t.Parallel()
	cfgDir := t.TempDir()
	path, err := WriteStackDump(cfgDir, "test-dump.log", "header: test\n\n")
	require.NoError(t, err)
	require.FileExists(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "header: test")
	assert.Contains(t, string(data), "goroutine")
}

func TestWriteStackDump_InvalidDir(t *testing.T) {
	t.Parallel()
	// Using a file path as the config dir should fail.
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(tmpFile, []byte("x"), 0o644))

	_, err := WriteStackDump(tmpFile, "dump.log", "header\n")
	assert.Error(t, err)
}

// writeDiagnosticDump

func TestWriteDiagnosticDump_CreatesFile(t *testing.T) {
	t.Parallel()
	cfgDir := t.TempDir()
	path, err := WriteDiagnosticDump(cfgDir, os.Interrupt, "/tmp/session.log")
	require.NoError(t, err)
	require.FileExists(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "signal: interrupt")
	assert.Contains(t, string(data), "session_log: /tmp/session.log")
}

// buildEventSink — additional cases

func TestBuildEventSink_DebugOnly(t *testing.T) {
	t.Parallel()
	sink := BuildEventSink(nil, true)
	require.Len(t, sink, 1) // log sink only
}

func TestBuildEventSink_DebugWithStructuredLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	logger, err := StartEventLogger(path)
	require.NoError(t, err)
	defer logger.Close()

	sink := BuildEventSink(logger, true)
	require.Len(t, sink, 2) // log sink + JSONL sink
}

// eventLogger.Close — nil safety

func TestEventLoggerClose_Nil(t *testing.T) {
	t.Parallel()
	var el *EventLogger
	assert.NoError(t, el.Close())
}

// sessionLogger.Close — nil safety

func TestSessionLoggerClose_Nil(t *testing.T) {
	t.Parallel()
	var sl *SessionLogger
	assert.NoError(t, sl.Close())
}

func TestStartEventLogger_EmptyPath(t *testing.T) {
	t.Parallel()
	logger, err := StartEventLogger("")
	assert.NoError(t, err)
	assert.Nil(t, logger)
}

func TestStartEventLogger_WhitespacePath(t *testing.T) {
	t.Parallel()
	logger, err := StartEventLogger("   ")
	assert.NoError(t, err)
	assert.Nil(t, logger)
}

// buildPipeline

func TestLogFileSuffix_ContainsPID(t *testing.T) {
	t.Parallel()
	now := time.Now()
	suffix := LogFileSuffix(now)
	assert.Contains(t, suffix, fmt.Sprintf("%d", os.Getpid()))
}

// loadConfig — basic test with default config

func TestStartSessionLoggerWritesFileAndRestoresLogger(t *testing.T) {
	origWriter := log.Writer()
	origFlags := log.Flags()
	var sentinel bytes.Buffer
	log.SetOutput(&sentinel)
	log.SetFlags(123)
	defer log.SetOutput(origWriter)
	defer log.SetFlags(origFlags)

	logger, err := StartSessionLogger(t.TempDir(), false)
	require.NoError(t, err)
	require.FileExists(t, logger.Path())

	log.Printf("captured line")

	require.NoError(t, logger.Close())

	data, err := os.ReadFile(logger.Path())
	require.NoError(t, err)
	assert.Contains(t, string(data), "rubichan session log started")
	assert.Contains(t, string(data), "captured line")
	assert.Contains(t, string(data), "rubichan session log finished")
	info, err := os.Stat(logger.Path())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Contains(t, filepath.Base(logger.Path()), strconv.Itoa(os.Getpid()))
	dirInfo, err := os.Stat(filepath.Dir(logger.Path()))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	assert.NotContains(t, sentinel.String(), "captured line")
	log.Print("restored line")
	assert.Contains(t, sentinel.String(), "restored line")
	assert.Equal(t, 123, log.Flags())
}

func TestStartSessionLoggerMirrorsToStderrInDebugMode(t *testing.T) {
	origWriter := log.Writer()
	origFlags := log.Flags()
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	os.Stderr = w
	log.SetFlags(123)
	defer log.SetOutput(origWriter)
	defer log.SetFlags(origFlags)
	defer func() { os.Stderr = origStderr }()

	logger, err := StartSessionLogger(t.TempDir(), true)
	require.NoError(t, err)

	log.Printf("debug line")

	require.NoError(t, logger.Close())
	require.NoError(t, w.Close())
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Contains(t, string(data), "debug line")
}

func TestStartEventLoggerWritesJSONLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "session.jsonl")
	logger, err := StartEventLogger(path)
	require.NoError(t, err)
	require.NotNil(t, logger)
	require.Equal(t, path, logger.path)

	_, err = logger.file.WriteString("{\"type\":\"command_result\"}\n")
	require.NoError(t, err)
	require.NoError(t, logger.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"type":"command_result"`)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestBuildEventSinkWithoutDebugAndEventLogIsNoOp(t *testing.T) {
	sink := BuildEventSink(nil, false)
	require.Len(t, sink, 0)
	assert.NotPanics(t, func() {
		sink.Emit(session.NewTurnStartedEvent("prompt", "model"))
	})
}

func TestBuildEventSinkIncludesJSONLWithoutDebug(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "session.jsonl")
	logger, err := StartEventLogger(path)
	require.NoError(t, err)
	require.NoError(t, logger.Close())

	sink := BuildEventSink(logger, false)
	require.Len(t, sink, 1)
}

func TestWritePanicDumpIncludesPanicAndSessionLog(t *testing.T) {
	cfgDir := t.TempDir()
	dumpPath, err := WritePanicDump(cfgDir, "boom", "/tmp/session.log")
	require.NoError(t, err)
	require.FileExists(t, dumpPath)

	data, err := os.ReadFile(dumpPath)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "panic: boom")
	assert.Contains(t, text, "session_log: /tmp/session.log")
	assert.Contains(t, text, "goroutine")
	info, err := os.Stat(dumpPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Contains(t, filepath.Base(dumpPath), strconv.Itoa(os.Getpid()))
}

func TestLogFileSuffixIncludesTimestampAndPID(t *testing.T) {
	now := time.Date(2026, time.March, 11, 21, 15, 30, 123456789, time.FixedZone("UTC+8", 8*3600))
	suffix := LogFileSuffix(now)
	assert.Equal(t, fmt.Sprintf("20260311-131530.123456789-%d", os.Getpid()), suffix)
}
