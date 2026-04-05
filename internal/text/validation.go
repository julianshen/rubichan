package text

import "strings"

// IsEmptyResponse reports whether a string is empty or contains only whitespace.
func IsEmptyResponse(s string) bool {
	return strings.TrimSpace(s) == ""
}
