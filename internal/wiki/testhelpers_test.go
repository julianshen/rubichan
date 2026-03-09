package wiki

import "context"

type cancelingLLMCompleter struct{}

func (c *cancelingLLMCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}
