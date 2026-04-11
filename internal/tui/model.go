package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/knowledgegraph"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/session"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/terminal"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Turn status constants
const (
	TurnStatusDone      = "done"
	TurnStatusStreaming = "streaming"
	TurnStatusError     = "error"

	// Archival configuration constants
	archiveCheckInterval  = 10 // archive every 10 turns
	minTurnsBeforeArchive = 50 // don't archive until we have this many
)

// approvalRequest carries a tool approval request from the agent goroutine
// to the TUI. The agent blocks on the response channel until the user decides.
type approvalRequest struct {
	tool          string
	input         string
	options       []ApprovalResult
	responseValue chan ApprovalResult
}

// approvalRequestMsg is the Bubble Tea message type for approval requests.
type approvalRequestMsg approvalRequest

// UIState represents the current state of the TUI.
type UIState int

const (
	// StateInput indicates the TUI is waiting for user input.
	StateInput UIState = iota
	// StateStreaming indicates the TUI is streaming a response from the agent.
	StateStreaming
	// StateAwaitingApproval indicates the TUI is waiting for user approval of a tool call.
	StateAwaitingApproval
	// StateConfigOverlay indicates the TUI is showing the config overlay.
	StateConfigOverlay
	// StateBootstrap indicates the TUI is running the bootstrap setup wizard.
	StateBootstrap
	// StateWikiOverlay indicates the TUI is showing the wiki form overlay.
	StateWikiOverlay
	// StateAboutOverlay indicates the TUI is showing the about overlay.
	StateAboutOverlay
	// StateUndoOverlay indicates the TUI is showing the undo overlay.
	StateUndoOverlay
	// StateInitKnowledgeGraphOverlay indicates the TUI is showing the knowledge graph bootstrap questionnaire.
	StateInitKnowledgeGraphOverlay
	// StateBootstrapProgressOverlay indicates the TUI is showing the bootstrap progress overlay.
	StateBootstrapProgressOverlay
)

// Model is the Bubble Tea model for the Rubichan TUI.
type Model struct {
	agent             *agent.Agent
	cfg               *config.Config
	configPath        string
	configForm        *ConfigForm
	input             *InputArea
	viewport          viewport.Model
	spinner           spinner.Model
	content           ContentBuffer
	rawAssistant      strings.Builder
	rawThinking       strings.Builder
	thinkingStartIdx  int
	thinkingEndIdx    int
	mdRenderer        *MarkdownRenderer
	toolBox           *ToolBoxRenderer
	turnRenderer      *TurnRenderer
	turnWindow        *TurnWindow // manages virtual scrolling
	turnCache         *TurnCache  // manages archival
	statusBar         *StatusBar
	approvalPrompt    *ApprovalPrompt
	pendingApproval   *approvalRequest
	activeOverlay     Overlay
	selection         MouseSelection // current text selection state
	clickTracker      clickTracker   // click counting for double/triple-click detection
	checkpointMgr     *checkpoint.Manager
	alwaysApproved    sync.Map
	alwaysDenied      sync.Map
	approvalCh        chan approvalRequest
	assistantStartIdx int
	assistantEndIdx   int
	diffSummary       string
	diffExpanded      bool
	state             UIState
	appName           string
	modelName         string
	width             int
	height            int
	turnCount         int
	maxTurns          int
	totalCost         float64
	quitting          bool
	eventCh           <-chan agent.TurnEvent
	cmdRegistry       *commands.Registry
	completion        *CompletionOverlay
	history           *InputHistory
	turnCancel        context.CancelFunc
	turnStartTime     time.Time
	ralph             *ralphLoopState
	wikiForm          *WikiForm
	fileCompletion    *FileCompletionOverlay
	toolCallArgs      map[string]string
	thinkingActive    bool
	thinkingMsg       string
	wikiRunning       bool
	wikiCfg           WikiCommandConfig
	wikiCancel        context.CancelFunc
	skillProvider     skillSummaryProvider
	activeSkills      []string
	agentPanelVisible bool
	planPanelVisible  bool
	plainMode         bool
	debug             bool
	lastPrompt        string
	termCaps          *terminal.Caps
	cmuxClient        cmux.Caller // nil when not running in cmux
	sessionState      *session.State
	eventSink         session.EventSink
	toolApprovalCount map[string]int     // per-turn count of times each tool was approved
	bootstrapCancel   context.CancelFunc // cancels bootstrap process if set
}

type skillSummaryProvider interface {
	GetAllSkillSummaries() []skills.SkillSummary
}

