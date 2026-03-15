package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeJSONLEvents(t *testing.T) {
	input := strings.NewReader(`
{"type":"turn_started","turn":{"prompt":"build api","model":"gpt-test"}}
{"type":"assistant_final","assistant":{"content":"done"}}
`)

	events, err := DecodeJSONLEvents(input)
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, EventTypeTurnStarted, events[0].Type)
	assert.Equal(t, "build api", events[0].Turn.Prompt)
	assert.Equal(t, EventTypeAssistantFinal, events[1].Type)
	assert.Equal(t, "done", events[1].Assistant.Content)
}

func TestDecodeJSONLEventsAcceptsLargeLines(t *testing.T) {
	large := strings.Repeat("x", 70*1024)
	line := fmt.Sprintf("{\"type\":\"assistant_final\",\"assistant\":{\"content\":\"%s\"}}\n", large)
	events, err := DecodeJSONLEvents(strings.NewReader(line))
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, EventTypeAssistantFinal, events[0].Type)
	assert.Equal(t, large, events[0].Assistant.Content)
}

func TestBuildTranscript(t *testing.T) {
	events := []Event{
		NewTurnStartedEvent("build a backend api", "gpt-test").WithActor(PrimaryActor()),
		NewToolCallEvent("1", "shell", []byte(`{"command":"npm install"}`)).WithActor(PrimaryActor()),
		NewToolResultEvent("1", "shell", "added 10 packages", false).WithActor(PrimaryActor()),
		NewAssistantFinalEvent("All checks passed.").WithActor(PrimaryActor()),
		NewSubagentDoneEvent("explorer", "found files", "results"),
		NewVerificationSnapshotEvent("Verification snapshot:\n- verdict: passed\n- reason: ok"),
		NewTurnCompletedEvent("updated app", 10, 5),
	}

	out := BuildTranscript(events)
	assert.Contains(t, out, "User (primary): build a backend api")
	assert.Contains(t, out, `Tool call (primary) [shell]: {"command":"npm install"}`)
	assert.Contains(t, out, "Tool result (primary) [shell]: added 10 packages")
	assert.Contains(t, out, "Assistant (primary): All checks passed.")
	assert.Contains(t, out, "Subagent (explorer) done: found files")
	assert.Contains(t, out, "Verification [passed]: ok")
	assert.Contains(t, out, "Turn completed: input_tokens=10 output_tokens=5 | diff=updated app")
}

func TestBuildTranscriptUsesRawInputFallback(t *testing.T) {
	events := []Event{
		NewToolCallEvent("1", "file", []byte(`{"operation":"patch","path":"server.js"`)).WithActor(PrimaryActor()),
	}

	out := BuildTranscript(events)
	assert.Contains(t, out, `Tool call (primary) [file]: {"operation":"patch","path":"server.js"`)
}

func TestBuildSummary(t *testing.T) {
	events := []Event{
		NewTurnStartedEvent("build a backend api", "gpt-test").WithActor(PrimaryActor()),
		NewToolCallEvent("1", "shell", []byte(`{"command":"npm install"}`)).WithActor(PrimaryActor()),
		NewToolResultEvent("1", "shell", "added 10 packages", false).WithActor(PrimaryActor()),
		NewAssistantFinalEvent("All checks passed.").WithActor(PrimaryActor()),
		NewSubagentDoneEvent("explorer", "found files", "results"),
		NewVerificationSnapshotEvent("Verification snapshot:\n- verdict: passed\n- reason: ok"),
	}

	summary := BuildSummary(events)
	assert.Equal(t, 6, summary.EventCount)
	assert.Equal(t, 1, summary.Turns)
	assert.Equal(t, 1, summary.ToolCalls)
	assert.Equal(t, 1, summary.ToolResults)
	assert.Equal(t, 1, summary.SubagentsCompleted)
	assert.Equal(t, "pass", summary.LastVerificationGate)
	assert.Equal(t, "passed", summary.LastVerificationVerdict)
	assert.Equal(t, "All checks passed.", summary.LastAssistantFinal)

	text := BuildSummaryText(summary)
	assert.Contains(t, text, "Events: 6")
	assert.Contains(t, text, "Last gate: pass")
	assert.Contains(t, text, "Last verification: passed (ok)")
}

