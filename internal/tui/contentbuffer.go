package tui

import "strings"

// SegmentType describes the kind of content tracked in a ContentBuffer.
type SegmentType int

const (
	SegmentTypeText SegmentType = iota
	SegmentTypeToolResult
)

// ContentSegment stores either plain text or a collapsible tool-result segment.
type ContentSegment struct {
	Type       SegmentType
	Text       string
	ToolResult *CollapsibleToolResult

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
	copy.ID = b.ToolResultCount()
	b.segments = append(b.segments, ContentSegment{
		Type:       SegmentTypeToolResult,
		ToolResult: &copy,
		dirty:      true,
	})
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
	}
	for i := range b.segments {
		seg := &b.segments[i]
		if seg.Type != SegmentTypeToolResult || seg.ToolResult == nil {
			continue
		}
		seg.ToolResult.Collapsed = !anyCollapsed
		seg.dirty = true
	}
}

func (b *ContentBuffer) CollapseAllToolResults() {
	for i := range b.segments {
		seg := &b.segments[i]
		if seg.Type != SegmentTypeToolResult || seg.ToolResult == nil {
			continue
		}
		if !seg.ToolResult.Collapsed {
			seg.ToolResult.Collapsed = true
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
	content := b.String()
	b.replaceTextRange(content, start, end, replacement)
}

func (b *ContentBuffer) ReplaceTextRangeWithWidth(width int, start, end int, replacement string) {
	content := b.Render(width)
	b.replaceTextRange(content, start, end, replacement)
}

func (b *ContentBuffer) replaceTextRange(content string, start, end int, replacement string) {
	if start < 0 || start > len(content) {
		return
	}
	if end < start {
		end = start
	}
	if end > len(content) {
		end = len(content)
	}
	b.Reset()
	b.AppendText(content[:start])
	b.AppendText(replacement)
	b.AppendText(content[end:])
}
