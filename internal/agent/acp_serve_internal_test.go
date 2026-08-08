package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeACPCarriesAWholeConversation is the end-to-end this slice exists to
// make possible: a client opens a session and prompts it, the reply streams
// back as session/update notifications, and the call closes with a stop reason.
//
// It is driven over a real pipe rather than by calling handlers directly,
// because the defect this whole rebuild was started for was a set of methods
// that each worked in isolation and could not complete one operation between
// them.
func TestServeACPCarriesAWholeConversation(t *testing.T) {
	t.Parallel()

	turn := func(_ context.Context, msg string) (<-chan agentsdk.TurnEvent, error) {
		ch := make(chan agentsdk.TurnEvent, 4)
		ch <- agentsdk.TurnEvent{Type: "text_delta", Text: "saw: " + msg}
		ch <- agentsdk.TurnEvent{Type: "done", ExitReason: agentsdk.ExitCompleted}
		close(ch)
		return ch, nil
	}

	client, done := startACP(t, turn)

	// initialize
	client.send(t, 1, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
	})
	var initResult acp.InitializeResult
	client.result(t, 1, &initResult)
	assert.Equal(t, acp.ProtocolVersion, initResult.ProtocolVersion)

	// session/new
	client.send(t, 2, "session/new", map[string]any{
		"cwd":        "/repo",
		"mcpServers": []any{},
	})
	var created acp.NewSessionResult
	client.result(t, 2, &created)
	require.NotEmpty(t, created.SessionID)

	// session/prompt: updates arrive before the response to the same call.
	client.send(t, 3, "session/prompt", map[string]any{
		"sessionId": created.SessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": "hello"}},
	})

	var chunks []string
	var stop acp.PromptResult
	for {
		msg := client.read(t)
		if msg.Method == acp.MethodSessionUpdate {
			var n struct {
				SessionID string `json:"sessionId"`
				Update    struct {
					SessionUpdate string           `json:"sessionUpdate"`
					Content       acp.ContentBlock `json:"content"`
				} `json:"update"`
			}
			require.NoError(t, json.Unmarshal(msg.Params, &n))
			assert.Equal(t, created.SessionID, n.SessionID)
			assert.Equal(t, "agent_message_chunk", n.Update.SessionUpdate)
			chunks = append(chunks, n.Update.Content.Text)
			continue
		}
		require.Nil(t, msg.Error, "session/prompt failed: %v", msg.Error)
		require.NotNil(t, msg.Result)
		require.NoError(t, json.Unmarshal(*msg.Result, &stop))
		break
	}

	assert.Equal(t, []string{"saw: hello"}, chunks,
		"the reply reaches the client only through session/update")
	assert.Equal(t, acp.StopEndTurn, stop.StopReason)

	client.close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveACP did not return after the client hung up")
	}
}

// TestServeACPRefusesAPromptForAnUnopenedSession pins that the session check is
// live over the wire, not only in the handler's unit test.
func TestServeACPRefusesAPromptForAnUnopenedSession(t *testing.T) {
	t.Parallel()

	client, _ := startACP(t, nil)

	// The handshake first, or the refusal under test would be masked by the
	// ordering check rather than the session lookup.
	client.send(t, 1, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
	})
	var initResult acp.InitializeResult
	client.result(t, 1, &initResult)

	client.send(t, 2, "session/prompt", map[string]any{
		"sessionId": "never-minted",
		"prompt":    []any{map[string]any{"type": "text", "text": "hello"}},
	})

	msg := client.read(t)
	require.NotNil(t, msg.Error)
	assert.Equal(t, acp.ErrorCodeInvalidParams, msg.Error.Code,
		"an unknown session is the client's mistake to fix")
	assert.Contains(t, msg.Error.Message, "never-minted")
}

// TestServeACPRefusesASessionBeforeInitialize pins ACP's ordering rule over the
// wire. A client that skips the handshake has agreed no protocol version and
// declared no capabilities, so serving it a session would mean running turns
// under a contract neither side stated.
func TestServeACPRefusesASessionBeforeInitialize(t *testing.T) {
	t.Parallel()

	client, _ := startACP(t, nil)

	client.send(t, 1, "session/new", map[string]any{
		"cwd":        "/repo",
		"mcpServers": []any{},
	})

	msg := client.read(t)
	require.NotNil(t, msg.Error)
	assert.Equal(t, acp.ErrorCodeInvalidParams, msg.Error.Code)
	assert.Contains(t, msg.Error.Message, "initialize")
}

// acpClient is the far side of a served ACP connection.
type acpClient struct {
	w   *io.PipeWriter
	r   *bufio.Scanner
	enc *json.Encoder
}

func startACP(t *testing.T, turn turnFunc) (*acpClient, chan struct{}) {
	t.Helper()

	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()

	// The registry carries the real handshake but no agent-backed tools: this
	// test is about the session wiring, and NewACPRegistry needs a live Agent
	// to enumerate tools from.
	registry := acp.NewCapabilityRegistry()
	handshake := acp.NewHandshake()
	acp.RegisterInitialize(registry, acp.InitializeConfig{
		AgentInfo:    acp.AgentInfo{Name: "rubichan", Version: agentVersion},
		Capabilities: acpAgentCapabilities(),
		Handshake:    handshake,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = serveACP(ctx, serverR, serverW, acpServeConfig{
			Registry:     registry,
			Capabilities: acpAgentCapabilities(),
			WorkingDir:   "/repo",
			Handshake:    handshake,
			Turn:         turn,
		})
	}()
	t.Cleanup(func() {
		cancel()
		_ = clientW.Close()
		_ = serverW.Close()
	})

	return &acpClient{
		w:   clientW,
		r:   bufio.NewScanner(clientR),
		enc: json.NewEncoder(clientW),
	}, done
}

func (c *acpClient) send(t *testing.T, id int, method string, params any) {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)

	sent := make(chan error, 1)
	go func() {
		sent <- c.enc.Encode(acp.Request{
			JSONRPC: "2.0", ID: id, Method: method, Params: raw,
		})
	}()
	select {
	case err := <-sent:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the served connection never read the request")
	}
}

