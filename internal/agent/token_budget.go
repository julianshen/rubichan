package agent

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	shorthandStartRe = regexp.MustCompile(`(?i)^\s*\+(\d+(?:\.\d+)?)\s*(k|m|b)\b`)
	shorthandEndRe   = regexp.MustCompile(`(?i)\s\+(\d+(?:\.\d+)?)\s*(k|m|b)\s*[.!?]?\s*$`)
	verboseRe        = regexp.MustCompile(`(?i)\b(?:use|spend)\s+(\d+(?:\.\d+)?)\s*(k|m|b)\s*tokens?\b`)
)

var multipliers = map[string]int{
	"k": 1_000,
	"m": 1_000_000,
	"b": 1_000_000_000,
}

// ParseTokenBudget parses a user message for token budget directives.
// Supports three formats:
//   - Shorthand at start: "+500k do this thing"
//   - Shorthand at end: "do this thing +500k"
//   - Verbose anywhere: "use 2M tokens" or "spend 500k tokens"
//
// Returns the parsed token count, the cleaned text (with directive removed),
// and true if a directive was found.
func ParseTokenBudget(text string) (int, string, bool) {
	// Fast path: no "+" in message — only verbose patterns possible.
	if !strings.Contains(text, "+") {
		if m := verboseRe.FindStringSubmatch(text); m != nil {
			stripped := strings.TrimSpace(verboseRe.ReplaceAllString(text, ""))
			return parseBudgetMatch(m[1], m[2]), stripped, true
		}
		return 0, text, false
	}

	// Try shorthand at start first (most specific).
	if m := shorthandStartRe.FindStringSubmatch(text); m != nil {
		stripped := strings.TrimSpace(shorthandStartRe.ReplaceAllString(text, ""))
		return parseBudgetMatch(m[1], m[2]), stripped, true
	}
	// Try shorthand at end.
	if m := shorthandEndRe.FindStringSubmatch(text); m != nil {
		stripped := strings.TrimSpace(shorthandEndRe.ReplaceAllString(text, ""))
		return parseBudgetMatch(m[1], m[2]), stripped, true
	}
	// Try verbose pattern.
	if m := verboseRe.FindStringSubmatch(text); m != nil {
		stripped := strings.TrimSpace(verboseRe.ReplaceAllString(text, ""))
		return parseBudgetMatch(m[1], m[2]), stripped, true
	}
	return 0, text, false
}

func parseBudgetMatch(value, suffix string) int {
	suffix = strings.ToLower(suffix)
	multiplier := multipliers[suffix]
	if multiplier == 0 {
		multiplier = 1
	}
	f, _ := strconv.ParseFloat(value, 64)
	return int(f * float64(multiplier))
}
