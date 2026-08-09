package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Action ids the agent's approval flow understands. They are what an adapter
// puts in a UIResponse, and agentsdk.ApprovalFlow maps them back to a decision.
const (
	uiActionAllow = "allow"
	uiActionDeny  = "deny"
)

// ACPPermissions asks the connected ACP client before a tool runs.
//
// It is a UIRequestHandler because that is the seam the agent already uses to
// put an approval in front of a human — the TUI installs one, and this is the
// same thing pointed at an editor over the wire. The request carries the tool
// call's id, so the permission prompt attaches to the tool_call the client was
// already streamed rather than appearing as a second, unexplained entry.
//
// The connection and session are bound for the duration of a turn rather than
// at construction, because neither exists when the agent is built: the agent is
// constructed first, then serves a connection, then a client opens a session on
// it. Outside a turn there is nobody to ask, and the handler says so instead of
// deciding.
type ACPPermissions struct {
	mu        sync.RWMutex
	client    acp.Requester
	sessionID string
}

// NewACPPermissions creates an unbound handler. It refuses every request until
// a turn binds it, which is the safe direction: a tool call with no live
// session behind it has no user waiting to answer for it.
func NewACPPermissions() *ACPPermissions {
	return &ACPPermissions{}
}

// bind attaches the handler to the connection and session a turn is running on.
func (p *ACPPermissions) bind(client acp.Requester, sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.client = client
	p.sessionID = sessionID
}

// unbind detaches after a turn. A stale binding would address a session that
// has ended, and the session id travels in every permission request.
func (p *ACPPermissions) unbind() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.client = nil
	p.sessionID = ""
}

func (p *ACPPermissions) binding() (acp.Requester, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.client, p.sessionID
}

// Request implements agentsdk.UIRequestHandler by putting the decision to the
// ACP client and reporting what it answered.
func (p *ACPPermissions) Request(ctx context.Context, req agentsdk.UIRequest) (agentsdk.UIResponse, error) {
	client, sessionID := p.binding()
	if client == nil {
		return agentsdk.UIResponse{}, fmt.Errorf(
			"acp: no session is active, so nobody can be asked to approve %q", req.Title)
	}

	allowed, err := acp.RequestToolPermission(ctx, client, acp.RequestPermissionParams{
		SessionID: sessionID,
		ToolCall:  permissionToolCall(req),
		Options:   permissionOptions(),
	})
	if err != nil {
		return agentsdk.UIResponse{}, err
	}

	action := uiActionDeny
	if allowed {
		action = uiActionAllow
	}
	// The id is echoed because the approval flow rejects a response that does
	// not match the request it made.
	return agentsdk.UIResponse{RequestID: req.ID, ActionID: action}, nil
}

// permissionToolCall describes to the client what it is asking about.
//
// The tool's name is preferred as the title over the flow's phrasing because
// the tool_call update already streamed uses the name, and a client matches the
// two by id and shows one entry.
//
// The input travels complete, twice: as rawInput, which is ACP's structured
// field for it, and as displayable content for clients that render only that.
// It is deliberately taken from UIRequest.Input rather than from the metadata
// preview — the preview stops at 2 KB, so a shell command longer than that
// would be approved on its prefix while the tool went on to run the whole
// thing, putting anything destructive past the cut outside what the user
// actually agreed to.
//
// Falling back to the preview when Input is absent is the safe direction: some
// visible description beats none, and every caller in this repository sets
// Input.
func permissionToolCall(req agentsdk.UIRequest) acp.PermissionToolCall {
	call := acp.PermissionToolCall{
		ToolCallID: req.ID,
		Title:      req.Title,
		Status:     acp.ToolCallPending,
	}
	if name := req.Metadata["tool"]; name != "" {
		call.Title = name
	}

	shown := req.Metadata["input"]
	if len(req.Input) > 0 {
		call.RawInput = req.Input
		shown = string(req.Input)
	}
	if shown != "" {
		call.Content = []acp.ToolCallContent{acp.TextToolCallContent(shown)}
	}
	return call
}

// permissionOptions are the choices offered to the user.
//
// ACP also defines allow_always and reject_always, and neither is offered here:
// nothing in this agent remembers a decision between tool calls, so "Always
// allow" would be answered by asking again on the next call. Offering a choice
// and then not keeping it is worse than not offering it, and it would be worst
// in exactly this dialog — the one where the user is deciding how much to trust
// the agent. They appear in the same change that adds somewhere to record them.
func permissionOptions() []acp.PermissionOption {
	return []acp.PermissionOption{
		{OptionID: "allow-once", Name: "Allow once", Kind: acp.PermissionAllowOnce},
		{OptionID: "reject-once", Name: "Reject", Kind: acp.PermissionRejectOnce},
	}
}
