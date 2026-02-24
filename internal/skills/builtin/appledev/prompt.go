package appledev

import _ "embed"

//go:embed system.md
var systemPrompt string

// SystemPrompt returns the Apple platform system prompt for injection into the agent.
func SystemPrompt() string {
	return systemPrompt
}
