package permissions

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

func TestYOLOClassifier_SafeToolBypass(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0) // no provider needed for bypass

	// read_file is in the safe-tool allowlist
	result, err := c.Classify("read_file", map[string]interface{}{"path": "/etc/passwd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != agentsdk.AutoApproved {
		t.Errorf("expected AutoApproved for safe tool, got %v", result)
	}
}

func TestYOLOClassifier_UnsafeToolWithoutProvider(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)

	result, err := c.Classify("write_file", map[string]interface{}{"path": "/etc/passwd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != agentsdk.ApprovalRequired {
		t.Errorf("expected ApprovalRequired without provider, got %v", result)
	}
}

func TestYOLOClassifier_ConsecutiveDenialFallback(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	c.SetMaxConsecutiveDenials(3)

	// First 3 denials: classifier returns ApprovalRequired.
	for i := 0; i < 3; i++ {
		result, _ := c.Classify("shell", nil)
		if result != agentsdk.ApprovalRequired {
			t.Errorf("iteration %d: expected ApprovalRequired, got %v", i, result)
		}
	}

	// 4th call: consecutive denials == 3 == max, so fallback triggers.
	result, _ := c.Classify("shell", nil)
	if result != agentsdk.ApprovalRequired {
		t.Errorf("iteration 3: expected ApprovalRequired (fallback), got %v", result)
	}
}

func TestYOLOClassifier_ResetDenialsOnSafeTool(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)
	c.SetMaxConsecutiveDenials(3)

	// 2 denials
	_, _ = c.Classify("shell", nil)
	_, _ = c.Classify("shell", nil)

	// Safe tool resets counter
	_, _ = c.Classify("read_file", nil)

	// Another 2 denials — should not trigger fallback since counter was reset
	_, _ = c.Classify("shell", nil)
	result, _ := c.Classify("shell", nil)
	if result != agentsdk.ApprovalRequired {
		t.Errorf("expected ApprovalRequired after reset, got %v", result)
	}
}

func TestYOLOClassifier_Stage1Heuristics(t *testing.T) {
	c := NewYOLOClassifier(nil, 0, 0)

	// Tools with "write" in name should be uncertain -> ApprovalRequired
	result, _ := c.Classify("write_file", nil)
	if result != agentsdk.ApprovalRequired {
		t.Errorf("expected ApprovalRequired for write tool, got %v", result)
	}

	// Tools with "shell" in name should be uncertain -> ApprovalRequired
	result, _ = c.Classify("shell", nil)
	if result != agentsdk.ApprovalRequired {
		t.Errorf("expected ApprovalRequired for shell tool, got %v", result)
	}

	// Unknown tools without dangerous keywords should be safe
	result, _ = c.Classify("some_info_tool", nil)
	if result != agentsdk.AutoApproved {
		t.Errorf("expected AutoApproved for unknown safe tool, got %v", result)
	}
}