type ralphLoopState struct {
	cfg       commands.RalphLoopConfig
	iteration int
	cancelled bool
}

// Ensure Model satisfies the tea.Model interface at compile time.
var _ tea.Model = (*Model)(nil)

// NewModel creates a new TUI Model with the given agent, application name,
// model name, maximum turns, config path, config, and command registry.
// The agent, config, and registry may be nil for testing purposes.
// A nil registry is replaced with an empty default.
func NewModel(a *agent.Agent, appName, modelName string, maxTurns int, configPath string, cfg *config.Config, cmdRegistry *commands.Registry) *Model {
	defaultReg := cmdRegistry == nil
	if defaultReg {
		cmdRegistry = commands.NewRegistry()
	}
	ia := NewInputArea()
	ia.SetWidth(80 - inputPromptWidth)

	vp := viewport.New(80, 20)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styleSpinner

	sb := NewStatusBar(80)
	sb.SetModel(modelName)
	sb.SetTurn(0, maxTurns)

	// Initial dark style; refreshed by SetTermCaps() once terminal capabilities are detected.
	mdRenderer, _ := NewMarkdownRenderer(80, true)

	// Create archive directory and initialize TurnCache + TurnWindow
	// Use default archive path (~/.rubichan/archive)
	// (Future: make configurable via cfg if needed)
	archiveDir := filepath.Join(os.Getenv("HOME"), ".rubichan", "archive")

	sessionID := fmt.Sprintf("session-%d", time.Now().Unix()) // unique per session
	cache := NewTurnCache(archiveDir, sessionID, minTurnsBeforeArchive)
	turnWindow := NewTurnWindow(cache)

	m := &Model{
		agent:             a,
		cfg:               cfg,
		configPath:        configPath,
		input:             ia,
		viewport:          vp,
		spinner:           sp,
		mdRenderer:        mdRenderer,
		toolBox:           NewToolBoxRenderer(80),
		turnRenderer:      &TurnRenderer{},
		turnWindow:        turnWindow,
		turnCache:         cache,
		statusBar:         sb,
		approvalCh:        make(chan approvalRequest),
		state:             StateInput,
		appName:           appName,
		modelName:         modelName,
		maxTurns:          maxTurns,
		width:             80,
		height:            24,
		cmdRegistry:       cmdRegistry,
		completion:        NewCompletionOverlay(cmdRegistry, 80),
		history:           NewInputHistory(100),
		sessionState:      session.NewState(),
		eventSink:         session.NewLogSink(log.Printf),
		toolApprovalCount: make(map[string]int),
	}

	// When no registry was provided, populate with default built-in commands.
	// Commands that need model callbacks (clear, model) reference the model
	// instance via closures.
	if defaultReg {
		_ = cmdRegistry.Register(commands.NewQuitCommand())
		_ = cmdRegistry.Register(commands.NewExitCommand())
		_ = cmdRegistry.Register(commands.NewConfigCommand())
		_ = cmdRegistry.Register(commands.NewAboutCommand())
		_ = cmdRegistry.Register(commands.NewInitKnowledgeGraphCommand())
		_ = cmdRegistry.Register(commands.NewRalphLoopCommand(m.StartRalphLoop))
		_ = cmdRegistry.Register(commands.NewCancelRalphCommand(m.CancelRalphLoop))
		_ = cmdRegistry.Register(commands.NewClearCommand(func() {
			if m.agent != nil {
				m.agent.ClearConversation()
			}
			m.ClearContent()
		}))
		_ = cmdRegistry.Register(commands.NewModelCommand(func(name string) {
			if m.agent != nil {
				m.agent.SetModel(name)
			}
			m.modelName = name
			m.statusBar.SetModel(name)
		}))
		_ = cmdRegistry.Register(commands.NewDebugVerificationSnapshotCommand(m.DebugVerificationSnapshot))
		_ = cmdRegistry.Register(commands.NewContextCommand(func() agentsdk.ContextBudget {
			if m.agent != nil {
				return m.agent.ContextBudget()
			}
			return agentsdk.ContextBudget{}
		}))
		_ = cmdRegistry.Register(commands.NewCompactCommand(func(ctx context.Context) (agentsdk.CompactResult, error) {
			if m.agent != nil {
				return m.agent.ForceCompact(ctx)
			}
			return agentsdk.CompactResult{}, nil
		}))
		_ = cmdRegistry.Register(commands.NewSessionsCommand(func() ([]store.Session, error) {
			if m.agent != nil {
				return m.agent.ListSessions(20)
			}
			return nil, nil
		}))
		_ = cmdRegistry.Register(commands.NewForkCommand(func(ctx context.Context) (string, error) {
			if m.agent != nil {
				return m.agent.ForkSession(ctx)
			}
			return "", fmt.Errorf("no agent")
		}))
		_ = cmdRegistry.Register(commands.NewUndoOverlayCommand())
		_ = cmdRegistry.Register(commands.NewHelpCommand(cmdRegistry))
	}

	m.content.AppendText(RenderBanner())
	m.content.AppendText("\n")
	m.viewport.SetContent(m.viewportContent())

	return m
}

