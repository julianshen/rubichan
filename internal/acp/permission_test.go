package acp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRequester answers one session/request_permission with a canned result.
type fakeRequester struct {
	result string
	err    error

	method string
	params json.RawMessage
}

func (f *fakeRequester) Request(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.method = method
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	f.params = raw
	if f.err != nil {
		return nil, f.err
	}
	return json.RawMessage(f.result), nil
}

// allowOnce and rejectOnce are the option pair a caller offers; the decoder
// resolves the client's optionId against them rather than trusting a kind the
// client echoed back.
var (
	allowOnce  = acp.PermissionOption{OptionID: "allow-once", Name: "Allow once", Kind: acp.PermissionAllowOnce}
	rejectOnce = acp.PermissionOption{OptionID: "reject-once", Name: "Reject", Kind: acp.PermissionRejectOnce}
)

func permissionParams() acp.RequestPermissionParams {
	return acp.RequestPermissionParams{
		SessionID: "sess-1",
		ToolCall:  acp.PermissionToolCall{ToolCallID: "call-1", Title: "shell"},
		Options:   []acp.PermissionOption{allowOnce, rejectOnce},
	}
}

// TestRequestToolPermissionAllowsWhenTheUserSelectsAllow is the whole point of
// the round trip: the client asked a human, the human said yes, and the tool
// runs on that answer rather than on the agent's own assumption.
func TestRequestToolPermissionAllowsWhenTheUserSelectsAllow(t *testing.T) {
	r := &fakeRequester{result: `{"outcome":{"outcome":"selected","optionId":"allow-once"}}`}

	allowed, err := acp.RequestToolPermission(context.Background(), r, permissionParams())

	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, acp.MethodSessionRequestPermission, r.method)
}

// TestRequestToolPermissionDeniesWhenTheUserSelectsReject is the answer that
// has to work for the mechanism to be worth having. Allowing on "yes" while
// quietly allowing on "no" too would be the same unattended execution with a
// dialog in front of it.
func TestRequestToolPermissionDeniesWhenTheUserSelectsReject(t *testing.T) {
	r := &fakeRequester{result: `{"outcome":{"outcome":"selected","optionId":"reject-once"}}`}

	allowed, err := acp.RequestToolPermission(context.Background(), r, permissionParams())

	require.NoError(t, err)
	assert.False(t, allowed)
}

// TestRequestToolPermissionDeniesWhenCancelled covers the outcome that carries
// no choice at all. Cancellation means nobody answered, which is not assent.
func TestRequestToolPermissionDeniesWhenCancelled(t *testing.T) {
	r := &fakeRequester{result: `{"outcome":{"outcome":"cancelled"}}`}

	allowed, err := acp.RequestToolPermission(context.Background(), r, permissionParams())

	// Not an error: the client did what the protocol allows. It is simply not
	// an approval.
	require.NoError(t, err)
	assert.False(t, allowed)
}

// TestRequestToolPermissionRefusesAnOptionItNeverOffered pins that the decision
// is resolved against the options the agent sent, not against whatever the
// client names. An id the agent never offered corresponds to no promise it
// made, so treating it as approval would let a malformed or hostile answer run
// a tool by inventing a choice.
func TestRequestToolPermissionRefusesAnOptionItNeverOffered(t *testing.T) {
	r := &fakeRequester{result: `{"outcome":{"outcome":"selected","optionId":"allow-always"}}`}

	allowed, err := acp.RequestToolPermission(context.Background(), r, permissionParams())

	require.Error(t, err)
	assert.False(t, allowed)
}

// TestRequestToolPermissionDeniesOnAnUnreadableAnswer covers the two ways an
// answer can carry no decision: unparseable JSON, and an outcome tag outside
// the protocol's two. Guessing at either would invent consent from noise.
func TestRequestToolPermissionDeniesOnAnUnreadableAnswer(t *testing.T) {
	for name, result := range map[string]string{
		"malformed":      `{"outcome":`,
		"unknown tag":    `{"outcome":{"outcome":"maybe"}}`,
		"absent outcome": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := &fakeRequester{result: result}

			allowed, err := acp.RequestToolPermission(context.Background(), r, permissionParams())

			require.Error(t, err)
			assert.False(t, allowed)
		})
	}
}

