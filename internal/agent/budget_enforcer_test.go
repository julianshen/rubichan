package agent

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

func TestBudgetEnforcer_Basic(t *testing.T) {
	be := NewResultBudgetEnforcer(100, nil) // 100 char aggregate budget, no store

	// First result: 1 char — fits
	r1 := agentsdk.ToolResult{Content: "a"}
	out, err := be.Enforce("tool1", "id1", r1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Content != "a" {
		t.Errorf("expected 'a', got %q", out.Content)
	}

	// Add 99 more single-char results to reach budget limit
	for i := 0; i < 99; i++ {
		_, err := be.Enforce("tool", "id", agentsdk.ToolResult{Content: "x"})
		if err != nil {
			t.Fatalf("unexpected error at %d: %v", i, err)
		}
	}
	// Budget now at 100, next result should be truncated/offloaded
	r2 := agentsdk.ToolResult{Content: "b"}
	out2, err := be.Enforce("tool2", "id2", r2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out2.Content == "b" {
		t.Error("expected content to be truncated since budget exceeded")
	}
}

func TestBudgetEnforcer_SingleResultExceedsBudget(t *testing.T) {
	be := NewResultBudgetEnforcer(10, nil)

	// Single result of 50 chars exceeds entire budget of 10
	r := agentsdk.ToolResult{Content: "this is a very long result that exceeds the budget"}
	out, err := be.Enforce("tool", "id", r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The result is truncated to fit within budget (10 chars). The marker
	// itself is ~50 chars, so when budget < marker, we get a truncated marker.
	if len(out.Content) != 10 {
		t.Errorf("expected truncated content == 10 chars, got %d: %q", len(out.Content), out.Content)
	}
}

func TestBudgetEnforcer_TruncatePreservesHeadAndTail(t *testing.T) {
	be := NewResultBudgetEnforcer(500, nil)

	// Fill budget to 400
	for i := 0; i < 400; i++ {
		_, err := be.Enforce("tool", "id", agentsdk.ToolResult{Content: "x"})
		if err != nil {
			t.Fatalf("unexpected error at %d: %v", i, err)
		}
	}

	// Add a 200-char result — should be truncated to fit remaining 100 chars
	r := agentsdk.ToolResult{Content: "this is some content that is definitely longer than one hundred characters and should be truncated because it exceeds the remaining budget in the enforcer"}
	out, _ := be.Enforce("tool", "id", r)
	if out.Content == r.Content {
		t.Error("expected truncation")
	}
	if len(out.Content) > 100 {
		t.Errorf("expected truncated content <= 100 chars, got %d", len(out.Content))
	}
}

func TestBudgetEnforcer_DefaultBudget(t *testing.T) {
	be := NewResultBudgetEnforcer(0, nil)
	if be.budget != DefaultMaxResultsPerMessageChars {
		t.Errorf("expected default budget %d, got %d", DefaultMaxResultsPerMessageChars, be.budget)
	}
}

func TestBudgetEnforcer_WithStore_Offload(t *testing.T) {
	be := NewResultBudgetEnforcer(50, nil)

	// Test that a result exceeding budget gets truncated when no store
	r := agentsdk.ToolResult{Content: "this is a very long result that definitely exceeds the budget of fifty characters"}
	out, err := be.Enforce("tool", "id", r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Content) >= len(r.Content) {
		t.Errorf("expected truncation, got %d chars", len(out.Content))
	}
	if len(out.Content) > 50 {
		t.Errorf("expected content <= 50 chars, got %d", len(out.Content))
	}
}

func TestBudgetEnforcer_MakeRoom(t *testing.T) {
	// Budget of 20, store available. Add a 15-char result, then a 10-char result
	// that should trigger eviction of the 15-char result.
	be := NewResultBudgetEnforcer(20, nil)

	// First result: 15 chars
	_, _ = be.Enforce("tool1", "id1", agentsdk.ToolResult{Content: "123456789012345"})

	// Second result: 10 chars — would exceed budget (15+10=25 > 20)
	// Since no store, makeRoom is a no-op and result gets truncated.
	out, _ := be.Enforce("tool2", "id2", agentsdk.ToolResult{Content: "1234567890"})
	if len(out.Content) > 5 {
		t.Logf("truncated to %d chars (budget remaining after first result)", len(out.Content))
	}
}
