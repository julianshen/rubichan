package acp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agentConfig() acp.InitializeConfig {
	return acp.InitializeConfig{
		AgentInfo: acp.AgentInfo{Name: "rubichan", Version: "1.0.0"},
		Capabilities: acp.AgentCapabilities{
			LoadSession: true,
			Prompt:      acp.PromptCapabilities{EmbeddedContext: true},
		},
	}
}

// TestInitializeAnswersWithTheAgreedVersion covers the handshake every ACP
// session opens with. protocolVersion is an integer, not a string: the schema
// page says otherwise, and the initialization page is explicit that it is a
// single integer naming the major version.
func TestInitializeAnswersWithTheAgreedVersion(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterInitialize(registry, agentConfig())

	raw, err := registry.Call("initialize", json.RawMessage(
		`{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true,"writeTextFile":true},"terminal":true},"clientInfo":{"name":"zed","version":"0.1.0"}}`))
	require.NoError(t, err)

	var got acp.InitializeResult
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, acp.ProtocolVersion, got.ProtocolVersion)
	assert.Equal(t, "rubichan", got.AgentInfo.Name)
	assert.True(t, got.AgentCapabilities.LoadSession)
	assert.NotNil(t, got.AuthMethods, "authMethods is required, and empty is not absent")
}

// TestInitializeDowngradesToItsLatestVersion pins the spec's negotiation rule:
// "When a client requests an unsupported protocol version, the Agent MUST
// respond with the latest version it supports." Not an error — the client
// decides whether it can live with the answer.
func TestInitializeDowngradesToItsLatestVersion(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterInitialize(registry, agentConfig())

	raw, err := registry.Call("initialize", json.RawMessage(
		`{"protocolVersion":99,"clientCapabilities":{}}`))
	require.NoError(t, err, "a version we cannot speak is not an error; it is a negotiation")

	var got acp.InitializeResult
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, acp.ProtocolVersion, got.ProtocolVersion,
		"the agent must answer with its own latest, letting the client decide")
}

// TestInitializeRecordsClientCapabilities pins that what the client says it can
// do is retained. The agent must not ask for fs/read_text_file from a client
// that never offered it — that is the mistake the deleted adapters made in the
// other direction, calling methods the peer did not serve.
func TestInitializeRecordsClientCapabilities(t *testing.T) {
	t.Parallel()

	var seen acp.ClientCapabilities
	cfg := agentConfig()
	cfg.OnInitialized = func(c acp.ClientCapabilities, _ acp.AgentInfo) { seen = c }

	registry := acp.NewCapabilityRegistry()
	acp.RegisterInitialize(registry, cfg)

	_, err := registry.Call("initialize", json.RawMessage(
		`{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true,"writeTextFile":false},"terminal":true}}`))
	require.NoError(t, err)

	assert.True(t, seen.FS.ReadTextFile)
	assert.False(t, seen.FS.WriteTextFile, "an unoffered capability must not read as available")
	assert.True(t, seen.Terminal)
}

func TestInitializeRejectsMalformedParams(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterInitialize(registry, agentConfig())

	_, err := registry.Call("initialize", json.RawMessage(`{"protocolVersion":"one"}`))
	require.Error(t, err, "protocolVersion is an integer; a string is not a negotiable version")
}

// TestInitializeOverTheWire checks the handshake through a real Conn, since
// that is how it will actually be reached.
func TestInitializeOverTheWire(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	acp.RegisterInitialize(registry, agentConfig())
	_, p := newConnPair(t, registry)

	_, err := p.toConn.Write(append(mustJSON(t, acp.Request{
		JSONRPC: "2.0", ID: int64(1), Method: "initialize",
		Params: json.RawMessage(`{"protocolVersion":1,"clientCapabilities":{}}`),
	}), '\n'))
	require.NoError(t, err)

	var resp acp.Response
	p.decode(t, &resp)
	require.Nil(t, resp.Error)
	require.NotNil(t, resp.Result)

	var got acp.InitializeResult
	require.NoError(t, json.Unmarshal(*resp.Result, &got))
	assert.Equal(t, acp.ProtocolVersion, got.ProtocolVersion)
}

var _ = context.Background