// SetAgent sets the agent on the model. This is used when the model needs to
// be created before the agent (e.g., to extract the approval function).
func (m *Model) SetAgent(a *agent.Agent) {
	m.agent = a
}

// GetAgent returns the model's agent. This is used by command callbacks that
// need to interact with the agent (e.g., clear conversation, switch model).
func (m *Model) GetAgent() *agent.Agent {
	return m.agent
}

// emptyToolCalls is a reusable empty slice to avoid allocations on every render
var emptyToolCalls = []RenderedToolCall{}

// extractTurnForRendering creates an immutable Turn snapshot from the model's
// current streaming state. This bridges Model's internal state to TurnRenderer
// for rendering. The Turn is a snapshot of state at call time, not a live reference.
func (m *Model) extractTurnForRendering() *Turn {
	turn := &Turn{
		ID:            fmt.Sprintf("turn-%d", m.turnCount),
		AssistantText: m.rawAssistant.String(),
		ThinkingText:  m.rawThinking.String(),
		Status:        TurnStatusDone,
		ErrorMsg:      "",
		StartTime:     m.turnStartTime,
		ToolCalls:     emptyToolCalls, // reuse constant
	}

	// TODO: Extract tool calls from model state (done in later task)

	return turn
}

// renderCurrentTurn uses TurnRenderer to render the current streaming turn.
// This method bridges Model's state management with TurnRenderer's rendering logic.
func (m *Model) renderCurrentTurn(ctx context.Context) (string, error) {
	turn := m.extractTurnForRendering()
	opts := RenderOptions{
		Width:          m.width,
		IsStreaming:    m.state == StateStreaming,
		CollapsedTools: m.diffExpanded,
		HighlightError: m.state == StateStreaming || m.state == StateAwaitingApproval,
		MaxToolLines:   500,
	}
	return m.turnRenderer.Render(ctx, turn, opts)
}

// ClearContent resets the content buffer and viewport. This is used by the
// /clear command callback to wipe the display.
func (m *Model) ClearContent() {
	m.content.Reset()
	m.diffSummary = ""
	m.diffExpanded = false
	m.toolCallArgs = nil
	if m.sessionState != nil {
		m.sessionState.ResetForPrompt("")
	}
	// Show compact banner after clear to reclaim vertical space.
	m.content.AppendText(RenderCompactBanner())
	m.content.AppendText("\n")
	m.viewport.SetContent(m.viewportContent())
}

// SwitchModel updates the model name and status bar. This is used by the
// /model command callback to reflect the switch in the TUI.
func (m *Model) SwitchModel(name string) {
	m.modelName = name
	m.statusBar.SetModel(name)
}

// syncCompletion updates the completion overlays based on the current input.
func (m *Model) syncCompletion() {
	if m.completion != nil {
		m.completion.Update(m.input.Value())
	}
	if m.fileCompletion != nil {
		m.fileCompletion.Update(m.input.Value())
	}
}

// SetFileCompletionSource sets the file completion source for @ mentions.
func (m *Model) SetFileCompletionSource(src *FileCompletionSource) {
	m.fileCompletion = NewFileCompletionOverlay(src, m.width)
}

// SetGitBranch sets the git branch name displayed in the status bar.
func (m *Model) SetGitBranch(branch string) {
	m.statusBar.SetGitBranch(branch)
}

// SetRunningAgents updates the list of running agents shown in the status bar
// and agent detail panel.
func (m *Model) SetRunningAgents(agents []AgentStatus) {
	m.statusBar.SetRunningAgents(agents)
}

// SetDebug enables or disables debug-only UI surfaces.
func (m *Model) SetDebug(enabled bool) {
	m.debug = enabled
}

