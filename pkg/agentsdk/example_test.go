package agentsdk_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ExampleNewAgent demonstrates creating an agent with a custom tool
// and running a conversation turn.
func ExampleNewAgent() {
	// Create a mock provider for demonstration.
	provider := &exampleProvider{}

	// Create and register a custom tool.
	registry := agentsdk.NewRegistry()
	_ = registry.Register(&greetTool{})

	// Create the agent with options.
	agent := agentsdk.NewAgent(provider,
		agentsdk.WithTools(registry),
		agentsdk.WithModel("claude-sonnet-4-5"),
		agentsdk.WithSystemPrompt("You are a helpful assistant."),
	)

	// Run a turn and consume events.
	events, err := agent.Turn(context.Background(), "Hello!")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	for ev := range events {
		switch ev.Type {
		case "text_delta":
			fmt.Print(ev.Text)
		case "done":
			fmt.Println()
		}
	}
	// Output: Hello from the agent!
}

// exampleProvider is a minimal LLMProvider for examples.
type exampleProvider struct{}

func (p *exampleProvider) Stream(_ context.Context, _ agentsdk.CompletionRequest) (<-chan agentsdk.StreamEvent, error) {
	ch := make(chan agentsdk.StreamEvent, 2)
	ch <- agentsdk.StreamEvent{Type: "text_delta", Text: "Hello from the agent!"}
	ch <- agentsdk.StreamEvent{Type: "stop", InputTokens: 10, OutputTokens: 5}
	close(ch)
	return ch, nil
}

// greetTool is a simple example tool.
type greetTool struct{}

func (t *greetTool) Name() string        { return "greet" }
func (t *greetTool) Description() string { return "Greets someone by name" }
func (t *greetTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
}
func (t *greetTool) Execute(_ context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return agentsdk.ToolResult{Content: "invalid input", IsError: true}, nil
	}
	return agentsdk.ToolResult{Content: fmt.Sprintf("Hello, %s!", args.Name)}, nil
}
