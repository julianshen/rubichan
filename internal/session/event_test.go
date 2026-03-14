package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVerificationSnapshot(t *testing.T) {
	verdict, reason := ParseVerificationSnapshot(`Verification snapshot:
- gate: pass
- verdict: passed
- reason: dependency resolution, schema/init, runtime, and API round-trip observed
- dependency resolution: true`)

	assert.Equal(t, "passed", verdict)
	assert.Equal(t, "dependency resolution, schema/init, runtime, and API round-trip observed", reason)
	assert.Equal(t, "pass", ParseVerificationGate(`Verification snapshot:
- gate: pass
- verdict: passed
- reason: ok`))
	assert.Equal(t, "soft_fail", ParseVerificationGate(`Verification snapshot:
- gate: soft_fail
- verdict: passed_with_warnings
- reason: missing schema/init evidence`))
}

func TestNewLogSinkEmitsJSON(t *testing.T) {
	var lines []string
	sink := NewLogSink(func(format string, args ...any) {
		lines = append(lines, strings.TrimSpace(sprintf(format, args...)))
	})

	sink.Emit(NewCommandResultEvent("/debug-verification-snapshot", "ok", []string{"alpha"}, nil))

	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "session event:")
	assert.Contains(t, lines[0], `"type":"command_result"`)
	assert.Contains(t, lines[0], `"command":"/debug-verification-snapshot"`)
	assert.Contains(t, lines[0], `"activated":["alpha"]`)
}

func TestNewJSONLSinkEmitsOneLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	sink := NewJSONLSink(&buf)

	sink.Emit(NewVerificationSnapshotEvent("Verification snapshot:\n- verdict: passed\n- reason: ok"))

	out := strings.TrimSpace(buf.String())
	assert.Contains(t, out, `"type":"verification_snapshot"`)
	assert.Contains(t, out, `"verdict":"passed"`)
	assert.Contains(t, out, `"reason":"ok"`)
}

func TestNewTurnAndToolEvents(t *testing.T) {
	turn := NewTurnStartedEvent("build a backend api", "gpt-test")
	assert.Equal(t, EventTypeTurnStarted, turn.Type)
	require.NotNil(t, turn.Turn)
	assert.Equal(t, "build a backend api", turn.Turn.Prompt)
	assert.Equal(t, "gpt-test", turn.Turn.Model)

	call := NewToolCallEvent("1", "shell", json.RawMessage(`{"command":"npm install"}`))
	assert.Equal(t, EventTypeToolCall, call.Type)
	require.NotNil(t, call.ToolCall)
	assert.Equal(t, "shell", call.ToolCall.Name)
	assert.JSONEq(t, `{"command":"npm install"}`, string(call.ToolCall.Input))

	malformed := NewToolCallEvent("2", "file", json.RawMessage(`{"operation":"patch","path":"server.js"`))
	assert.Equal(t, EventTypeToolCall, malformed.Type)
	require.NotNil(t, malformed.ToolCall)
	assert.Nil(t, malformed.ToolCall.Input)
	assert.Contains(t, malformed.ToolCall.RawInput, `"path":"server.js"`)

	result := NewToolResultEvent("1", "shell", "added 10 packages", false)
	assert.Equal(t, EventTypeToolResult, result.Type)
	require.NotNil(t, result.ToolResult)
	assert.Equal(t, "added 10 packages", result.ToolResult.Content)

	assistant := NewAssistantFinalEvent("hello world")
	assert.Equal(t, EventTypeAssistantFinal, assistant.Type)
	require.NotNil(t, assistant.Assistant)
	assert.Equal(t, "hello world", assistant.Assistant.Content)

	withActor := assistant.WithActor(PrimaryActor())
	require.NotNil(t, withActor.Actor)
	assert.Equal(t, "primary", withActor.Actor.Name)
	assert.Equal(t, "agent", withActor.Actor.Kind)

	subagent := NewSubagentDoneEvent("explorer", "done", "found files")
	assert.Equal(t, EventTypeSubagentDone, subagent.Type)
	require.NotNil(t, subagent.Actor)
	assert.Equal(t, "explorer", subagent.Actor.Name)
	assert.Equal(t, "subagent", subagent.Actor.Kind)
	require.NotNil(t, subagent.Subagent)
	assert.Equal(t, "done", subagent.Subagent.Summary)

	done := NewTurnCompletedEvent("diff summary", 10, 5)
	assert.Equal(t, EventTypeTurnCompleted, done.Type)
	require.NotNil(t, done.Turn)
	assert.Equal(t, "diff summary", done.Turn.DiffSummary)
	assert.Equal(t, 10, done.Turn.InputTokens)
	assert.Equal(t, 5, done.Turn.OutputTokens)

	plan := NewPlanUpdatedEvent("turn_done", []PlanItem{{Step: "Backend verification", Status: PlanStatusCompleted}})
	assert.Equal(t, EventTypePlanUpdated, plan.Type)
	require.NotNil(t, plan.Plan)
	assert.Equal(t, "turn_done", plan.Plan.Reason)
	require.Len(t, plan.Plan.Steps, 1)
	assert.Equal(t, "Backend verification", plan.Plan.Steps[0].Step)
	assert.Equal(t, "completed", plan.Plan.Steps[0].Status)

	gate := NewGateFailedEvent("verification", "missing API round-trip evidence")
	assert.Equal(t, EventTypeGateFailed, gate.Type)
	require.NotNil(t, gate.Gate)
	assert.Equal(t, "verification", gate.Gate.Name)

	cp := NewCheckpointCreatedEvent("turn-1", "turn_started")
	assert.Equal(t, EventTypeCheckpointCreated, cp.Type)
	require.NotNil(t, cp.Checkpoint)
	assert.Equal(t, "turn-1", cp.Checkpoint.ID)

	restored := NewCheckpointRestoredEvent("turn-1", "manual_restore")
	assert.Equal(t, EventTypeCheckpointRestored, restored.Type)
	require.NotNil(t, restored.Checkpoint)
	assert.Equal(t, "manual_restore", restored.Checkpoint.Reason)
}

func sprintf(format string, args ...any) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(fmt.Sprintf(format, args...)), "\n", " "))
}