// inbound is either a response to one of our calls or a notification from the
// agent; the two are told apart by whether a method is present.
type inbound struct {
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params"`
	ID     any              `json:"id"`
	Result *json.RawMessage `json:"result"`
	Error  *acp.RPCError    `json:"error"`
}

func (c *acpClient) read(t *testing.T) inbound {
	t.Helper()
	type scanned struct {
		line []byte
		ok   bool
	}
	got := make(chan scanned, 1)
	go func() {
		ok := c.r.Scan()
		got <- scanned{append([]byte(nil), c.r.Bytes()...), ok}
	}()

	select {
	case s := <-got:
		require.True(t, s.ok, "the connection closed before answering")
		var msg inbound
		require.NoError(t, json.Unmarshal(s.line, &msg))
		return msg
	case <-time.After(3 * time.Second):
		t.Fatal("no message from the served connection within 3s")
		return inbound{}
	}
}

// result reads until the response to id arrives, ignoring notifications.
func (c *acpClient) result(t *testing.T, id int, v any) {
	t.Helper()
	for {
		msg := c.read(t)
		if msg.Method != "" {
			continue
		}
		require.Nil(t, msg.Error, "request %d failed: %v", id, msg.Error)
		require.NotNil(t, msg.Result)
		require.NoError(t, json.Unmarshal(*msg.Result, v))
		return
	}
}

func (c *acpClient) close() { _ = c.w.Close() }

// acpStubProvider is a provider that is never actually streamed from: the
// ServeACP test only completes a handshake, which touches no provider.
type acpStubProvider struct{}

func (acpStubProvider) Stream(context.Context, provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent)
	close(ch)
	return ch, nil
}

func (acpStubProvider) Name() string { return "acp-stub" }

// TestServeACPWiresARealAgent covers the exported entry point rather than the
// seam beneath it. It asserts only that a real Agent can be served and answer
// its handshake — the turn behaviour is covered against an injected turn, which
// does not need a live provider.
func TestServeACPWiresARealAgent(t *testing.T) {
	t.Parallel()

	a := New(
		acpStubProvider{},
		tools.NewRegistry(),
		func(context.Context, string, json.RawMessage) (bool, error) { return true, nil },
		&config.Config{
			Provider: config.ProviderConfig{Model: "test-model"},
			Agent:    config.AgentConfig{MaxTurns: 1},
		},
	)

	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = ServeACP(ctx, a, serverR, serverW)
	}()
	t.Cleanup(func() {
		cancel()
		_ = clientW.Close()
		_ = serverW.Close()
	})

	client := &acpClient{w: clientW, r: bufio.NewScanner(clientR), enc: json.NewEncoder(clientW)}
	client.send(t, 1, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
	})

	var got acp.InitializeResult
	client.result(t, 1, &got)
	assert.Equal(t, "rubichan", got.AgentInfo.Name)

	client.close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeACP did not return after the client hung up")
	}
}

// TestServeACPRefusesASecondConnectionToOneAgent closes the session-isolation
// hole at connection scope. RegisterSession's sole-session guard lives in the
// store it creates, and that store is per connection — so serving one Agent
// twice produced two "sole" sessions sharing a single conversation, which is
// the exact defect the guard was added to prevent, one level up.
//
// The claim is permanent. An earlier version released it on return, which left
// the sequential case broken: a later connection would open a fresh session and
// inherit the earlier one's history off the agent.
func TestServeACPRefusesASecondConnectionToOneAgent(t *testing.T) {
	t.Parallel()

	a := newACPTestAgent(t)

	firstR, firstW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- ServeACP(ctx, a, firstR, io.Discard) }()
	t.Cleanup(func() {
		cancel()
		_ = firstW.Close()
	})

	// Wait for the first connection to have claimed the agent, rather than
	// racing it: the claim happens on the serving goroutine.
	require.Eventually(t, a.acpServing.Load, 2*time.Second, 5*time.Millisecond,
		"the first ServeACP never claimed the agent")

	err := ServeACP(context.Background(), a, strings.NewReader(""), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already served")

	// End the first connection, then try again. This is the sequential case:
	// the second client would get a new session id backed by the first
	// client's conversation, so it must still be refused.
	cancel()
	_ = firstW.Close()
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the first ServeACP did not return")
	}

	err = ServeACP(context.Background(), a, strings.NewReader(""), io.Discard)
	require.Error(t, err,
		"a finished connection must not free the agent: its conversation carries over")
	assert.Contains(t, err.Error(), "already served")
}

// newACPTestAgent builds an agent that can be served but never streamed from.
func newACPTestAgent(t *testing.T) *Agent {
	t.Helper()
	return New(
		acpStubProvider{},
		tools.NewRegistry(),
		func(context.Context, string, json.RawMessage) (bool, error) { return true, nil },
		&config.Config{
			Provider: config.ProviderConfig{Model: "test-model"},
			Agent:    config.AgentConfig{MaxTurns: 1},
		},
	)
}
