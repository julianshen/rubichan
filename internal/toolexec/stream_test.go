package toolexec

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
)

func TestWithToolEventEmitterRoundTrip(t *testing.T) {
	var called bool
	emit := tools.ToolEventEmitter(func(ev tools.ToolEvent) {
		called = true
	})

	ctx := WithToolEventEmitter(context.Background(), emit)
	got := ToolEventEmitterFromContext(ctx)

	assert.NotNil(t, got)
	got(tools.ToolEvent{Stage: tools.EventDelta, Content: "test"})
	assert.True(t, called)
}

func TestWithToolEventEmitterNilReturnsOriginalContext(t *testing.T) {
	ctx := context.Background()
	out := WithToolEventEmitter(ctx, nil)
	assert.Equal(t, ctx, out, "nil emitter should return the original context unchanged")
}

func TestToolEventEmitterFromContextMissing(t *testing.T) {
	got := ToolEventEmitterFromContext(context.Background())
	assert.Nil(t, got, "missing emitter should return nil")
}
