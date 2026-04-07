package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Position tests ---

func TestPositionOrdering(t *testing.T) {
	p1 := Position{Line: 0, Col: 5}
	p2 := Position{Line: 0, Col: 10}
	p3 := Position{Line: 1, Col: 0}

	assert.True(t, p1.Line == 0 && p1.Col == 5)
	assert.True(t, p2.Line == 0 && p2.Col == 10)
	assert.True(t, p3.Line == 1 && p3.Col == 0)
}

// --- MouseSelection tests ---

func TestMouseSelection_IsEmpty(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 5, Col: 10},
		End:    Position{Line: 5, Col: 10},
		Active: true,
	}
	assert.True(t, sel.IsEmpty())

	sel.End.Col = 11
	assert.False(t, sel.IsEmpty())

	sel.End.Line = 6
	assert.False(t, sel.IsEmpty())
}

func TestMouseSelection_Normalized_AlreadyOrdered(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 0, Col: 5},
		End:    Position{Line: 1, Col: 10},
		Active: true,
	}
	start, end := sel.Normalized()
	assert.Equal(t, sel.Start, start)
	assert.Equal(t, sel.End, end)
}

func TestMouseSelection_Normalized_ReverseOrder(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 1, Col: 10},
		End:    Position{Line: 0, Col: 5},
		Active: true,
	}
	start, end := sel.Normalized()
	assert.Equal(t, Position{Line: 0, Col: 5}, start)
	assert.Equal(t, Position{Line: 1, Col: 10}, end)
}

func TestMouseSelection_Normalized_SameLineDifferentCols(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 5, Col: 15},
		End:    Position{Line: 5, Col: 5},
		Active: true,
	}
	start, end := sel.Normalized()
	assert.Equal(t, Position{Line: 5, Col: 5}, start)
	assert.Equal(t, Position{Line: 5, Col: 15}, end)
}

func TestMouseSelection_ContainsLine_FirstLine(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 5, Col: 10},
		End:    Position{Line: 10, Col: 20},
		Active: true,
	}
	assert.True(t, sel.ContainsLine(5))
	assert.True(t, sel.ContainsLine(7))
	assert.True(t, sel.ContainsLine(10))
	assert.False(t, sel.ContainsLine(4))
	assert.False(t, sel.ContainsLine(11))
}

func TestMouseSelection_ColRangeForLine_FirstLine(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 5, Col: 10},
		End:    Position{Line: 5, Col: 20},
		Active: true,
	}
	start, end := sel.ColRangeForLine(5, 30)
	assert.Equal(t, 10, start)
	assert.Equal(t, 20, end)
}

func TestMouseSelection_ColRangeForLine_FirstOfMultiple(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 5, Col: 10},
		End:    Position{Line: 7, Col: 20},
		Active: true,
	}
	// First line: should select from col 10 to end of line
	start, end := sel.ColRangeForLine(5, 100)
	assert.Equal(t, 10, start)
	assert.Equal(t, 100, end)
}

func TestMouseSelection_ColRangeForLine_MiddleLine(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 5, Col: 10},
		End:    Position{Line: 7, Col: 20},
		Active: true,
	}
	// Middle line: entire line selected
	start, end := sel.ColRangeForLine(6, 100)
	assert.Equal(t, 0, start)
	assert.Equal(t, 100, end)
}

func TestMouseSelection_ColRangeForLine_LastLine(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 5, Col: 10},
		End:    Position{Line: 7, Col: 20},
		Active: true,
	}
	// Last line: select from 0 to col 20
	start, end := sel.ColRangeForLine(7, 100)
	assert.Equal(t, 0, start)
	assert.Equal(t, 20, end)
}

func TestMouseSelection_ColRangeForLine_ClampsToLineLength(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 5, Col: 10},
		End:    Position{Line: 5, Col: 50},
		Active: true,
	}
	// Line is only 30 chars
	start, end := sel.ColRangeForLine(5, 30)
	assert.Equal(t, 10, start)
	assert.Equal(t, 30, end) // clamped to line length
}

// --- ClickTracker tests ---

func TestClickTracker_SingleClick(t *testing.T) {
	ct := &clickTracker{}
	now := time.Now()
	count := ct.Register(10, 20, now)
	assert.Equal(t, 1, count)
}

func TestClickTracker_DoubleClick(t *testing.T) {
	ct := &clickTracker{}
	now := time.Now()

	count1 := ct.Register(10, 20, now)
	assert.Equal(t, 1, count1)

	// Same position, 100ms later (within 400ms)
	count2 := ct.Register(10, 20, now.Add(100*time.Millisecond))
	assert.Equal(t, 2, count2)
}

func TestClickTracker_TripleClick(t *testing.T) {
	ct := &clickTracker{}
	now := time.Now()

	ct.Register(10, 20, now)
	ct.Register(10, 20, now.Add(100*time.Millisecond))
	count3 := ct.Register(10, 20, now.Add(200*time.Millisecond))
	assert.Equal(t, 3, count3)
}

func TestClickTracker_ResetOnPositionChange(t *testing.T) {
	ct := &clickTracker{}
	now := time.Now()

	ct.Register(10, 20, now)
	ct.Register(10, 20, now.Add(100*time.Millisecond))
	// Different position within timeout window
	count := ct.Register(15, 20, now.Add(200*time.Millisecond))
	assert.Equal(t, 1, count) // reset to 1
}

func TestClickTracker_ResetOnTimeout(t *testing.T) {
	ct := &clickTracker{}
	now := time.Now()

	ct.Register(10, 20, now)
	ct.Register(10, 20, now.Add(100*time.Millisecond))
	// Same position but well beyond 400ms timeout (550ms from original is 450ms from last)
	count := ct.Register(10, 20, now.Add(550*time.Millisecond))
	assert.Equal(t, 1, count) // reset to 1
}

func TestClickTracker_CountWrapsAt3(t *testing.T) {
	ct := &clickTracker{}
	now := time.Now()

	ct.Register(10, 20, now)
	ct.Register(10, 20, now.Add(100*time.Millisecond))
	ct.Register(10, 20, now.Add(200*time.Millisecond))
	// Fourth click still returns 3 (triple-click mode)
	count := ct.Register(10, 20, now.Add(300*time.Millisecond))
	require.Equal(t, 3, count)
	// Fifth click also returns 3
	count = ct.Register(10, 20, now.Add(400*time.Millisecond))
	assert.Equal(t, 3, count)
}
