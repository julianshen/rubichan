package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/sourcegraph/conc/pool"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
)

// ApprovalFunc is called before executing a tool to get user approval.
// Returns true if the tool execution is approved.
type ApprovalFunc func(ctx context.Context, tool string, input json.RawMessage) (bool, error)

// AutoApproveChecker tests if a tool would be auto-approved without blocking.
// When the approval function (or the object that provides it) implements this
// interface, the agent can execute auto-approved tools in parallel.
type AutoApproveChecker interface {
	IsAutoApproved(tool string) bool
}

// AlwaysAutoApprove is an AutoApproveChecker that approves all tools.
// Use for headless mode or --auto-approve where all tools are pre-approved.
type AlwaysAutoApprove struct{}

// IsAutoApproved always returns true.
func (AlwaysAutoApprove) IsAutoApproved(_ string) bool { return true }

// CheckApproval always returns AutoApproved, implementing ApprovalChecker.
func (AlwaysAutoApprove) CheckApproval(_ string, _ json.RawMessage) ApprovalResult {
	return AutoApproved
}

// TurnEvent represents a streaming event emitted during an agent turn.
type TurnEvent struct {
	Type           string           // "text_delta", "tool_call", "tool_result", "error", "done", "subagent_done"
	Text           string           // text content for text_delta events
	ToolCall       *ToolCallEvent   // populated for tool_call events
	ToolResult     *ToolResultEvent // populated for tool_result events
	ToolProgress   *ToolProgressEvent
	Error          error           // populated for error events
	InputTokens    int             // populated for done events: total input tokens used
	OutputTokens   int             // populated for done events: total output tokens used
	DiffSummary    string          // populated for done events: markdown-formatted cumulative file change summary
	SubagentResult *SubagentResult // populated for subagent_done events
}

// ToolCallEvent contains details about a tool being called.
type ToolCallEvent struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResultEvent contains details about a tool execution result.
type ToolResultEvent struct {
	ID             string
	Name           string
	Content        string
	DisplayContent string // shown to user; falls back to Content if empty
	IsError        bool
}

