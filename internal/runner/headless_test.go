// internal/runner/headless_test.go
package runner

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/session"
)

// makeEventCh creates a closed channel pre-filled with the given events.
func makeEventCh(events ...agent.TurnEvent) <-chan agent.TurnEvent {
	ch := make(chan agent.TurnEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func TestHeadlessRunnerBasic(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "text_delta", Text: "Hello "},
			agent.TurnEvent{Type: "text_delta", Text: "World"},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "say hello", "generic")
	require.NoError(t, err)

	assert.Equal(t, "say hello", result.Prompt)
	assert.Equal(t, "Hello World", result.Response)
	assert.Equal(t, "generic", result.Mode)
	assert.Empty(t, result.ToolCalls)
	assert.Empty(t, result.Error)
}

func TestHeadlessRunnerWithToolCalls(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "file", Input: []byte(`{"op":"read"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "file", Content: "package main", IsError: false,
			}},
			agent.TurnEvent{Type: "text_delta", Text: "Done"},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "read file", "generic")
	require.NoError(t, err)

	assert.Equal(t, "Done", result.Response)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "file", result.ToolCalls[0].Name)
	assert.Equal(t, "package main", result.ToolCalls[0].Result)
}

func TestHeadlessRunnerError(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "error", Error: assert.AnError},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "fail", "generic")
	require.NoError(t, err)

	assert.NotEmpty(t, result.Error)
	assert.Contains(t, result.Summary, "Run failed before producing a textual response")
}

func TestHeadlessRunnerTimeout(t *testing.T) {
	turnFn := func(ctx context.Context, msg string) (<-chan agent.TurnEvent, error) {
		ch := make(chan agent.TurnEvent)
		go func() {
			defer close(ch)
			<-ctx.Done()
			ch <- agent.TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- agent.TurnEvent{Type: "done"}
		}()
		return ch, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(ctx, "slow", "generic")
	require.NoError(t, err)

	assert.NotEmpty(t, result.Error)
}

func TestHeadlessRunnerSummaryForToolOnlyRun(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "search", Input: []byte(`{"pattern":"todo"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "search", Content: "src/App.tsx", IsError: false,
			}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "inspect app", "generic")
	require.NoError(t, err)

	assert.Equal(t, result.Summary, result.Response)
	assert.Contains(t, result.Summary, "Run completed without a textual response after 1 tool call")
}