func TestBuildSummaryDerivesVerificationFromToolEvidence(t *testing.T) {
	events := []Event{
		NewTurnStartedEvent("Verify this Python backend SQLite todo API by installing deps, starting server, calling /todos and /stats, and checking the database.", "gpt-test").WithActor(PrimaryActor()),
		NewToolCallEvent("1", "shell", []byte(`{"command":"python3 -m pip install -r requirements.txt"}`)).WithActor(PrimaryActor()),
		NewToolResultEvent("1", "shell", "Successfully installed requirements", false).WithActor(PrimaryActor()),
		NewToolCallEvent("2", "shell", []byte(`{"command":"python3 init_schema.py"}`)).WithActor(PrimaryActor()),
		NewToolResultEvent("2", "shell", "Database initialized", false).WithActor(PrimaryActor()),
		NewToolCallEvent("3", "process", []byte(`{"command":"python3 -m uvicorn main:app --host 127.0.0.1 --port 8010"}`)).WithActor(PrimaryActor()),
		NewToolResultEvent("3", "process", "status: running", false).WithActor(PrimaryActor()),
		NewToolCallEvent("4", "shell", []byte(`{"command":"curl -i http://127.0.0.1:8010/stats"}`)).WithActor(PrimaryActor()),
		NewToolResultEvent("4", "shell", "HTTP/1.1 200 OK\n{\"total\":3,\"completed\":2,\"active\":1}", false).WithActor(PrimaryActor()),
	}

	summary := BuildSummary(events)
	assert.Equal(t, "pass", summary.LastVerificationGate)
	assert.Equal(t, "passed", summary.LastVerificationVerdict)
	assert.Equal(t, "dependency resolution, schema/init, runtime, and API round-trip observed", summary.LastVerificationReason)

	text := BuildSummaryText(summary)
	assert.Contains(t, text, "Last gate: pass")
	assert.Contains(t, text, "Last verification: passed (dependency resolution, schema/init, runtime, and API round-trip observed)")
}

func TestMarshalSummaryJSON(t *testing.T) {
	payload, err := MarshalSummaryJSON(ReplaySummary{EventCount: 3, Turns: 1})
	require.NoError(t, err)

	var decoded ReplaySummary
	require.NoError(t, json.Unmarshal(payload, &decoded))
	assert.Equal(t, 3, decoded.EventCount)
	assert.Equal(t, 1, decoded.Turns)
}

func TestBuildTranscriptIncludesPlanGateAndCheckpointEvents(t *testing.T) {
	events := []Event{
		NewCheckpointCreatedEvent("turn-1", "turn_started").WithActor(PrimaryActor()),
		NewPlanUpdatedEvent("turn_done", []PlanItem{{Step: "Backend verification", Status: PlanStatusReverifyRequired}}).WithActor(PrimaryActor()),
		NewGateFailedEvent("verification", "verification was invalidated by later edits").WithActor(PrimaryActor()),
	}

	out := BuildTranscript(events)
	assert.Contains(t, out, "Checkpoint created (primary) [turn-1]: turn_started")
	assert.Contains(t, out, "Plan updated (primary): turn_done | Backend verification=reverify_required")
	assert.Contains(t, out, "Gate failed (primary) [verification]: verification was invalidated by later edits")

	summary := BuildSummary(events)
	assert.Equal(t, 1, summary.PlanUpdates)
	assert.Equal(t, 1, summary.GateFailures)
	assert.Equal(t, 1, summary.CheckpointsCreated)
	assert.Equal(t, "verification: verification was invalidated by later edits", summary.LastGateFailure)

	text := BuildSummaryText(summary)
	assert.Contains(t, text, "Plan updates: 1")
	assert.Contains(t, text, "Gate failures: 1")
	assert.Contains(t, text, "Checkpoints created: 1")
	assert.Contains(t, text, "Last gate failure: verification: verification was invalidated by later edits")
}
