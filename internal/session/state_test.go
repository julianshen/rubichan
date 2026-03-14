package session

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
)

func TestStateBuildVerificationSnapshotPassed(t *testing.T) {
	s := NewState()
	s.ResetForPrompt("Create a backend-only todo API using Node.js and SQLite")
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t1",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"npm install express better-sqlite3"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t1",
			Name:    "shell",
			Content: "added 10 packages",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t2",
			Name:  "file",
			Input: json.RawMessage(`{"operation":"write","path":"schema.sql"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t2",
			Name:    "file",
			Content: "CREATE TABLE todos (id integer primary key, title text, completed integer)",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t3",
			Name:  "process",
			Input: json.RawMessage(`{"operation":"exec","command":"node index.js"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t3",
			Name:    "process",
			Content: "Todo API server listening on http://localhost:3000",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t4",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"curl -s -X POST http://localhost:3000/todos && curl -s http://localhost:3000/stats"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t4",
			Name:    "shell",
			Content: "{\"id\":1,\"title\":\"Test Todo\",\"completed\":false}\n{\"total\":1,\"completed\":0,\"pending\":1}",
		},
	})

	summary := s.BuildVerificationSnapshot()
	assert.Contains(t, summary, "verdict: passed")
	assert.Contains(t, summary, "api round-trip: true")
}

func TestStateBuildVerificationSnapshotInvalidated(t *testing.T) {
	s := NewState()
	s.ResetForPrompt("Create a backend-only todo API using Node.js and SQLite")
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t1",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"npm install express better-sqlite3"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t1",
			Name:    "shell",
			Content: "added 10 packages",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t2",
			Name:  "file",
			Input: json.RawMessage(`{"operation":"write","path":"schema.sql"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t2",
			Name:    "file",
			Content: "CREATE TABLE todos (id integer primary key, title text, completed integer)",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t3",
			Name:  "process",
			Input: json.RawMessage(`{"operation":"exec","command":"node index.js"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t3",
			Name:    "process",
			Content: "Todo API server listening on http://localhost:3000",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t4",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"curl -s -X POST http://localhost:3000/todos && curl -s http://localhost:3000/todos"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t4",
			Name:    "shell",
			Content: "{\"id\":1,\"title\":\"Test Todo\",\"completed\":false}\n[{\"id\":1,\"title\":\"Test Todo\",\"completed\":false}]",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t5",
			Name:  "file",
			Input: json.RawMessage(`{"operation":"patch","path":"index.js"}`),
		},
	})

	summary := s.BuildVerificationSnapshot()
	assert.Contains(t, summary, "verdict: failed")
	assert.Contains(t, summary, "invalidated by later edits")
}

