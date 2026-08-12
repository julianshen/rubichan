package a2atools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// DispatchTool picks the best-matching known agent for a task description
// (via Registry.Match's deterministic tag/word overlap, not an LLM call)
// and calls it.
type DispatchTool struct {
	cfg        *config.Config
	registry   *a2a.Registry
	httpClient *http.Client  // overridable in tests; nil means defaultA2AHTTPClient (SSRF-guarded)
	pollEvery  time.Duration // overridable in tests
	pollFor    time.Duration
}

// NewDispatchTool creates a DispatchTool over reg.
func NewDispatchTool(cfg *config.Config, reg *a2a.Registry) *DispatchTool {
	return &DispatchTool{
		cfg:       cfg,
		registry:  reg,
		pollEvery: 500 * time.Millisecond,
		pollFor:   2 * time.Minute,
	}
}

func (d *DispatchTool) Name() string { return "a2a_dispatch" }

func (d *DispatchTool) Description() string {
	return "Find the best-matching known A2A agent for a task description (by skill tags, " +
		"not an LLM call) and call it. Returns an error if no agent matches."
}

func (d *DispatchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task_description": {"type": "string", "description": "What needs to be done"},
			"fanout": {"type": "boolean", "description": "If true, dispatch to every matching agent above the threshold instead of just the best match"}
		},
		"required": ["task_description"]
	}`)
}

type dispatchInput struct {
	TaskDescription string `json:"task_description"`
	Fanout          bool   `json:"fanout"`
}

func (d *DispatchTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	var in dispatchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	matches := d.registry.Match(in.TaskDescription)
	if len(matches) == 0 {
		return agentsdk.ToolResult{Content: "no known agent matches this task description", IsError: true}, nil
	}

	if !in.Fanout {
		return d.dispatchTo(ctx, matches[0], in.TaskDescription)
	}

	// fanout=true: Registry.Match already excludes agents with zero
	// tag/word overlap (see registry.go), so its full result list already
	// is "every agent with a genuine match" — call each one and return all
	// results together, uninterpreted, for the calling agent turn to
	// synthesize itself (per the design spec's Phase 3 data flow).
	var b strings.Builder
	anySucceeded := false
	for i, name := range matches {
		if i > 0 {
			b.WriteString("\n")
		}
		res, err := d.dispatchTo(ctx, name, in.TaskDescription)
		if err != nil {
			return agentsdk.ToolResult{}, err
		}
		b.WriteString(res.Content)
		if !res.IsError {
			anySucceeded = true
		}
	}
	return agentsdk.ToolResult{Content: b.String(), IsError: !anySucceeded}, nil
}

// dispatchTo sends taskDescription to the named agent, polling until the
// resulting task reaches a terminal state (or the configured pollFor
// deadline passes), and formats the outcome as a single ToolResult prefixed
// with the agent's name. It is the unit of work reused for both the
// single-best-match path and, when fanout is requested, once per matched
// agent.
func (d *DispatchTool) dispatchTo(ctx context.Context, name, taskDescription string) (agentsdk.ToolResult, error) {
	url, apiKey, err := resolveAgentURL(d.cfg, name)
	if err != nil {
		return agentsdk.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client := a2a.NewClient(url, apiKey, httpClientOrDefault(d.httpClient))
	msg := a2a.Message{Role: "user", Parts: []a2a.Part{{Kind: "text", Text: taskDescription}}}

	deadline := time.Now().Add(d.pollFor)

	sendCtx, cancel := context.WithTimeout(ctx, d.pollFor)
	task, err := client.Send(sendCtx, msg)
	cancel()
	if err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("dispatch to %s failed: %v", name, err), IsError: true}, nil
	}

	// Not every A2A agent responds synchronously the way rubichan's own
	// server currently does; Send may legitimately return a task still in
	// submitted/working state. Poll until it reaches a terminal state or
	// the deadline passes.
	for !isTerminal(task.State) && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return agentsdk.ToolResult{Content: "dispatch canceled", IsError: true}, nil
		case <-time.After(d.pollEvery):
		}

		// Bound each poll call so a remote agent that accepts the
		// connection but never responds can't block Execute past the
		// configured pollFor budget.
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		pollCtx, cancel := context.WithTimeout(ctx, remaining)
		task, err = client.GetTask(pollCtx, task.ID)
		cancel()
		if err != nil {
			return agentsdk.ToolResult{Content: fmt.Sprintf("poll failed: %v", err), IsError: true}, nil
		}
	}

	if task.State != a2a.TaskCompleted {
		if !isTerminal(task.State) {
			return agentsdk.ToolResult{Content: fmt.Sprintf("gave up waiting for %s after %s: task last reported state %q", name, d.pollFor, task.State), IsError: true}, nil
		}
		return agentsdk.ToolResult{Content: fmt.Sprintf("%s ended in state %q: %s", name, task.State, task.Error), IsError: true}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[%s] ", name)
	for _, art := range task.Artifacts {
		for _, p := range art.Parts {
			b.WriteString(p.Text)
		}
	}
	return agentsdk.ToolResult{Content: b.String()}, nil
}
