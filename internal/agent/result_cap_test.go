package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// stubTool is the minimal agentsdk.Tool implementation shared by
// applyResultCap's test fixtures. Tests layer additional methods on
// embedded stubTool to opt into extension interfaces like ResultCapped.
type stubTool struct{ name string }

func (s stubTool) Name() string                 { return s.name }
func (s stubTool) Description() string          { return "" }
func (s stubTool) InputSchema() json.RawMessage { return nil }
func (s stubTool) Execute(context.Context, json.RawMessage) (agentsdk.ToolResult, error) {
	return agentsdk.ToolResult{}, nil
}

// fakeCappedTool implements agentsdk.ResultCapped on top of stubTool.
type fakeCappedTool struct {
	stubTool
	capBytes int
}

func (t *fakeCappedTool) MaxResultBytes() int { return t.capBytes }

// plainTool satisfies agentsdk.Tool but does not implement ResultCapped — it is exempt.
type plainTool struct{ stubTool }

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
	// Use distinct head/tail sentinels so a head-only regression would
	// be caught — a plain strings.Repeat("a", ...) payload tautologically
	// passes HasPrefix/HasSuffix checks even if the tail was dropped.
	head := "HEAD_SENTINEL_"
	tail := "_TAIL_SENTINEL"
	body := strings.Repeat("x", 10000)
	big := head + body + tail

	res := agentsdk.ToolResult{Content: big}
	capped := applyResultCap(&fakeCappedTool{capBytes: 2000}, res)

	if len(capped.Content) >= len(big) {
		t.Fatalf("content not truncated, got %d bytes (original %d)", len(capped.Content), len(big))
	}
	if !strings.Contains(capped.Content, "truncated") {
		t.Fatalf("truncation marker missing from: %q", capped.Content)
	}
	if !strings.Contains(capped.Content, head) {
		t.Fatalf("head sentinel missing from truncated content — head slice regression?")
	}
	if !strings.Contains(capped.Content, tail) {
		t.Fatalf("tail sentinel missing from truncated content — tail slice regression (head-only truncation)?")
	}
}

func TestApplyResultCapNilToolIsNoOp(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10000)
	res := agentsdk.ToolResult{Content: big}
	var tool agentsdk.Tool // typed nil interface
	capped := applyResultCap(tool, res)
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
