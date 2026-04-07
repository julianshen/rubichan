package tui

import "time"

// Position represents a location in the content (line and column).
type Position struct {
	Line int // 0-based content line index
	Col  int // 0-based column index (in visible characters)
}

// MouseSelection tracks the current text selection state.
type MouseSelection struct {
	Start    Position // selection start
	End      Position // selection end
	Active   bool     // whether a selection is active
	Dragging bool     // whether user is currently dragging
}

// IsEmpty returns true if Start equals End (no selection made).
func (s MouseSelection) IsEmpty() bool {
	return s.Start == s.End
}

// Normalized returns (start, end) with start guaranteed to be before end.
// Handles reversed selections (when user drags backwards).
func (s MouseSelection) Normalized() (start, end Position) {
	if s.Start.Line < s.End.Line {
		return s.Start, s.End
	}
	if s.Start.Line > s.End.Line {
		return s.End, s.Start
	}
	// Same line: compare columns
	if s.Start.Col <= s.End.Col {
		return s.Start, s.End
	}
	return s.End, s.Start
}

// ContainsLine returns true if the given content line falls within the selection range.
func (s MouseSelection) ContainsLine(line int) bool {
	start, end := s.Normalized()
	return line >= start.Line && line <= end.Line
}

// ColRangeForLine returns the (startCol, endCol) range for the given content line.
// lineLen is the number of visible characters in the line (clamping).
// Returns the columns in the plain-text version of that line.
func (s MouseSelection) ColRangeForLine(line, lineLen int) (startCol, endCol int) {
	start, end := s.Normalized()

	// For a line not in selection, return (0, 0)
	if line < start.Line || line > end.Line {
		return 0, 0
	}

	// Single-line selection
	if start.Line == end.Line {
		// Clamp to actual line length
		startCol := start.Col
		if startCol > lineLen {
			startCol = lineLen
		}
		endCol := end.Col
		if endCol > lineLen {
			endCol = lineLen
		}
		return startCol, endCol
	}

	// Multi-line selection

	// First line: from startCol to end of line
	if line == start.Line {
		startCol := start.Col
		if startCol > lineLen {
			startCol = lineLen
		}
		return startCol, lineLen
	}

	// Last line: from beginning to endCol
	if line == end.Line {
		endCol := end.Col
		if endCol > lineLen {
			endCol = lineLen
		}
		return 0, endCol
	}

	// Middle lines: entire line selected
	return 0, lineLen
}

// clickTracker detects single, double, and triple clicks.
type clickTracker struct {
	count    int       // current click count (1, 2, 3)
	lastX    int       // last X coordinate
	lastY    int       // last Y coordinate
	lastTime time.Time // last click time
}

// Register records a new click and returns the click count (1, 2, or 3).
// Resets to 1 if position changes or > 400ms has passed since the last click.
func (ct *clickTracker) Register(x, y int, now time.Time) int {
	// Reset if position changed or timeout exceeded
	timeout := 400 * time.Millisecond
	if x != ct.lastX || y != ct.lastY || now.Sub(ct.lastTime) > timeout {
		ct.count = 1
	} else {
		ct.count++
		// Cap at 3 (triple-click, no more increments)
		if ct.count > 3 {
			ct.count = 3
		}
	}
	ct.lastX = x
	ct.lastY = y
	ct.lastTime = now
	return ct.count
}
