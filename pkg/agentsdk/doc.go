// Package agentsdk provides the public API for building applications on top
// of Rubichan's agent core.
//
// External consumers (web UIs, NATS bridges, chatbots) import this package
// to create and run AI agents without depending on internal/ packages.
//
// # Quick Start
//
// Create an agent with an LLM provider and run a conversation turn:
//
//	agent := agentsdk.NewAgent(myProvider,
//	    agentsdk.WithTools(myRegistry),
//	    agentsdk.WithModel("claude-sonnet-4-5"),
//	    agentsdk.WithSystemPrompt("You are a helpful assistant."),
//	)
//
//	events, err := agent.Turn(ctx, "Hello")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for ev := range events {
//	    switch ev.Type {
//	    case "text_delta":
//	        fmt.Print(ev.Text)
//	    case "tool_call":
//	        fmt.Printf("Calling tool: %s\n", ev.ToolCall.Name)
//	    case "tool_result":
//	        fmt.Printf("Tool result: %s\n", ev.ToolResult.Content)
//	    case "error":
//	        fmt.Printf("Error: %v\n", ev.Error)
//	    case "done":
//	        fmt.Printf("\n[tokens: in=%d out=%d]\n", ev.InputTokens, ev.OutputTokens)
//	    }
//	}
//
// # Architecture
//
// The package is organized into several layers:
//
//   - Types: Message, ContentBlock, ToolDef, StreamEvent, CompletionRequest
//   - Interfaces: LLMProvider, Tool, ApprovalChecker, Summarizer, SubagentSpawner
//   - Agent: NewAgent() constructor with functional options, Turn() for streaming
//   - Registry: Standalone tool registry for managing available tools
//   - Config: AgentConfig with sensible defaults, Logger interface
//
// # Event Types
//
// The Turn() method returns a channel of TurnEvent. Event types are:
//
//   - "text_delta": Incremental text from the LLM (ev.Text)
//   - "tool_call": The LLM wants to call a tool (ev.ToolCall)
//   - "tool_result": A tool execution completed (ev.ToolResult)
//   - "tool_progress": Streaming output from a tool (ev.ToolProgress)
//   - "error": An error occurred (ev.Error)
//   - "done": The turn completed (ev.InputTokens, ev.OutputTokens)
//   - "subagent_done": A child agent completed (ev.SubagentResult)
//
// # Custom Tools
//
// Implement the Tool interface to create custom tools:
//
//	type MyTool struct{}
//
//	func (t *MyTool) Name() string              { return "my_tool" }
//	func (t *MyTool) Description() string       { return "Does something useful" }
//	func (t *MyTool) InputSchema() json.RawMessage {
//	    return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)
//	}
//	func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
//	    var args struct{ Query string `json:"query"` }
//	    json.Unmarshal(input, &args)
//	    return agentsdk.ToolResult{Content: "result for: " + args.Query}, nil
//	}
//
// Register tools with a Registry and pass it to the agent:
//
//	registry := agentsdk.NewRegistry()
//	registry.Register(&MyTool{})
//	agent := agentsdk.NewAgent(provider, agentsdk.WithTools(registry))
package agentsdk
