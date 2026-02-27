package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
)

// ApprovalFunc is called before executing a tool to get user approval.
// Returns true if the tool execution is approved.
type ApprovalFunc func(ctx context.Context, tool string, input json.RawMessage) (bool, error)

// TurnEvent represents a streaming event emitted during an agent turn.
type TurnEvent struct {
	Type       string           // "text_delta", "tool_call", "tool_result", "error", "done"
	Text       string           // text content for text_delta events
	ToolCall   *ToolCallEvent   // populated for tool_call events
	ToolResult *ToolResultEvent // populated for tool_result events
	Error      error            // populated for error events
}

// ToolCallEvent contains details about a tool being called.
type ToolCallEvent struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResultEvent contains details about a tool execution result.
type ToolResultEvent struct {
	ID      string
	Name    string
	Content string
	IsError bool
}

// AgentOption is a functional option for configuring an Agent.
type AgentOption func(*Agent)

// WithSkillRuntime attaches a skill runtime to the agent, enabling hook
// dispatch and prompt fragment injection.
func WithSkillRuntime(rt *skills.Runtime) AgentOption {
	return func(a *Agent) {
		a.skillRuntime = rt
	}
}

// WithStore attaches a persistence store to the agent, enabling automatic
// session and message saving.
func WithStore(st *store.Store) AgentOption {
	return func(a *Agent) {
		a.store = st
	}
}

// WithResumeSession configures the agent to resume an existing session
// instead of creating a new one.
func WithResumeSession(sessionID string) AgentOption {
	return func(a *Agent) {
		a.resumeSessionID = sessionID
	}
}

// WithCompactionStrategies configures the compaction strategy chain for the
// context manager. Strategies run in order from lightest to heaviest.
func WithCompactionStrategies(strategies ...CompactionStrategy) AgentOption {
	return func(a *Agent) {
		a.context.SetStrategies(strategies)
	}
}

// WithSummarizer attaches an LLM-backed summarizer to the agent, enabling
// the summarization compaction strategy.
func WithSummarizer(s Summarizer) AgentOption {
	return func(a *Agent) {
		a.summarizer = s
	}
}

// WithMemoryStore attaches a memory store for cross-session learning.
func WithMemoryStore(ms MemoryStore) AgentOption {
	return func(a *Agent) {
		a.memoryStore = ms
	}
}

// WithAgentMD injects project-level AGENT.md content into the system prompt.
func WithAgentMD(content string) AgentOption {
	return func(a *Agent) {
		a.agentMD = content
	}
}

// namedPrompt is a named system prompt section appended after the base prompt.
type namedPrompt struct {
	Name    string
	Content string
}

// WithExtraSystemPrompt appends a named section to the system prompt.
// Multiple calls accumulate sections. Each section appears as:
//
//	## {name}
//	{content}
func WithExtraSystemPrompt(name, content string) AgentOption {
	return func(a *Agent) {
		a.extraPrompts = append(a.extraPrompts, namedPrompt{Name: name, Content: content})
	}
}

// Agent orchestrates the conversation loop between the user, LLM, and tools.
type Agent struct {
	provider        provider.LLMProvider
	tools           *tools.Registry
	conversation    *Conversation
	context         *ContextManager
	approve         ApprovalFunc
	model           string
	maxTurns        int
	skillRuntime    *skills.Runtime
	store           *store.Store
	sessionID       string
	resumeSessionID string
	agentMD         string
	extraPrompts    []namedPrompt
	summarizer      Summarizer
	scratchpad      *Scratchpad
	memoryStore     MemoryStore
}

