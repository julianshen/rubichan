package agent

import (
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// fakeCappedTool implements agentsdk.ResultCapped with a configurable byte cap.
// It only has the methods the applyResultCap helper needs — not the full Tool interface.
type fakeCappedTool struct {
	capBytes int
}

func (t *fakeCappedTool) MaxResultBytes() int { return t.capBytes }

// plainTool does not implement ResultCapped — it is exempt.
type plainTool struct{}

func TestApplyResultCapBelowCap(t *testing.T) {
	t.Parallel()
	res := agentsdk.ToolResult{Content: "hello"}
	capped := applyResultCap(&fakeCappedTool{capBytes: 100}, res)
	if capped.Content != "hello" {
		t.Fatalf("unexpected content: %q", capped.Content)
	}
}

func TestApplyResultCapAboveCapTruncatesAndMarks(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	capped := applyResultCap(&fakeCappedTool{capBytes: 500}, res)
	if len(capped.Content) > 600 {
		t.Fatalf("content not truncated, got %d bytes", len(capped.Content))
	}
	if !strings.Contains(capped.Content, "truncated") {
		t.Fatalf("truncation marker missing from: %q", capped.Content)
	}
	// Head and tail should both be preserved (both are 'a's, but we
	// verify via length that we didn't just keep the head.
	if !strings.HasPrefix(capped.Content, "a") || !strings.HasSuffix(capped.Content, "a") {
		t.Fatalf("head or tail missing")
	}
}

func TestApplyResultCapNilToolIsNoOp(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	capped := applyResultCap(nil, res)
	if capped.Content != big {
		t.Fatalf("nil tool should be no-op")
	}
}

func TestApplyResultCapUncappedToolIsNoOp(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	capped := applyResultCap(plainTool{}, res)
	if capped.Content != big {
		t.Fatalf("plain tool should be exempt")
	}
}

func TestApplyResultCapZeroCapIsNoOp(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	capped := applyResultCap(&fakeCappedTool{capBytes: 0}, res)
	if capped.Content != big {
		t.Fatalf("cap=0 should be exempt (opt-out)")
	}
}

func TestApplyResultCapNegativeCapIsNoOp(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	capped := applyResultCap(&fakeCappedTool{capBytes: -1}, res)
	if capped.Content != big {
		t.Fatalf("negative cap should be exempt")
	}
}

func TestApplyResultCapTinyCapKeepsHeadOnly(t *testing.T) {
	t.Parallel()
	// When the cap is too small to split head+tail around the marker,
	// the helper should still truncate without crashing. The exact
	// behavior (head-only vs head+tail) is an impl detail — but the
	// result must be smaller than the original and contain the marker.
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	capped := applyResultCap(&fakeCappedTool{capBytes: 80}, res)
	if len(capped.Content) >= len(big) {
		t.Fatalf("tiny cap should still shrink content")
	}
	if !strings.Contains(capped.Content, "truncated") {
		t.Fatalf("truncation marker missing")
	}
}

// TestShellToolReportsCap guards against a future refactor accidentally
// removing the shell tool's ResultCapped opt-in. The shell tool must
// implement agentsdk.ResultCapped with a positive byte cap.
func TestShellToolReportsCap(t *testing.T) {
	t.Parallel()
	st := tools.NewShellTool(t.TempDir(), 0)
	capped, ok := interface{}(st).(agentsdk.ResultCapped)
	if !ok {
		t.Fatalf("shell tool does not implement agentsdk.ResultCapped")
	}
	if capped.MaxResultBytes() <= 0 {
		t.Fatalf("shell tool cap must be positive, got %d", capped.MaxResultBytes())
	}
}
