package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/sourcegraph/conc/pool"

	"github.com/julianshen/rubichan/pkg/agentsdk"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
)

// Event types (TurnEvent, ToolCallEvent, etc.), approval types (ApprovalFunc,
// ApprovalChecker, etc.), and other shared types are defined in
// pkg/agentsdk/ and re-exported via sdk_aliases.go.

// AgentOption is a functional option for configuring an Agent.
type AgentOption func(*Agent)

// WithSkillRuntime attaches a skill runtime to the agent, enabling hook
// dispatch and prompt fragment injection.
func WithSkillRuntime(rt *skills.Runtime) AgentOption {
	return func(a *Agent) {
		a.skillRuntime = rt
	}
}

// WithCheckpointManager attaches a checkpoint manager to the agent, enabling
// undo/rewind support and automatic file-state capture via the pipeline.
func WithCheckpointManager(mgr *checkpoint.Manager) AgentOption {
	return func(a *Agent) {
		a.checkpointMgr = mgr
	}
}

// WithMode sets the agent's execution mode for turn-level skill trigger
// evaluation (e.g. "interactive", "headless", "code-review").
func WithMode(mode string) AgentOption {
	return func(a *Agent) {
		a.mode = mode
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

// WithUIRequestHandler attaches a generalized UI interaction handler.
// When set, approval prompts can be rendered via structured UI requests
// instead of only via the legacy boolean ApprovalFunc callback.
func WithUIRequestHandler(handler UIRequestHandler) AgentOption {
	return func(a *Agent) {
		a.uiRequestHandler = handler
	}
}

// WithParallelPolicy attaches a policy that determines which auto-approved
// tools may execute in parallel. When set, only tools passing both
// auto-approval AND CanParallelize run concurrently; others run sequentially.
func WithParallelPolicy(policy ToolParallelPolicy) AgentOption {
	return func(a *Agent) {
		a.parallelPolicy = policy
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

// WithCapabilities sets the model capability flags on the agent. These flags
// are threaded into CompletionRequests to tune tool dispatch and prompt
// construction for different model families.
func WithCapabilities(caps provider.ModelCapabilities) AgentOption {
	return func(a *Agent) { a.capabilities = caps }
}

// WorkingDir returns the agent's effective working directory.
// The value is frozen at construction time and never changes.
func (a *Agent) WorkingDir() string {
	return a.workingDir
}

// Undo reverts the most recent file modification captured by the checkpoint
// manager. Returns the restored file path or an error.
func (a *Agent) Undo(ctx context.Context) (string, error) {
	if a.checkpointMgr == nil {
		return "", fmt.Errorf("checkpoint manager not configured")
	}
	return a.checkpointMgr.Undo(ctx)
}

// RewindToTurn restores all files modified since the given turn number.
// Returns the list of restored file paths or an error.
func (a *Agent) RewindToTurn(ctx context.Context, turn int) ([]string, error) {
	if a.checkpointMgr == nil {
		return nil, fmt.Errorf("checkpoint manager not configured")
	}
	return a.checkpointMgr.RewindToTurn(ctx, turn)
}

// Checkpoints returns the current checkpoint stack. Returns nil if no
// checkpoint manager is configured.
func (a *Agent) Checkpoints() []checkpoint.Checkpoint {
	if a.checkpointMgr == nil {
		return nil
	}
	return a.checkpointMgr.List()
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

// WithUserHooks attaches a user hook runner to the agent. On construction
// the runner's hooks are registered into the skill runtime so they fire
// alongside skill-contributed hooks.
func WithUserHooks(runner *hooks.UserHookRunner) AgentOption {
	return func(a *Agent) {
		a.userHookRunner = runner
	}
}

// WithIdentityMD injects project-level IDENTITY.md content into the system prompt.
func WithIdentityMD(content string) AgentOption {
	return func(a *Agent) {
		a.identityMD = content
	}
}

// WithSoulMD injects project-level SOUL.md content into the system prompt.
func WithSoulMD(content string) AgentOption {
	return func(a *Agent) {
		a.soulMD = content
	}
}

// namedPrompt is a named system prompt section appended after the base prompt.
type namedPrompt struct {
	Name    string
	Content string
}

// WithLogger attaches a structured logger to the agent. If not set,
// agentsdk.DefaultLogger() is used.
func WithLogger(l Logger) AgentOption {
	return func(a *Agent) { a.logger = l }
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

// WithRateLimiter sets a shared rate limiter that throttles LLM API requests.
// A nil limiter disables rate limiting.
func WithRateLimiter(rl *SharedRateLimiter) AgentOption {
	return func(a *Agent) {
		a.rateLimiter = rl
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
	uiRequestHandler UIRequestHandler
	model            string
	maxTurns         int
	basePrompt       string
	staticPrompts    []PromptSection
	skillRuntime     *skills.Runtime
	store            *store.Store
	sessionID        string
	resumeSessionID  string
	agentMD          string
	identityMD       string
	soulMD           string
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
	parallelPolicy   ToolParallelPolicy
	logger           Logger
	mode             string
	checkpointMgr    *checkpoint.Manager
	userHookRunner   *hooks.UserHookRunner
	turnNumber       atomic.Int32
	rateLimiter      *SharedRateLimiter
	capabilities     provider.ModelCapabilities
}

const maxUIRequestInputBytes = 2048

// New creates a new Agent with the given provider, tool registry, approval
// function, and configuration. Optional AgentOption values can be provided
// to attach a skill runtime.
func New(p provider.LLMProvider, t *tools.Registry, approve ApprovalFunc, cfg *config.Config, opts ...AgentOption) *Agent {
	systemPrompt := buildSystemPrompt(cfg)
	a := &Agent{
		provider:     p,
		tools:        t,
		basePrompt:   systemPrompt,
		conversation: NewConversation(systemPrompt),
		context:      newContextManagerFromConfig(cfg),
		approve:      approve,
		model:        cfg.Provider.Model,
		maxTurns:     cfg.Agent.MaxTurns,
		scratchpad:   NewScratchpad(),
		capabilities: agentsdk.DefaultCapabilities(),
	}
	for _, opt := range opts {
		opt(a)
	}
	// Ensure logger is always available.
	if a.logger == nil {
		a.logger = agentsdk.DefaultLogger()
	}
	// Freeze working directory at construction time.
	if a.workingDir == "" {
		a.workingDir, _ = os.Getwd()
	}
	// Load cross-session memories into system prompt.
	var memories []MemoryEntry
	if a.memoryStore != nil {
		wd := a.WorkingDir()
		loaded, err := a.memoryStore.LoadMemories(wd)
		if err != nil {
			a.logger.Warn("failed to load memories: %v", err)
		} else {
			memories = loaded
		}
	}
	a.staticPrompts = a.assembleSystemPromptSections(memories)
	a.conversation = NewConversation(renderPromptSections(a.staticPrompts))
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
				a.logger.Warn("failed to resume session %s: %v", a.resumeSessionID, err)
			} else {
				a.sessionID = sess.ID
				a.conversation = NewConversation(sess.SystemPrompt)
				a.staticPrompts = []PromptSection{{
					Name:      "",
					Content:   sess.SystemPrompt,
					Cacheable: true,
				}}
				// Prefer compacted snapshot for resume (avoids re-exceeding
				// context limits). Fall back to full message history for
				// sessions that were never compacted.
				snapMsgs, snapErr := a.store.GetSnapshot(sess.ID)
				if snapErr == nil && snapMsgs != nil {
					a.conversation.LoadFromMessages(snapMsgs)
				} else {
					msgs, err := a.store.GetMessages(sess.ID)
					if err != nil {
						a.logger.Warn("failed to load messages: %v", err)
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
				a.logger.Warn("failed to create session: %v", err)
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
			a.logger.Warn("failed to register read_result tool: %v", err)
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

		// Checkpoint middleware captures file state before write/patch operations.
		// TODO: Reposition after ShellSafety when Classifier/RuleEngine/ShellSafety
		// middlewares are wired in (spec: Hook→...→ShellSafety→Checkpoint→PostHook).
		if a.checkpointMgr != nil {
			middlewares = append(middlewares, toolexec.CheckpointMiddleware(a.checkpointMgr, func() int {
				return int(a.turnNumber.Load())
			}))
		}

		// Post-hook middleware for after-tool-result dispatch.
		middlewares = append(middlewares, toolexec.PostHookMiddleware(hookAdapter))

		// Output offloader middleware when persistence is available.
		if a.resultStore != nil {
			offloader := &toolexec.ResultStoreAdapter{Offloader: a.resultStore}
			middlewares = append(middlewares, toolexec.OutputManagerMiddleware(offloader))
		}

		a.pipeline = toolexec.NewPipeline(toolexec.RegistryExecutor(t), middlewares...)
	}

	// Register user-defined hooks (from config and AGENT.md) into the skill runtime.
	if a.userHookRunner != nil && a.skillRuntime != nil {
		a.userHookRunner.RegisterInto(a.skillRuntime)
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
			a.logger.Warn("failed to register tool_search tool: %v", err)
		}
	}

	// Register compact_context tool for agent-initiated compaction.
	// Skip if already present (e.g. subagent inheriting parent's filtered registry).
	compactTool := tools.NewCompactContextTool(&agentCompactor{agent: a})
	if _, exists := a.tools.Get(compactTool.Name()); !exists {
		if err := a.tools.Register(compactTool); err != nil {
			a.logger.Warn("failed to register compact_context tool: %v", err)
		}
	}

	// Register skill_manager tool when store is available.
	if a.store != nil {
		skillsDir := cfg.Skills.UserDir
		if skillsDir == "" {
			if home, err := os.UserHomeDir(); err == nil {
				skillsDir = filepath.Join(home, ".config", "rubichan", "skills")
			}
		}
		if skillsDir != "" {
			registryURL := defaultRegistryURL
			adapter := &skillManagerAdapter{
				registry:  skills.NewRegistryClient(registryURL, a.store, 5*time.Minute),
				store:     a.store,
				skillsDir: skillsDir,
				activator: a.skillRuntime,
			}
			skillMgrTool := tools.NewSkillManagerTool(adapter)
			if _, exists := a.tools.Get(skillMgrTool.Name()); !exists {
				if err := a.tools.Register(skillMgrTool); err != nil {
					a.logger.Warn("failed to register skill_manager tool: %v", err)
				}
			}
		}
	}

	return a
}

// buildSystemPrompt constructs the system prompt from configuration.
func buildSystemPrompt(_ *config.Config) string {
	return persona.BaseSystemPrompt()
}

func (a *Agent) assembleSystemPromptSections(memories []MemoryEntry) []PromptSection {
	sections := []PromptSection{{
		Name:      "System",
		Content:   a.basePrompt,
		Cacheable: true,
	}}

	identity := persona.IdentityPrompt()
	if a.identityMD != "" {
		identity += "\n\n### Workspace Identity (from IDENTITY.md)\n\n" + a.identityMD
	}
	sections = append(sections, PromptSection{
		Name:      "Identity",
		Content:   identity,
		Cacheable: true,
	})

	soul := persona.SoulPrompt()
	if a.soulMD != "" {
		soul += "\n\n### Workspace Soul (from SOUL.md)\n\n" + a.soulMD
	}
	sections = append(sections, PromptSection{
		Name:      "Soul",
		Content:   soul,
		Cacheable: true,
	})

	if a.agentMD != "" {
		sections = append(sections, PromptSection{
			Name:      "Project Guidelines (from AGENT.md)",
			Content:   a.agentMD,
			Cacheable: true,
		})
	}

	for _, ep := range a.extraPrompts {
		sections = append(sections, PromptSection{
			Name:      ep.Name,
			Content:   ep.Content,
			Cacheable: true,
		})
	}

	if len(memories) > 0 {
		var sb strings.Builder
		for _, m := range memories {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", m.Tag, m.Content))
		}
		sections = append(sections, PromptSection{
			Name:      "Prior Session Insights",
			Content:   sb.String(),
			Cacheable: true,
		})
	}

	return sections
}

func renderPromptSections(sections []PromptSection) string {
	pb := NewPromptBuilder()
	for _, section := range sections {
		pb.AddSection(section)
	}
	prompt, _ := pb.Build()
	return prompt
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
// preserving the system prompt. It acquires turnMu to prevent races
// with a concurrent Turn goroutine.
func (a *Agent) ClearConversation() {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	a.conversation.Clear()
}

// InjectUserContext adds a user message to the conversation without starting
// a turn. Used for shell escape output so the LLM sees it on subsequent turns.
func (a *Agent) InjectUserContext(text string) {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	a.conversation.AddUser(text)
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

	// Snapshot messages under the lock to prevent races with Turn.
	a.turnMu.Lock()
	msgs := a.conversation.Messages()
	a.turnMu.Unlock()

	extractor := NewMemoryExtractor(a.summarizer)
	memories, err := extractor.Extract(ctx, msgs)
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
// It acquires turnMu to prevent races with the Turn goroutine reading a.model.
func (a *Agent) SetModel(model string) {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	a.model = model
}

// SessionID returns the current session ID.
func (a *Agent) SessionID() string {
	return a.sessionID
}

// ForkSession creates a fork of the current session and switches to it.
// Serialized with Turn() via turnMu to prevent session ID mutation during
// an active turn's message persistence.
// Returns the new session ID.
func (a *Agent) ForkSession(ctx context.Context) (string, error) {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()

	if a.store == nil {
		return "", fmt.Errorf("fork session: store not configured")
	}
	newID := uuid.New().String()
	if err := a.store.ForkSession(a.sessionID, newID); err != nil {
		return "", fmt.Errorf("fork session: %w", err)
	}
	a.sessionID = newID
	return newID, nil
}

// ListSessions returns recent sessions from the store filtered by working directory.
func (a *Agent) ListSessions(limit int) ([]store.Session, error) {
	if a.store == nil {
		return nil, fmt.Errorf("list sessions: store not configured")
	}
	sessions, err := a.store.ListSessions(limit)
	if err != nil {
		return nil, err
	}
	// Filter by working directory
	var filtered []store.Session
	for _, s := range sessions {
		if s.WorkingDir == a.workingDir {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
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
		a.logger.Warn("failed to persist message: %v", err)
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
		a.logger.Warn("failed to save snapshot: %v", err)
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
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				a.logger.Error("agent panic recovered: %v\n%s", r, stack)
				// Non-blocking send to avoid deadlocking turnMu if channel
				// buffer is full (e.g., reader stopped consuming events).
				select {
				case ch <- TurnEvent{
					Type:  "error",
					Error: fmt.Errorf("agent panic: %v\n%s", r, stack),
				}:
				default:
				}
				// Maintain contract: every turn ends with a "done" event.
				select {
				case ch <- TurnEvent{Type: "done"}:
				default:
				}
			}
		}()
		a.runLoop(ctx, ch, 0, userMessage)
	}()
	return ch, nil
}

// DiffTracker returns the agent's diff tracker, or nil if none is attached.
func (a *Agent) DiffTracker() *tools.DiffTracker {
	return a.diffTracker
}

// ContextBudget returns the current context usage breakdown.
func (a *Agent) ContextBudget() agentsdk.ContextBudget {
	return a.context.Budget()
}

// ForceCompact triggers manual compaction and returns before/after metrics.
// The error return is always nil currently — reserved for future strategy errors.
func (a *Agent) ForceCompact(ctx context.Context) (agentsdk.CompactResult, error) {
	result := a.context.ForceCompact(ctx, a.conversation)
	return result, nil
}

// buildSystemPromptWithFragments returns the assembled system prompt,
// cache breakpoint offsets, and the final skill prompt fragment text used
// for telemetry. Cacheable (static) sections are placed first for optimal
// provider caching.
func (a *Agent) buildSystemPromptWithFragments(ctx context.Context) (string, []int, string) {
	type skillPromptFragmentData struct {
		SkillName string `json:"skill_name"`
		Prompt    string `json:"prompt"`
	}

	type promptBuildContext struct {
		BaseSystemPrompt      string                    `json:"base_system_prompt"`
		SkillPromptFragments  []skillPromptFragmentData `json:"skill_prompt_fragments"`
		ContextBudgetTotal    int                       `json:"context_budget_total"`
		ContextBudgetMaxOut   int                       `json:"context_budget_max_output_tokens"`
		ContextBudgetWindow   int                       `json:"context_budget_effective_window"`
		ContextBudgetSystem   int                       `json:"context_budget_system_prompt_tokens"`
		ContextBudgetSkills   int                       `json:"context_budget_skill_prompt_tokens"`
		ContextBudgetTools    int                       `json:"context_budget_tool_description_tokens"`
		ContextBudgetMessages int                       `json:"context_budget_conversation_tokens"`
	}

	type promptBuildMutation struct {
		ReplaceBaseSystemPrompt        string
		ReplaceBaseSystemPromptPresent bool
		AppendSystemPrompt             string
		ReplaceSkillFragments          []skillPromptFragmentData
		ReplaceSkillFragmentsPresent   bool
		AppendSkillFragments           []skillPromptFragmentData
	}

	normalizeFragments := func(raw any) ([]skillPromptFragmentData, bool) {
		switch v := raw.(type) {
		case nil:
			return []skillPromptFragmentData{}, true
		case []skillPromptFragmentData:
			return append([]skillPromptFragmentData(nil), v...), true
		case []any:
			out := make([]skillPromptFragmentData, 0, len(v))
			for _, item := range v {
				fragMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				name, _ := fragMap["skill_name"].(string)
				prompt, _ := fragMap["prompt"].(string)
				if name != "" && prompt != "" {
					out = append(out, skillPromptFragmentData{SkillName: name, Prompt: prompt})
				}
			}
			return out, true
		default:
			return nil, false
		}
	}

	mergePromptBuildMutation := func(dst *promptBuildMutation, data map[string]any) {
		if data == nil {
			return
		}
		if v, ok := data["replace_base_system_prompt"].(string); ok {
			dst.ReplaceBaseSystemPrompt = v
			dst.ReplaceBaseSystemPromptPresent = true
		}
		if v, ok := data["append_system_prompt"].(string); ok {
			dst.AppendSystemPrompt = v
		}
		if raw, ok := data["replace_skill_fragments"]; ok {
			if fragments, parsed := normalizeFragments(raw); parsed {
				dst.ReplaceSkillFragments = fragments
				dst.ReplaceSkillFragmentsPresent = true
			}
		}
		if raw, ok := data["append_skill_fragments"]; ok {
			if fragments, parsed := normalizeFragments(raw); parsed {
				dst.AppendSkillFragments = fragments
			}
		}
	}

	buildSkillPromptText := func(fragments []skillPromptFragmentData) string {
		var sb strings.Builder
		for _, f := range fragments {
			if f.Prompt == "" {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(f.Prompt)
		}
		return sb.String()
	}

	baseSystemPrompt := a.conversation.SystemPrompt()
	builtFragments := make([]skillPromptFragmentData, 0)
	if a.skillRuntime != nil {
		fragments := a.skillRuntime.GetBudgetedPromptFragments()
		builtFragments = make([]skillPromptFragmentData, 0, len(fragments))
		for _, f := range fragments {
			if f.ResolvedPrompt == "" {
				continue
			}
			builtFragments = append(builtFragments, skillPromptFragmentData{
				SkillName: f.SkillName,
				Prompt:    f.ResolvedPrompt,
			})
		}
	}

	if a.skillRuntime != nil {
		budget := a.context.Budget()
		hookEvent := skills.HookEvent{
			Phase: skills.HookOnBeforePromptBuild,
			Ctx:   ctx,
			Data: map[string]any{
				"prompt_build": promptBuildContext{
					BaseSystemPrompt:      baseSystemPrompt,
					SkillPromptFragments:  builtFragments,
					ContextBudgetTotal:    budget.Total,
					ContextBudgetMaxOut:   budget.MaxOutputTokens,
					ContextBudgetWindow:   budget.EffectiveWindow(),
					ContextBudgetSystem:   budget.SystemPrompt,
					ContextBudgetSkills:   budget.SkillPrompts,
					ContextBudgetTools:    budget.ToolDescriptions,
					ContextBudgetMessages: budget.Conversation,
				},
			},
		}
		if result, err := a.skillRuntime.DispatchHook(hookEvent); err != nil {
			a.logger.Warn("before-prompt-build hook failed: %v", err)
		} else if result != nil {
			mutation := promptBuildMutation{}
			mergePromptBuildMutation(&mutation, hookEvent.Data)
			mergePromptBuildMutation(&mutation, result.Modified)

			if mutation.ReplaceBaseSystemPromptPresent {
				baseSystemPrompt = mutation.ReplaceBaseSystemPrompt
			}
			if mutation.AppendSystemPrompt != "" {
				baseSystemPrompt = strings.TrimSpace(baseSystemPrompt + "\n\n" + mutation.AppendSystemPrompt)
			}
			if mutation.ReplaceSkillFragmentsPresent {
				builtFragments = mutation.ReplaceSkillFragments
			}
			if len(mutation.AppendSkillFragments) > 0 {
				builtFragments = append(builtFragments, mutation.AppendSkillFragments...)
			}
		}
	}

	pb := NewPromptBuilder()

	// When hooks mutate the base system prompt, intentionally bypass the static
	// prompt pipeline and treat the result as a single cacheable section. That
	// preserves hook control over structure at the cost of static section
	// reordering and cache-hint optimizations.
	if len(a.staticPrompts) > 0 && baseSystemPrompt == a.conversation.SystemPrompt() {
		for _, section := range a.staticPrompts {
			pb.AddSection(section)
		}
	} else {
		pb.AddSection(PromptSection{
			Name:      "",
			Content:   baseSystemPrompt,
			Cacheable: true,
		})
	}

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

	// Skill prompt fragments — use budgeted selection to respect context budget.
	if a.skillRuntime != nil {
		for _, f := range builtFragments {
			pb.AddSection(PromptSection{
				Name:      f.SkillName,
				Content:   f.Prompt,
				Cacheable: false,
			})
		}
	}

	systemPrompt, cacheBreakpoints := pb.Build()
	return systemPrompt, cacheBreakpoints, buildSkillPromptText(builtFragments)
}

// makeDoneEvent constructs a "done" TurnEvent, attaching the cumulative diff
// summary from the DiffTracker if one is attached, and the context budget.
func (a *Agent) makeDoneEvent(inputTokens, outputTokens int) TurnEvent {
	budget := a.context.Budget()
	event := TurnEvent{
		Type:          "done",
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		ContextBudget: &budget,
	}
	if a.diffTracker != nil {
		event.DiffSummary = a.diffTracker.Summarize()
	}
	return event
}

// runLoop iteratively processes LLM responses and tool calls.
func (a *Agent) runLoop(ctx context.Context, ch chan<- TurnEvent, turnCount int, lastUserMessage string) {
	var totalInputTokens, totalOutputTokens int
	var lastPendingToolSignature string
	repeatedPendingToolRounds := 0
	if a.skillRuntime != nil {
		triggerCtx := a.buildSkillTriggerContext(lastUserMessage)
		if err := a.skillRuntime.EvaluateAndActivate(triggerCtx); err != nil {
			ch <- TurnEvent{Type: "error", Error: fmt.Errorf("activate skills for turn: %w", err)}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}
	}
	for ; turnCount < a.maxTurns; turnCount++ {
		// Track turn number for checkpoint middleware.
		a.turnNumber.Store(int32(turnCount))

		if ctx.Err() != nil {
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		// Build the system prompt with cache breakpoints.
		systemPrompt, cacheBreakpoints, skillPromptText := a.buildSystemPromptWithFragments(ctx)

		// Select tools via DeferralManager to stay within budget.
		allToolDefs := a.tools.SelectForContext(a.conversation.Messages())
		budget := a.context.Budget()
		activeTools, _ := a.deferral.SelectForContext(allToolDefs, budget.EffectiveWindow())

		if a.capabilities.MaxToolCount > 0 {
			activeTools = tools.ApplyMaxToolCount(activeTools, a.capabilities.MaxToolCount)
		}

		// Append tool discovery hint for models that benefit from explicit guidance.
		if a.capabilities.NeedsToolDiscoveryHint {
			toolHint := a.deferral.ToolSummary(activeTools)
			systemPrompt = systemPrompt + "\n\n" + toolHint
		}

		// Branch on native tool use capability: models without native support
		// receive tool definitions rendered as text in the system prompt instead.
		useNativeTools := a.capabilities.SupportsNativeToolUse
		var reqTools []provider.ToolDef
		if useNativeTools {
			reqTools = activeTools
		} else {
			// Render tools into system prompt as text for non-native models.
			toolPrompt := tools.RenderToolsAsText(activeTools)
			systemPrompt = systemPrompt + "\n\n" + toolPrompt
		}

		// Measure component-level token usage before the LLM call.
		// Skill prompt fragments are included in systemPrompt via PromptBuilder
		// but tracked separately for budget visibility.
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
			Tools:            reqTools,
			MaxTokens:        4096,
			CacheBreakpoints: cacheBreakpoints,
		}

		if a.rateLimiter != nil {
			if !a.rateLimiter.AllowNow() {
				ch <- TurnEvent{Type: "rate_limited"}
			}
			if err := a.rateLimiter.Wait(ctx); err != nil {
				ch <- TurnEvent{Type: "error", Error: fmt.Errorf("rate limiter: %w", err)}
				ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
				return
			}
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
		var streamErr bool
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
				if event.ToolUse == nil {
					// Finalize any in-progress tool to prevent input corruption.
					finalizeText()
					finalizeTool()
					ch <- TurnEvent{Type: "error", Error: fmt.Errorf("provider sent tool_use event with nil ToolUse")}
					continue
				}
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
				streamErr = true
				ch <- TurnEvent{Type: "error", Error: event.Error}

			case "stop":
				// Will be handled after the loop
			}
		}

		// Capture accumulated text before finalizing, for text-based tool extraction.
		accumulatedText := currentTextBuf

		// Finalize any remaining text or tool
		finalizeText()
		finalizeTool()

		// On stream error, discard partial blocks to prevent conversation corruption
		if streamErr {
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		// For non-native models, parse <tool_use> XML blocks from the text response
		// and inject them into pendingTools so the normal execution path handles them.
		if !useNativeTools && len(pendingTools) == 0 && accumulatedText != "" {
			textCalls := tools.ParseTextToolCalls(accumulatedText)
			if len(textCalls) == 0 && strings.Contains(accumulatedText, "<tool_use>") {
				a.logger.Warn("model attempted tool call in text but XML parsing found no valid blocks")
			}
			if len(textCalls) > 0 {
				// Strip <tool_use> XML from the text block so the model
				// doesn't see its own XML format echoed back on the next turn.
				for i := range blocks {
					if blocks[i].Type == "text" {
						blocks[i].Text = strings.TrimSpace(tools.StripToolUseXML(blocks[i].Text))
					}
				}
			}
			for _, tc := range textCalls {
				pendingTools = append(pendingTools, tc)
				blocks = append(blocks, provider.ContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				})
			}
		}

		// If the LLM returned no content at all (no text, no tool calls),
		// emit an error and add a placeholder assistant message to keep the
		// conversation valid (every user message must be followed by an
		// assistant message).
		if len(blocks) == 0 && len(pendingTools) == 0 {
			placeholder := "[empty response from model]"
			blocks = append(blocks, provider.ContentBlock{Type: "text", Text: placeholder})
			ch <- TurnEvent{Type: "error", Error: fmt.Errorf("empty response from model")}
		}

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

		signature := pendingToolSignature(pendingTools)
		if !hasTextContent(blocks) && signature == lastPendingToolSignature {
			repeatedPendingToolRounds++
		} else {
			lastPendingToolSignature = signature
			repeatedPendingToolRounds = 1
		}
		if repeatedPendingToolRounds >= maxRepeatedPendingToolRounds {
			ch <- TurnEvent{Type: "error", Error: fmt.Errorf("detected no progress after %d repeated tool-only rounds", repeatedPendingToolRounds)}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		// Check if the model signaled task completion via task_complete tool.
		// All sibling tools in the same batch are executed before exiting —
		// the model often pairs task_complete with a final write or commit.
		for _, tc := range pendingTools {
			if tc.Name == tools.TaskCompleteName {
				a.executeTools(ctx, ch, pendingTools)
				ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
				return
			}
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

const maxRepeatedPendingToolRounds = 3

func hasTextContent(blocks []provider.ContentBlock) bool {
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			return true
		}
	}
	return false
}

func pendingToolSignature(pendingTools []provider.ToolUseBlock) string {
	var b strings.Builder
	for _, tc := range pendingTools {
		b.WriteString(tc.Name)
		b.WriteByte(':')
		b.Write(tc.Input)
		b.WriteByte('\n')
	}
	return b.String()
}

func (a *Agent) buildSkillTriggerContext(lastUserMessage string) skills.TriggerContext {
	entries, err := os.ReadDir(a.WorkingDir())
	projectFiles := make([]string, 0, len(entries))
	if err != nil {
		a.logger.Warn("failed to read working directory for skill triggers: %v", err)
	} else {
		for _, entry := range entries {
			projectFiles = append(projectFiles, entry.Name())
		}
	}

	return skills.TriggerContext{
		LastUserMessage: lastUserMessage,
		Mode:            a.mode,
		ProjectFiles:    projectFiles,
	}
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

type plannedToolCall struct {
	index          int
	tc             provider.ToolUseBlock
	approvalResult ApprovalResult
}

func (a *Agent) approvalResultForTool(tc provider.ToolUseBlock) ApprovalResult {
	if a.approvalChecker == nil {
		return ApprovalRequired
	}
	return a.approvalChecker.CheckApproval(tc.Name, tc.Input)
}

func (a *Agent) planToolCalls(pendingTools []provider.ToolUseBlock) []plannedToolCall {
	planned := make([]plannedToolCall, 0, len(pendingTools))
	for i, tc := range pendingTools {
		planned = append(planned, plannedToolCall{
			index:          i,
			tc:             tc,
			approvalResult: a.approvalResultForTool(tc),
		})
	}
	return planned
}

func toolErrorResult(tc provider.ToolUseBlock, msg string) toolExecResult {
	return toolExecResult{
		toolUseID: tc.ID,
		content:   msg,
		isError:   true,
		event:     makeToolResultEvent(tc.ID, tc.Name, msg, "", true),
	}
}

func (a *Agent) approvalToolErrorResult(tc provider.ToolUseBlock, msg string, err error) toolExecResult {
	if err != nil {
		a.logger.Error("approval failure for tool %s (%s): %v", tc.Name, tc.ID, err)
	}
	return toolErrorResult(tc, msg)
}

// executeTools runs the pending tool calls, parallelizing auto-approved ones.
// Returns true if the context was cancelled during execution.
func (a *Agent) executeTools(ctx context.Context, ch chan<- TurnEvent, pendingTools []provider.ToolUseBlock) bool {
	if a.approvalChecker == nil {
		// No checker — fall back to sequential execution.
		return a.executePlannedToolsSequential(ctx, ch, a.planToolCalls(pendingTools))
	}

	plannedTools := a.planToolCalls(pendingTools)

	// Partition into auto-approved and needs-approval using input-sensitive check.
	var autoApproved, autoDenied, needsApproval []plannedToolCall
	for _, planned := range plannedTools {
		switch planned.approvalResult {
		case AutoApproved, TrustRuleApproved:
			autoApproved = append(autoApproved, planned)
		case AutoDenied:
			autoDenied = append(autoDenied, planned)
		default:
			needsApproval = append(needsApproval, planned)
		}
	}

	// When a parallel policy is set, auto-approved tools that fail
	// CanParallelize are moved to sequential execution (after parallel batch).
	var sequentialApproved []plannedToolCall
	if a.parallelPolicy != nil {
		var parallelizable []plannedToolCall
		for _, it := range autoApproved {
			if a.parallelPolicy.CanParallelize(it.tc.Name) {
				parallelizable = append(parallelizable, it)
			} else {
				sequentialApproved = append(sequentialApproved, it)
			}
		}
		autoApproved = parallelizable
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
		results[it.index] = a.approvalToolErrorResult(it.tc, "Tool call denied by user (deny-always).", nil)
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

	// Execute auto-approved but non-parallelizable tools sequentially.
	for _, it := range sequentialApproved {
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
		results[it.index] = a.executeSingleTool(ctx, ch, it.tc)
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

		results[it.index] = a.executeSingleToolWithApproval(ctx, ch, it.tc, it.approvalResult)
	}

	// Emit all results and update conversation in original tool call order.
	for i := range pendingTools {
		r := results[i]
		a.conversation.AddToolResult(r.toolUseID, r.content, r.isError)
		a.persistToolResult(r.toolUseID, r.content, r.isError)
		ch <- r.event
	}

	// Snapshot after all tool results so a resume picks up from here.
	a.saveSnapshotIfNeeded()

	return false
}

func (a *Agent) executePlannedToolsSequential(ctx context.Context, ch chan<- TurnEvent, plannedTools []plannedToolCall) bool {
	for _, planned := range plannedTools {
		if ctx.Err() != nil {
			return true
		}

		ch <- TurnEvent{
			Type: "tool_call",
			ToolCall: &ToolCallEvent{
				ID:    planned.tc.ID,
				Name:  planned.tc.Name,
				Input: planned.tc.Input,
			},
		}

		r := a.executeSingleToolWithApproval(ctx, ch, planned.tc, planned.approvalResult)
		a.conversation.AddToolResult(r.toolUseID, r.content, r.isError)
		a.persistToolResult(r.toolUseID, r.content, r.isError)
		ch <- r.event
	}

	// Snapshot after all tool results so a resume picks up from here.
	a.saveSnapshotIfNeeded()

	return false
}

// executeSingleToolWithApproval runs approval check then delegates to executeSingleTool.
func (a *Agent) executeSingleToolWithApproval(ctx context.Context, ch chan<- TurnEvent, tc provider.ToolUseBlock, approvalResult ApprovalResult) toolExecResult {
	if approvalResult == AutoApproved || approvalResult == TrustRuleApproved {
		return a.executeSingleTool(ctx, ch, tc)
	}
	if approvalResult == AutoDenied {
		return a.approvalToolErrorResult(tc, "Tool call denied by user (deny-always).", nil)
	}
	if a.uiRequestHandler == nil && a.approve == nil {
		return a.approvalToolErrorResult(tc, "approval function not configured", nil)
	}

	approved, denyAlways, approvalErr := a.requestToolApproval(ctx, ch, tc)
	if approvalErr != nil {
		return a.approvalToolErrorResult(tc, "approval error", approvalErr)
	}
	if !approved {
		if denyAlways {
			return a.approvalToolErrorResult(tc, "Tool call denied by user (deny-always).", nil)
		}
		return a.approvalToolErrorResult(tc, "tool call denied by user", nil)
	}

	return a.executeSingleTool(ctx, ch, tc)
}

func (a *Agent) requestToolApproval(ctx context.Context, ch chan<- TurnEvent, tc provider.ToolUseBlock) (approved bool, denyAlways bool, err error) {
	if a.uiRequestHandler != nil {
		req := UIRequest{
			ID:      tc.ID,
			Kind:    UIKindApproval,
			Title:   fmt.Sprintf("Approve %s tool call", tc.Name),
			Message: "Review and choose how to proceed.",
			Actions: []UIAction{
				{ID: "allow", Label: "Allow", Default: true},
				{ID: "deny", Label: "Deny", Style: "danger"},
				{ID: "allow_always", Label: "Always Allow"},
				{ID: "deny_always", Label: "Always Deny", Style: "danger"},
			},
			Metadata: map[string]string{
				"tool":  tc.Name,
				"input": truncateUIInput(tc.Input),
			},
		}
		ch <- TurnEvent{Type: "ui_request", UIRequest: &req}
		resp, reqErr := a.uiRequestHandler.Request(ctx, req)
		if reqErr != nil {
			return false, false, reqErr
		}
		if resp.RequestID != req.ID {
			return false, false, fmt.Errorf("unexpected UI response id %q for request %q", resp.RequestID, req.ID)
		}
		ch <- TurnEvent{Type: "ui_response", UIResponse: &resp}
		switch strings.ToLower(resp.ActionID) {
		case "allow", "allow_always", "yes":
			// "allow_always" cache persistence is handled by the UI adapter.
			return true, false, nil
		case "deny_always":
			return false, true, nil
		case "deny", "no":
			return false, false, nil
		default:
			return false, false, fmt.Errorf("unsupported UI approval action %q", resp.ActionID)
		}
	}

	if a.approve == nil {
		return false, false, fmt.Errorf("approval function not configured")
	}

	approved, approvalErr := a.approve(ctx, tc.Name, tc.Input)
	if approvalErr != nil {
		return false, false, approvalErr
	}
	return approved, false, nil
}

func truncateUIInput(input json.RawMessage) string {
	s := string(input)
	if len(s) <= maxUIRequestInputBytes {
		return s
	}
	return s[:maxUIRequestInputBytes] + "...(truncated)"
}

// executeSingleTool delegates tool execution to the pipeline.
// Used by both the parallel and sequential paths. Recovers from panics
// so that a single tool crash produces an error tool_result instead of
// unwinding the entire turn and leaving dangling tool_use blocks.
func (a *Agent) executeSingleTool(ctx context.Context, ch chan<- TurnEvent, tc provider.ToolUseBlock) (res toolExecResult) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			a.logger.Error("tool %s panicked: %v\n%s", tc.Name, r, stack)
			msg := fmt.Sprintf("tool panicked: %v", r)
			res = toolExecResult{
				toolUseID: tc.ID,
				content:   msg,
				isError:   true,
				event:     makeToolResultEvent(tc.ID, tc.Name, msg, "", true),
			}
		}
	}()
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
			a.persistMessage("user", []provider.ContentBlock{{Type: "text", Text: wakeMsg}})
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
