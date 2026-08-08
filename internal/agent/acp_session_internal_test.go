package agent

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStopReasonForCoversEveryExitReason is the test that keeps this mapping
// honest as the agent grows. ACP has five stop reasons and the agent has far
// more exit reasons, so the map is lossy by necessity — but it must be lossy on
// purpose. A default branch folding unknown reasons into end_turn would report
// a clean finish for a turn that crashed, and would do so silently for every
// exit reason added after this was written.
func TestStopReasonForCoversEveryExitReason(t *testing.T) {
	t.Parallel()

	// Turns that ended. ACP has no vocabulary for "the model stopped making
	// progress" or "a stop hook intervened", and end_turn is the honest
	// approximation: the turn is over and nothing failed.
	ended := map[agentsdk.TurnExitReason]acp.StopReason{
		agentsdk.ExitCompleted:          acp.StopEndTurn,
		agentsdk.ExitTaskComplete:       acp.StopEndTurn,
		agentsdk.ExitStopHookPrevented:  acp.StopEndTurn,
		agentsdk.ExitNoProgress:         acp.StopEndTurn,
		agentsdk.ExitEmptyResponse:      acp.StopEndTurn,
		agentsdk.ExitDiminishingReturns: acp.StopEndTurn,
		agentsdk.ExitMaxTurns:           acp.StopMaxTurnRequests,
		agentsdk.ExitCancelled:          acp.StopCancelled,
		agentsdk.ExitMaxOutputTokens:    acp.StopMaxTokens,
		agentsdk.ExitBudgetExceeded:     acp.StopMaxTokens,
	}

	// Turns that broke. These must surface as errors: reporting one as a stop
	// reason tells the client the model finished when the agent fell over.
	// ExitUnknown is documented as a bug, so it belongs here rather than being
	// quietly answered as a completed turn.
	failed := []agentsdk.TurnExitReason{
		agentsdk.ExitUnknown,
		agentsdk.ExitProviderError,
		agentsdk.ExitRateLimited,
		agentsdk.ExitSkillActivationFailed,
		agentsdk.ExitCompactionFailed,
		agentsdk.ExitContextOverflow,
		agentsdk.ExitPanic,
	}

	for reason, want := range ended {
		got, err := stopReasonFor(reason)
		require.NoError(t, err, "exit reason %v", reason)
		assert.Equal(t, want, got, "exit reason %v", reason)
	}

	for _, reason := range failed {
		_, err := stopReasonFor(reason)
		assert.Error(t, err, "exit reason %v must not be reported as a clean stop", reason)
	}

	// The guard: every reason the agent defines is classified above. A new one
	// fails here rather than silently acquiring a default.
	assert.Len(t, ended, len(allExitReasons)-len(failed),
		"a TurnExitReason was added without deciding how ACP should report it")
}

// TestPromptTextRendersTheBlocksTheAgentAccepts covers the flattening of an ACP
// prompt into the single string Turn takes. A resource_link must survive as
// something the model can act on: dropping it would answer a question about a
// file the user explicitly attached without ever mentioning it.
func TestPromptTextRendersTheBlocksTheAgentAccepts(t *testing.T) {
	t.Parallel()

	got := promptText([]acp.ContentBlock{
		{Type: acp.ContentText, Text: "explain this"},
		{Type: acp.ContentResourceLink, URI: "file:///src/main.go", Name: "main.go"},
		{Type: acp.ContentText, Text: "briefly"},
	})

	assert.Contains(t, got, "explain this")
	assert.Contains(t, got, "briefly")
	assert.Contains(t, got, "main.go")
	assert.Contains(t, got, "file:///src/main.go")
}

// fakeNotifier records what a turn streamed instead of writing it to a stream.
type fakeNotifier struct {
	sent []acp.SessionNotification
}

func (f *fakeNotifier) Notify(method string, params any) error {
	if n, ok := params.(acp.SessionNotification); ok {
		f.sent = append(f.sent, n)
	}
	return nil
}