// SetPlainMode reduces TUI chrome and redraw-heavy regions for terminal
// automation and PTY capture.
func (m *Model) SetPlainMode(enabled bool) {
	m.plainMode = enabled
	if enabled {
		m.content.Reset()
	}
	m.reflowViewport()
	m.viewport.SetContent(m.viewportContent())
}

// SetTermCaps sets the terminal capabilities. When set, it refreshes the
// markdown renderer to match the terminal's background brightness.
func (m *Model) SetTermCaps(caps *terminal.Caps) {
	m.termCaps = caps
	m.refreshRenderers()
}

// TermCaps returns the terminal capabilities, or nil if not detected.
func (m *Model) TermCaps() *terminal.Caps {
	return m.termCaps
}

// SetCheckpointManager sets the checkpoint manager for undo/rewind support.
func (m *Model) SetCheckpointManager(mgr *checkpoint.Manager) {
	m.checkpointMgr = mgr
}

// SetCmuxClient sets the cmux client for rich sidebar/notification dispatch.
// Pass nil when not running inside cmux.
func (m *Model) SetCmuxClient(client cmux.Caller) {
	m.cmuxClient = client
}

// notifyIfSupported sends a desktop notification if the terminal supports it.
// Tries cmux first; falls back to OSC terminal notifications on failure.
func (m *Model) notifyIfSupported(message string) {
	if m.cmuxClient != nil {
		if cmux.CallerNotify(m.cmuxClient, "Rubichan", "", message) {
			return
		}
	}
	if m.termCaps != nil && m.termCaps.Notifications {
		terminal.Notify(os.Stderr, message)
	}
}

func (m *Model) refreshRenderers() {
	darkBg := true
	if m.termCaps != nil {
		darkBg = m.termCaps.DarkBackground
	}
	mdRenderer, err := NewMarkdownRenderer(m.width, darkBg)
	if err == nil {
		m.mdRenderer = mdRenderer
	} else if m.debug {
		log.Printf("refresh markdown renderer: %v", err)
	}
	m.toolBox = NewToolBoxRenderer(m.width)
}

func buildVerificationSnapshot(prompt string, results []CollapsibleToolResult) string {
	state := session.NewState()
	state.ResetForPrompt(prompt)
	for i, tr := range results {
		id := fmt.Sprintf("ui-%d", i)
		state.ApplyEvent(agentsdk.TurnEvent{
			Type: "tool_call",
			ToolCall: &agentsdk.ToolCallEvent{
				ID:    id,
				Name:  tr.Name,
				Input: json.RawMessage(tr.Args),
			},
		})
		state.ApplyEvent(agentsdk.TurnEvent{
			Type: "tool_result",
			ToolResult: &agentsdk.ToolResultEvent{
				ID:      id,
				Name:    tr.Name,
				Content: tr.Content,
				IsError: tr.IsError,
			},
		})
	}
	return state.BuildVerificationSnapshot()
}

// BuildVerificationSnapshot exposes the current backend verification snapshot
// logic to non-TUI hosts that want the same debug surface.
func BuildVerificationSnapshot(prompt string, results []CollapsibleToolResult) string {
	return buildVerificationSnapshot(prompt, results)
}

// DebugVerificationSnapshot returns the current verification snapshot for the
// active turn state, if the latest prompt looks like a backend verification task.
func (m *Model) DebugVerificationSnapshot() string {
	if m.sessionState == nil {
		return ""
	}
	return m.sessionState.BuildVerificationSnapshot()
}

func (m *Model) emitSessionEvent(evt session.Event) {
	if m.eventSink != nil {
		m.eventSink.Emit(evt.WithActor(session.PrimaryActor()))
	}
}

// SetEventSink overrides the session event sink used by the TUI model.
func (m *Model) SetEventSink(sink session.EventSink) {
	m.eventSink = sink
}

// SetSkillSummaryProvider wires a skill runtime-backed provider into the TUI
// so the header can show currently active skills.
func (m *Model) SetSkillSummaryProvider(provider skillSummaryProvider) {
	m.skillProvider = provider
	m.refreshActiveSkills()
	m.reflowViewport()
}

// SetWikiConfig sets the wiki command configuration and registers the /wiki command.
func (m *Model) SetWikiConfig(cfg WikiCommandConfig) {
	m.wikiCfg = cfg
	_ = m.cmdRegistry.Register(NewWikiCommand(cfg))
}

