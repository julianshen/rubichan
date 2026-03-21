package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunReplayBuildsTranscriptFromEventLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	err := os.WriteFile(path, []byte(
		`{"type":"turn_started","actor":{"name":"primary","kind":"agent"},"turn":{"prompt":"build api","model":"gpt-test"}}`+"\n"+
			`{"type":"assistant_final","actor":{"name":"primary","kind":"agent"},"assistant":{"content":"done"}}`+"\n"+
			`{"type":"subagent_done","actor":{"name":"explorer","kind":"subagent"},"subagent":{"summary":"found files"}}`+"\n",
	), 0o600)
	require.NoError(t, err)

	var out bytes.Buffer
	require.NoError(t, runReplay(context.Background(), path, "text", false, false, true, false, &out))
	text := out.String()
	assert.Contains(t, text, "User (primary): build api")
	assert.Contains(t, text, "Assistant (primary): done")
	assert.Contains(t, text, "Subagent (explorer) done: found files")
}

func TestRunReplaySummaryOnlyText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	err := os.WriteFile(path, []byte(
		`{"type":"turn_started","actor":{"name":"primary","kind":"agent"},"turn":{"prompt":"build api","model":"gpt-test"}}`+"\n"+
			`{"type":"verification_snapshot","verification":{"verdict":"passed","reason":"ok"}}`+"\n",
	), 0o600)
	require.NoError(t, err)

	var out bytes.Buffer
	require.NoError(t, runReplay(context.Background(), path, "text", true, false, true, false, &out))
	text := out.String()
	assert.Contains(t, text, "Events: 2")
	assert.Contains(t, text, "Last gate: pass")
	assert.Contains(t, text, "Last verification: passed (ok)")
}

func TestRunReplaySummaryOnlyTextDerivesVerificationFromToolEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	err := os.WriteFile(path, []byte(
		`{"type":"turn_started","actor":{"name":"primary","kind":"agent"},"turn":{"prompt":"Verify this Python backend SQLite todo API by installing deps, starting server, calling /todos and /stats, and checking the database.","model":"gpt-test"}}`+"\n"+
			`{"type":"tool_call","actor":{"name":"primary","kind":"agent"},"tool_call":{"id":"1","name":"shell","input":{"command":"python3 -m pip install -r requirements.txt"}}}`+"\n"+
			`{"type":"tool_result","actor":{"name":"primary","kind":"agent"},"tool_result":{"id":"1","name":"shell","content":"Successfully installed requirements","display_content":"Successfully installed requirements"}}`+"\n"+
			`{"type":"tool_call","actor":{"name":"primary","kind":"agent"},"tool_call":{"id":"2","name":"shell","input":{"command":"python3 init_schema.py"}}}`+"\n"+
			`{"type":"tool_result","actor":{"name":"primary","kind":"agent"},"tool_result":{"id":"2","name":"shell","content":"Database initialized","display_content":"Database initialized"}}`+"\n"+
			`{"type":"tool_call","actor":{"name":"primary","kind":"agent"},"tool_call":{"id":"3","name":"process","input":{"command":"python3 -m uvicorn main:app --host 127.0.0.1 --port 8010"}}}`+"\n"+
			`{"type":"tool_result","actor":{"name":"primary","kind":"agent"},"tool_result":{"id":"3","name":"process","content":"status: running","display_content":"status: running"}}`+"\n"+
			`{"type":"tool_call","actor":{"name":"primary","kind":"agent"},"tool_call":{"id":"4","name":"shell","input":{"command":"curl -i http://127.0.0.1:8010/stats"}}}`+"\n"+
			`{"type":"tool_result","actor":{"name":"primary","kind":"agent"},"tool_result":{"id":"4","name":"shell","content":"HTTP/1.1 200 OK\n{\"total\":3,\"completed\":2,\"active\":1}","display_content":"HTTP/1.1 200 OK\n{\"total\":3,\"completed\":2,\"active\":1}"}}`+"\n",
	), 0o600)
	require.NoError(t, err)

	var out bytes.Buffer
	require.NoError(t, runReplay(context.Background(), path, "text", true, false, true, false, &out))
	text := out.String()
	assert.Contains(t, text, "Events: 9")
	assert.Contains(t, text, "Last gate: pass")
	assert.Contains(t, text, "Last verification: passed (dependency resolution, schema/init, runtime, and API round-trip observed)")
}

func TestRunReplayJSONSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	err := os.WriteFile(path, []byte(
		`{"type":"turn_started","actor":{"name":"primary","kind":"agent"},"turn":{"prompt":"build api","model":"gpt-test"}}`+"\n"+
			`{"type":"assistant_final","actor":{"name":"primary","kind":"agent"},"assistant":{"content":"done"}}`+"\n",
	), 0o600)
	require.NoError(t, err)

	var out bytes.Buffer
	require.NoError(t, runReplay(context.Background(), path, "json", true, false, true, false, &out))
	assert.Contains(t, out.String(), `"event_count": 2`)
	assert.Contains(t, out.String(), `"last_assistant_final": "done"`)
}

func TestRunReplayFollowText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runReplay(ctx, path, "text", false, true, true, false, &out)
	}()

	time.Sleep(50 * time.Millisecond) // let file watcher initialize
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"assistant_final","actor":{"name":"primary","kind":"agent"},"assistant":{"content":"done"}}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "Assistant (primary): done")
	}, 5*time.Second, 50*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestRunReplayFollowSinceBeginningFalseSkipsExistingEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(
		`{"type":"assistant_final","actor":{"name":"primary","kind":"agent"},"assistant":{"content":"old"}}`+"\n",
	), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runReplay(ctx, path, "text", false, true, false, false, &out)
	}()

	time.Sleep(50 * time.Millisecond) // let file watcher initialize
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"assistant_final","actor":{"name":"primary","kind":"agent"},"assistant":{"content":"new"}}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "Assistant (primary): new")
	}, 5*time.Second, 50*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.NotContains(t, out.String(), "old")
}

func TestRunReplayFollowClearOnUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runReplay(ctx, path, "text", true, true, true, true, &out)
	}()

	time.Sleep(50 * time.Millisecond) // let file watcher initialize
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"verification_snapshot","verification":{"verdict":"passed","reason":"ok"}}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "Last verification: passed (ok)")
	}, 5*time.Second, 50*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Contains(t, out.String(), "\x1b[H\x1b[2J")
}
