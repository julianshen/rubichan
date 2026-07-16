package agentsdk

import "encoding/json"

// StreamAccumulator is the state machine that assembles a provider stream
// into assistant content blocks and pending tool calls. Both the SDK agent
// loop and the internal agent loop share this accumulation logic; loop-
// specific behavior plugs in via KeepText and OnToolFinalized.
//
// The zero value is not usable; construct with NewStreamAccumulator.
// It is not safe for concurrent use — a stream is consumed by one goroutine.
type StreamAccumulator struct {
	blocks       []ContentBlock
	pendingTools []ToolUseBlock
	textBuf      string
	currentTool  *ToolUseBlock
	toolInputBuf string

	// KeepText decides whether accumulated text is committed as a content
	// block when finalized. Defaults to keeping any non-empty string.
	KeepText func(string) bool

	// OnToolFinalized fires after a tool_use block is committed to Blocks
	// and PendingTools — the seam for mid-stream tool dispatch. May be nil.
	OnToolFinalized func(tc ToolUseBlock)
}

// NewStreamAccumulator creates an empty accumulator.
func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{
		KeepText: func(s string) bool { return s != "" },
	}
}

// AddText routes a text delta. During tool accumulation, text deltas carry
// the tool's input JSON rather than user-visible text; the return value
// reports which happened (true = consumed as tool input).
func (s *StreamAccumulator) AddText(text string) bool {
	if s.currentTool != nil {
		s.toolInputBuf += text
		return true
	}
	s.textBuf += text
	return false
}

// AddToolInput appends an input_json_delta fragment to the in-progress
// tool. Returns false (no-op) when no tool is being accumulated.
func (s *StreamAccumulator) AddToolInput(text string) bool {
	if s.currentTool == nil {
		return false
	}
	s.toolInputBuf += text
	return true
}

// StartTool finalizes any pending text and any in-progress tool, then
// begins accumulating a new tool call. The input slice is copied.
func (s *StreamAccumulator) StartTool(tu ToolUseBlock) {
	s.finalizeText()
	s.FinalizeTool()
	s.currentTool = &ToolUseBlock{
		ID:    tu.ID,
		Name:  tu.Name,
		Input: append(json.RawMessage(nil), tu.Input...),
	}
	s.toolInputBuf = ""
}

// FinalizeTool commits the in-progress tool call, if any, to Blocks and
// PendingTools (content_block_stop timing). Buffered input JSON overrides
// the tool's initial Input only when non-empty. Fires OnToolFinalized.
func (s *StreamAccumulator) FinalizeTool() {
	if s.currentTool == nil {
		return
	}
	if s.toolInputBuf != "" {
		s.currentTool.Input = json.RawMessage(s.toolInputBuf)
	}
	tc := *s.currentTool
	s.pendingTools = append(s.pendingTools, tc)
	s.blocks = append(s.blocks, ContentBlock{
		Type:  "tool_use",
		ID:    tc.ID,
		Name:  tc.Name,
		Input: tc.Input,
	})
	s.currentTool = nil
	s.toolInputBuf = ""
	if s.OnToolFinalized != nil {
		s.OnToolFinalized(tc)
	}
}

// finalizeText commits accumulated text as a content block when KeepText
// accepts it. Rejected text stays buffered — it prefixes any text that
// streams after an intervening tool call, matching the loops' historical
// behavior of clearing the buffer only on commit.
func (s *StreamAccumulator) finalizeText() {
	if s.KeepText(s.textBuf) {
		s.blocks = append(s.blocks, ContentBlock{Type: "text", Text: s.textBuf})
		s.textBuf = ""
	}
}

// Finish finalizes any remaining text and in-progress tool at stream end.
func (s *StreamAccumulator) Finish() {
	s.finalizeText()
	s.FinalizeTool()
}

// HasPartialTool reports whether a tool call is still being accumulated.
func (s *StreamAccumulator) HasPartialTool() bool {
	return s.currentTool != nil
}

// DropInvalidPartialTool discards the in-progress tool call when its
// buffered input is invalid JSON — the signature of a stream truncated
// mid-tool-call. Returns true if a tool was discarded. Tools whose input
// arrived whole (empty buffer) are never dropped.
func (s *StreamAccumulator) DropInvalidPartialTool() bool {
	if s.currentTool == nil || s.toolInputBuf == "" {
		return false
	}
	if json.Valid([]byte(s.toolInputBuf)) {
		return false
	}
	s.currentTool = nil
	s.toolInputBuf = ""
	return true
}

// CurrentText returns the uncommitted text buffer. Callers that extract
// tool calls from text (non-native models) read this before Finish.
func (s *StreamAccumulator) CurrentText() string {
	return s.textBuf
}

// Reset discards all accumulated state, e.g. after a stream error, so a
// corrupt partial stream cannot pollute the conversation.
func (s *StreamAccumulator) Reset() {
	s.blocks = nil
	s.pendingTools = nil
	s.textBuf = ""
	s.currentTool = nil
	s.toolInputBuf = ""
}

// Blocks returns the accumulated content blocks.
func (s *StreamAccumulator) Blocks() []ContentBlock {
	return s.blocks
}

// PendingTools returns the finalized tool calls in stream order.
func (s *StreamAccumulator) PendingTools() []ToolUseBlock {
	return s.pendingTools
}
