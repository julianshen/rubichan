package agentsdk

// ForkParams captures cache-safe parameters for creating a forked agent.
// Sending identical values ensures the provider's prompt cache is shared.
type ForkParams struct {
	SystemPrompt     string
	Model            string
	Tools            []ToolDef
	CacheBreakpoints []int
	MaxTokens        int
	Temperature      *float64
}

// ForkResult holds the outcome of a forked agent run.
type ForkResult struct {
	Summary      string
	InputTokens  int
	OutputTokens int
	Error        error
}