func (m *Model) refreshActiveSkills() {
	if m.skillProvider == nil {
		m.activeSkills = nil
		m.statusBar.SetSkillSummary("")
		return
	}

	summaries := m.skillProvider.GetAllSkillSummaries()
	active := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		if summary.State == skills.SkillStateActive {
			active = append(active, summary.Name)
		}
	}
	m.activeSkills = active
	m.statusBar.SetSkillSummary(summarizeActiveSkills(active))
}

func (m *Model) headerRows() int {
	if m.plainMode {
		return 0
	}
	rows := 2 // title + divider
	if len(m.activeSkills) > 0 {
		rows++
	}
	if m.planPanelVisible && m.sessionState != nil {
		items := m.sessionState.Plan()
		if len(items) > 0 {
			rows += planPanelHeight(items) + 1 // panel + divider
		}
	}
	return rows
}

func (m *Model) footerRows() int {
	rows := 1 + m.input.Height() // status line + input area
	if m.completion != nil && m.completion.View() != "" {
		rows++
	}
	if m.fileCompletion != nil && m.fileCompletion.View() != "" {
		rows++
	}
	return rows
}

func (m *Model) reflowViewport() {
	viewportHeight := m.height - m.headerRows() - m.footerRows()
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	m.viewport.Width = m.width
	m.viewport.Height = viewportHeight
}

// mouseToContent converts terminal coordinates (x, y) to content-space coordinates (line, col).
// Takes into account the header height and viewport scroll offset.
func (m *Model) mouseToContent(x, y int) (line, col int) {
	vpRow := y - m.headerRows()
	return m.viewport.YOffset + vpRow, x
}

// copySelection copies the currently selected text to the system clipboard.
// Does nothing if there is no active selection or selection is empty.
func (m *Model) copySelection() {
	if !m.selection.Active || m.selection.IsEmpty() {
		return
	}
	text := extractSelectedText(m.contentPlainLines(), m.selection)
	_ = clipboard.WriteAll(text) // ignore errors (clipboard may not be available)
}

// contentPlainLines returns the rendered content with all ANSI escape sequences stripped,
// split into individual lines. Used for text selection and extraction.
func (m *Model) contentPlainLines() []string {
	return plainLines(m.content.Render(m.width))
}

// doQuit performs all quit-side-effects: unblocking approval, canceling wiki and bootstrap, clearing overlay.
func (m *Model) doQuit() {
	m.quitting = true
	if m.pendingApproval != nil {
		m.pendingApproval.responseValue <- ApprovalNo
		m.pendingApproval = nil
	}
	if m.wikiCancel != nil {
		m.wikiCancel()
	}
	if m.bootstrapCancel != nil {
		m.bootstrapCancel()
		m.bootstrapCancel = nil
	}
	m.activeOverlay = nil
}

// scrollToLastError positions the viewport at the last error message.
// Since errors are typically recent, we approximate by scrolling to the bottom.
func (m *Model) scrollToLastError() {
	lastErrorIdx := m.content.LastErrorIndex()
	if lastErrorIdx < 0 {
		return
	}

	// For a more precise implementation, we'd need to expose segment-level
	// rendering to compute the byte offset. For now, we approximate by
	// scrolling to the bottom since errors are typically recent.
	m.viewport.GotoBottom()
}

// MakeApprovalFunc returns an agent.ApprovalFunc that bridges the agent's
// synchronous approval requests to the TUI's async keypress handling.
// Tools previously marked "always" are auto-approved without prompting.
func (m *Model) MakeApprovalFunc() agent.ApprovalFunc {
	return func(_ context.Context, tool string, input json.RawMessage) (bool, error) {
		if actionID, handled := m.checkAutoApproval(tool, string(input)); handled {
			switch actionID {
			case "allow_always", "allow":
				return true, nil
			case "deny_always", "deny":
				return false, nil
			default:
				return false, fmt.Errorf("unsupported cached approval action: %s", actionID)
			}
		}

		respCh := make(chan ApprovalResult, 1)
		m.approvalCh <- approvalRequest{
			tool:          tool,
			input:         string(input),
			options:       nil,
			responseValue: respCh,
		}
		result := <-respCh
		return result == ApprovalYes || result == ApprovalAlways, nil
	}
}

