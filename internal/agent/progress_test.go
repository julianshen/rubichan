package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProgressTracker_Record(t *testing.T) {
	pt := NewProgressTracker()

	pt.Record(1, "wrote file", "src/main.go", "ok")
	pt.Record(1, "ran command", "go test ./...", "ok")
	pt.Record(2, "searched", "TODO", "12 matches")

	entries := pt.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	if entries[0].Turn != 1 || entries[0].Action != "wrote file" || entries[0].Detail != "src/main.go" || entries[0].Result != "ok" {
		t.Errorf("entry 0 mismatch: %+v", entries[0])
	}
	if entries[2].Action != "searched" || entries[2].Detail != "TODO" || entries[2].Result != "12 matches" {
		t.Errorf("entry 2 mismatch: %+v", entries[2])
	}
}

func TestProgressTracker_MaxEntries(t *testing.T) {
	pt := NewProgressTracker()

	// Record 60 entries — should trim to 50 (the last 50).
	for i := 0; i < 60; i++ {
		pt.Record(i, "action", "detail", "ok")
	}

	entries := pt.Entries()
	if len(entries) != 50 {
		t.Fatalf("expected 50 entries after trimming, got %d", len(entries))
	}

	// The oldest entry should be turn 10 (entries 0-9 were trimmed).
	if entries[0].Turn != 10 {
		t.Errorf("expected oldest entry turn=10, got %d", entries[0].Turn)
	}
	if entries[49].Turn != 59 {
		t.Errorf("expected newest entry turn=59, got %d", entries[49].Turn)
	}
}

func TestProgressTracker_Render_Empty(t *testing.T) {
	pt := NewProgressTracker()
	if got := pt.Render(); got != "" {
		t.Errorf("expected empty string for no entries, got %q", got)
	}
}

func TestProgressTracker_Render(t *testing.T) {
	pt := NewProgressTracker()
	pt.Record(1, "wrote file", "src/main.go", "ok")
	pt.Record(2, "ran command", "go test ./...", "ok")
	pt.Record(3, "searched", `"TODO" in internal/`, "12 matches")

	rendered := pt.Render()

	// Should have header row.
	if !strings.Contains(rendered, "| # | Action | Detail | Result |") {
		t.Error("missing header row")
	}
	// Should have separator.
	if !strings.Contains(rendered, "|---|--------|--------|--------|") {
		t.Error("missing separator row")
	}
	// Should have data rows.
	if !strings.Contains(rendered, "| 1 | wrote file | src/main.go | ok |") {
		t.Error("missing entry 1")
	}
	if !strings.Contains(rendered, "| 2 | ran command | go test ./... | ok |") {
		t.Error("missing entry 2")
	}
	if !strings.Contains(rendered, "| 3 | searched |") {
		t.Error("missing entry 3")
	}
}

func TestProgressTracker_Render_EscapesPipes(t *testing.T) {
	pt := NewProgressTracker()
	pt.Record(1, "ran command", "echo foo | bar", "ok")

	rendered := pt.Render()
	if !strings.Contains(rendered, `echo foo \| bar`) {
		t.Errorf("pipes in detail not escaped: %s", rendered)
	}
}

func TestClassifyToolAction_File(t *testing.T) {
	tests := []struct {
		op     string
		path   string
		action string
		detail string
	}{
		{"write", "/tmp/main.go", "wrote file", "/tmp/main.go"},
		{"read", "/tmp/config.yaml", "read file", "/tmp/config.yaml"},
		{"patch", "/tmp/handler.go", "patched file", "/tmp/handler.go"},
		{"delete", "/tmp/old.txt", "file op", "/tmp/old.txt"},
	}

	for _, tt := range tests {
		input, _ := json.Marshal(map[string]string{"operation": tt.op, "path": tt.path})
		action, detail := classifyToolAction("file", input)
		if action != tt.action {
			t.Errorf("op=%s: expected action=%q, got %q", tt.op, tt.action, action)
		}
		if detail != tt.detail {
			t.Errorf("op=%s: expected detail=%q, got %q", tt.op, tt.detail, detail)
		}
	}
}

func TestClassifyToolAction_Shell(t *testing.T) {
	// Short command.
	input, _ := json.Marshal(map[string]string{"command": "go test ./..."})
	action, detail := classifyToolAction("shell", input)
	if action != "ran command" {
		t.Errorf("expected action='ran command', got %q", action)
	}
	if detail != "go test ./..." {
		t.Errorf("expected detail='go test ./...', got %q", detail)
	}

	// Long command should be truncated at 80 chars.
	longCmd := strings.Repeat("x", 100)
	input, _ = json.Marshal(map[string]string{"command": longCmd})
	_, detail = classifyToolAction("shell", input)
	if len(detail) != 83 { // 80 + "..."
		t.Errorf("expected truncated detail of length 83, got %d", len(detail))
	}
	if !strings.HasSuffix(detail, "...") {
		t.Error("truncated command should end with ...")
	}
}

func TestClassifyToolAction_Search(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"pattern": "TODO"})
	action, detail := classifyToolAction("search", input)
	if action != "searched" {
		t.Errorf("expected action='searched', got %q", action)
	}
	if detail != "TODO" {
		t.Errorf("expected detail='TODO', got %q", detail)
	}
}

func TestClassifyToolAction_Task(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"description": "refactor the handler"})
	action, detail := classifyToolAction("task", input)
	if action != "spawned task" {
		t.Errorf("expected 'spawned task', got %q", action)
	}
	if detail != "refactor the handler" {
		t.Errorf("expected detail, got %q", detail)
	}
}

func TestClassifyToolAction_TaskComplete(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"summary": "done"})
	action, detail := classifyToolAction("task_complete", input)
	if action != "completed task" {
		t.Errorf("expected 'completed task', got %q", action)
	}
	if detail != "done" {
		t.Errorf("expected 'done', got %q", detail)
	}
}

func TestClassifyToolAction_Unknown(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"foo": "bar"})
	action, detail := classifyToolAction("custom_tool", input)
	if action != "custom_tool" {
		t.Errorf("expected action='custom_tool', got %q", action)
	}
	if detail != "" {
		t.Errorf("expected empty detail for unknown tool, got %q", detail)
	}
}

func TestClassifyToolAction_InvalidJSON(t *testing.T) {
	action, detail := classifyToolAction("file", json.RawMessage(`not json`))
	if action != "file" {
		t.Errorf("expected fallback action='file', got %q", action)
	}
	if detail != "" {
		t.Errorf("expected empty detail for invalid JSON, got %q", detail)
	}
}

func TestTruncateResult(t *testing.T) {
	if got := truncateResult("short", 60); got != "short" {
		t.Errorf("expected 'short', got %q", got)
	}
	long := strings.Repeat("a", 100)
	got := truncateResult(long, 60)
	if len(got) != 63 { // 60 + "..."
		t.Errorf("expected length 63, got %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Error("truncated result should end with ...")
	}
}