// New creates a new Agent with the given provider, tool registry, approval
// function, and configuration. Optional AgentOption values can be provided
// to attach a skill runtime.
func New(p provider.LLMProvider, t *tools.Registry, approve ApprovalFunc, cfg *config.Config, opts ...AgentOption) *Agent {
	systemPrompt := buildSystemPrompt(cfg)
	a := &Agent{
		provider:     p,
		tools:        t,
		conversation: NewConversation(systemPrompt),
		context:      NewContextManager(cfg.Agent.ContextBudget),
		approve:      approve,
		model:        cfg.Provider.Model,
		maxTurns:     cfg.Agent.MaxTurns,
		scratchpad:   NewScratchpad(),
	}
	for _, opt := range opts {
		opt(a)
	}
	// Rebuild system prompt if AGENT.md content was provided.
	if a.agentMD != "" {
		prompt := a.conversation.SystemPrompt() +
			"\n\n## Project Guidelines (from AGENT.md)\n\n" + a.agentMD
		a.conversation = NewConversation(prompt)
	}
	// Append extra system prompt sections (e.g., from apple-dev skill).
	if len(a.extraPrompts) > 0 {
		prompt := a.conversation.SystemPrompt()
		for _, ep := range a.extraPrompts {
			prompt += "\n\n## " + ep.Name + "\n\n" + ep.Content
		}
		a.conversation = NewConversation(prompt)
	}
	// Load cross-session memories into system prompt.
	if a.memoryStore != nil {
		wd, _ := os.Getwd()
		memories, err := a.memoryStore.LoadMemories(wd)
		if err != nil {
			log.Printf("warning: failed to load memories: %v", err)
		} else if len(memories) > 0 {
			prompt := a.conversation.SystemPrompt()
			prompt += "\n\n## Prior Session Insights\n\n"
			for _, m := range memories {
				prompt += fmt.Sprintf("- **%s**: %s\n", m.Tag, m.Content)
			}
			a.conversation = NewConversation(prompt)
		}
	}
	// If a summarizer was provided, insert the summarization strategy
	// between tool clearing and truncation in the compaction chain.
	if a.summarizer != nil {
		a.context.SetStrategies([]CompactionStrategy{
			NewToolResultClearingStrategy(),
			NewSummarizationStrategy(context.Background(), a.summarizer),
			&truncateStrategy{},
		})
	}
	if a.store != nil {
		if a.resumeSessionID != "" {
			// Resume existing session.
			sess, err := a.store.GetSession(a.resumeSessionID)
			if err != nil || sess == nil {
				log.Printf("warning: failed to resume session %s: %v", a.resumeSessionID, err)
			} else {
				a.sessionID = sess.ID
				a.conversation = NewConversation(sess.SystemPrompt)
				msgs, err := a.store.GetMessages(sess.ID)
				if err != nil {
					log.Printf("warning: failed to load messages: %v", err)
				} else {
					providerMsgs := make([]provider.Message, len(msgs))
					for i, m := range msgs {
						providerMsgs[i] = provider.Message{
							Role:    m.Role,
							Content: m.Content,
						}
					}
					a.conversation.LoadFromMessages(providerMsgs)
				}
			}
		}

		if a.sessionID == "" {
			// Create new session (either no resume requested, or resume failed).
			a.sessionID = uuid.New().String()
			wd, _ := os.Getwd()
			sess := store.Session{
				ID:           a.sessionID,
				Model:        a.model,
				WorkingDir:   wd,
				SystemPrompt: a.conversation.SystemPrompt(),
			}
			if err := a.store.CreateSession(sess); err != nil {
				log.Printf("warning: failed to create session: %v", err)
				a.store = nil // disable persistence for this session
			}
		}
	}
	return a
}

// buildSystemPrompt constructs the system prompt from configuration.
func buildSystemPrompt(_ *config.Config) string {
	return "You are a helpful AI coding assistant. You can read and write files, " +
		"execute shell commands, and help with software development tasks."
}

// ClearConversation removes all messages from the conversation history,
// preserving the system prompt.
func (a *Agent) ClearConversation() {
	a.conversation.Clear()
}

// ScratchpadAccess returns the agent's scratchpad for external use (e.g., by NotesTool).
func (a *Agent) ScratchpadAccess() ScratchpadAccess {
	return a.scratchpad
}

// SaveMemories extracts reusable insights from the conversation and persists
// them. Call on session end for cross-session learning.
func (a *Agent) SaveMemories(ctx context.Context) error {
	if a.memoryStore == nil || a.summarizer == nil {
		return nil
	}

	extractor := NewMemoryExtractor(a.summarizer)
	memories, err := extractor.Extract(ctx, a.conversation.Messages())
	if err != nil {
		return fmt.Errorf("extracting memories: %w", err)
	}

	wd, _ := os.Getwd()
	for _, m := range memories {
		if err := a.memoryStore.SaveMemory(wd, m.Tag, m.Content); err != nil {
			log.Printf("warning: failed to save memory %q: %v", m.Tag, err)
		}
	}
	return nil
}

// SetModel changes the model used for LLM completions.
func (a *Agent) SetModel(model string) {
	a.model = model
}

// persistToolResult saves a tool result message to the store.
func (a *Agent) persistToolResult(toolUseID, content string, isError bool) {
	a.persistMessage("user", []provider.ContentBlock{
		{Type: "tool_result", ToolUseID: toolUseID, Text: content, IsError: isError},
	})
}

// persistMessage saves a message to the store. Errors are logged but non-fatal.
func (a *Agent) persistMessage(role string, content []provider.ContentBlock) {
	if a.store == nil {
		return
	}
	if err := a.store.AppendMessage(a.sessionID, role, content); err != nil {
		log.Printf("warning: failed to persist message: %v", err)
	}
}