// MakeUIRequestHandler returns a generalized UI interaction handler for the
// current model. Approval requests are rendered with the existing inline
// approval prompt and translated into structured action IDs.
func (m *Model) MakeUIRequestHandler() agent.UIRequestHandler {
	return agent.UIRequestFunc(func(ctx context.Context, req agent.UIRequest) (agent.UIResponse, error) {
		if req.Kind != agent.UIKindApproval {
			return agent.UIResponse{}, fmt.Errorf("unsupported UI request kind: %s", req.Kind)
		}

		tool := req.Metadata["tool"]
		input := req.Metadata["input"]
		if tool == "" {
			// Fallback for non-standard adapters that omit tool metadata.
			tool = req.Title
		}
		if actionID, handled := m.checkAutoApproval(tool, input); handled {
			return agent.UIResponse{RequestID: req.ID, ActionID: actionID}, nil
		}

		respCh := make(chan ApprovalResult, 1)
		m.approvalCh <- approvalRequest{
			tool:          tool,
			input:         input,
			options:       optionsFromUIActions(req.Actions),
			responseValue: respCh,
		}

		select {
		case <-ctx.Done():
			return agent.UIResponse{}, ctx.Err()
		case result := <-respCh:
			return agent.UIResponse{
				RequestID: req.ID,
				ActionID:  actionIDFromApprovalResult(result, req.Actions),
			}, nil
		}
	})
}

// IsAutoApproved checks if a tool was previously marked "always approve"
// by the user. This implements agent.AutoApproveChecker to enable parallel
// execution of auto-approved tools.
func (m *Model) IsAutoApproved(tool string) bool {
	_, ok := m.alwaysApproved.Load(tool)
	return ok
}

// CheckApproval implements agent.ApprovalChecker by checking the session
// cache of "always approved" and "deny always" tools. AutoDenied is checked
// first so that explicit user denials cannot be overridden by trust rules.
func (m *Model) CheckApproval(tool string, _ json.RawMessage) agent.ApprovalResult {
	if _, ok := m.alwaysDenied.Load(tool); ok {
		return agent.AutoDenied
	}
	if _, ok := m.alwaysApproved.Load(tool); ok {
		return agent.AutoApproved
	}
	return agent.ApprovalRequired
}

// waitForApproval returns a tea.Cmd that blocks until an approval request
// arrives on the approval channel, then delivers it as an approvalRequestMsg.
func (m *Model) waitForApproval() tea.Cmd {
	ch := m.approvalCh
	return func() tea.Msg {
		req := <-ch
		return approvalRequestMsg(req)
	}
}

func actionIDFromApprovalResult(result ApprovalResult, actions []agent.UIAction) string {
	// Prefer returning an ID that was actually offered in req.Actions.
	// This avoids emitting canonical IDs that the caller did not provide.
	for _, action := range actions {
		if mapped, ok := approvalResultFromActionID(action.ID); ok && mapped == result {
			return action.ID
		}
	}

	// Fallback to canonical IDs when actions are missing or unknown.
	switch result {
	case ApprovalAlways:
		return "allow_always"
	case ApprovalDenyAlways:
		return "deny_always"
	case ApprovalYes:
		return "allow"
	default:
		return "deny"
	}
}

func approvalResultFromActionID(actionID string) (ApprovalResult, bool) {
	switch strings.ToLower(actionID) {
	case "allow", "yes":
		return ApprovalYes, true
	case "deny", "no":
		return ApprovalNo, true
	case "allow_always", "always":
		return ApprovalAlways, true
	case "deny_always":
		return ApprovalDenyAlways, true
	default:
		return ApprovalPending, false
	}
}

func optionsFromUIActions(actions []agent.UIAction) []ApprovalResult {
	if len(actions) == 0 {
		return nil
	}
	seen := map[ApprovalResult]bool{}
	opts := make([]ApprovalResult, 0, len(actions))
	for _, action := range actions {
		mapped, ok := approvalResultFromActionID(action.ID)
		if !ok {
			continue
		}
		if seen[mapped] {
			continue
		}
		seen[mapped] = true
		opts = append(opts, mapped)
	}
	return opts
}

func (m *Model) checkAutoApproval(tool, input string) (actionID string, handled bool) {
	// Auto-deny if user previously chose "deny always" for this tool.
	if _, ok := m.alwaysDenied.Load(tool); ok {
		return "deny_always", true
	}

	// Auto-approve if user previously chose "always" for this tool,
	// but only if the current command is not destructive. Destructive
	// commands must always be re-validated per invocation to prevent
	// a benign "shell ls" approval from auto-approving "shell rm -rf /".
	if _, ok := m.alwaysApproved.Load(tool); ok {
		opts := OptionsForRisk(tool, input)
		for _, o := range opts {
			if o == ApprovalAlways {
				return "allow_always", true
			}
		}
		// Destructive command — fall through to interactive prompt.
	}

	return "", false
}

