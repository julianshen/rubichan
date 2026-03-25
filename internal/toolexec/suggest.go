package toolexec

import "strings"

// ToolNamer provides the list of registered tool names for suggestion.
type ToolNamer interface {
	Names() []string
}

// SuggestToolName returns the closest matching tool name from available names,
// or empty string if no reasonable match is found. Uses keyword overlap and
// substring matching rather than edit distance for simplicity.
func SuggestToolName(unknown string, available []string) string {
	if len(available) == 0 {
		return ""
	}

	unknown = strings.ToLower(unknown)

	// First pass: check if unknown contains a tool name or vice versa.
	for _, name := range available {
		lower := strings.ToLower(name)
		if strings.Contains(unknown, lower) || strings.Contains(lower, unknown) {
			return name
		}
	}

	// Second pass: split unknown by common separators and check keyword overlap.
	unknownParts := splitToolName(unknown)
	bestMatch := ""
	bestScore := 0

	for _, name := range available {
		nameParts := splitToolName(strings.ToLower(name))
		score := keywordOverlap(unknownParts, nameParts)
		if score > bestScore {
			bestScore = score
			bestMatch = name
		}
	}

	if bestScore > 0 {
		return bestMatch
	}
	return ""
}

// splitToolName splits a tool name by underscores, hyphens, and camelCase boundaries.
func splitToolName(name string) []string {
	// Split by underscores and hyphens.
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-'
	})
	return parts
}

// keywordOverlap counts how many parts from a appear in b.
func keywordOverlap(a, b []string) int {
	bSet := make(map[string]bool, len(b))
	for _, part := range b {
		bSet[part] = true
	}
	count := 0
	for _, part := range a {
		if bSet[part] {
			count++
		}
	}
	return count
}
