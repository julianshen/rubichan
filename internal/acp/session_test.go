package acp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sessionConfig is a session setup that accepts nothing beyond text, matching
// what this agent's handshake actually declares.
func sessionConfig(prompt acp.PromptFunc) acp.SessionConfig {
	return acp.SessionConfig{
		Capabilities: acp.AgentCapabilities{},
		WorkingDir:   "/repo",
		Prompt:       prompt,
	}
}

// TestSessionNewMintsASessionID covers the method every ACP conversation opens
// with.
//
// This previously asserted that two calls yield different ids. That assertion
// is gone because a second session/new is now refused outright — see
// TestSessionNewRefusesASecondSession for why an id that lies about isolation
// is worse than no id at all.
func TestSessionNewMintsASessionID(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(nil))

	raw, err := registry.Call("session/new", json.RawMessage(`{"cwd":"/repo","mcpServers":[]}`))
	require.NoError(t, err)

	var got acp.NewSessionResult
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.NotEmpty(t, got.SessionID)
}

// TestSessionNewRequiresAnAbsoluteCwd pins the spec's constraint on cwd. A
// relative path is not merely unusual: the agent and the client resolve it
// against different working directories, so it names two different places while
// looking like agreement.
func TestSessionNewRequiresAnAbsoluteCwd(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(nil))

	for _, tc := range []struct {
		name   string
		params string
	}{
		{"relative", `{"cwd":"repo/src","mcpServers":[]}`},
		{"absent", `{"mcpServers":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := registry.Call("session/new", json.RawMessage(tc.params))
			require.Error(t, err)
			assert.ErrorIs(t, err, acp.ErrInvalidParams)
			assert.Contains(t, err.Error(), "cwd")
		})
	}
}

// TestSessionNewRefusesWorkItCannotDo covers the two session/new inputs this
// agent has no implementation for. Accepting either silently is the same defect
// as accepting an undeclared content block: the client goes on to prompt
// believing its MCP tools are connected and its extra directories are in scope,
// and nothing ever tells it otherwise.
//
// Empty is not refused — a client that always sends "mcpServers":[] is asking
// for nothing and should get a session.
func TestSessionNewRefusesWorkItCannotDo(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(nil))

	for _, tc := range []struct {
		name   string
		params string
		want   string
	}{
		{
			"mcp servers",
			`{"cwd":"/repo","mcpServers":[{"name":"fs","command":"srv"}]}`,
			"mcpServers",
		},
		{
			"additional directories",
			`{"cwd":"/repo","mcpServers":[],"additionalDirectories":["/other"]}`,
			"additionalDirectories",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := registry.Call("session/new", json.RawMessage(tc.params))
			require.Error(t, err)
			assert.ErrorIs(t, err, acp.ErrInvalidParams)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestSessionNewAcceptsEmptyOptionalLists is the other side of the refusal: the
// common client payload must still open a session.
func TestSessionNewAcceptsEmptyOptionalLists(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(nil))

	_, err := registry.Call("session/new", json.RawMessage(
		`{"cwd":"/repo","mcpServers":[],"additionalDirectories":[]}`))
	assert.NoError(t, err)
}

// TestSessionPromptRejectsUnknownSession is the routing check. An id the agent
// never minted has no conversation behind it, and answering it as though it did
// would run the turn against a fresh context the client believes is its own.
func TestSessionPromptRejectsUnknownSession(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(nil))

	_, err := registry.Call("session/prompt", json.RawMessage(
		`{"sessionId":"never-minted","prompt":[{"type":"text","text":"hi"}]}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, acp.ErrInvalidParams)
	assert.Contains(t, err.Error(), "never-minted")
}

// TestSessionPromptRunsTheTurn is the point of the slice: a prompt reaches the
// agent, with the session's cwd attached, and its stop reason reaches the
// client.
func TestSessionPromptRunsTheTurn(t *testing.T) {
	t.Parallel()

	var gotReq acp.PromptRequest
	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(
		func(_ context.Context, req acp.PromptRequest) (acp.StopReason, error) {
			gotReq = req
			return acp.StopEndTurn, nil
		}))

	raw, err := registry.Call("session/new", json.RawMessage(`{"cwd":"/repo","mcpServers":[]}`))
	require.NoError(t, err)
	var created acp.NewSessionResult
	require.NoError(t, json.Unmarshal(raw, &created))

	raw, err = registry.Call("session/prompt", json.RawMessage(
		`{"sessionId":"`+created.SessionID+`","prompt":[{"type":"text","text":"summarise this"}]}`))
	require.NoError(t, err)

	var got acp.PromptResult
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, acp.StopEndTurn, got.StopReason)

	assert.Equal(t, created.SessionID, gotReq.SessionID)
	assert.Equal(t, "/repo", gotReq.Cwd)
	require.Len(t, gotReq.Prompt, 1)
	assert.Equal(t, "summarise this", gotReq.Prompt[0].Text)
}

