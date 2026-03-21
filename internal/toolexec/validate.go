package toolexec

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// SchemaValidationMiddleware returns a Middleware that validates tool input
// against the tool's declared InputSchema before execution. It checks that
// the input is a valid JSON object and that all required fields are present.
// Parsed required-field lists are cached per tool name to avoid re-parsing
// schema JSON on every invocation.
func SchemaValidationMiddleware(lookup ToolLookup) Middleware {
	var mu sync.RWMutex
	cache := make(map[string][]string) // tool name → required fields

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			required := cachedRequired(&mu, cache, lookup, tc.Name)
			if required == nil {
				return next(ctx, tc)
			}

			if err := validateInput(tc.Input, required); err != nil {
				return Result{
					Content: fmt.Sprintf("invalid input for tool %q: %s", tc.Name, err),
					IsError: true,
				}
			}
			return next(ctx, tc)
		}
	}
}

// cachedRequired returns the required fields for a tool, parsing and caching
// on first access. Returns nil if the tool has no schema or no required fields.
func cachedRequired(mu *sync.RWMutex, cache map[string][]string, lookup ToolLookup, name string) []string {
	mu.RLock()
	req, cached := cache[name]
	mu.RUnlock()
	if cached {
		return req
	}

	tool, ok := lookup.Get(name)
	if !ok {
		return nil
	}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		return nil
	}

	var schemaDef struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &schemaDef); err != nil {
		return nil
	}

	// Store empty slice (not nil) to distinguish "parsed, no required fields"
	// from "not yet cached". Tools with schemas but no required fields still
	// get JSON structure validation.
	req = schemaDef.Required
	if req == nil {
		req = []string{}
	}

	mu.Lock()
	cache[name] = req
	mu.Unlock()
	return req
}

// validateInput checks that input is a valid JSON object and that all
// required fields are present.
func validateInput(input json.RawMessage, required []string) error {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return fmt.Errorf("input must be a JSON object: %w", err)
	}

	for _, field := range required {
		if _, ok := obj[field]; !ok {
			return fmt.Errorf("missing required field %q", field)
		}
	}

	return nil
}
