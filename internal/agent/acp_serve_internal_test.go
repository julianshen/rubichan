package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/acp"
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

	client.send(t, 1, "session/prompt", map[string]any{
		"sessionId": "never-minted",
		"prompt":    []any{map[string]any{"type": "text", "text": "hello"}},
	})

	msg := client.read(t)
	require.NotNil(t, msg.Error)
	assert.Equal(t, acp.ErrorCodeInvalidParams, msg.Error.Code,
		"an unknown session is the client's mistake to fix")
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
	acp.RegisterInitialize(registry, acp.InitializeConfig{
		AgentInfo:    acp.AgentInfo{Name: "rubichan", Version: agentVersion},
		Capabilities: acpAgentCapabilities(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = serveACP(ctx, serverR, serverW, registry, acpAgentCapabilities(), turn)
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