// TestRequestToolPermissionDeniesWhenTheClientCannotBeAsked covers the case
// that makes this safe to ship against clients that do not implement the
// method: the request fails, so the tool does not run. A client answering
// -32601 denies rather than surprises.
func TestRequestToolPermissionDeniesWhenTheClientCannotBeAsked(t *testing.T) {
	r := &fakeRequester{err: acp.ErrMethodNotFound}

	allowed, err := acp.RequestToolPermission(context.Background(), r, permissionParams())

	require.Error(t, err)
	assert.False(t, allowed)
}

// TestRequestToolPermissionSendsTheShapeTheSpecDefines checks the wire form
// rather than the round trip. A client demultiplexes on these field names, and
// one that does not recognise the params shows the user nothing to decide —
// which the round-trip tests above cannot detect, because the fake answers
// whatever it is asked.
func TestRequestToolPermissionSendsTheShapeTheSpecDefines(t *testing.T) {
	r := &fakeRequester{result: `{"outcome":{"outcome":"cancelled"}}`}
	params := permissionParams()
	params.ToolCall.Status = acp.ToolCallPending
	params.ToolCall.Content = []acp.ToolCallContent{acp.TextToolCallContent(`{"command":"ls"}`)}

	_, err := acp.RequestToolPermission(context.Background(), r, params)
	require.NoError(t, err)

	var sent struct {
		SessionID string `json:"sessionId"`
		ToolCall  struct {
			ToolCallID string `json:"toolCallId"`
			Title      string `json:"title"`
			Status     string `json:"status"`
			Content    []struct {
				Type    string `json:"type"`
				Content struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"content"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	require.NoError(t, json.Unmarshal(r.params, &sent))

	assert.Equal(t, "sess-1", sent.SessionID)
	assert.Equal(t, "call-1", sent.ToolCall.ToolCallID)
	assert.Equal(t, "shell", sent.ToolCall.Title)
	assert.Equal(t, "pending", sent.ToolCall.Status)
	require.Len(t, sent.ToolCall.Content, 1)
	assert.Equal(t, "content", sent.ToolCall.Content[0].Type)
	assert.Equal(t, "text", sent.ToolCall.Content[0].Content.Type)
	assert.Equal(t, `{"command":"ls"}`, sent.ToolCall.Content[0].Content.Text)
	require.Len(t, sent.Options, 2)
	assert.Equal(t, "allow-once", sent.Options[0].OptionID)
	assert.Equal(t, "Allow once", sent.Options[0].Name)
	assert.Equal(t, "allow_once", sent.Options[0].Kind)
	assert.Equal(t, "reject_once", sent.Options[1].Kind)
}

// TestRequestToolPermissionHonoursTheAlwaysKinds pins the other half of ACP's
// option vocabulary. Nothing in this repository offers an always-option yet —
// there is no store that would remember one — but the decoder is where the
// meaning of a kind lives, and an allow_always that failed closed would be a
// silent denial the moment a caller does offer it.
func TestRequestToolPermissionHonoursTheAlwaysKinds(t *testing.T) {
	for _, tc := range []struct {
		kind acp.PermissionOptionKind
		want bool
	}{
		{acp.PermissionAllowAlways, true},
		{acp.PermissionRejectAlways, false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			params := permissionParams()
			params.Options = []acp.PermissionOption{{OptionID: "chosen", Kind: tc.kind}}
			r := &fakeRequester{result: `{"outcome":{"outcome":"selected","optionId":"chosen"}}`}

			allowed, err := acp.RequestToolPermission(context.Background(), r, params)

			require.NoError(t, err)
			assert.Equal(t, tc.want, allowed)
		})
	}
}

// TestRequestToolPermissionRefusesAnOptionOfUnknownKind guards the agent's own
// mistake rather than the client's: an option built with a kind outside the
// four ACP defines was offered to a user whose choice the agent cannot
// interpret. Denying says so instead of picking a meaning.
func TestRequestToolPermissionRefusesAnOptionOfUnknownKind(t *testing.T) {
	params := permissionParams()
	params.Options = []acp.PermissionOption{{OptionID: "chosen", Kind: "probably_fine"}}
	r := &fakeRequester{result: `{"outcome":{"outcome":"selected","optionId":"chosen"}}`}

	allowed, err := acp.RequestToolPermission(context.Background(), r, params)

	require.Error(t, err)
	assert.False(t, allowed)
}