// Turn initiates a new agent turn with the given user message. It returns a
// channel of TurnEvent that streams events as the agent processes the turn.
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error) {
	a.conversation.AddUser(userMessage)
	a.persistMessage("user", []provider.ContentBlock{{Type: "text", Text: userMessage}})
	a.context.Compact(a.conversation)

	ch := make(chan TurnEvent, 64)
	go func() {
		defer close(ch)
		a.runLoop(ctx, ch, 0)
	}()
	return ch, nil
}

// buildSystemPromptWithFragments returns the base system prompt with any
// skill prompt fragments and scratchpad content appended.
func (a *Agent) buildSystemPromptWithFragments() string {
	base := a.conversation.SystemPrompt()

	result := base

	// Inject scratchpad notes if any exist.
	if a.scratchpad != nil {
		rendered := a.scratchpad.Render()
		if rendered != "" {
			result += "\n\n" + rendered
		}
	}

	if a.skillRuntime == nil {
		return result
	}

	fragments := a.skillRuntime.GetPromptFragments()
	for _, f := range fragments {
		if f.ResolvedPrompt != "" {
			result += "\n\n" + f.ResolvedPrompt
		}
	}
	return result
}

// dispatchHook dispatches a hook event via the skill runtime. If no runtime is
// configured, it returns nil. This is safe to call even when skillRuntime is nil.
func (a *Agent) dispatchHook(event skills.HookEvent) (*skills.HookResult, error) {
	if a.skillRuntime == nil {
		return nil, nil
	}
	return a.skillRuntime.DispatchHook(event)
}

