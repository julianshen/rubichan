package tui

import "strings"

// SegmentType describes the kind of content tracked in a ContentBuffer.
type SegmentType int

const (
	SegmentTypeText SegmentType = iota
	SegmentTypeToolResult
	SegmentTypeThinking
)

// ContentSegment stores either plain text, a collapsible tool-result, or a
// collapsible thinking segment.
type ContentSegment struct {
	Type       SegmentType
	Text       string
	ToolResult *CollapsibleToolResult
	Thinking   *CollapsibleThinking

	dirty      bool
	lastWidth  int
	lastRender string
}

// ContentBuffer stores the rendered transcript as typed segments.
// It owns tool-result lifecycle state (append/toggle/collapse/render cache).
type ContentBuffer struct {
	segments []ContentSegment
}

func NewContentBuffer() *ContentBuffer {
	return &ContentBuffer{}
}

// AppendText appends plain text content to the transcript.
func (b *ContentBuffer) AppendText(text string) {
	if text == "" {
		return
	}
	b.segments = append(b.segments, ContentSegment{
		Type:  SegmentTypeText,
		Text:  text,
		dirty: true,
	})
}

// WriteString maintains compatibility with existing call sites that used
// strings.Builder directly.
func (b *ContentBuffer) WriteString(text string) {
	b.AppendText(text)
}

// AppendToolResult appends a collapsible tool-result segment.
func (b *ContentBuffer) AppendToolResult(result CollapsibleToolResult) {
	copy := result
	copy.ID = b.nextCollapsibleID()
	b.segments = append(b.segments, ContentSegment{
		Type:       SegmentTypeToolResult,
		ToolResult: &copy,
		dirty:      true,
	})
}

// AppendThinking appends a collapsible thinking segment.
func (b *ContentBuffer) AppendThinking(t CollapsibleThinking) {
	copy := t
	copy.ID = b.nextCollapsibleID()
	b.segments = append(b.segments, ContentSegment{
		Type:     SegmentTypeThinking,
		Thinking: &copy,
		dirty:    true,
	})
}

// nextCollapsibleID returns a monotonically increasing ID across both tool
// results and thinking segments so IDs never collide.
func (b *ContentBuffer) nextCollapsibleID() int {
	maxID := -1
	for i := range b.segments {
		if b.segments[i].Type == SegmentTypeToolResult && b.segments[i].ToolResult != nil {
			if b.segments[i].ToolResult.ID > maxID {
				maxID = b.segments[i].ToolResult.ID
			}
		}
		if b.segments[i].Type == SegmentTypeThinking && b.segments[i].Thinking != nil {
			if b.segments[i].Thinking.ID > maxID {
				maxID = b.segments[i].Thinking.ID
			}
		}
	}
	return maxID + 1
}

// ToggleToolResult toggles collapsed/expanded state for a specific tool-result ID.
func (b *ContentBuffer) ToggleToolResult(id int) bool {
	for i := range b.segments {
		seg := &b.segments[i]
		if seg.Type != SegmentTypeToolResult || seg.ToolResult == nil {
			continue
		}
		if seg.ToolResult.ID != id {
			continue
		}
		seg.ToolResult.Collapsed = !seg.ToolResult.Collapsed
		seg.dirty = true
		return true
	}
	return false
}

func (b *ContentBuffer) ToggleAllToolResults() {
	anyCollapsed := false
	for i := range b.segments {
		seg := &b.segments[i]
		if seg.Type == SegmentTypeToolResult && seg.ToolResult != nil && seg.ToolResult.Collapsed {
			anyCollapsed = true
			break
		}
		if seg.Type == SegmentTypeThinking && seg.Thinking != nil && seg.Thinking.Collapsed {
			anyCollapsed = true
			break
		}
	}
	for i := range b.segments {
		seg := &b.segments[i]
		if seg.Type == SegmentTypeToolResult && seg.ToolResult != nil {
			seg.ToolResult.Collapsed = !anyCollapsed
			seg.dirty = true
		}
		if seg.Type == SegmentTypeThinking && seg.Thinking != nil {
			seg.Thinking.Collapsed = !anyCollapsed
			seg.dirty = true
		}
	}
}

func (b *ContentBuffer) CollapseAllToolResults() {
	for i := range b.segments {
		seg := &b.segments[i]
		if seg.Type == SegmentTypeToolResult && seg.ToolResult != nil && !seg.ToolResult.Collapsed {
			seg.ToolResult.Collapsed = true
			seg.dirty = true
		}
		if seg.Type == SegmentTypeThinking && seg.Thinking != nil && !seg.Thinking.Collapsed {
			seg.Thinking.Collapsed = true
			seg.dirty = true
		}
	}
}

func (b *ContentBuffer) ToggleFullExpandMostRecent() {
	for i := len(b.segments) - 1; i >= 0; i-- {
		seg := &b.segments[i]
		if seg.Type != SegmentTypeToolResult || seg.ToolResult == nil {
			continue
		}
		tr := seg.ToolResult
		if !tr.Collapsed && tr.LineCount > maxToolResultLines {
			tr.FullyExpanded = !tr.FullyExpanded
			seg.dirty = true
			return
		}
	}
}

// HasCollapsible returns true if the buffer contains any collapsible segments
// (tool results or thinking blocks).
func (b *ContentBuffer) HasCollapsible() bool {
	for i := range b.segments {
		if b.segments[i].Type == SegmentTypeToolResult && b.segments[i].ToolResult != nil {
			return true
		}
		if b.segments[i].Type == SegmentTypeThinking && b.segments[i].Thinking != nil {
			return true
		}
	}
	return false
}

