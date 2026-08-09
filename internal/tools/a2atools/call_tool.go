package a2atools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// CallTool sends a message to a remote A2A agent and returns its result,
// polling if the task doesn't complete synchronously.
type CallTool struct {
	cfg        *config.Config
	tasks      a2a.TaskStore
	httpClient *http.Client
	pollEvery  time.Duration // overridable in tests
	pollFor    time.Duration
}

// NewCallTool creates a CallTool. s backs the client-side TaskStore
// (role "client") so a crash mid-poll is resumable.
func NewCallTool(cfg *config.Config, s *store.Store) *CallTool {
	return &CallTool{
		cfg:       cfg,
		tasks:     a2a.NewStoreTaskStore(s, "client"),
		pollEvery: 500 * time.Millisecond,
		pollFor:   2 * time.Minute,
	}
}

func (c *CallTool) Name() string { return "a2a_call" }

func (c *CallTool) Description() string {
	return "Send a message to a remote A2A (Agent2Agent) agent and return its result. " +
		"Pass a configured agent name or a raw URL."
}

func (c *CallTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"agent": {"type": "string", "description": "A configured agent name or a raw base URL"},
			"message": {"type": "string", "description": "The message to send"}
		},
		"required": ["agent", "message"]
	}`)
}

type callInput struct {
	Agent   string `json:"agent"`
	Message string `json:"message"`
}

func (c *CallTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	var in callInput
	if err := json.Unmarshal(input, &in); err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	url, apiKey, err := resolveAgentURL(c.cfg, in.Agent)
	if err != nil {
		return agentsdk.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client := a2a.NewClient(url, apiKey, c.httpClient)
	msg := a2a.Message{Role: "user", Parts: []a2a.Part{{Kind: "text", Text: in.Message}}}

	deadline := time.Now().Add(c.pollFor)

	sendCtx, cancel := context.WithTimeout(ctx, c.pollFor)
	task, err := client.Send(sendCtx, msg)
	cancel()
	if err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("call failed: %v", err), IsError: true}, nil
	}
	if err := c.tasks.Create(ctx, *task); err != nil {
		log.Printf("warning: a2a task store create failed for task %s: %v", task.ID, err)
	}

	for !isTerminal(task.State) && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return agentsdk.ToolResult{Content: "call canceled", IsError: true}, nil
		case <-time.After(c.pollEvery):
		}

		// Bound each poll call so a remote agent that accepts the
		// connection but never responds can't block Execute past
		// the configured pollFor budget.
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
		if err := c.tasks.Update(ctx, *task); err != nil {
			log.Printf("warning: a2a task store update failed for task %s: %v", task.ID, err)
		}
	}

	if task.State == a2a.TaskCompleted {
		var b strings.Builder
		for _, art := range task.Artifacts {
			for _, p := range art.Parts {
				b.WriteString(p.Text)
			}
		}
		return agentsdk.ToolResult{Content: b.String()}, nil
	}

	if !isTerminal(task.State) {
		return agentsdk.ToolResult{Content: fmt.Sprintf("gave up waiting after %s: task last reported state %q", c.pollFor, task.State), IsError: true}, nil
	}
	return agentsdk.ToolResult{Content: fmt.Sprintf("task ended in state %q: %s", task.State, task.Error), IsError: true}, nil
}

func isTerminal(s a2a.TaskState) bool {
	switch s {
	case a2a.TaskCompleted, a2a.TaskFailed, a2a.TaskCanceled, a2a.TaskRejected:
		return true
	default:
		return false
	}
}
