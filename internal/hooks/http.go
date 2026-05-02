package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxHookResponseSize limits how much data we read from a hook response.
// Prevents OOM from misconfigured or malicious hook servers.
const maxHookResponseSize = 1 << 20 // 1 MiB

// sharedHTTPClient is reused across hook calls for connection pooling.
// No timeout here — per-request timeouts are set via context in executeHTTPHook.
var sharedHTTPClient = &http.Client{}

// failOpen returns a continue result and a wrapped error. Used for all
// error paths in executeHTTPHook so a broken hook cannot block execution.
func failOpen(err error) (HookResult, error) {
	return HookResult{Continue: true}, err
}

// executeHTTPHook sends a POST request to the configured URL with the
// event data as JSON. Returns the hook result parsed from the response.
//
// Fail-open design: all errors return Continue=true so a broken hook
// cannot block execution. Operators should monitor logs for hook failures.
func executeHTTPHook(cfg UserHookConfig, data map[string]interface{}) (HookResult, error) {
	payload := map[string]interface{}{
		"event": cfg.Event,
		"data":  data,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return failOpen(fmt.Errorf("marshal failed: %w", err))
	}

	// Use context timeout only; http.Client.Timeout is redundant and can
	// cause confusing error types when both fire.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return failOpen(fmt.Errorf("request build failed: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return failOpen(fmt.Errorf("HTTP request failed: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxHookResponseSize))
	if err != nil {
		return failOpen(fmt.Errorf("read body failed: %w", err))
	}

	var result HookResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Non-JSON response: log the parse error and continue.
		return failOpen(fmt.Errorf("unmarshal hook response: %w", err))
	}

	if result.Cancel {
		return result, nil
	}
	return HookResult{
		Continue:       true,
		Message:        result.Message,
		UpdatedInput:   result.UpdatedInput,
		ModifiedOutput: result.ModifiedOutput,
	}, nil
}

// HookResult captures the response from a hook execution.
//
// Hooks can control execution via Cancel, return messages for logging,
// mutate tool input via UpdatedInput, or modify output via ModifiedOutput.
type HookResult struct {
	Continue       bool                   `json:"continue"`
	Cancel         bool                   `json:"cancel"`
	Message        string                 `json:"message"`
	UpdatedInput   map[string]interface{} `json:"updated_input,omitempty"`
	ModifiedOutput string                 `json:"modified_output,omitempty"`
}
