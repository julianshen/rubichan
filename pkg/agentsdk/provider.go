package agentsdk

import "context"

// LLMProvider defines the interface for interacting with an LLM provider.
type LLMProvider interface {
	Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error)
}