// TestACPPromptStreamsTheTurn is the whole point of the slice: assistant text
// leaves the agent as session/update notifications while the turn runs, and the
// call closes with a mapped stop reason. Without the streaming half, a client
// would receive a stop reason and never the reply.
func TestACPPromptStreamsTheTurn(t *testing.T) {
	t.Parallel()

	turn := func(_ context.Context, _ string) (<-chan agentsdk.TurnEvent, error) {
		ch := make(chan agentsdk.TurnEvent, 8)
		ch <- agentsdk.TurnEvent{Type: "text_delta", Text: "hel"}
		ch <- agentsdk.TurnEvent{Type: "text_delta", Text: "lo"}
		ch <- agentsdk.TurnEvent{
			Type:     "tool_call",
			ToolCall: &agentsdk.ToolCallEvent{ID: "c1", Name: "read_file"},
		}
		ch <- agentsdk.TurnEvent{
			Type:       "tool_result",
			ToolResult: &agentsdk.ToolResultEvent{ID: "c1", Name: "read_file"},
		}
		ch <- agentsdk.TurnEvent{Type: "done", ExitReason: agentsdk.ExitCompleted}
		close(ch)
		return ch, nil
	}

	notifier := &fakeNotifier{}
	prompt := acpPromptFunc(notifier, turn)

	stop, err := prompt(context.Background(), acp.PromptRequest{
		SessionID: "sess-1",
		Cwd:       "/repo",
		Prompt:    []acp.ContentBlock{{Type: acp.ContentText, Text: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, acp.StopEndTurn, stop)

	require.Len(t, notifier.sent, 4, "two text chunks, one tool call, one tool update")
	for _, n := range notifier.sent {
		assert.Equal(t, "sess-1", n.SessionID)
	}
}

// TestACPPromptReportsATurnThatBroke keeps a failed turn from being answered as
// a clean one. The done event carries the failure, so nothing else in the
// stream would reveal it.
func TestACPPromptReportsATurnThatBroke(t *testing.T) {
	t.Parallel()

	turn := func(_ context.Context, _ string) (<-chan agentsdk.TurnEvent, error) {
		ch := make(chan agentsdk.TurnEvent, 1)
		ch <- agentsdk.TurnEvent{Type: "done", ExitReason: agentsdk.ExitProviderError}
		close(ch)
		return ch, nil
	}

	_, err := acpPromptFunc(&fakeNotifier{}, turn)(context.Background(), acp.PromptRequest{
		SessionID: "sess-1",
		Prompt:    []acp.ContentBlock{{Type: acp.ContentText, Text: "hi"}},
	})
	assert.Error(t, err)
}

// TestACPPromptRejectsATurnThatNeverFinished covers a stream that closes with no
// done event. There is no exit reason to map, and answering end_turn would
// invent one — the turn's outcome is genuinely unknown.
func TestACPPromptRejectsATurnThatNeverFinished(t *testing.T) {
	t.Parallel()

	turn := func(_ context.Context, _ string) (<-chan agentsdk.TurnEvent, error) {
		ch := make(chan agentsdk.TurnEvent, 1)
		ch <- agentsdk.TurnEvent{Type: "text_delta", Text: "partial"}
		close(ch)
		return ch, nil
	}

	_, err := acpPromptFunc(&fakeNotifier{}, turn)(context.Background(), acp.PromptRequest{
		SessionID: "sess-1",
		Prompt:    []acp.ContentBlock{{Type: acp.ContentText, Text: "hi"}},
	})
	assert.Error(t, err)
}

// TestStopReasonForRejectsAReasonItDoesNotKnow exercises the guard branch. It
// is unreachable through the agent's own constants, which is the point: if one
// is ever added without being classified, this is the behaviour it gets —
// a refusal naming the gap, not a silent end_turn.
func TestStopReasonForRejectsAReasonItDoesNotKnow(t *testing.T) {
	t.Parallel()

	_, err := stopReasonFor(agentsdk.TurnExitReason(9999))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclassified")
}

// TestACPPromptReportsATurnThatNeverStarted covers the failure before any event
// exists. There is nothing to stream and no exit reason to map, so the error
// has to carry the outcome by itself.
func TestACPPromptReportsATurnThatNeverStarted(t *testing.T) {
	t.Parallel()

	turn := func(context.Context, string) (<-chan agentsdk.TurnEvent, error) {
		return nil, assert.AnError
	}

	_, err := acpPromptFunc(&fakeNotifier{}, turn)(context.Background(), acp.PromptRequest{
		SessionID: "sess-1",
		Prompt:    []acp.ContentBlock{{Type: acp.ContentText, Text: "hi"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start turn")
}
