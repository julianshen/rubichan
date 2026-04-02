// Package depcheck provides shared utilities for detecting dependency-resolution
// commands across the codebase.
package depcheck

import "strings"

// LooksLikeDependencyCommandIntent returns true when a command string contains
// indicators that the user intended to install or resolve dependencies (e.g.,
// "install", "pip", "go mod", etc.).
func LooksLikeDependencyCommandIntent(command string) bool {
	return strings.Contains(command, "install") ||
		strings.Contains(command, "npm i") ||
		strings.Contains(command, "pnpm i") ||
		strings.Contains(command, "yarn add") ||
		strings.Contains(command, "pip") ||
		strings.Contains(command, "go mod") ||
		strings.Contains(command, "go get") ||
		strings.Contains(command, "mvn")
}
