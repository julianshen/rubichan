package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// approvalRequest is what the agent's approval flow hands a UI adapter for a
// shell call. Built here rather than inline so every test asks about the same
// tool call and the shape stays close to agentsdk.ApprovalFlow's.
func approvalRequest() agentsdk.UIRequest {
	return agentsdk.UIRequest{
		ID:      "call-1",
		Kind:    agentsdk.UIKindApproval,
		Title:   "Approve shell tool call",
		Actions: []agentsdk.UIAction{{ID: "allow"}, {ID: "deny"}},
		Metadata: map[string]string{
			"tool":  "shell",
			"input": `{"command":"rm -rf /"}`,
		},
	}
}

// TestACPPermissionsAsksTheClientAndHonoursYes is the round trip --acp existed
// without: a tool needs approval, the client asks a human, and the answer is
// what decides. Until this existed the mode had to demand --auto-approve.
func TestACPPermissionsAsksTheClientAndHonoursYes(t *testing.T) {
	t.Parallel()

	client := &fakePeer{answer: `{"outcome":{"outcome":"selected","optionId":"allow-once"}}`}
	perms := NewACPPermissions()
	perms.bind(client, "sess-1")

	resp, err := perms.Request(context.Background(), approvalRequest())

	require.NoError(t, err)
	assert.Equal(t, "call-1", resp.RequestID, "the approval flow rejects a response whose id does not match")
	assert.Equal(t, "allow", resp.ActionID)
}

// TestACPPermissionsHonoursNo is the answer that has to work for asking to mean
// anything.
func TestACPPermissionsHonoursNo(t *testing.T) {
	t.Parallel()

	client := &fakePeer{answer: `{"outcome":{"outcome":"selected","optionId":"reject-once"}}`}
	perms := NewACPPermissions()
	perms.bind(client, "sess-1")

	resp, err := perms.Request(context.Background(), approvalRequest())

	require.NoError(t, err)
	assert.Equal(t, "deny", resp.ActionID)
}

// TestACPPermissionsRefusesWhenNobodyIsConnected covers a tool call with no
// live session behind it. There is no one to ask, so there is no approval;
// answering "allow" would be the agent consenting on the user's behalf at
// exactly the moment it has lost contact with them.
func TestACPPermissionsRefusesWhenNobodyIsConnected(t *testing.T) {
	t.Parallel()

	_, err := NewACPPermissions().Request(context.Background(), approvalRequest())

	require.Error(t, err)
}

// TestACPPermissionsDeniesWhenTheClientCannotAnswer covers the client half of
// the same principle: a session is bound, but the request failed. Clients are
// not obliged to have implemented session/request_permission, and one that
// answers -32601 arrives here — as a tool that does not run, rather than as one
// that runs because asking did not work.
func TestACPPermissionsDeniesWhenTheClientCannotAnswer(t *testing.T) {
	t.Parallel()

	perms := NewACPPermissions()
	perms.bind(&fakePeer{askErr: acp.ErrMethodNotFound}, "sess-1")

	_, err := perms.Request(context.Background(), approvalRequest())

	require.Error(t, err)
}

// TestACPPermissionsShowsWhatIsBeingApproved is the difference between consent
// and a dialog. A client that renders only "Approve shell tool call" asks the
// user to authorise a command they were never shown, so the tool name and the
// input it was called with have to reach the far side.
func TestACPPermissionsShowsWhatIsBeingApproved(t *testing.T) {
	t.Parallel()

	client := &fakePeer{answer: `{"outcome":{"outcome":"cancelled"}}`}
	perms := NewACPPermissions()
	perms.bind(client, "sess-1")

	_, err := perms.Request(context.Background(), approvalRequest())
	require.NoError(t, err)

	require.Len(t, client.requests(), 1)
	var sent acp.RequestPermissionParams
	require.NoError(t, json.Unmarshal(client.requests()[0], &sent))

	assert.Equal(t, "sess-1", sent.SessionID)
	assert.Equal(t, "call-1", sent.ToolCall.ToolCallID,
		"the id must match the tool_call update already streamed, or the client renders a second entry")
	assert.Equal(t, "shell", sent.ToolCall.Title)
	require.Len(t, sent.ToolCall.Content, 1)
	assert.Contains(t, sent.ToolCall.Content[0].Content.Text, "rm -rf /")
}