func (b *ContentBuffer) ToolResultCount() int {
	count := 0
	for i := range b.segments {
		if b.segments[i].Type == SegmentTypeToolResult && b.segments[i].ToolResult != nil {
			count++
		}
	}
	return count
}

func (b *ContentBuffer) ToolResults() []CollapsibleToolResult {
	results := make([]CollapsibleToolResult, 0, b.ToolResultCount())
	for i := range b.segments {
		if b.segments[i].Type == SegmentTypeToolResult && b.segments[i].ToolResult != nil {
			results = append(results, *b.segments[i].ToolResult)
		}
	}
	return results
}

// Render returns fully rendered transcript content for the given width.
func (b *ContentBuffer) Render(width int) string {
	if len(b.segments) == 0 {
		return ""
	}
	renderer := NewToolBoxRenderer(width)
	var out strings.Builder
	for i := range b.segments {
		seg := &b.segments[i]
		if !seg.dirty && seg.lastWidth == width {
			out.WriteString(seg.lastRender)
			continue
		}
		var rendered string
		switch seg.Type {
		case SegmentTypeText:
			rendered = seg.Text
		case SegmentTypeToolResult:
			if seg.ToolResult != nil {
				rendered = seg.ToolResult.Render(renderer)
			}
		case SegmentTypeThinking:
			if seg.Thinking != nil {
				rendered = seg.Thinking.Render(renderer)
			}
		}
		seg.lastRender = rendered
		seg.lastWidth = width
		seg.dirty = false
		out.WriteString(rendered)
	}
	return out.String()
}

func (b *ContentBuffer) Len() int {
	return len(b.String())
}

func (b *ContentBuffer) LenWithWidth(width int) int {
	return len(b.Render(width))
}

func (b *ContentBuffer) String() string {
	return b.Render(80)
}

func (b *ContentBuffer) Reset() {
	b.segments = nil
}

func (b *ContentBuffer) ReplaceTextRange(start, end int, replacement string) {
	b.replaceTextRangeWithWidth(80, start, end, replacement)
}

func (b *ContentBuffer) ReplaceTextRangeWithWidth(width int, start, end int, replacement string) {
	b.replaceTextRangeWithWidth(width, start, end, replacement)
}

func (b *ContentBuffer) replaceTextRangeWithWidth(width, start, end int, replacement string) {
	contentLen := b.LenWithWidth(width)
	if start < 0 || start > contentLen {
		return
	}
	if end < start {
		end = start
	}
	if end > contentLen {
		end = contentLen
	}

	newSegments := make([]ContentSegment, 0, len(b.segments)+2)
	cursor := 0
	inserted := false

	for i := range b.segments {
		seg := b.segments[i]
		segRendered := b.segmentRender(seg, width)
		segLen := len(segRendered)
		segStart := cursor
		segEnd := cursor + segLen

		switch {
		case segEnd <= start:
			newSegments = append(newSegments, cloneSegment(seg))
		case segStart >= end:
			if !inserted {
				newSegments = appendTextSegment(newSegments, replacement)
				inserted = true
			}
			newSegments = append(newSegments, cloneSegment(seg))
		default:
			if seg.Type == SegmentTypeText {
				localStart := start - segStart
				if localStart < 0 {
					localStart = 0
				}
				if localStart > len(seg.Text) {
					localStart = len(seg.Text)
				}

				localEnd := end - segStart
				if localEnd < 0 {
					localEnd = 0
				}
				if localEnd > len(seg.Text) {
					localEnd = len(seg.Text)
				}
				if localEnd < localStart {
					localEnd = localStart
				}

				newSegments = appendTextSegment(newSegments, seg.Text[:localStart])
				if !inserted {
					newSegments = appendTextSegment(newSegments, replacement)
					inserted = true
				}
				newSegments = appendTextSegment(newSegments, seg.Text[localEnd:])
			} else {
				// Tool-result segments are atomic; keep them interactive even when
				// replacement indexes overlap rendered output.
				if !inserted {
					newSegments = appendTextSegment(newSegments, replacement)
					inserted = true
				}
				newSegments = append(newSegments, cloneSegment(seg))
			}
		}

		cursor = segEnd
	}

	if !inserted {
		newSegments = appendTextSegment(newSegments, replacement)
	}

	b.segments = newSegments
}

func appendTextSegment(segments []ContentSegment, text string) []ContentSegment {
	if text == "" {
		return segments
	}
	return append(segments, ContentSegment{
		Type:  SegmentTypeText,
		Text:  text,
		dirty: true,
	})
}

func cloneSegment(seg ContentSegment) ContentSegment {
	cloned := seg
	if seg.ToolResult != nil {
		tool := *seg.ToolResult
		cloned.ToolResult = &tool
	}
	if seg.Thinking != nil {
		thinking := *seg.Thinking
		cloned.Thinking = &thinking
	}
	cloned.dirty = true
	cloned.lastWidth = 0
	cloned.lastRender = ""
	return cloned
}

func (b *ContentBuffer) segmentRender(seg ContentSegment, width int) string {
	switch seg.Type {
	case SegmentTypeText:
		return seg.Text
	case SegmentTypeToolResult:
		if seg.ToolResult == nil {
			return ""
		}
		return seg.ToolResult.Render(NewToolBoxRenderer(width))
	case SegmentTypeThinking:
		if seg.Thinking == nil {
			return ""
		}
		return seg.Thinking.Render(NewToolBoxRenderer(width))
	default:
		return ""
	}
}
