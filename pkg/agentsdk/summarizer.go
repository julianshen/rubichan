package agentsdk

import "context"

// Summarizer condenses a sequence of messages into a short text summary.
type Summarizer interface {
	Summarize(ctx context.Context, messages []Message) (string, error)
}