// TestACPPermissionsSendsTheWholeCommandNotAPreview closes the gap between
// "the user was shown something" and "the user was shown what will run".
//
// The approval flow's metadata preview stops at 2 KB. A shell command longer
// than that would be approved on its prefix while the tool executed the whole
// thing, so anything destructive past the cut gets consent it was never shown
// for. The complete input travels as rawInput, which is ACP's field for exactly
// this.
func TestACPPermissionsSendsTheWholeCommandNotAPreview(t *testing.T) {
	t.Parallel()

	full := `{"command":"` + strings.Repeat("x", 5000) + `; rm -rf /"}`
	req := approvalRequest()
	req.Input = json.RawMessage(full)
	req.Metadata["input"] = full[:2048] + "...(truncated)"

	client := &fakePeer{answer: `{"outcome":{"outcome":"cancelled"}}`}
	perms := NewACPPermissions()
	perms.bind(client, "sess-1")

	_, err := perms.Request(context.Background(), req)
	require.NoError(t, err)

	var sent acp.RequestPermissionParams
	require.NoError(t, json.Unmarshal(client.requests()[0], &sent))

	assert.JSONEq(t, full, string(sent.ToolCall.RawInput),
		"the client must receive the input the tool will actually run")
	require.Len(t, sent.ToolCall.Content, 1)
	assert.Contains(t, sent.ToolCall.Content[0].Content.Text, "rm -rf /",
		"the displayable copy must not stop before the part that matters")
}

// TestACPPermissionsOffersOnlyChoicesItCanKeep pins the option list to what
// this agent implements. ACP also defines allow_always and reject_always, and
// nothing here remembers a decision between tool calls — offering "Always
// allow" and then asking again next time is a promise broken in the one place
// a user is deciding whether to trust the agent.
func TestACPPermissionsOffersOnlyChoicesItCanKeep(t *testing.T) {
	t.Parallel()

	client := &fakePeer{answer: `{"outcome":{"outcome":"cancelled"}}`}
	perms := NewACPPermissions()
	perms.bind(client, "sess-1")

	_, err := perms.Request(context.Background(), approvalRequest())
	require.NoError(t, err)

	var sent acp.RequestPermissionParams
	require.NoError(t, json.Unmarshal(client.requests()[0], &sent))

	require.Len(t, sent.Options, 2)
	for _, opt := range sent.Options {
		assert.NotEmpty(t, opt.Name, "a client renders the option by name; an unnamed one is an empty button")
		assert.Contains(t,
			[]acp.PermissionOptionKind{acp.PermissionAllowOnce, acp.PermissionRejectOnce},
			opt.Kind,
			"an always-option would be a decision nothing in this agent remembers")
	}
}

// TestACPPermissionsIsBoundOnlyForTheTurn ties the binding to a running prompt.
// The session id is part of every permission request, so a stale binding would
// address a session that has ended; and a tool call arriving outside a turn has
// no user waiting on it to answer.
func TestACPPermissionsIsBoundOnlyForTheTurn(t *testing.T) {
	t.Parallel()

	perms := NewACPPermissions()
	client := &fakePeer{answer: `{"outcome":{"outcome":"selected","optionId":"allow-once"}}`}

	var duringTurn error
	turn := func(_ context.Context, _ string) (<-chan agentsdk.TurnEvent, error) {
		// Stands in for a tool asking permission while the turn runs.
		_, duringTurn = perms.Request(context.Background(), approvalRequest())

		ch := make(chan agentsdk.TurnEvent, 1)
		ch <- agentsdk.TurnEvent{Type: "done", ExitReason: agentsdk.ExitCompleted}
		close(ch)
		return ch, nil
	}

	_, err := acpPromptFunc(client, turn, perms)(context.Background(), acp.PromptRequest{
		SessionID: "sess-1",
		Prompt:    []acp.ContentBlock{{Type: acp.ContentText, Text: "hi"}},
	})
	require.NoError(t, err)
	require.NoError(t, duringTurn, "a tool running inside the turn must be able to ask the client")

	_, afterTurn := perms.Request(context.Background(), approvalRequest())
	assert.Error(t, afterTurn, "the binding must not outlive the turn it was made for")
}
