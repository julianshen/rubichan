package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Notifier is the half of an ACP connection a running turn needs: the ability
// to push an update without waiting for an answer. Narrowed to one method so a
// turn cannot originate requests, and so tests need not stand up a connection.
type Notifier interface {
	Notify(method string, params any) error
}

// turnFunc is the shape of Agent.Turn. Declared here rather than imported: the
// identical type lives in internal/shell, and depending on a sibling mode
// package for a function signature would couple the agent to one of its
// callers.
type turnFunc func(ctx context.Context, userMessage string) (<-chan agentsdk.TurnEvent, error)

// allExitReasons is every TurnExitReason the agent can report. It exists so the
// ACP mapping can be checked for completeness: adding a reason without deciding
// how a client should be told about it fails a test rather than silently
// acquiring whatever the default branch does.
var allExitReasons = []agentsdk.TurnExitReason{
	agentsdk.ExitUnknown,
	agentsdk.ExitCompleted,
	agentsdk.ExitMaxTurns,
	agentsdk.ExitCancelled,
	agentsdk.ExitProviderError,
	agentsdk.ExitRateLimited,
	agentsdk.ExitSkillActivationFailed,
	agentsdk.ExitTaskComplete,
	agentsdk.ExitNoProgress,
	agentsdk.ExitEmptyResponse,
	agentsdk.ExitCompactionFailed,
	agentsdk.ExitContextOverflow,
	agentsdk.ExitMaxOutputTokens,
	agentsdk.ExitPanic,
	agentsdk.ExitDiminishingReturns,
	agentsdk.ExitStopHookPrevented,
	agentsdk.ExitBudgetExceeded,
}

// stopReasonFor translates the agent's exit reason into the one ACP defines.
//
// The map is lossy — ACP has five stop reasons and the agent has seventeen exit
// reasons — but it is lossy deliberately. Reasons that mean "the turn ended"
// collapse onto end_turn; reasons that mean "the turn broke" become errors, so
// a client is told the agent failed rather than that the model finished.
func stopReasonFor(r agentsdk.TurnExitReason) (acp.StopReason, error) {
	switch r {
	case agentsdk.ExitCompleted,
		agentsdk.ExitTaskComplete,
		agentsdk.ExitStopHookPrevented,
		agentsdk.ExitDiminishingReturns:
		return acp.StopEndTurn, nil

	case agentsdk.ExitMaxTurns:
		return acp.StopMaxTurnRequests, nil

	case agentsdk.ExitCancelled:
		return acp.StopCancelled, nil

	case agentsdk.ExitMaxOutputTokens, agentsdk.ExitBudgetExceeded:
		return acp.StopMaxTokens, nil

	// Everything below is a failed turn. ExitUnknown is included on purpose:
	// it is documented as a bug, and answering it as a completed turn would
	// hide the code path that forgot to set a reason.
	// ExitNoProgress and ExitEmptyResponse are here because the agent emits an
	// explicit error event immediately before each — tool_phase.go for the
	// first, assemble_turn.go for the second. They are the agent declaring the
	// turn failed, so end_turn would contradict it. The empty-response case is
	// the worst of the two: the client would receive a clean stop reason and
	// not one chunk of assistant text.
	case agentsdk.ExitUnknown,
		agentsdk.ExitProviderError,
		agentsdk.ExitRateLimited,
		agentsdk.ExitSkillActivationFailed,
		agentsdk.ExitCompactionFailed,
		agentsdk.ExitContextOverflow,
		agentsdk.ExitPanic,
		agentsdk.ExitNoProgress,
		agentsdk.ExitEmptyResponse:
		return "", fmt.Errorf("turn failed: %s", r)

	default:
		// No default folding into end_turn: an unclassified reason is a gap in
		// this mapping, and saying so beats guessing.
		return "", fmt.Errorf("unclassified turn exit reason: %s", r)
	}
}

// promptText flattens an ACP prompt into the single string Turn accepts.
//
// A resource_link is rendered rather than dropped. The client attached it
// deliberately, and answering as though it were never sent produces a reply
// about a file the user believes was in scope.
func promptText(blocks []acp.ContentBlock) string {
	var b strings.Builder
	for _, block := range blocks {
		switch block.Type {
		case acp.ContentText:
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(block.Text)
		case acp.ContentResourceLink:
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "[attached resource: %s (%s)]", block.Name, block.URI)
		}
	}
	return b.String()
}

