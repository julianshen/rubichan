// Package modelcheck answers "is this provider and model actually usable?"
// for the CLI's --test flag. It reports the capabilities rubichan believes a
// model has, then verifies the belief against the live endpoint: one
// completion to prove connectivity and streaming, and — when the model is
// expected to support them — one tool call to prove native tool use.
//
// The probes live here rather than in cmd/ because what counts as a working
// model is product behaviour. Composition stays with the caller: it loads
// config and builds the provider, then hands both to Run.
package modelcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// probeToolName is the tool offered during the tool-use probe. Nothing
// executes it — emitting the call is the whole signal.
const probeToolName = "capability_probe"

// Run reports model capabilities to out and probes the endpoint to confirm
// them. It returns an error when the model cannot be reached, when the stream
// ends without a stop event, or when a probe fails outright. A model that
// simply declines to call the probe tool is reported as inconclusive rather
// than failed: the model answered, it just did not take the bait.
func Run(ctx context.Context, out io.Writer, p provider.LLMProvider, providerName, model string) error {
	if p == nil {
		return fmt.Errorf("provider is nil")
	}
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("model is not configured")
	}

	caps := provider.DetectCapabilities(providerName, model)
	fmt.Fprintf(out, "Provider: %s\nModel: %s\n", providerName, model)
	fmt.Fprintf(out, "Capabilities: native_tool_use=%t system_prompt=%t tool_discovery_hint=%t max_tool_count=%d reasoning_effort=%q\n",
		caps.SupportsNativeToolUse,
		caps.SupportsSystemPrompt,
		caps.NeedsToolDiscoveryHint,
		caps.MaxToolCount,
		caps.ReasoningEffort,
	)

	if err := probeConnectivity(ctx, out, p, model); err != nil {
		return err
	}

	if caps.SupportsNativeToolUse {
		toolSupported, err := probeToolSupport(ctx, p, model)
		if err != nil {
			return err
		}
		if !toolSupported {
			fmt.Fprintln(out, "Tool support: INCONCLUSIVE (no tool_use emitted during probe)")
		} else {
			fmt.Fprintln(out, "Tool support: PASS")
		}
	} else {
		// Unreachable through provider.DetectCapabilities as it stands:
		// agentsdk.DefaultCapabilities enables native tool use and no
		// profile disables it, so every provider/model pair reports true.
		// Kept because the capability is a real field the agent consults
		// elsewhere — if a profile ever turns it off, this is the honest
		// report — but it is untested for that reason.
		fmt.Fprintln(out, "Tool support: SKIPPED (model capability indicates no native tool use)")
	}

	fmt.Fprintln(out, "\nModel test: PASS")
	return nil
}

// probeConnectivity asks for a one-word reply and echoes whatever comes back,
// proving the endpoint is reachable and that streaming works end to end.
func probeConnectivity(ctx context.Context, out io.Writer, p provider.LLMProvider, model string) error {
	req := provider.CompletionRequest{
		Model:     model,
		Messages:  []provider.Message{provider.NewUserMessage("Reply with exactly: OK")},
		MaxTokens: 16,
	}

	// The probe owns its context so that returning early — on an error
	// event, or on any path added later — releases the producer. Providers
	// send with `select { case ch <- evt: case <-ctx.Done(): }` and an error
	// event is not the end of the stream: the Ollama and SSE-compatible
	// parsers both emit a parse error and keep scanning. Without this,
	// abandoning the channel parked that goroutine and its connection for
	// the life of the process, since Run is handed context.Background().
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := p.Stream(ctx, req)
	if err != nil {
		return fmt.Errorf("model connectivity test failed: %w", err)
	}

	var sawStop bool
	for evt := range stream {
		switch evt.Type {
		case "text_delta":
			if evt.Text != "" {
				fmt.Fprint(out, evt.Text)
			}
		case "stop":
			sawStop = true
		case "error":
			return fmt.Errorf("model stream test failed: %w", evt.Error)
		}
	}

	if !sawStop {
		return fmt.Errorf("model stream ended without stop event")
	}

	return nil
}

// probeToolSupport asks the model to call one trivial tool and reports whether
// it did. A false return is not a failure — see Run.
func probeToolSupport(ctx context.Context, p provider.LLMProvider, model string) (bool, error) {
	req := provider.CompletionRequest{
		Model: model,
		Messages: []provider.Message{provider.NewUserMessage(
			"Call the " + probeToolName + " tool with an empty JSON object and do not output any text.",
		)},
		Tools: []provider.ToolDef{{
			Name:        probeToolName,
			Description: "Capability probe tool for testing native tool use support",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		}},
		MaxTokens: 64,
	}

	// Owned context, for the same reason as probeConnectivity.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := p.Stream(ctx, req)
	if err != nil {
		return false, fmt.Errorf("tool support test request failed: %w", err)
	}

	var sawStop, calledProbe bool
	for evt := range stream {
		switch evt.Type {
		case "tool_use":
			// Recorded rather than returned on: the stream has to be
			// consumed to the end either way. Providers send with
			// `select { case ch <- evt: case <-ctx.Done(): }`, and this
			// probe's context is not cancelled on the way out, so
			// abandoning the channel would strand the producer goroutine
			// and its connection. Returning early also hid whatever the
			// model did next — Ollama emits tool calls and then reports a
			// missing done signal when the connection drops.
			if evt.ToolUse != nil && evt.ToolUse.Name == probeToolName {
				calledProbe = true
			}
		case "error":
			return false, fmt.Errorf("tool support stream failed: %w", evt.Error)
		case "stop":
			sawStop = true
		}
	}

	// A clean stop is required even when the probe was called, matching how
	// Run treats the connectivity probe: a stream that never finished has not
	// demonstrated anything, whatever it managed to emit first.
	if !sawStop {
		return false, fmt.Errorf("tool support stream ended without stop event")
	}

	return calledProbe, nil
}