// TestSessionPromptRejectsUndeclaredContent wires the capability gate into the
// method that receives client content. The decoder is tested on its own; this
// pins that session/prompt actually calls it rather than decoding the array
// itself and skipping the check.
func TestSessionPromptRejectsUndeclaredContent(t *testing.T) {
	t.Parallel()

	called := false
	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(
		func(context.Context, acp.PromptRequest) (acp.StopReason, error) {
			called = true
			return acp.StopEndTurn, nil
		}))

	raw, err := registry.Call("session/new", json.RawMessage(`{"cwd":"/repo","mcpServers":[]}`))
	require.NoError(t, err)
	var created acp.NewSessionResult
	require.NoError(t, json.Unmarshal(raw, &created))

	_, err = registry.Call("session/prompt", json.RawMessage(
		`{"sessionId":"`+created.SessionID+`","prompt":[{"type":"image","data":"aGk=","mimeType":"image/png"}]}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, acp.ErrInvalidParams)
	assert.False(t, called, "an inadmissible prompt must not reach the agent at all")
}

// TestSessionPromptReportsRefusalWithoutInventingOne separates the two ways a
// turn can end badly. A handler error is the agent failing; it must not be
// dressed up as a StopReason, which would tell the client the model declined
// when in fact the agent broke.
func TestSessionPromptReportsRefusalWithoutInventingOne(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(
		func(context.Context, acp.PromptRequest) (acp.StopReason, error) {
			return "", assert.AnError
		}))

	raw, err := registry.Call("session/new", json.RawMessage(`{"cwd":"/repo","mcpServers":[]}`))
	require.NoError(t, err)
	var created acp.NewSessionResult
	require.NoError(t, json.Unmarshal(raw, &created))

	_, err = registry.Call("session/prompt", json.RawMessage(
		`{"sessionId":"`+created.SessionID+`","prompt":[{"type":"text","text":"hi"}]}`))
	require.Error(t, err)
	assert.NotErrorIs(t, err, acp.ErrInvalidParams,
		"the agent failed, not the client's payload; -32603, not -32602")
}

// TestSessionUpdateShapes pins the wire shape of the notifications a turn
// streams back. The discriminator is a field named sessionUpdate, not the
// JSON-RPC method, and a client demultiplexes on it — so a wrong or missing
// discriminator is a silently ignored update rather than a loud failure.
func TestSessionUpdateShapes(t *testing.T) {
	t.Parallel()

	t.Run("agent message chunk", func(t *testing.T) {
		t.Parallel()

		got := decodeUpdate(t, acp.NewSessionNotification("sess-1",
			acp.AgentMessageChunk("hello")))
		assert.Equal(t, "sess-1", got["sessionId"])
		update := got["update"].(map[string]any)
		assert.Equal(t, "agent_message_chunk", update["sessionUpdate"])
		content := update["content"].(map[string]any)
		assert.Equal(t, "text", content["type"])
		assert.Equal(t, "hello", content["text"])
	})

	t.Run("tool call", func(t *testing.T) {
		t.Parallel()

		got := decodeUpdate(t, acp.NewSessionNotification("sess-1",
			acp.ToolCall("call-1", "read_file")))
		update := got["update"].(map[string]any)
		assert.Equal(t, "tool_call", update["sessionUpdate"])
		assert.Equal(t, "call-1", update["toolCallId"])
		assert.Equal(t, "read_file", update["title"])
		assert.Equal(t, "pending", update["status"])
	})

	t.Run("tool call update carries a terminal status", func(t *testing.T) {
		t.Parallel()

		got := decodeUpdate(t, acp.NewSessionNotification("sess-1",
			acp.ToolCallUpdate("call-1", acp.ToolCallFailed)))
		update := got["update"].(map[string]any)
		assert.Equal(t, "tool_call_update", update["sessionUpdate"])
		assert.Equal(t, "call-1", update["toolCallId"])
		assert.Equal(t, "failed", update["status"])
	})
}

func decodeUpdate(t *testing.T, n acp.SessionNotification) map[string]any {
	t.Helper()
	raw, err := json.Marshal(n)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	return got
}

// TestSessionPromptWithNoHandlerSaysSo covers a registry wired without a prompt
// handler. Answering a stop reason would report a turn that never ran; the
// client is told the method is unserved instead.
func TestSessionPromptWithNoHandlerSaysSo(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(nil))

	raw, err := registry.Call("session/new", json.RawMessage(`{"cwd":"/repo","mcpServers":[]}`))
	require.NoError(t, err)
	var created acp.NewSessionResult
	require.NoError(t, json.Unmarshal(raw, &created))

	_, err = registry.Call("session/prompt", json.RawMessage(
		`{"sessionId":"`+created.SessionID+`","prompt":[{"type":"text","text":"hi"}]}`))
	require.Error(t, err)
	assert.NotErrorIs(t, err, acp.ErrInvalidParams,
		"the client's payload was fine; the agent is the one missing a handler")
}

// TestSessionMethodsRejectMalformedParams covers the decode failure both
// methods share. Malformed JSON is the client's to fix, so it must not surface
// as an internal error.
func TestSessionMethodsRejectMalformedParams(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, sessionConfig(nil))

	for _, method := range []string{"session/new", "session/prompt"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			_, err := registry.Call(method, json.RawMessage(`{"cwd":`))
			require.Error(t, err)
			assert.ErrorIs(t, err, acp.ErrInvalidParams)
		})
	}
}

// TestSessionNewRefusesADirectoryItCannotServe is the fix for accepting a cwd
// and then ignoring it. The agent's working directory is frozen when it is
// constructed, so a session opened for anywhere else would run every file and
// shell tool against the startup directory while the client believed otherwise.
//
// Validating that cwd is absolute and then discarding it was worse than not
// validating at all: it looked like the field had been honoured.
func TestSessionNewRefusesADirectoryItCannotServe(t *testing.T) {
	t.Parallel()

	cfg := sessionConfig(nil)
	cfg.WorkingDir = "/repo"
	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, cfg)

	_, err := registry.Call("session/new", json.RawMessage(`{"cwd":"/other-project","mcpServers":[]}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, acp.ErrInvalidParams)
	assert.Contains(t, err.Error(), "/other-project")
	assert.Contains(t, err.Error(), "/repo", "the client needs to know which directory it can have")
}

// TestSessionNewAcceptsItsOwnDirectory covers the matching case, including a
// non-canonical spelling of the same path.
func TestSessionNewAcceptsItsOwnDirectory(t *testing.T) {
	t.Parallel()

	// A fresh registry per spelling: the single-session limit would refuse the
	// second call regardless of its cwd, which would hide what is under test.
	for _, cwd := range []string{"/repo", "/repo/", "/repo/./"} {
		cfg := sessionConfig(nil)
		cfg.WorkingDir = "/repo"
		registry := acp.NewCapabilityRegistry()
		acp.RegisterSession(registry, cfg)

		_, err := registry.Call("session/new", json.RawMessage(`{"cwd":"`+cwd+`","mcpServers":[]}`))
		assert.NoError(t, err, "cwd %q names the agent's own directory", cwd)
	}
}

// TestSessionNewRefusesASecondSession pins the session-isolation limit. ACP
// says a session is an independent context with its own history; this agent
// owns exactly one conversation, so a second session id would hand the client
// a fresh-looking session that silently shares the first one's history.
//
// Refusing is the honest position until sessions get their own agent state.
func TestSessionNewRefusesASecondSession(t *testing.T) {
	t.Parallel()

	cfg := sessionConfig(nil)
	cfg.WorkingDir = "/repo"
	registry := acp.NewCapabilityRegistry()
	acp.RegisterSession(registry, cfg)

	_, err := registry.Call("session/new", json.RawMessage(`{"cwd":"/repo","mcpServers":[]}`))
	require.NoError(t, err)

	_, err = registry.Call("session/new", json.RawMessage(`{"cwd":"/repo","mcpServers":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one session",
		"the client must learn the limit, not receive an id that lies about isolation")
}