// ToolProgressEvent contains a streaming progress chunk from a tool execution.
type ToolProgressEvent struct {
	ID      string
	Name    string
	Stage   tools.EventStage
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
// When set, WithSummarizer will not override the custom strategy chain.
func WithCompactionStrategies(strategies ...CompactionStrategy) AgentOption {
	return func(a *Agent) {
		a.customStrategies = true
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

// WithApprovalChecker attaches an input-sensitive checker that determines
// whether tool calls can be auto-approved based on both tool name and input.
func WithApprovalChecker(checker ApprovalChecker) AgentOption {
	return func(a *Agent) {
		a.approvalChecker = checker
	}
}

// WithAutoApproveChecker attaches a legacy tool-name-only checker by wrapping
// it into an ApprovalChecker. Prefer WithApprovalChecker for input-sensitive rules.
func WithAutoApproveChecker(checker AutoApproveChecker) AgentOption {
	return func(a *Agent) {
		a.approvalChecker = &autoApproveAdapter{checker: checker}
	}
}

// WithDiffTracker attaches a DiffTracker to the agent for turn-level
// cumulative change awareness. The tracker is reset at the start of each
// turn and summarized in the "done" event.
func WithDiffTracker(dt *tools.DiffTracker) AgentOption {
	return func(a *Agent) {
		a.diffTracker = dt
	}
}

// WithWakeManager attaches a WakeManager for receiving background subagent
// completion events during the agent loop.
func WithWakeManager(wm *WakeManager) AgentOption {
	return func(a *Agent) {
		a.wakeManager = wm
	}
}

// WithWorkingDir overrides the working directory for all directory-scoped
// operations (file tool, shell tool, skill discovery, session, memories).
// If empty, os.Getwd() is used as fallback.
func WithWorkingDir(dir string) AgentOption {
	return func(a *Agent) { a.workingDir = dir }
}

// WorkingDir returns the agent's effective working directory.
// The value is frozen at construction time and never changes.
func (a *Agent) WorkingDir() string {
	return a.workingDir
}

// WithPipeline attaches a tool execution pipeline to the agent.
func WithPipeline(p *toolexec.Pipeline) AgentOption {
	return func(a *Agent) {
		a.pipeline = p
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
	provider         provider.LLMProvider
	tools            *tools.Registry
	conversation     *Conversation
	context          *ContextManager
	approve          ApprovalFunc
	approvalChecker  ApprovalChecker
	model            string
	maxTurns         int
	skillRuntime     *skills.Runtime
	store            *store.Store
	sessionID        string
	resumeSessionID  string
	agentMD          string
	extraPrompts     []namedPrompt
	summarizer       Summarizer
	scratchpad       *Scratchpad
	memoryStore      MemoryStore
	customStrategies bool
	resultStore      *ResultStore
	promptBuilder    *PromptBuilder
	deferral         *tools.DeferralManager
	diffTracker      *tools.DiffTracker
	turnMu           sync.Mutex // serializes Turn() calls to prevent DiffTracker race
	wakeManager      *WakeManager
	pipeline         *toolexec.Pipeline
	workingDir       string // override working directory (empty = os.Getwd)
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
		context:      newContextManagerFromConfig(cfg),
		approve:      approve,
		model:        cfg.Provider.Model,
		maxTurns:     cfg.Agent.MaxTurns,
		scratchpad:   NewScratchpad(),
	}
	for _, opt := range opts {
		opt(a)
	}
	// Freeze working directory at construction time.
	if a.workingDir == "" {
		a.workingDir, _ = os.Getwd()
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
		wd := a.WorkingDir()
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
	// If a summarizer was provided and the caller didn't set custom
	// strategies, insert summarization between tool clearing and truncation.
	if a.summarizer != nil && !a.customStrategies {
		a.context.SetStrategies([]CompactionStrategy{
			NewToolResultClearingStrategy(),
			NewSummarizationStrategy(a.summarizer),
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
				// Prefer compacted snapshot for resume (avoids re-exceeding
				// context limits). Fall back to full message history for
				// sessions that were never compacted.
				snapMsgs, snapErr := a.store.GetSnapshot(sess.ID)
				if snapErr == nil && snapMsgs != nil {
					a.conversation.LoadFromMessages(snapMsgs)
				} else {
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
		}

		if a.sessionID == "" {
			// Create new session (either no resume requested, or resume failed).
			a.sessionID = uuid.New().String()
			wd := a.WorkingDir()
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

	// Initialize context management subsystems.
	if a.store != nil && a.sessionID != "" {
		threshold := cfg.Agent.ResultOffloadThreshold
		if threshold <= 0 {
			threshold = 4096
		}
		a.resultStore = NewResultStore(a.store, a.sessionID, threshold)

		// Register read_result tool so the LLM can retrieve offloaded results.
		readResultTool := tools.NewReadResultTool(a.resultStore)
		if err := a.tools.Register(readResultTool); err != nil {
			log.Printf("warning: failed to register read_result tool: %v", err)
		}
	}
	a.promptBuilder = NewPromptBuilder()

	// Ensure a pipeline is always available. When no pipeline is provided
	// via WithPipeline, create a default one with hook and output middlewares
	// matching the behavior of the former legacy execution path.
	if a.pipeline == nil {
		var middlewares []toolexec.Middleware

		// Hook middleware for before-tool-call dispatch.
		hookAdapter := &toolexec.SkillHookAdapter{Runtime: a.skillRuntime}
		middlewares = append(middlewares, toolexec.HookMiddleware(hookAdapter))

		// Post-hook middleware for after-tool-result dispatch.
		middlewares = append(middlewares, toolexec.PostHookMiddleware(hookAdapter))

		// Output offloader middleware when persistence is available.
		if a.resultStore != nil {
			offloader := &toolexec.ResultStoreAdapter{Offloader: a.resultStore}
			middlewares = append(middlewares, toolexec.OutputManagerMiddleware(offloader))
		}

		a.pipeline = toolexec.NewPipeline(toolexec.RegistryExecutor(t), middlewares...)
	}

	// Initialize tool deferral manager.
	deferralThreshold := cfg.Agent.ToolDeferralThreshold
	if deferralThreshold <= 0 {
		deferralThreshold = 0.10
	}
	a.deferral = tools.NewDeferralManager(deferralThreshold)

	// Register tool_search tool so the LLM can discover deferred tools.
	// Skip if already present (e.g. subagent inheriting parent's filtered registry).
	toolSearchTool := tools.NewToolSearchTool(a.deferral)
	if _, exists := a.tools.Get(toolSearchTool.Name()); !exists {
		if err := a.tools.Register(toolSearchTool); err != nil {
			log.Printf("warning: failed to register tool_search tool: %v", err)
		}
	}

	// Register compact_context tool for agent-initiated compaction.
	// Skip if already present (e.g. subagent inheriting parent's filtered registry).
	compactTool := tools.NewCompactContextTool(&agentCompactor{agent: a})
	if _, exists := a.tools.Get(compactTool.Name()); !exists {
		if err := a.tools.Register(compactTool); err != nil {
			log.Printf("warning: failed to register compact_context tool: %v", err)
		}
	}

	return a
}

// buildSystemPrompt constructs the system prompt from configuration.
func buildSystemPrompt(_ *config.Config) string {
	return persona.SystemPrompt()
}

// agentCompactor adapts the Agent's ForceCompact to the tools.Compactor interface,
// bridging the agent and tools packages without a circular import.
type agentCompactor struct {
	agent *Agent
}

func (ac *agentCompactor) ForceCompact(ctx context.Context) tools.CompactResult {
	r := ac.agent.context.ForceCompact(ctx, ac.agent.conversation)
	return tools.CompactResult{
		BeforeTokens:   r.BeforeTokens,
		AfterTokens:    r.AfterTokens,
		BeforeMsgCount: r.BeforeMsgCount,
		AfterMsgCount:  r.AfterMsgCount,
		StrategiesRun:  r.StrategiesRun,
	}
}

// newContextManagerFromConfig creates a ContextManager with thresholds from config.
func newContextManagerFromConfig(cfg *config.Config) *ContextManager {
	cm := NewContextManager(cfg.Agent.ContextBudget, cfg.Agent.MaxOutputTokens)
	if cfg.Agent.CompactTrigger > 0 || cfg.Agent.HardBlock > 0 {
		cm.SetThresholds(cfg.Agent.CompactTrigger, cfg.Agent.HardBlock)
	}
	return cm
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

	wd := a.WorkingDir()
	for _, m := range memories {
		if err := a.memoryStore.SaveMemory(wd, m.Tag, m.Content); err != nil {
			return fmt.Errorf("saving memory %q: %w", m.Tag, err)
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

// saveSnapshotIfNeeded persists the current conversation state as a
// resume snapshot if persistence is enabled.
func (a *Agent) saveSnapshotIfNeeded() {
	if a.store == nil || a.sessionID == "" {
		return
	}
	tokens := a.context.EstimateTokens(a.conversation)
	if err := a.store.SaveSnapshot(a.sessionID, a.conversation.Messages(), tokens); err != nil {
		log.Printf("warning: failed to save snapshot: %v", err)
	}
}

// Turn initiates a new agent turn with the given user message. It returns a
// channel of TurnEvent that streams events as the agent processes the turn.
// Concurrent calls are serialized to prevent DiffTracker race conditions.
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error) {
	a.turnMu.Lock()

	a.conversation.AddUser(userMessage)
	a.persistMessage("user", []provider.ContentBlock{{Type: "text", Text: userMessage}})
	a.context.Compact(ctx, a.conversation)
	a.saveSnapshotIfNeeded()

	// Reset the diff tracker so each turn starts with a clean slate.
	if a.diffTracker != nil {
		a.diffTracker.Reset()
	}

	ch := make(chan TurnEvent, 64)
	go func() {
		defer a.turnMu.Unlock()
		defer close(ch)
		a.runLoop(ctx, ch, 0)
	}()
	return ch, nil
}

// DiffTracker returns the agent's diff tracker, or nil if none is attached.
func (a *Agent) DiffTracker() *tools.DiffTracker {
	return a.diffTracker
}

// buildSystemPromptWithFragments returns the assembled system prompt and
// cache breakpoint offsets using the PromptBuilder. Cacheable (static)
// sections are placed first for optimal provider caching.
func (a *Agent) buildSystemPromptWithFragments() (string, []int) {
	pb := NewPromptBuilder()

	// Base system prompt — static across turns.
	pb.AddSection(PromptSection{
		Name:      "System",
		Content:   a.conversation.SystemPrompt(),
		Cacheable: true,
	})

	// Scratchpad — dynamic, changes as user adds notes.
	if a.scratchpad != nil {
		rendered := a.scratchpad.Render()
		if rendered != "" {
			pb.AddSection(PromptSection{
				Name:      "Scratchpad",
				Content:   rendered,
				Cacheable: false,
			})
		}
	}

	// Skill prompt fragments — dynamic, can change between turns.
	if a.skillRuntime != nil {
		for _, f := range a.skillRuntime.GetPromptFragments() {
			if f.ResolvedPrompt != "" {
				pb.AddSection(PromptSection{
					Name:      f.SkillName,
					Content:   f.ResolvedPrompt,
					Cacheable: false,
				})
			}
		}
	}

	return pb.Build()
}

// getSkillPromptText returns the concatenated skill prompt fragments for
// token tracking. The text is already included in the system prompt via
// PromptBuilder; this method extracts it separately for MeasureUsage.
func (a *Agent) getSkillPromptText() string {
	if a.skillRuntime == nil {
		return ""
	}
	var sb strings.Builder
	for _, f := range a.skillRuntime.GetPromptFragments() {
		if f.ResolvedPrompt != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(f.ResolvedPrompt)
		}
	}
	return sb.String()
}

// makeDoneEvent constructs a "done" TurnEvent, attaching the cumulative diff
// summary from the DiffTracker if one is attached.
func (a *Agent) makeDoneEvent(inputTokens, outputTokens int) TurnEvent {
	event := TurnEvent{
		Type:         "done",
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
	if a.diffTracker != nil {
		event.DiffSummary = a.diffTracker.Summarize()
	}
	return event
}

// runLoop iteratively processes LLM responses and tool calls.
func (a *Agent) runLoop(ctx context.Context, ch chan<- TurnEvent, turnCount int) {
	var totalInputTokens, totalOutputTokens int
	for ; turnCount < a.maxTurns; turnCount++ {
		if ctx.Err() != nil {
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		// Build the system prompt with cache breakpoints.
		systemPrompt, cacheBreakpoints := a.buildSystemPromptWithFragments()

		// Select tools via DeferralManager to stay within budget.
		allToolDefs := a.tools.SelectForContext(a.conversation.Messages())
		budget := a.context.Budget()
		activeTools, _ := a.deferral.SelectForContext(allToolDefs, budget.EffectiveWindow())

		// Measure component-level token usage before the LLM call.
		// Skill prompt fragments are included in systemPrompt via PromptBuilder
		// but tracked separately for budget visibility.
		skillPromptText := a.getSkillPromptText()
		a.context.MeasureUsage(a.conversation, systemPrompt, skillPromptText, activeTools)

		// If at hard block threshold, force compaction before proceeding.
		if a.context.IsBlocked(a.conversation) {
			a.context.ForceCompact(ctx, a.conversation)
			a.saveSnapshotIfNeeded()
		}

		req := provider.CompletionRequest{
			Model:            a.model,
			System:           systemPrompt,
			Messages:         a.conversation.Messages(),
			Tools:            activeTools,
			MaxTokens:        4096,
			CacheBreakpoints: cacheBreakpoints,
		}

		stream, err := a.provider.Stream(ctx, req)
		if err != nil {
			ch <- TurnEvent{Type: "error", Error: fmt.Errorf("provider stream: %w", err)}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
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
			// Accumulate token usage from every stream event.
			totalInputTokens += event.InputTokens
			totalOutputTokens += event.OutputTokens

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
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		// Execute tool calls — parallelize auto-approved tools when possible.
		if cancelled := a.executeTools(ctx, ch, pendingTools); cancelled {
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		// Drain any pending wake events from background subagents.
		a.drainWakeEvents(ch)

		// Continue to the next turn after tool results.
	}

	// Reached max turns.
	ch <- TurnEvent{Type: "error", Error: fmt.Errorf("max turns (%d) exceeded", a.maxTurns)}
	ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
}

// maxParallelTools is the upper bound on concurrent tool goroutines.
const maxParallelTools = 8

// toolExecResult holds the result of a single tool execution for batching.
type toolExecResult struct {
	event TurnEvent
	// Fields for conversation/persistence, applied after all tools finish.
	toolUseID string
	content   string
	isError   bool
}

// executeTools runs the pending tool calls, parallelizing auto-approved ones.
// Returns true if the context was cancelled during execution.
func (a *Agent) executeTools(ctx context.Context, ch chan<- TurnEvent, pendingTools []provider.ToolUseBlock) bool {
	if a.approvalChecker == nil {
		// No checker — fall back to sequential execution.
		return a.executeToolsSequential(ctx, ch, pendingTools)
	}

	// Partition into auto-approved and needs-approval using input-sensitive check.
	type indexedTool struct {
		index int
		tc    provider.ToolUseBlock
	}
	var autoApproved, autoDenied, needsApproval []indexedTool
	for i, tc := range pendingTools {
		result := a.approvalChecker.CheckApproval(tc.Name, tc.Input)
		switch result {
		case AutoApproved, TrustRuleApproved:
			autoApproved = append(autoApproved, indexedTool{index: i, tc: tc})
		case AutoDenied:
			autoDenied = append(autoDenied, indexedTool{index: i, tc: tc})
		default:
			needsApproval = append(needsApproval, indexedTool{index: i, tc: tc})
		}
	}

	// Emit tool_call events for auto-approved tools upfront (in order).
	// Needs-approval tool_call events are emitted just before approval,
	// matching the sequential path behavior.
	for _, it := range autoApproved {
		ch <- TurnEvent{
			Type: "tool_call",
			ToolCall: &ToolCallEvent{
				ID:    it.tc.ID,
				Name:  it.tc.Name,
				Input: it.tc.Input,
			},
		}
	}

	// Results array indexed by original position.
	results := make([]toolExecResult, len(pendingTools))

	// Emit tool_call events for auto-denied tools so headless runners can
	// match results by call ID, then fill in denied results immediately.
	for _, it := range autoDenied {
		ch <- TurnEvent{
			Type: "tool_call",
			ToolCall: &ToolCallEvent{
				ID:    it.tc.ID,
				Name:  it.tc.Name,
				Input: it.tc.Input,
			},
		}
		results[it.index] = toolExecResult{
			toolUseID: it.tc.ID,
			content:   "Tool call denied by user (deny-always).",
			isError:   true,
			event: TurnEvent{
				Type: "tool_result",
				ToolResult: &ToolResultEvent{
					ID:      it.tc.ID,
					Name:    it.tc.Name,
					Content: "Tool call denied by user (deny-always).",
					IsError: true,
				},
			},
		}
	}

	// Execute auto-approved tools in parallel (hooks + execution, no approval).
	if len(autoApproved) > 0 {
		if ctx.Err() != nil {
			return true
		}

		maxG := len(autoApproved)
		if maxG > maxParallelTools {
			maxG = maxParallelTools
		}
		p := pool.New().WithMaxGoroutines(maxG)
		var mu sync.Mutex

		for _, it := range autoApproved {
			p.Go(func() {
				res := a.executeSingleTool(ctx, ch, it.tc)
				mu.Lock()
				results[it.index] = res
				mu.Unlock()
			})
		}
		p.Wait()
		if ctx.Err() != nil {
			return true
		}
	}

	// Execute needs-approval tools sequentially.
	for _, it := range needsApproval {
		if ctx.Err() != nil {
			return true
		}

		ch <- TurnEvent{
			Type: "tool_call",
			ToolCall: &ToolCallEvent{
				ID:    it.tc.ID,
				Name:  it.tc.Name,
				Input: it.tc.Input,
			},
		}

		results[it.index] = a.executeSingleToolWithApproval(ctx, ch, it.tc)
	}

	// Emit all results and update conversation in original tool call order.
	for i := range pendingTools {
		r := results[i]
		a.conversation.AddToolResult(r.toolUseID, r.content, r.isError)
		a.persistToolResult(r.toolUseID, r.content, r.isError)
		ch <- r.event
	}

	return false
}

// executeToolsSequential is the original sequential tool execution path.
func (a *Agent) executeToolsSequential(ctx context.Context, ch chan<- TurnEvent, pendingTools []provider.ToolUseBlock) bool {
	for _, tc := range pendingTools {
		if ctx.Err() != nil {
			return true
		}

		ch <- TurnEvent{
			Type: "tool_call",
			ToolCall: &ToolCallEvent{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			},
		}

		r := a.executeSingleToolWithApproval(ctx, ch, tc)
		a.conversation.AddToolResult(r.toolUseID, r.content, r.isError)
		a.persistToolResult(r.toolUseID, r.content, r.isError)
		ch <- r.event
	}
	return false
}

// executeSingleToolWithApproval runs approval check then delegates to executeSingleTool.
func (a *Agent) executeSingleToolWithApproval(ctx context.Context, ch chan<- TurnEvent, tc provider.ToolUseBlock) toolExecResult {
	// Check approval.
	approved, approvalErr := a.approve(ctx, tc.Name, tc.Input)
	if approvalErr != nil {
		result := fmt.Sprintf("approval error: %s", approvalErr)
		return toolExecResult{
			toolUseID: tc.ID,
			content:   result,
			isError:   true,
			event:     makeToolResultEvent(tc.ID, tc.Name, result, "", true),
		}
	}
	if !approved {
		result := "tool call denied by user"
		return toolExecResult{
			toolUseID: tc.ID,
			content:   result,
			isError:   true,
			event:     makeToolResultEvent(tc.ID, tc.Name, result, "", true),
		}
	}

	return a.executeSingleTool(ctx, ch, tc)
}

// executeSingleTool delegates tool execution to the pipeline.
// Used by both the parallel and sequential paths.
func (a *Agent) executeSingleTool(ctx context.Context, ch chan<- TurnEvent, tc provider.ToolUseBlock) toolExecResult {
	emit := func(ev tools.ToolEvent) {
		ch <- TurnEvent{
			Type: "tool_progress",
			ToolProgress: &ToolProgressEvent{
				ID:      tc.ID,
				Name:    tc.Name,
				Stage:   ev.Stage,
				Content: ev.Content,
				IsError: ev.IsError,
			},
		}
	}
	result := a.pipeline.Execute(toolexec.WithToolEventEmitter(ctx, emit), toolexec.ToolCall{
		ID: tc.ID, Name: tc.Name, Input: tc.Input,
	})
	return toolExecResult{
		toolUseID: tc.ID,
		content:   result.Content,
		isError:   result.IsError,
		event:     makeToolResultEvent(tc.ID, tc.Name, result.Content, result.DisplayContent, result.IsError),
	}
}

// drainWakeEvents non-blockingly reads all pending wake events from the
// WakeManager, injects them into the conversation as user messages, and
// emits subagent_done TurnEvents.
func (a *Agent) drainWakeEvents(ch chan<- TurnEvent) {
	if a.wakeManager == nil {
		return
	}
	for {
		select {
		case wake := <-a.wakeManager.Events():
			wakeMsg := fmt.Sprintf("[Background task %q completed (agent: %s)]\n%s",
				wake.TaskID, wake.AgentName, wake.Result.Output)
			a.conversation.AddUser(wakeMsg)
			ch <- TurnEvent{Type: "subagent_done", Text: wakeMsg, SubagentResult: wake.Result}
		default:
			return
		}
	}
}

func makeToolResultEvent(id, name, content, displayContent string, isError bool) TurnEvent {
	return TurnEvent{
		Type: "tool_result",
		ToolResult: &ToolResultEvent{
			ID:             id,
			Name:           name,
			Content:        content,
			DisplayContent: displayContent,
			IsError:        isError,
		},
	}
}
