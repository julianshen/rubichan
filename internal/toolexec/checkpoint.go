package toolexec

import (
	"context"
	"encoding/json"
	"log"

	"github.com/julianshen/rubichan/internal/checkpoint"
)

// CheckpointMiddleware returns a Middleware that captures file state before
// write/patch operations. If mgr is nil, the middleware passes through.
func CheckpointMiddleware(mgr *checkpoint.Manager, turnCounter func() int) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			if mgr == nil || tc.Name != "file" {
				return next(ctx, tc)
			}

			var input struct {
				Operation string `json:"operation"`
				Path      string `json:"path"`
			}
			if err := json.Unmarshal(tc.Input, &input); err != nil {
				return next(ctx, tc)
			}

			if input.Operation != "write" && input.Operation != "patch" {
				return next(ctx, tc)
			}

			turn := 0
			if turnCounter != nil {
				turn = turnCounter()
			}

			if _, err := mgr.Capture(ctx, input.Path, turn, input.Operation); err != nil {
				log.Printf("checkpoint capture failed: %v", err)
			}

			return next(ctx, tc)
		}
	}
}