func TestStateBuildVerificationSnapshotTreatsProcessRunAsRuntimeEvidence(t *testing.T) {
	s := NewState()
	s.ResetForPrompt("Build and fully verify a more complex Node.js + SQLite todo backend")
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t1",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"npm install"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t1",
			Name:    "shell",
			Content: "added 154 packages, and audited 155 packages in 1s",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t2",
			Name:  "file",
			Input: json.RawMessage(`{"operation":"read","path":"db.js"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t2",
			Name:    "file",
			Content: "CREATE TABLE IF NOT EXISTS todos (id integer primary key, title text, completed integer)",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t3",
			Name:  "process",
			Input: json.RawMessage(`{"operation":"exec","command":"node server.js"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t3",
			Name:    "process",
			Content: "process_id: 777a76c5\nTodo API listening on port 3000",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t4",
			Name:  "process",
			Input: json.RawMessage(`{"operation":"read_output","process_id":"777a76c5"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t4",
			Name:    "process",
			Content: "status: running\nTodo API listening on port 3000",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t5",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"curl -s -X POST http://127.0.0.1:3000/todos -H 'Content-Type: application/json' -d '{\"title\":\"Buy milk\",\"priority\":2}'"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t5",
			Name:    "shell",
			Content: "{\"id\":1,\"title\":\"Buy milk\",\"completed\":0}",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t6",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"curl -s 'http://127.0.0.1:3000/stats'"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t6",
			Name:    "shell",
			Content: "{\"total\":1,\"completed\":0,\"pending\":1}",
		},
	})

	summary := s.BuildVerificationSnapshot()
	assert.Contains(t, summary, "verdict: passed")
	assert.Contains(t, summary, "runtime: true")
	assert.Contains(t, summary, "api round-trip: true")
}

func TestStateBuildVerificationSnapshotRecognizesNpmCiAndPythonAPIEvidence(t *testing.T) {
	s := NewState()
	s.ResetForPrompt("Run backend verification for a Node.js SQLite todo API")
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t1",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"npm ci"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t1",
			Name:    "shell",
			Content: "added 154 packages, and audited 155 packages in 1s",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t2",
			Name:  "file",
			Input: json.RawMessage(`{"operation":"read","path":"db.js"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t2",
			Name:    "file",
			Content: "db.all(\"PRAGMA table_info('todos')\")\nCREATE TABLE IF NOT EXISTS todos (...)",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t3",
			Name:  "process",
			Input: json.RawMessage(`{"operation":"exec","command":"node server.js"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t3",
			Name:    "process",
			Content: "Todo API listening on port 3000",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t4",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"python3 - << 'PY' ... PY"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t4",
			Name:    "shell",
			Content: "POST /todos 201 {'id':1,'title':'demo'}\nFinal /stats 200 {'total':0}\nGET /todos 200",
		},
	})

	summary := s.BuildVerificationSnapshot()
	assert.Contains(t, summary, "gate: pass")
	assert.Contains(t, summary, "dependency resolution: true")
	assert.Contains(t, summary, "api round-trip: true")
}

func TestStatePlanTransitionsToCompleted(t *testing.T) {
	s := NewState()
	s.ResetForPrompt("Verify this backend SQLite todo API end to end")
	assert.Len(t, s.Plan(), 1)
	assert.Equal(t, PlanStatusInProgress, s.Plan()[0].Status)

	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t1",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"python3 -m pip install -r requirements.txt"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t1",
			Name:    "shell",
			Content: "Successfully installed requirements",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t2",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"python3 init_schema.py"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t2",
			Name:    "shell",
			Content: "Database initialized",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t3",
			Name:  "process",
			Input: json.RawMessage(`{"command":"python3 -m uvicorn main:app --host 127.0.0.1 --port 8010"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t3",
			Name:    "process",
			Content: "status: running",
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t4",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"curl -i http://127.0.0.1:8010/stats"}`),
		},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "t4",
			Name:    "shell",
			Content: "HTTP/1.1 200 OK\n{\"total\":3,\"completed\":2,\"active\":1}",
		},
	})

	_ = s.BuildVerificationSnapshot()
	assert.Equal(t, PlanStatusCompleted, s.Plan()[0].Status)
}

func TestStatePlanTransitionsToReverifyRequiredAfterEdit(t *testing.T) {
	s := NewState()
	s.ResetForPrompt("Verify this backend SQLite todo API end to end")
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{ID: "t1", Name: "shell", Input: json.RawMessage(`{"command":"npm install"}`)},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{ID: "t1", Name: "shell", Content: "added 10 packages"},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{ID: "t2", Name: "file", Input: json.RawMessage(`{"operation":"write","path":"schema.sql"}`)},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{ID: "t2", Name: "file", Content: "CREATE TABLE todos (id integer primary key, title text, completed integer)"},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{ID: "t3", Name: "process", Input: json.RawMessage(`{"command":"node server.js"}`)},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{ID: "t3", Name: "process", Content: "status: running"},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{ID: "t4", Name: "shell", Input: json.RawMessage(`{"command":"curl -i http://localhost:3000/stats"}`)},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{ID: "t4", Name: "shell", Content: "HTTP/1.1 200 OK\n{\"total\":1}"},
	})
	_ = s.BuildVerificationSnapshot()
	assert.Equal(t, PlanStatusCompleted, s.Plan()[0].Status)

	// Any later edit should invalidate verification and require re-verification.
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{ID: "t5", Name: "file", Input: json.RawMessage(`{"operation":"patch","path":"server.js"}`)},
	})
	_ = s.BuildVerificationSnapshot()
	assert.Equal(t, PlanStatusReverifyRequired, s.Plan()[0].Status)
}

func TestStateBuildVerificationSnapshotSchemaMissingIsSoftFail(t *testing.T) {
	s := NewState()
	s.ResetForPrompt("Verify this backend SQLite todo API end to end")
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{ID: "t1", Name: "shell", Input: json.RawMessage(`{"command":"npm install"}`)},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{ID: "t1", Name: "shell", Content: "added 10 packages"},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{ID: "t2", Name: "process", Input: json.RawMessage(`{"operation":"exec","command":"node server.js"}`)},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{ID: "t2", Name: "process", Content: "status: running"},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{ID: "t3", Name: "shell", Input: json.RawMessage(`{"command":"curl -i http://localhost:3000/stats"}`)},
	})
	s.ApplyEvent(agentsdk.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{ID: "t3", Name: "shell", Content: "HTTP/1.1 200 OK\n{\"total\":1}"},
	})

	snapshot := s.BuildVerificationSnapshot()
	assert.Contains(t, snapshot, "gate: soft_fail")
	assert.Contains(t, snapshot, "verdict: passed_with_warnings")
	assert.Contains(t, snapshot, "missing schema/init evidence")
	assert.Equal(t, PlanStatusCompleted, s.Plan()[0].Status)
}