func TestHeadlessRunnerTreatsToolOnlyMaxTurnsAsIncompleteSuccess(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "process", Input: []byte(`{"operation":"exec"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "process", Content: "process_id: abc123", IsError: false,
			}},
			agent.TurnEvent{Type: "error", Error: assert.AnError},
			agent.TurnEvent{Type: "error", Error: context.DeadlineExceeded},
			agent.TurnEvent{Type: "error", Error: errMaxTurnsExceededStub{}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "verify app", "generic")
	require.NoError(t, err)

	assert.Equal(t, result.Summary, result.Response)
	assert.Empty(t, result.Error)
	assert.Contains(t, result.Summary, "Run completed through tool evidence")
}

func TestHeadlessRunnerFrontendTaskRequiresBuildEvidenceForToolOnlySuccess(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "file", Input: []byte(`{"operation":"read","path":"src/App.jsx"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "file", Content: "export default function App() {}", IsError: false,
			}},
			agent.TurnEvent{Type: "error", Error: errMaxTurnsExceededStub{}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a React Vite todo app with shadcn styling", "generic")
	require.NoError(t, err)

	assert.Equal(t, result.Summary, result.Response)
	assert.Contains(t, result.Error, "max turns")
	assert.Contains(t, result.Summary, "Run failed after 1 tool call")
}

func TestHeadlessRunnerFrontendTaskAllowsToolOnlySuccessWithBuildEvidence(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "shell", Input: []byte(`{"command":"npm run build"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "shell", Content: "vite v4.5.14 building for production...\n✓ built in 739ms", IsError: false,
			}},
			agent.TurnEvent{Type: "error", Error: errMaxTurnsExceededStub{}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Refactor this React Vite frontend app and verify build", "generic")
	require.NoError(t, err)

	assert.Equal(t, result.Summary, result.Response)
	assert.Empty(t, result.Error)
	assert.Contains(t, result.Summary, "Run completed through tool evidence")
}

func TestHeadlessRunnerFrontendTaskRequiresBuildAfterLatestEdit(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "shell", Input: []byte(`{"command":"npm run build"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "shell", Content: "vite v4.5.14 building for production...\n✓ built in 739ms", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t2", Name: "file", Input: []byte(`{"operation":"patch","path":"src/App.jsx","old_string":"foo","new_string":"bar"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t2", Name: "file", Content: "patched src/App.jsx", IsError: false,
			}},
			agent.TurnEvent{Type: "error", Error: errMaxTurnsExceededStub{}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Improve this React Vite todo app UI and verify build", "generic")
	require.NoError(t, err)

	assert.Equal(t, result.Summary, result.Response)
	assert.Contains(t, result.Error, "max turns")
	assert.Contains(t, result.Summary, "Run failed after 2 tool call")
}

func TestHeadlessRunnerSummarizesFrontendBuildFailure(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "shell", Input: []byte(`{"command":"npm run build"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID:      "t1",
				Name:    "shell",
				Content: "vite v4.5.14 building for production...\nTransform failed with 1 error:\nsrc/App.tsx:38:4: ERROR: Unexpected \"=\"",
				IsError: true,
			}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a React Vite todo app with shadcn styling", "generic")
	require.NoError(t, err)

	assert.Contains(t, result.Summary, "frontend build failure")
	assert.Contains(t, result.Summary, "src/App.tsx:38:4")
}

func TestHeadlessRunnerBuildsFrontendEvidenceSummary(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "file", Input: []byte(`{"operation":"write","path":"src/App.tsx","content":"export default function App(){}"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "file", Content: "wrote src/App.tsx", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t2", Name: "shell", Input: []byte(`{"command":"npm run build"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t2", Name: "shell", Content: "vite v4.5.14 building for production...\n✓ built in 441ms", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t3", Name: "process", Input: []byte(`{"operation":"read_output","process_id":"abc"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t3", Name: "process", Content: "HTTP/1.1 200 OK", IsError: false,
			}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a React Vite todo app with shadcn styling", "generic")
	require.NoError(t, err)

	assert.Contains(t, result.EvidenceSummary, "- Verification verdict: passed")
	assert.Contains(t, result.EvidenceSummary, "- Reason: latest build evidence is green")
	assert.Contains(t, result.EvidenceSummary, "- Build: passed")
	assert.Contains(t, result.EvidenceSummary, "- Runtime: reachable (HTTP 200)")
	assert.Contains(t, result.EvidenceSummary, "src/App.tsx")
}

func TestHeadlessRunnerBuildsFrontendEvidenceSummaryWithoutBuildEvidence(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "file", Input: []byte(`{"operation":"write","path":"package.json","content":"{}"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "file", Content: "wrote package.json", IsError: false,
			}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a React Vite todo app with shadcn styling", "generic")
	require.NoError(t, err)

	assert.Contains(t, result.EvidenceSummary, "- Verification verdict: failed")
	assert.Contains(t, result.EvidenceSummary, "- Reason: no build evidence")
	assert.Contains(t, result.EvidenceSummary, "- Build: no evidence")
}

func TestHeadlessRunnerEmitsStructuredSessionEvents(t *testing.T) {
	turnFn := func(_ context.Context, _ string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "shell", Input: []byte(`{"command":"npm install"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "shell", Content: "added 10 packages", IsError: false,
			}},
			agent.TurnEvent{Type: "text_delta", Text: "done"},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	var events []session.Event
	r := NewHeadlessRunner(turnFn)
	r.SetModelName("openai/gpt-5")
	r.SetEventSink(session.SinkFunc(func(evt session.Event) {
		events = append(events, evt)
	}))

	_, err := r.Run(context.Background(), "verify backend", "generic")
	require.NoError(t, err)
	require.NotEmpty(t, events)

	var hasTurnStart bool
	var hasToolCall bool
	var hasToolResult bool
	var hasVerification bool
	var hasTurnDone bool
	for _, evt := range events {
		switch evt.Type {
		case session.EventTypeTurnStarted:
			hasTurnStart = true
			require.NotNil(t, evt.Turn)
			assert.Equal(t, "openai/gpt-5", evt.Turn.Model)
		case session.EventTypeToolCall:
			hasToolCall = true
			require.NotNil(t, evt.ToolCall)
			assert.Equal(t, "shell", evt.ToolCall.Name)
			assert.True(t, json.Valid(evt.ToolCall.Input))
		case session.EventTypeToolResult:
			hasToolResult = true
			require.NotNil(t, evt.ToolResult)
			assert.Equal(t, "shell", evt.ToolResult.Name)
		case session.EventTypeVerificationSnapshot:
			hasVerification = true
		case session.EventTypeTurnCompleted:
			hasTurnDone = true
		}
		require.NotNil(t, evt.Actor)
		assert.Equal(t, "primary", evt.Actor.Name)
	}
	assert.True(t, hasTurnStart)
	assert.True(t, hasToolCall)
	assert.True(t, hasToolResult)
	assert.True(t, hasVerification)
	assert.True(t, hasTurnDone)
}

func TestHeadlessRunnerTurnCompletedPreservesDonePayload(t *testing.T) {
	turnFn := func(_ context.Context, _ string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "done", DiffSummary: "real diff summary", InputTokens: 123, OutputTokens: 45},
		), nil
	}

	var events []session.Event
	r := NewHeadlessRunner(turnFn)
	r.SetEventSink(session.SinkFunc(func(evt session.Event) {
		events = append(events, evt)
	}))

	_, err := r.Run(context.Background(), "verify", "generic")
	require.NoError(t, err)

	var done *session.Event
	for i := range events {
		if events[i].Type == session.EventTypeTurnCompleted {
			done = &events[i]
			break
		}
	}
	require.NotNil(t, done)
	require.NotNil(t, done.Turn)
	assert.Equal(t, "real diff summary", done.Turn.DiffSummary)
	assert.Equal(t, 123, done.Turn.InputTokens)
	assert.Equal(t, 45, done.Turn.OutputTokens)
}

func TestFrontendVerificationVerdictRequiresBuildAfterLatestEdit(t *testing.T) {
	toolCalls := []output.ToolCallLog{
		{
			Name:  "shell",
			Input: json.RawMessage(`{"command":"npm run build"}`),
		},
		{
			Name:  "file",
			Input: json.RawMessage(`{"operation":"patch","path":"src/App.tsx","old_string":"a","new_string":"b"}`),
		},
	}

	verdict, reason := frontendVerificationVerdict(toolCalls)
	assert.Equal(t, "failed", verdict)
	assert.Equal(t, "no build evidence", reason)
}

func TestFindFrontendBuildFailureIgnoresEarlierFailureAfterSuccessfulRetry(t *testing.T) {
	toolCalls := []output.ToolCallLog{
		{Name: "shell", Input: json.RawMessage(`{"command":"npm run build"}`), Result: "build failed", IsError: true},
		{Name: "shell", Input: json.RawMessage(`{"command":"npm run build"}`), Result: "build succeeded", IsError: false},
	}
	assert.Equal(t, "", findFrontendBuildFailure(toolCalls))
}

func TestFindBackendValidationFailureIgnoresEarlierFailureAfterSuccessfulRetry(t *testing.T) {
	toolCalls := []output.ToolCallLog{
		{Name: "shell", Input: json.RawMessage(`{"command":"go test ./..."}`), Result: "FAIL", IsError: true},
		{Name: "shell", Input: json.RawMessage(`{"command":"go test ./..."}`), Result: "ok", IsError: false},
	}
	assert.Equal(t, "", findBackendValidationFailure(toolCalls))
}

func TestBackendDependencyFailureReasonIgnoresEarlierFailureAfterSuccessfulRetry(t *testing.T) {
	toolCalls := []output.ToolCallLog{
		{Name: "shell", Input: json.RawMessage(`{"command":"npm install"}`), Result: "ERR", IsError: true},
		{Name: "shell", Input: json.RawMessage(`{"command":"npm install"}`), Result: "added packages", IsError: false},
	}
	assert.Equal(t, "", backendDependencyFailureReason(toolCalls))
}

func TestBackendValidationEvidenceLineUsesOnlyPostEditCalls(t *testing.T) {
	toolCalls := []output.ToolCallLog{
		{Name: "shell", Input: json.RawMessage(`{"command":"go test ./..."}`), Result: "ok", IsError: false},
		{Name: "file", Input: json.RawMessage(`{"operation":"patch","path":"main.go"}`), Result: "patched", IsError: false},
	}
	assert.Equal(t, "- Validation: no evidence", backendValidationEvidenceLine(toolCalls))
}

func TestFrontendBuildEvidenceLineUsesOnlyPostEditCalls(t *testing.T) {
	toolCalls := []output.ToolCallLog{
		{Name: "shell", Input: json.RawMessage(`{"command":"npm run build"}`), Result: "built in 123ms", IsError: false},
		{Name: "file", Input: json.RawMessage(`{"operation":"patch","path":"src/App.tsx"}`), Result: "patched", IsError: false},
	}
	assert.Equal(t, "- Build: no evidence", frontendBuildEvidenceLine(toolCalls))
}

func TestLooksLikeBackendFullstackTaskDoesNotClassifyGenericCrudFrontendPrompt(t *testing.T) {
	assert.False(t, looksLikeBackendFullstackTask("Create a React Vite CRUD app"))
	assert.True(t, looksLikeBackendFullstackTask("Create a backend CRUD API with sqlite"))
}

func TestIsFileModificationRecognizesExtendedFileOperations(t *testing.T) {
	for _, op := range []string{"delete", "create", "rename", "move", "append"} {
		tc := output.ToolCallLog{Name: "file", Input: json.RawMessage(`{"operation":"` + op + `"}`)}
		assert.True(t, isFileModification(tc), "operation %s should be considered edit", op)
	}
}

func TestBackendAPIEvidenceAcceptsSingleQuotedPythonTranscript(t *testing.T) {
	tc := output.ToolCallLog{
		Name:   "shell",
		Input:  json.RawMessage(`{"command":"python3 - << 'PY' ... PY"}`),
		Result: "POST /todos 201 {'id':1,'title':'demo'}\nFinal /stats 200 {'total':0}\nGET /todos 200",
	}
	assert.True(t, backendAPIEvidenceInToolCall(tc))
}

func TestHeadlessRunnerBackendTaskRequiresAPIRoundTripForToolOnlySuccess(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "file", Input: []byte(`{"operation":"write","path":"schema.sql","content":"CREATE TABLE todos(id integer primary key);"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "file", Content: "wrote schema.sql", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t2", Name: "shell", Input: []byte(`{"command":"go mod tidy"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t2", Name: "shell", Content: "go: downloading modernc.org/sqlite v1.35.0", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t3", Name: "shell", Input: []byte(`{"command":"go build ./..."}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t3", Name: "shell", Content: "build succeeded", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t4", Name: "process", Input: []byte(`{"operation":"read_output","process_id":"abc"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t4", Name: "process", Content: "Server listening on :8080", IsError: false,
			}},
			agent.TurnEvent{Type: "error", Error: errMaxTurnsExceededStub{}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a fullstack todo application with a Go backend, SQLite, and a frontend", "generic")
	require.NoError(t, err)

	assert.Contains(t, result.Error, "max turns")
	assert.Contains(t, result.EvidenceSummary, "- Verification verdict: failed")
	assert.Contains(t, result.EvidenceSummary, "- Reason: no API round-trip evidence")
	assert.Contains(t, result.EvidenceSummary, "- Dependency resolution: passed")
	assert.Contains(t, result.EvidenceSummary, "- Schema/init: observed")
	assert.Contains(t, result.EvidenceSummary, "- API round-trip: no evidence")
}

func TestHeadlessRunnerBackendVerificationInvalidatedByLaterEdit(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "shell", Input: []byte(`{"command":"npm install express better-sqlite3"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "shell", Content: "added 102 packages", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t2", Name: "file", Input: []byte(`{"operation":"write","path":"schema.sql","content":"CREATE TABLE todos(id integer primary key, title text, completed integer);"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t2", Name: "file", Content: "wrote schema.sql", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t3", Name: "process", Input: []byte(`{"operation":"exec","command":"node index.js"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t3", Name: "process", Content: "Todo API server listening on http://localhost:3000", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t4", Name: "shell", Input: []byte(`{"command":"curl -s -X POST http://localhost:3000/todos -H 'Content-Type: application/json' -d '{\"title\":\"Test Todo\"}' && curl -s http://localhost:3000/todos"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t4", Name: "shell", Content: "{\"id\":1,\"title\":\"Test Todo\",\"completed\":false}\n[{\"id\":1,\"title\":\"Test Todo\",\"completed\":false}]", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t5", Name: "file", Input: []byte(`{"operation":"patch","path":"index.js","old_string":"3000","new_string":"3001"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t5", Name: "file", Content: "patched index.js", IsError: false,
			}},
			agent.TurnEvent{Type: "error", Error: errMaxTurnsExceededStub{}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a backend-only todo API using Node.js and SQLite", "generic")
	require.NoError(t, err)

	assert.Contains(t, result.EvidenceSummary, "- Verification verdict: failed")
	assert.Contains(t, result.EvidenceSummary, "- Reason: a previously successful backend verification was invalidated by later file edits")
}

func TestHeadlessRunnerBuildsBackendEvidenceSummary(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "file", Input: []byte(`{"operation":"write","path":"schema.sql","content":"CREATE TABLE todos(id integer primary key, title text, completed integer);"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "file", Content: "wrote schema.sql", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t2", Name: "shell", Input: []byte(`{"command":"go mod tidy"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t2", Name: "shell", Content: "go: downloading modernc.org/sqlite v1.35.0", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t3", Name: "shell", Input: []byte(`{"command":"go build ./..."}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t3", Name: "shell", Content: "build succeeded", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t4", Name: "process", Input: []byte(`{"operation":"read_output","process_id":"abc"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t4", Name: "process", Content: "Server listening on :8080", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t5", Name: "shell", Input: []byte(`{"command":"curl -i http://localhost:8080/api/todos && curl -i -X POST http://localhost:8080/api/todos -d '{\"title\":\"demo\"}'"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t5", Name: "shell", Content: "HTTP/1.1 200 OK\n[]\nHTTP/1.1 201 Created\n{\"id\":1,\"title\":\"demo\",\"completed\":false}", IsError: false,
			}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a fullstack todo application with a Go backend, SQLite, and a frontend", "generic")
	require.NoError(t, err)

	assert.Contains(t, result.EvidenceSummary, "- Verification verdict: passed")
	assert.Contains(t, result.EvidenceSummary, "- Validation: passed")
	assert.Contains(t, result.EvidenceSummary, "- Dependency resolution: passed")
	assert.Contains(t, result.EvidenceSummary, "- Schema/init: observed")
	assert.Contains(t, result.EvidenceSummary, "- Runtime: server started")
	assert.Contains(t, result.EvidenceSummary, "- API round-trip: observed")
}

func TestHeadlessRunnerAllowsBackendPassFromRuntimeAndAPIRoundTrip(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "shell", Input: []byte(`{"command":"npm install express better-sqlite3"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "shell", Content: "added 102 packages", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t2", Name: "file", Input: []byte(`{"operation":"write","path":"schema.sql","content":"CREATE TABLE IF NOT EXISTS todos (id integer primary key, title text, completed integer);"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t2", Name: "file", Content: "wrote schema.sql", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t3", Name: "process", Input: []byte(`{"operation":"exec","command":"node index.js"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t3", Name: "process", Content: "Todo API server listening on http://localhost:3000", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t4", Name: "shell", Input: []byte(`{"command":"curl -s -X POST http://localhost:3000/todos -H 'Content-Type: application/json' -d '{\"title\":\"Test Todo\"}' && curl -s http://localhost:3000/todos && sqlite3 todos.db 'SELECT * FROM todos;'"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t4", Name: "shell", Content: "{\"id\":1,\"title\":\"Test Todo\",\"completed\":false}\n[{\"id\":1,\"title\":\"Test Todo\",\"completed\":false}]\n1|Test Todo|0", IsError: false,
			}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a backend-only todo API using Node.js and SQLite", "generic")
	require.NoError(t, err)

	assert.Contains(t, result.EvidenceSummary, "- Verification verdict: passed")
	assert.Contains(t, result.EvidenceSummary, "- Validation: runtime/API verification only")
	assert.Contains(t, result.EvidenceSummary, "- Dependency resolution: passed")
	assert.Contains(t, result.EvidenceSummary, "- API round-trip: observed")
}

func TestHeadlessRunnerRecognizesMavenCommandsWithFlags(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "shell", Input: []byte(`{"command":"mvn -B -q compile"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "shell", Content: "BUILD SUCCESS", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t2", Name: "file", Input: []byte(`{"operation":"write","path":"src/main/java/com/example/todo/TodoServer.java","content":"CREATE TABLE IF NOT EXISTS todos"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t2", Name: "file", Content: "wrote TodoServer.java", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t3", Name: "shell", Input: []byte(`{"command":"mvn -B -q package"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t3", Name: "shell", Content: "BUILD SUCCESS", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t4", Name: "process", Input: []byte(`{"operation":"exec","command":"mvn -B -q exec:java"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t4", Name: "process", Content: "Server listening on http://localhost:4567", IsError: false,
			}},
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t5", Name: "shell", Input: []byte(`{"command":"curl -s -X POST http://localhost:4567/todos -H 'Content-Type: application/json' -d '{\"title\":\"Test Todo\"}' && curl -s http://localhost:4567/todos"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t5", Name: "shell", Content: "{\"id\":1,\"title\":\"Test Todo\",\"completed\":false}\n[{\"id\":1,\"title\":\"Test Todo\",\"completed\":false}]", IsError: false,
			}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a backend-only todo API using Java and SQLite", "generic")
	require.NoError(t, err)

	assert.Contains(t, result.EvidenceSummary, "- Verification verdict: passed")
	assert.Contains(t, result.EvidenceSummary, "- Validation: passed")
	assert.Contains(t, result.EvidenceSummary, "- Dependency resolution: passed")
	assert.Contains(t, result.EvidenceSummary, "- Runtime: server started")
	assert.Contains(t, result.EvidenceSummary, "- API round-trip: observed")
}

func TestHeadlessRunnerSummarizesBackendValidationFailure(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "shell", Input: []byte(`{"command":"go get modernc.org/sqlite@v1.16.2"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "shell", Content: "go: modernc.org/sqlite@v1.16.2: unknown revision v1.16.2", IsError: true,
			}},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "Create a fullstack todo application with a Go backend, SQLite, and a frontend", "generic")
	require.NoError(t, err)

	assert.Contains(t, result.Summary, "backend validation failure")
	assert.Contains(t, result.Summary, "unknown revision v1.16.2")
	assert.Contains(t, result.EvidenceSummary, "- Verification verdict: failed")
}

func TestIsDependencyResolutionCommandRecognizesNpmCI(t *testing.T) {
	tc := output.ToolCallLog{
		Name:  "shell",
		Input: json.RawMessage(`{"command":"npm ci"}`),
	}
	assert.True(t, isDependencyResolutionCommand(tc))
}

type errMaxTurnsExceededStub struct{}

func (errMaxTurnsExceededStub) Error() string {
	return "max turns (12) exceeded"
}