// handleCommand processes slash commands entered by the user.
// It delegates to the command registry and interprets the result's Action.
// Returns a tea.Cmd if the command produces one (e.g., tea.Quit), or nil.
func (m *Model) handleCommand(line string) tea.Cmd {
	parts, err := commands.ParseLine(line)
	if err != nil {
		m.content.WriteString(persona.ErrorMessage(err.Error()))
		m.setContentAndAutoScroll()
		return nil
	}
	return m.handleCommandParts(line, parts)
}

func (m *Model) handleCommandParts(line string, parts []string) tea.Cmd {
	if len(parts) == 0 {
		return nil
	}

	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	slashOnly := name == ""
	if slashOnly {
		name = "help"
	}

	slashCmd, ok := m.cmdRegistry.Get(name)
	if !ok {
		if slashOnly {
			m.content.WriteString("Type /help to show available commands.\n")
			m.setContentAndAutoScroll()
			return nil
		}
		m.content.WriteString(fmt.Sprintf("Unknown command: %s\n", parts[0]))
		m.setContentAndAutoScroll()
		return nil
	}

	result, err := slashCmd.Execute(context.Background(), parts[1:])
	if err != nil {
		m.content.WriteString(persona.ErrorMessage(err.Error()))
		m.setContentAndAutoScroll()
		return nil
	}
	prevActive := append([]string(nil), m.activeSkills...)
	m.refreshActiveSkills()
	m.reflowViewport()

	if result.Output != "" {
		m.content.WriteString(result.Output + "\n")
		m.setContentAndAutoScroll()
	}
	activated, deactivated := diffActiveSkills(prevActive, m.activeSkills)
	m.appendSkillStateChanges(activated, deactivated)
	m.emitSessionEvent(session.NewCommandResultEvent(line, result.Output, activated, deactivated))
	logSlashCommand(line, result.Output, activated, deactivated)
	if startCmd := m.maybeStartRalphLoop(); startCmd != nil {
		return tea.Batch(startCmd, m.spinner.Tick)
	}

	switch result.Action {
	case commands.ActionQuit:
		m.quitting = true
		return tea.Quit
	case commands.ActionOpenConfig:
		if m.cfg == nil {
			m.content.WriteString("No config available\n")
			m.setContentAndAutoScroll()
			return nil
		}
		overlay, initCmd := NewConfigOverlay(m.cfg, m.configPath)
		m.activeOverlay = overlay
		m.state = StateConfigOverlay
		return initCmd
	case commands.ActionOpenWiki:
		if m.wikiRunning {
			m.content.WriteString("Wiki generation is already running.\n")
			m.setContentAndAutoScroll()
			return nil
		}
		overlay, initCmd := NewWikiOverlay(m.wikiCfg.WorkDir)
		m.activeOverlay = overlay
		m.state = StateWikiOverlay
		return initCmd
	case commands.ActionOpenUndo:
		var cps []checkpoint.Checkpoint
		if m.checkpointMgr != nil {
			cps = m.checkpointMgr.List()
		}
		m.activeOverlay = NewUndoOverlay(cps, m.width)
		m.state = StateUndoOverlay
		return nil
	case commands.ActionOpenAbout:
		m.activeOverlay = NewAboutOverlay(m.width, m.height)
		m.state = StateAboutOverlay
		return nil
	case commands.ActionInitKnowledgeGraph:
		m.activeOverlay = NewInitKnowledgeGraphOverlay(m.width, m.height)
		m.state = StateInitKnowledgeGraphOverlay
		return nil
	}

	return nil
}

func summarizeActiveSkills(active []string) string {
	if len(active) == 0 {
		return ""
	}
	if len(active) <= 2 {
		return strings.Join(active, ", ")
	}
	return fmt.Sprintf("%d active (%s, %s, +%d)", len(active), active[0], active[1], len(active)-2)
}

func (m *Model) appendSkillStateChanges(activated, deactivated []string) {
	if len(activated) == 0 && len(deactivated) == 0 {
		return
	}

	for _, name := range activated {
		m.content.WriteString(fmt.Sprintf("Skill %q activated.\n", name))
	}
	for _, name := range deactivated {
		m.content.WriteString(fmt.Sprintf("Skill %q deactivated.\n", name))
	}
	m.setContentAndAutoScroll()
}

