package agent

import (
	"context"
	"fmt"
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
		agentsdk.ExitNoProgress,
		agentsdk.ExitEmptyResponse,
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
	case agentsdk.ExitUnknown,
		agentsdk.ExitProviderError,
		agentsdk.ExitRateLimited,
		agentsdk.ExitSkillActivationFailed,
		agentsdk.ExitCompactionFailed,
		agentsdk.ExitContextOverflow,
		agentsdk.ExitPanic:
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
			exit  agentsdk.TurnExitReason
			ended bool
		)
		for ev := range events {
			switch ev.Type {
			case agentsdk.EventTextDelta:
				notify(acp.AgentMessageChunk(ev.Text))
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
		return stopReasonFor(exit)
	}
}
