package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- stripANSI tests ---

func TestStripANSI_NoEscapes(t *testing.T) {
	input := "hello world"
	output := stripANSI(input)
	assert.Equal(t, "hello world", output)
}

func TestStripANSI_SingleColorCode(t *testing.T) {
	// Red color: \x1b[31m
	input := "\x1b[31mhello\x1b[0m"
	output := stripANSI(input)
	assert.Equal(t, "hello", output)
}

func TestStripANSI_MultipleStyles(t *testing.T) {
	// Bold + blue
	input := "\x1b[1m\x1b[34mtext\x1b[0m"
	output := stripANSI(input)
	assert.Equal(t, "text", output)
}

func TestStripANSI_EmptyString(t *testing.T) {
	output := stripANSI("")
	assert.Equal(t, "", output)
}

func TestStripANSI_OnlyEscapes(t *testing.T) {
	input := "\x1b[31m\x1b[0m"
	output := stripANSI(input)
	assert.Equal(t, "", output)
}

func TestStripANSI_MixedContent(t *testing.T) {
	// "hello " + red color code + "world" + reset
	input := "hello \x1b[31mworld\x1b[0m!"
	output := stripANSI(input)
	assert.Equal(t, "hello world!", output)
}

// --- extractSelectedText tests ---

func TestExtractSelectedText_SingleLine(t *testing.T) {
	lines := []string{"hello world"}
	sel := MouseSelection{
		Start:  Position{Line: 0, Col: 0},
		End:    Position{Line: 0, Col: 5},
		Active: true,
	}
	text := extractSelectedText(lines, sel)
	assert.Equal(t, "hello", text)
}

func TestExtractSelectedText_MultiLine(t *testing.T) {
	lines := []string{
		"hello world",
		"foo bar",
		"baz qux",
	}
	sel := MouseSelection{
		Start:  Position{Line: 0, Col: 6},  // "world"
		End:    Position{Line: 2, Col: 3},  // "baz"
		Active: true,
	}
	text := extractSelectedText(lines, sel)
	// Should be: "world" + "\n" + "foo bar" + "\n" + "baz"
	assert.Equal(t, "world\nfoo bar\nbaz", text)
}

func TestExtractSelectedText_EmptySelection(t *testing.T) {
	lines := []string{"hello"}
	sel := MouseSelection{
		Start:  Position{Line: 0, Col: 5},
		End:    Position{Line: 0, Col: 5},
		Active: true,
	}
	text := extractSelectedText(lines, sel)
	assert.Equal(t, "", text)
}

func TestExtractSelectedText_ClampsToLineLength(t *testing.T) {
	lines := []string{"hello"}
	sel := MouseSelection{
		Start:  Position{Line: 0, Col: 2},
		End:    Position{Line: 0, Col: 100}, // beyond line length
		Active: true,
	}
	text := extractSelectedText(lines, sel)
	assert.Equal(t, "llo", text)
}

func TestExtractSelectedText_OutOfBounds(t *testing.T) {
	lines := []string{"hello"}
	sel := MouseSelection{
		Start:  Position{Line: 10, Col: 0},
		End:    Position{Line: 20, Col: 5},
		Active: true,
	}
	text := extractSelectedText(lines, sel)
	// Out of bounds selection returns empty
	assert.Equal(t, "", text)
}

func TestExtractSelectedText_ReversedSelection(t *testing.T) {
	lines := []string{"hello world"}
	sel := MouseSelection{
		Start:  Position{Line: 0, Col: 10},
		End:    Position{Line: 0, Col: 0},
		Active: true,
	}
	text := extractSelectedText(lines, sel)
	// Should normalize to (0, 0) to (0, 10)
	assert.Equal(t, "hello worl", text)
}

// --- wordBoundaries tests ---

func TestWordBoundaries_MiddleOfWord(t *testing.T) {
	line := "hello world"
	start, end := wordBoundaries(line, 2) // cursor on 'l' in "hello"
	assert.Equal(t, 0, start)
	assert.Equal(t, 5, end)
}

func TestWordBoundaries_AtWordStart(t *testing.T) {
	line := "hello world"
	start, end := wordBoundaries(line, 0) // cursor on 'h'
	assert.Equal(t, 0, start)
	assert.Equal(t, 5, end)
}

func TestWordBoundaries_AtWordEnd(t *testing.T) {
	line := "hello world"
	start, end := wordBoundaries(line, 4) // cursor on 'o' (last letter)
	assert.Equal(t, 0, start)
	assert.Equal(t, 5, end)
}

func TestWordBoundaries_OnWhitespace(t *testing.T) {
	line := "hello world"
	start, end := wordBoundaries(line, 5) // cursor on space
	assert.Equal(t, 5, start)
	assert.Equal(t, 5, end)
}

func TestWordBoundaries_SecondWord(t *testing.T) {
	line := "hello world test"
	start, end := wordBoundaries(line, 8) // 'r' in "world"
	assert.Equal(t, 6, start)
	assert.Equal(t, 11, end)
}

func TestWordBoundaries_WithUnderscore(t *testing.T) {
	line := "hello_world test"
	start, end := wordBoundaries(line, 5) // '_' is part of word
	assert.Equal(t, 0, start)
	assert.Equal(t, 11, end)
}

func TestWordBoundaries_WithDot(t *testing.T) {
	line := "hello.world test"
	start, end := wordBoundaries(line, 5) // '.' is part of word
	assert.Equal(t, 0, start)
	assert.Equal(t, 11, end)
}

func TestWordBoundaries_EmptyLine(t *testing.T) {
	line := ""
	start, end := wordBoundaries(line, 0)
	assert.Equal(t, 0, start)
	assert.Equal(t, 0, end)
}

func TestWordBoundaries_OnlyWhitespace(t *testing.T) {
	line := "   "
	start, end := wordBoundaries(line, 1)
	assert.Equal(t, 1, start)
	assert.Equal(t, 1, end)
}

func TestWordBoundaries_Mixed_Alphanumeric(t *testing.T) {
	line := "var123_test"
	start, end := wordBoundaries(line, 5) // '3' in middle
	assert.Equal(t, 0, start)
	assert.Equal(t, 11, end)
}