func logSlashCommand(line, output string, activated, deactivated []string) {
	parts := []string{fmt.Sprintf("slash command: %s", strings.TrimSpace(line))}
	if trimmed := strings.TrimSpace(output); trimmed != "" {
		parts = append(parts, fmt.Sprintf("output=%q", singleLinePreview(trimmed, 160)))
	}
	if len(activated) > 0 {
		parts = append(parts, fmt.Sprintf("activated=%s", strings.Join(activated, ",")))
	}
	if len(deactivated) > 0 {
		parts = append(parts, fmt.Sprintf("deactivated=%s", strings.Join(deactivated, ",")))
	}
	log.Print(strings.Join(parts, " "))
}

func singleLinePreview(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func diffActiveSkills(before, after []string) (activated []string, deactivated []string) {
	beforeSet := make(map[string]struct{}, len(before))
	afterSet := make(map[string]struct{}, len(after))

	for _, name := range before {
		beforeSet[name] = struct{}{}
	}
	for _, name := range after {
		afterSet[name] = struct{}{}
		if _, ok := beforeSet[name]; !ok {
			activated = append(activated, name)
		}
	}
	for _, name := range before {
		if _, ok := afterSet[name]; !ok {
			deactivated = append(deactivated, name)
		}
	}
	return activated, deactivated
}

func (m *Model) StartRalphLoop(cfg commands.RalphLoopConfig) error {
	if m.state != StateInput {
		return fmt.Errorf("Ralph loop can only start while idle")
	}
	if m.agent == nil {
		return fmt.Errorf("no agent configured")
	}
	m.ralph = &ralphLoopState{cfg: cfg}
	return nil
}

func (m *Model) CancelRalphLoop() bool {
	if m.ralph == nil {
		return false
	}
	m.ralph.cancelled = true
	if m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
	}
	return true
}

func (m *Model) maybeStartRalphLoop() tea.Cmd {
	if m.ralph == nil || m.state != StateInput || m.agent == nil || m.ralph.iteration != 0 {
		return nil
	}
	prompt := m.ralph.cfg.Prompt
	m.diffSummary = ""
	m.diffExpanded = false
	m.content.WriteString(styleUserPrompt.Render("❯ ") + prompt + "\n")
	m.setContentAndAutoScroll()
	m.assistantStartIdx = m.content.LenWithWidth(m.width)
	m.state = StateStreaming
	m.statusBar.ClearElapsed()
	m.turnStartTime = time.Now()
	return m.startTurn(m.agent, prompt)
}

// runBootstrap executes the bootstrap and sends progress updates.
// The context can be cancelled via the bootstrapCancel field to interrupt the process.
func (m *Model) runBootstrap(ctx context.Context, profile *knowledgegraph.BootstrapProfile) tea.Msg {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	// Phase 2: Analysis
	var allEntities []*knowledgegraph.ProposedEntity

	// Modules
	select {
	case <-ctx.Done():
		return BootstrapProgressMsg{
			Phase: "error",
			Error: "bootstrap cancelled",
		}
	default:
	}
	modules, err := knowledgegraph.DiscoverModules(cwd)
	if err == nil {
		allEntities = append(allEntities, modules...)
	}

	// Decisions from git
	select {
	case <-ctx.Done():
		return BootstrapProgressMsg{
			Phase: "error",
			Error: "bootstrap cancelled",
		}
	default:
	}
	decisions, err := knowledgegraph.DiscoverDecisionsFromGit(cwd, profile)
	if err == nil {
		allEntities = append(allEntities, decisions...)
	}

	// Integrations
	select {
	case <-ctx.Done():
		return BootstrapProgressMsg{
			Phase: "error",
			Error: "bootstrap cancelled",
		}
	default:
	}
	integrations, err := knowledgegraph.DiscoverIntegrations(cwd)
	if err == nil {
		allEntities = append(allEntities, integrations...)
	}

	// Phase 3: Entity Creation
	select {
	case <-ctx.Done():
		return BootstrapProgressMsg{
			Phase: "error",
			Error: "bootstrap cancelled",
		}
	default:
	}
	knowledgeDir := filepath.Join(cwd, ".knowledge")

	metadata, err := knowledgegraph.WriteBootstrapEntities(knowledgeDir, allEntities, profile)
	if err != nil {
		return BootstrapProgressMsg{
			Phase: "error",
			Error: err.Error(),
		}
	}

	// Complete
	m.content.WriteString(fmt.Sprintf("✅ Knowledge graph bootstrap complete! Created %d entities in .knowledge/\n", len(metadata.CreatedEntities)))
	m.setContentAndAutoScroll()

	return BootstrapProgressMsg{
		Phase: "complete",
	}
}
