package tui

import "unicode"

// stripANSI is defined in approval.go and removes ANSI escape sequences.
// It's reused here for text extraction.

// extractSelectedText extracts the text within the selection from the given lines.
// Handles multi-line selections, clamps to line lengths, and normalizes reversed selections.
// Returns empty string if selection is out of bounds or empty.
func extractSelectedText(lines []string, sel MouseSelection) string {
	if sel.IsEmpty() {
		return ""
	}

	start, end := sel.Normalized()

	// Bounds check
	if start.Line >= len(lines) || end.Line >= len(lines) || start.Line < 0 || end.Line < 0 {
		return ""
	}

	// Single-line selection
	if start.Line == end.Line {
		line := lines[start.Line]
		runes := []rune(line)
		startCol := start.Col
		if startCol > len(runes) {
			startCol = len(runes)
		}
		endCol := end.Col
		if endCol > len(runes) {
			endCol = len(runes)
		}
		if startCol >= endCol {
			return ""
		}
		return string(runes[startCol:endCol])
	}

	// Multi-line selection
	var result []rune

	// First line: from startCol to end
	firstLine := lines[start.Line]
	firstRunes := []rune(firstLine)
	startCol := start.Col
	if startCol > len(firstRunes) {
		startCol = len(firstRunes)
	}
	result = append(result, firstRunes[startCol:]...)

	// Middle lines: entire lines
	for i := start.Line + 1; i < end.Line; i++ {
		result = append(result, '\n')
		result = append(result, []rune(lines[i])...)
	}

	// Last line: from beginning to endCol
	result = append(result, '\n')
	lastLine := lines[end.Line]
	lastRunes := []rune(lastLine)
	endCol := end.Col
	if endCol > len(lastRunes) {
		endCol = len(lastRunes)
	}
	result = append(result, lastRunes[:endCol]...)

	return string(result)
}

// wordBoundaries finds the start and end column indices of the word at the given column.
// Word characters: letters, digits, underscore, dot.
// Non-word characters (spaces, punctuation) return (col, col) with no selection.
// Returns the boundaries as rune indices (not byte indices).
func wordBoundaries(line string, col int) (start, end int) {
	runes := []rune(line)

	// Out of bounds
	if col < 0 || col >= len(runes) {
		return col, col
	}

	currentRune := runes[col]

	// Check if the character at col is a word character
	if !isWordChar(currentRune) {
		return col, col
	}

	// Find start: go backward while word chars
	start = col
	for start > 0 && isWordChar(runes[start-1]) {
		start--
	}

	// Find end: go forward while word chars
	end = col + 1
	for end < len(runes) && isWordChar(runes[end]) {
		end++
	}

	return start, end
}

// isWordChar returns true if the rune is considered part of a word.
// Word chars: Unicode letters, digits, underscore, dot.
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.'
}