// acpPromptFunc adapts a turn to ACP's session/prompt.
//
// The turn's events are streamed as session/update notifications while it runs.
// This is not decoration: session/prompt's result carries only a stop reason,
// so without these the client would learn that a turn finished and never what
// it said.
func acpPromptFunc(n Notifier, turn turnFunc) acp.PromptFunc {
	return func(ctx context.Context, req acp.PromptRequest) (acp.StopReason, error) {
		events, err := turn(ctx, promptText(req.Prompt))
		if err != nil {
			return "", fmt.Errorf("start turn: %w", err)
		}

		notify := func(update any) {
			// A failed notification is logged by the connection, not fatal here:
			// abandoning a running turn because one update did not reach the
			// client would lose the work as well as the message.
			_ = n.Notify(acp.MethodSessionUpdate, acp.NewSessionNotification(req.SessionID, update))
		}

		var (
			exit    agentsdk.TurnExitReason
			ended   bool
			lastErr error
		)
		for ev := range events {
			switch ev.Type {
			case agentsdk.EventTextDelta:
				notify(acp.AgentMessageChunk(ev.Text))
			case "error":
				// Retained, not reported yet. The agent emits error events it
				// then recovers from — a model fallback is one — so the done
				// event decides the outcome. But when the turn does fail, this
				// is the only text saying *why*: the exit reason is a category,
				// not a diagnosis. Dropping it left the client with
				// "turn failed: empty_response" and nothing else.
				if ev.Error != nil {
					lastErr = ev.Error
				}
			case "tool_call":
				if ev.ToolCall != nil {
					notify(acp.ToolCall(ev.ToolCall.ID, ev.ToolCall.Name))
				}
			case "tool_result":
				if ev.ToolResult != nil {
					status := acp.ToolCallCompleted
					if ev.ToolResult.IsError {
						status = acp.ToolCallFailed
					}
					notify(acp.ToolCallUpdate(ev.ToolResult.ID, status))
				}
			case "done":
				exit, ended = ev.ExitReason, true
			}
		}

		// A stream that closed without a done event leaves the turn's outcome
		// genuinely unknown. Answering end_turn would invent one.
		if !ended {
			return "", fmt.Errorf("turn ended without a done event")
		}

		stop, err := stopReasonFor(exit)
		if err != nil && lastErr != nil {
			return "", fmt.Errorf("%w: %v", err, lastErr)
		}
		return stop, err
	}
}

// ServeACP serves the Agent Client Protocol over r/w until the stream ends or
// ctx is cancelled.
//
// This is the composition root for ACP: the agent core holds no protocol state,
// so the registry, the connection and the session wiring are assembled here and
// nowhere else.
// One agent serves one connection at a time. Every session opened on any
// connection runs through this agent's single conversation, so a second
// connection would get its own session store, pass its own sole-session check,
// and still append to the first connection's history.
func ServeACP(ctx context.Context, a *Agent, r io.Reader, w io.Writer) error {
	// The claim is permanent, not held for the duration of the connection.
	//
	// Releasing it on return was the obvious thing and it was wrong: the
	// conversation lives on the agent and nothing resets it between
	// connections, so a second client would open a fresh ACP session and
	// inherit the first client's history. That is a cross-client leak, and it
	// is worse than a one-shot API.
	//
	// Making reuse safe means resetting or replacing every piece of
	// session-owned agent state — conversation, context manager, session
	// memory, persistence — which is the per-session-state feature this slice
	// deliberately does not attempt. Until that exists, an agent serves ACP
	// once and a second connection needs a second agent.
	if !a.acpServing.CompareAndSwap(false, true) {
		return fmt.Errorf("acp: this agent has already served an ACP connection; construct a new agent to serve another, because its conversation would otherwise carry over")
	}

	registry, handshake := newACPRegistry(a)
	return serveACP(ctx, r, w, acpServeConfig{
		Registry:     registry,
		Capabilities: acpAgentCapabilities(),
		WorkingDir:   a.WorkingDir(),
		Handshake:    handshake,
		Turn:         a.Turn,
	})
}

// acpServeConfig is what serving ACP needs from its surroundings. Grouped into
// a struct rather than passed positionally because the list had grown past the
// point where a caller could tell two adjacent strings apart.
type acpServeConfig struct {
	Registry     *acp.CapabilityRegistry
	Capabilities acp.AgentCapabilities
	WorkingDir   string
	// Handshake gates the session methods on initialize. Nil disables the
	// check, which is what a test serving no handshake wants.
	Handshake *acp.Handshake
	Turn      turnFunc
}

// serveACP is ServeACP with its collaborators passed in, so the wiring can be
// exercised against a turn that does not need a live provider.
func serveACP(ctx context.Context, r io.Reader, w io.Writer, cfg acpServeConfig) error {
	conn := acp.NewConn(r, w, cfg.Registry)

	// Session methods are registered after the connection exists because the
	// prompt handler streams through it, and the connection is built from the
	// registry. Ordering it this way is safe rather than merely convenient:
	// RegisterMethod is mutex-guarded, and Serve has not started reading yet, so
	// no request can arrive against a half-populated registry.
	acp.RegisterSession(cfg.Registry, acp.SessionConfig{
		Capabilities: cfg.Capabilities,
		WorkingDir:   cfg.WorkingDir,
		Handshake:    cfg.Handshake,
		Prompt:       acpPromptFunc(conn, cfg.Turn),
	})

	return conn.Serve(ctx)
}
