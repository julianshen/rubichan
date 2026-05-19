package agent

import (
	"regexp"
	"strconv"
	"strings"
)

// Token budget parsing patterns.
// Shorthand (+500k) anchored to start/end to avoid false positives.
// Verbose (use/spend 2M tokens) matches anywhere.
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
// Returns the parsed token count and true if a directive was found.
func ParseTokenBudget(text string) (int, bool) {
	// Try shorthand at start first (most specific).
	if m := shorthandStartRe.FindStringSubmatch(text); m != nil {
		return parseBudgetMatch(m[1], m[2]), true
	}
	// Try shorthand at end.
	if m := shorthandEndRe.FindStringSubmatch(text); m != nil {
		// Avoid double-counting when input is just "+500k" (already matched by start).
		if !shorthandStartRe.MatchString(text) {
			return parseBudgetMatch(m[1], m[2]), true
		}
	}
	// Try verbose pattern.
	if m := verboseRe.FindStringSubmatch(text); m != nil {
		return parseBudgetMatch(m[1], m[2]), true
	}
	return 0, false
}

// StripTokenBudget removes budget directives from the user message,
// returning the cleaned text. Preserves surrounding content.
func StripTokenBudget(text string) string {
	// Remove shorthand at start.
	text = shorthandStartRe.ReplaceAllString(text, "")
	// Remove shorthand at end.
	text = shorthandEndRe.ReplaceAllString(text, "")
	// Remove verbose pattern.
	text = verboseRe.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
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