// runLoop iteratively processes LLM responses and tool calls.
func (a *Agent) runLoop(ctx context.Context, ch chan<- TurnEvent, turnCount int) {
	for ; turnCount < a.maxTurns; turnCount++ {
		if ctx.Err() != nil {
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- TurnEvent{Type: "done"}
			return
		}

		// Build the system prompt, injecting prompt fragments from skills.
		systemPrompt := a.buildSystemPromptWithFragments()

		req := provider.CompletionRequest{
			Model:     a.model,
			System:    systemPrompt,
			Messages:  a.conversation.Messages(),
			Tools:     a.tools.SelectForContext(a.conversation.Messages()),
			MaxTokens: 4096,
		}

		stream, err := a.provider.Stream(ctx, req)
		if err != nil {
			ch <- TurnEvent{Type: "error", Error: fmt.Errorf("provider stream: %w", err)}
			ch <- TurnEvent{Type: "done"}
			return
		}

		// Accumulate assistant content blocks and track tool calls
		var blocks []provider.ContentBlock
		var pendingTools []provider.ToolUseBlock
		var currentTextBuf string
		var currentTool *provider.ToolUseBlock
		var toolInputBuf string

		finalizeTool := func() {
			if currentTool != nil {
				currentTool.Input = json.RawMessage(toolInputBuf)
				pendingTools = append(pendingTools, *currentTool)
				blocks = append(blocks, provider.ContentBlock{
					Type:  "tool_use",
					ID:    currentTool.ID,
					Name:  currentTool.Name,
					Input: currentTool.Input,
				})
				currentTool = nil
				toolInputBuf = ""
			}
		}

		finalizeText := func() {
			if currentTextBuf != "" {
				blocks = append(blocks, provider.ContentBlock{
					Type: "text",
					Text: currentTextBuf,
				})
				currentTextBuf = ""
			}
		}

		for event := range stream {
			switch event.Type {
			case "text_delta":
				if currentTool != nil {
					// Accumulating tool input JSON fragments
					toolInputBuf += event.Text
				} else {
					// Regular text content
					currentTextBuf += event.Text
					ch <- TurnEvent{Type: "text_delta", Text: event.Text}
				}

			case "tool_use":
				// Finalize any pending text block
				finalizeText()
				// Finalize any previous tool
				finalizeTool()
				// Start new tool accumulation
				currentTool = &provider.ToolUseBlock{
					ID:   event.ToolUse.ID,
					Name: event.ToolUse.Name,
				}

			case "error":
				ch <- TurnEvent{Type: "error", Error: event.Error}

			case "stop":
				// Will be handled after the loop
			}
		}

		// Finalize any remaining text or tool
		finalizeText()
		finalizeTool()

		// Add assistant message with accumulated blocks
		if len(blocks) > 0 {
			a.conversation.AddAssistant(blocks)
			a.persistMessage("assistant", blocks)
		}

		// If no pending tool calls, we're done
		if len(pendingTools) == 0 {
			ch <- TurnEvent{Type: "done"}
			return
		}

		// Execute tool calls
		for _, tc := range pendingTools {
			if ctx.Err() != nil {
				ch <- TurnEvent{Type: "error", Error: ctx.Err()}
				ch <- TurnEvent{Type: "done"}
				return
			}

			ch <- TurnEvent{
				Type: "tool_call",
				ToolCall: &ToolCallEvent{
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				},
			}

			// Dispatch HookOnBeforeToolCall hook. If cancelled, skip execution.
			hookResult, hookErr := a.dispatchHook(skills.HookEvent{
				Phase: skills.HookOnBeforeToolCall,
				Data: map[string]any{
					"tool_name": tc.Name,
					"input":     string(tc.Input),
				},
				Ctx: ctx,
			})
			if hookErr != nil {
				result := fmt.Sprintf("hook error: %s", hookErr)
				a.conversation.AddToolResult(tc.ID, result, true)
				a.persistToolResult(tc.ID, result, true)
				ch <- TurnEvent{
					Type: "tool_result",
					ToolResult: &ToolResultEvent{
						ID:      tc.ID,
						Name:    tc.Name,
						Content: result,
						IsError: true,
					},
				}
				continue
			}
			if hookResult != nil && hookResult.Cancel {
				result := "tool call cancelled by skill"
				a.conversation.AddToolResult(tc.ID, result, true)
				a.persistToolResult(tc.ID, result, true)
				ch <- TurnEvent{
					Type: "tool_result",
					ToolResult: &ToolResultEvent{
						ID:      tc.ID,
						Name:    tc.Name,
						Content: result,
						IsError: true,
					},
				}
				continue
			}

			// Check approval
			approved, approvalErr := a.approve(ctx, tc.Name, tc.Input)
			if approvalErr != nil {
				result := fmt.Sprintf("approval error: %s", approvalErr)
				a.conversation.AddToolResult(tc.ID, result, true)
				a.persistToolResult(tc.ID, result, true)
				ch <- TurnEvent{
					Type: "tool_result",
					ToolResult: &ToolResultEvent{
						ID:      tc.ID,
						Name:    tc.Name,
						Content: result,
						IsError: true,
					},
				}
				continue
			}

			if !approved {
				result := "tool call denied by user"
				a.conversation.AddToolResult(tc.ID, result, true)
				a.persistToolResult(tc.ID, result, true)
				ch <- TurnEvent{
					Type: "tool_result",
					ToolResult: &ToolResultEvent{
						ID:      tc.ID,
						Name:    tc.Name,
						Content: result,
						IsError: true,
					},
				}
				continue
			}

			// Look up and execute the tool
			tool, found := a.tools.Get(tc.Name)
			if !found {
				result := fmt.Sprintf("unknown tool: %s", tc.Name)
				a.conversation.AddToolResult(tc.ID, result, true)
				a.persistToolResult(tc.ID, result, true)
				ch <- TurnEvent{
					Type: "tool_result",
					ToolResult: &ToolResultEvent{
						ID:      tc.ID,
						Name:    tc.Name,
						Content: result,
						IsError: true,
					},
				}
				continue
			}

			toolResult, execErr := tool.Execute(ctx, tc.Input)
			if execErr != nil {
				result := fmt.Sprintf("tool execution error: %s", execErr)
				a.conversation.AddToolResult(tc.ID, result, true)
				a.persistToolResult(tc.ID, result, true)
				ch <- TurnEvent{
					Type: "tool_result",
					ToolResult: &ToolResultEvent{
						ID:      tc.ID,
						Name:    tc.Name,
						Content: result,
						IsError: true,
					},
				}
				continue
			}

			// Dispatch HookOnAfterToolResult hook. If modified, use the new content.
			afterResult, afterErr := a.dispatchHook(skills.HookEvent{
				Phase: skills.HookOnAfterToolResult,
				Data: map[string]any{
					"tool_name": tc.Name,
					"content":   toolResult.Content,
					"is_error":  toolResult.IsError,
				},
				Ctx: ctx,
			})
			if afterErr == nil && afterResult != nil && afterResult.Modified != nil {
				if modContent, ok := afterResult.Modified["content"].(string); ok {
					toolResult.Content = modContent
				}
			}

			a.conversation.AddToolResult(tc.ID, toolResult.Content, toolResult.IsError)
			a.persistToolResult(tc.ID, toolResult.Content, toolResult.IsError)
			ch <- TurnEvent{
				Type: "tool_result",
				ToolResult: &ToolResultEvent{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: toolResult.Content,
					IsError: toolResult.IsError,
				},
			}
		}

		// Continue to the next turn after tool results.
	}

	// Reached max turns.
	ch <- TurnEvent{Type: "error", Error: fmt.Errorf("max turns (%d) exceeded", a.maxTurns)}
	ch <- TurnEvent{Type: "done"}
}
