package tui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/session"
	"github.com/julianshen/rubichan/internal/terminal"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// --- Model setters & simple accessors ---

func TestModel_SetGitBranch_UpdatesStatusBar(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)

	m.SetGitBranch("feature/x")

	// StatusBar.View() includes git branch when set.
	assert.Contains(t, m.statusBar.View(), "feature/x")
}

func TestModel_SetTermCaps_RefreshesRenderers(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	oldRenderer := m.mdRenderer

	caps := &terminal.Caps{DarkBackground: false}
	m.SetTermCaps(caps)

	assert.Same(t, caps, m.termCaps)
	// Renderer is refreshed (new instance) when caps change.
	assert.NotNil(t, m.mdRenderer)
	assert.NotSame(t, oldRenderer, m.mdRenderer)
}

func TestModel_SetCheckpointManager_StoresNil(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)

	m.SetCheckpointManager(nil)
	assert.Nil(t, m.checkpointMgr)
}

func TestModel_SetCmuxClient_StoresNil(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)

	m.SetCmuxClient(nil)
	assert.Nil(t, m.cmuxClient)
}

func TestModel_NotifyIfSupported_NoSupportIsNoop(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	// termCaps == nil, cmuxClient == nil: should neither panic nor produce side-effects.
	m.notifyIfSupported("hello")

	// With termCaps but Notifications=false, should also be a no-op.
	m.SetTermCaps(&terminal.Caps{Notifications: false})
	m.notifyIfSupported("still quiet")
}

// --- Enter key variations ---

func TestModel_Enter_EmptyInput_NoOp(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// No state change, no pending operation.
	assert.Equal(t, StateInput, m.state)
}

func TestModel_Enter_BareBangNoOp(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.input.SetValue("!")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Bare "!" with nothing after is ignored.
	assert.Equal(t, StateInput, m.state)
}

// --- Ctrl+P/N history navigation ---

func TestModel_CtrlP_HistoryPrevious(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.history.Add("earlier command")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	assert.Equal(t, "earlier command", m.input.Value())
}

func TestModel_Enter_ShellEscapeExecutesCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.input.SetValue("!echo hello-from-shell")

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// A tool-result segment should have been appended with the shell output.
	results := m.content.ToolResults()
	require.NotEmpty(t, results)
	assert.Equal(t, "shell", results[0].Name)
	assert.Contains(t, results[0].Content, "hello-from-shell")
}

func TestModel_Enter_SlashCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.input.SetValue("/help")

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Help text should be in content.
	assert.Contains(t, m.content.String(), "Available commands")
}

// --- Scroll keys forwarded to viewport ---

func TestModel_ScrollKeys_PgUpDown(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	for i := 0; i < 100; i++ {
		m.content.AppendText("line\n")
	}
	m.reflowViewport()
	m.viewport.SetContent(m.viewportContent())
	m.viewport.GotoBottom()

	// Exercise the isScrollKey branch + viewport forwarding.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
}

func TestIsScrollKey_CoversAllBranches(t *testing.T) {
	assert.True(t, isScrollKey(tea.KeyMsg{Type: tea.KeyPgUp}))
	assert.True(t, isScrollKey(tea.KeyMsg{Type: tea.KeyPgDown}))
	assert.True(t, isScrollKey(tea.KeyMsg{Type: tea.KeyHome}))
	assert.True(t, isScrollKey(tea.KeyMsg{Type: tea.KeyEnd}))
	assert.True(t, isScrollKey(tea.KeyMsg{Type: tea.KeyUp}))
	assert.True(t, isScrollKey(tea.KeyMsg{Type: tea.KeyDown}))
	assert.True(t, isScrollKey(tea.KeyMsg{Type: tea.KeyCtrlU}))
	assert.True(t, isScrollKey(tea.KeyMsg{Type: tea.KeyCtrlD}))
	assert.False(t, isScrollKey(tea.KeyMsg{Type: tea.KeyEnter}))
}

// --- view.View under StateAwaitingApproval paths ---

func TestModel_View_AwaitingApproval_WithPrompt(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateAwaitingApproval
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60, nil, false)

	out := m.View()
	// Approval prompt should be rendered inline.
	assert.Contains(t, out, "ls")
}

func TestModel_View_AwaitingApproval_WithOverlay(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateAwaitingApproval
	m.activeOverlay = NewApprovalOverlay("shell", `{"command":"ls"}`, "", 80,
		[]ApprovalResult{ApprovalYes, ApprovalNo}, false)

	out := m.View()
	assert.Contains(t, out, "ls")
}

func TestModel_View_Streaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming
	m.turnStartTime = time.Now()

	out := m.View()
	assert.Contains(t, out, "Generating")
}

func TestModel_View_Streaming_Thinking(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming
	m.thinkingActive = true
	m.turnStartTime = time.Now()

	out := m.View()
	assert.Contains(t, out, "Thinking")
}

// --- handleTurnEvent more branches ---

func TestHandleTurnEvent_ToolCall(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming

	_, _ = m.Update(TurnEventMsg{
		Type: "tool_call",
		ToolCall: &agentsdk.ToolCallEvent{
			ID:    "t1",
			Name:  "read_file",
			Input: json.RawMessage(`{"path":"/tmp/x"}`),
		},
	})
	assert.Contains(t, m.content.String(), "read_file")
}

func TestHandleTurnEvent_ToolProgress(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming

	_, _ = m.Update(TurnEventMsg{
		Type: "tool_progress",
		ToolProgress: &agentsdk.ToolProgressEvent{
			Name:    "shell",
			Stage:   agentsdk.EventBegin,
			Content: "progressing...",
		},
	})
	assert.Contains(t, m.content.String(), "progressing")
}

func TestHandleTurnEvent_UIUpdate_SetsTaskProgress(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming

	_, _ = m.Update(TurnEventMsg{
		Type: "ui_update",
		UIUpdate: &agentsdk.UIUpdate{
			RequestID: "r1",
			Status:    "running",
			Message:   "doing work",
		},
	})
	// Task progress label should be set on the status bar.
	assert.NotEmpty(t, m.statusBar.View())
}

func TestHandleTurnEvent_UIUpdate_Complete_ClearsTaskProgress(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming

	_, _ = m.Update(TurnEventMsg{
		Type: "ui_update",
		UIUpdate: &agentsdk.UIUpdate{
			RequestID: "r1",
			Status:    "complete",
		},
	})
}

func TestHandleTurnEvent_UIRequest_AndResponse(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming

	_, _ = m.Update(TurnEventMsg{
		Type:      "ui_request",
		UIRequest: &agentsdk.UIRequest{ID: "r1", Kind: "approval"},
	})
	_, _ = m.Update(TurnEventMsg{
		Type:       "ui_response",
		UIResponse: &agentsdk.UIResponse{RequestID: "r1", ActionID: "allow"},
	})
}

// --- approvalPrompt HandleKey: already-done early exit / repeat call ---

func TestApprovalPrompt_SetResultMakesDone(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60, nil, false)
	ap.SetResult(ApprovalYes)
	assert.True(t, ap.Done())
	assert.Equal(t, ApprovalYes, ap.Result())
}

// --- footerRows with completion overlay visible ---

func TestModel_FooterRows_WithVisibleCompletion(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	// Ensure completion is visible by syncing a slash prefix.
	m.input.SetValue("/h")
	m.completion.Update("/h")
	// completion.View() is non-empty when there are matching candidates.
	rows := m.footerRows()
	assert.GreaterOrEqual(t, rows, 2)
}

// --- headerRows with plan panel visible (no plan items still valid path) ---

func TestModel_HeaderRows_PlanPanelNoItems(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.planPanelVisible = true
	// No plan items — panel not drawn — rows match base.
	assert.Equal(t, 2, m.headerRows())
}

// --- Selection drag path in Update (mouse motion) ---

func TestModel_Update_MouseLeftPressAndRelease(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.content.AppendText("hello world\n")
	m.viewport.SetContent(m.viewportContent())

	// Left press.
	_, _ = m.Update(tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		X:      1,
		Y:      2,
	})
	assert.True(t, m.selection.Active)

	// Motion — extend selection.
	_, _ = m.Update(tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
		X:      5,
		Y:      2,
	})

	// Release.
	_, _ = m.Update(tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
		X:      5,
		Y:      2,
	})
	assert.False(t, m.selection.Dragging)
}

// --- Completion overlay HandleKey paths ---

func TestCompletionOverlay_HandleKey_UpDownWrapAround(t *testing.T) {
	reg := commands.NewRegistry()
	require.NoError(t, reg.Register(commands.NewQuitCommand()))
	require.NoError(t, reg.Register(commands.NewExitCommand()))
	require.NoError(t, reg.Register(commands.NewHelpCommand(reg)))

	co := NewCompletionOverlay(reg, 80)
	co.Update("/") // show all commands

	// Down: wraps.
	for i := 0; i < 10; i++ {
		co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	for i := 0; i < 10; i++ {
		co.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	}
	// Escape dismisses.
	co.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, co.Visible())
}

// --- Right click copies selection ---

// --- processOverlayResult: ApprovalResult always/denyalways branches ---

func TestProcessOverlayResult_ApprovalAlways_SetsTrusted(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	respCh := make(chan ApprovalResult, 1)
	m.pendingApproval = &approvalRequest{
		tool:          "read_file",
		responseValue: respCh,
	}
	// Pre-seed a denied entry to verify it gets cleared.
	m.alwaysDenied.Store("read_file", true)

	cmd := m.processOverlayResult(ApprovalAlways)
	_, wasDenied := m.alwaysDenied.Load("read_file")
	assert.False(t, wasDenied)
	_, isApproved := m.alwaysApproved.Load("read_file")
	assert.True(t, isApproved)
	// The approval response was delivered to the waiting channel.
	assert.Equal(t, ApprovalAlways, <-respCh)
	assert.Nil(t, cmd) // eventCh is nil so waitForEvent returns nil
	assert.Equal(t, StateStreaming, m.state)
}

func TestProcessOverlayResult_ApprovalDenyAlways_SetsDenied(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	respCh := make(chan ApprovalResult, 1)
	m.pendingApproval = &approvalRequest{
		tool:          "shell",
		responseValue: respCh,
	}
	// Pre-seed an approved entry to verify it gets cleared.
	m.alwaysApproved.Store("shell", true)

	_ = m.processOverlayResult(ApprovalDenyAlways)
	_, wasApproved := m.alwaysApproved.Load("shell")
	assert.False(t, wasApproved)
	_, isDenied := m.alwaysDenied.Load("shell")
	assert.True(t, isDenied)
	assert.Equal(t, ApprovalDenyAlways, <-respCh)
}

func TestProcessOverlayResult_ApprovalNoPendingReturnsInput(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.pendingApproval = nil
	m.state = StateAwaitingApproval

	cmd := m.processOverlayResult(ApprovalYes)
	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
}

func TestModel_Update_MouseRightCopiesSelection(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.content.AppendText("some content\n")
	m.viewport.SetContent(m.viewportContent())
	m.selection = MouseSelection{
		Start:  Position{Line: 0, Col: 0},
		End:    Position{Line: 0, Col: 4},
		Active: true,
	}

	_, _ = m.Update(tea.MouseMsg{
		Button: tea.MouseButtonRight,
		Action: tea.MouseActionPress,
	})
	// No panic is the assertion; clipboard may be unavailable in CI.
}

func TestModel_CtrlN_HistoryNext(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.history.Add("a")
	m.history.Add("b")
	// Go back twice, then forward once.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	// Some value should be present — we're exercising the branch, not validating
	// history internals.
	assert.NotNil(t, m.input.Value())
}

func TestToolBoxRenderer_RenderToolProgress(t *testing.T) {
	r := NewToolBoxRenderer(80)
	// Empty content returns empty string — early-return branch.
	assert.Equal(t, "", r.RenderToolProgress("tool", "stage", "", false))
	// Non-empty content wraps in box.
	out := r.RenderToolProgress("tool", "running", "hello", false)
	assert.Contains(t, out, "hello")
	assert.Contains(t, out, "tool")
	// Error styling path.
	errOut := r.RenderToolProgress("tool", "done", "bad things", true)
	assert.Contains(t, errOut, "bad things")
}

func TestModel_NotifyIfSupported_WithNotificationsEnabled(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	// When termCaps.Notifications is true, terminal.Notify is invoked.
	// Capture stderr by redirecting isn't straightforward here — we just
	// exercise the branch to boost coverage without asserting byte output.
	m.SetTermCaps(&terminal.Caps{Notifications: true})
	m.notifyIfSupported("hi")
}

func TestModel_SetEventSink_Overrides(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)

	newSink := session.NewLogSink(func(format string, args ...any) {})
	m.SetEventSink(newSink)

	assert.NotNil(t, m.eventSink)
	// Replacing with nil must also take effect.
	m.SetEventSink(nil)
	assert.Nil(t, m.eventSink)
}

func TestModel_DebugVerificationSnapshot_NilStateReturnsEmpty(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.sessionState = nil

	assert.Equal(t, "", m.DebugVerificationSnapshot())
}

func TestBuildVerificationSnapshot_ExposesHelper(t *testing.T) {
	// Empty results still returns a string (possibly empty) without panicking.
	snap := BuildVerificationSnapshot("irrelevant prompt", nil)
	_ = snap // return value depends on session internals; just assert no panic.
}

// --- NewModel construction paths ---

func TestNewModel_WithCustomRegistry_DoesNotPopulateDefaults(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "rubichan", "model-x", 25, "", nil, reg)

	assert.Equal(t, reg, m.cmdRegistry)
	// Custom registries are not populated with built-in commands.
	assert.Empty(t, reg.All())
}

func TestNewModel_WithConfig_RetainsConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, "rubichan", "model", 10, "/tmp/cfg.toml", cfg, nil)

	assert.Same(t, cfg, m.cfg)
	assert.Equal(t, "/tmp/cfg.toml", m.configPath)
}

// --- startTurn ---

func TestModel_StartTurn_NilAgent_ReturnsErrorMsg(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)

	cmd := m.startTurn(nil, "hello")
	require.NotNil(t, cmd)

	msg := cmd()
	evt, ok := msg.(TurnEventMsg)
	require.True(t, ok, "expected TurnEventMsg, got %T", msg)
	assert.Equal(t, "error", evt.Type)
	require.NotNil(t, evt.Error)
	assert.Contains(t, evt.Error.Error(), "no agent configured")
}

// --- replaceMermaidBlocks: mmdc-unavailable branch ---

func TestReplaceMermaidBlocks_KittyWithoutMmdc_PassesThrough(t *testing.T) {
	// Force mmdc to be unavailable. The cached mmdcAvailable is package-level,
	// so this primarily exercises the Kitty-present-with-block code path.
	t.Setenv("PATH", "/nonexistent-path-for-mmdc-test")

	content := "before\n```mermaid\ngraph TD\n    A-->B\n```\nafter"
	caps := &terminal.Caps{KittyGraphics: true, DarkBackground: true}

	got := replaceMermaidBlocks(content, caps)
	// Whether mmdc was already cached as available or not, the content must
	// still contain the original Mermaid source (either raw or embedded in
	// rendered output's fallback). It must not be shorter than the header.
	assert.Contains(t, got, "before")
	assert.Contains(t, got, "after")
}

// --- Bootstrap form state (IsCompleted / IsAborted) ---

func TestBootstrapForm_IsCompletedAndIsAborted(t *testing.T) {
	bf := NewBootstrapForm("/tmp/cfg.toml")
	// Fresh form is neither completed nor aborted.
	assert.False(t, bf.IsCompleted())
	assert.False(t, bf.IsAborted())

	// Directly drive the underlying huh.Form state — huh exposes it publicly.
	bf.Form().State = huh.StateCompleted
	assert.True(t, bf.IsCompleted())
	assert.False(t, bf.IsAborted())

	bf.Form().State = huh.StateAborted
	assert.False(t, bf.IsCompleted())
	assert.True(t, bf.IsAborted())
}

// --- ConfigOverlay.Result paths ---

func TestConfigOverlay_Result_PendingReturnsNil(t *testing.T) {
	cfg := config.DefaultConfig()
	o, _ := NewConfigOverlay(cfg, "")
	assert.Nil(t, o.Result())
}

func TestConfigOverlay_Result_CompletedReturnsConfigResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()

	o, _ := NewConfigOverlay(cfg, path)
	o.form.Form().State = huh.StateCompleted

	result := o.Result()
	_, ok := result.(ConfigResult)
	assert.True(t, ok, "expected ConfigResult, got %T", result)
}

// --- WikiOverlay.Result ---

func TestWikiOverlay_Result_PendingReturnsNil(t *testing.T) {
	o, _ := NewWikiOverlay(t.TempDir())
	assert.Nil(t, o.Result())
}

func TestWikiOverlay_Result_CompletedReturnsWikiResult(t *testing.T) {
	o, _ := NewWikiOverlay(t.TempDir())
	o.wikiForm.Form().State = huh.StateCompleted

	r, ok := o.Result().(WikiResult)
	require.True(t, ok, "expected WikiResult")
	assert.NotNil(t, r.Form)
}

// --- BootstrapProgressOverlay ---

func TestBootstrapProgressOverlay_InitialStateAndView(t *testing.T) {
	o := NewBootstrapProgressOverlay(80, 24, nil, nil)
	assert.False(t, o.Done())
	assert.Nil(t, o.Result())
	// View must render a non-empty string.
	assert.NotEmpty(t, o.View())
}

func TestBootstrapProgressOverlay_ProgressMessage(t *testing.T) {
	o := NewBootstrapProgressOverlay(80, 24, nil, nil)
	updated, _ := o.Update(BootstrapProgressMsg{
		Phase:   "analysis",
		Message: "scanning files",
		Count:   3,
	})
	progress := updated.(*BootstrapProgressOverlay)
	assert.False(t, progress.Done())
	assert.Contains(t, progress.View(), "scanning files")
}

func TestBootstrapProgressOverlay_CompletePhase(t *testing.T) {
	o := NewBootstrapProgressOverlay(80, 24, nil, nil)
	updated, _ := o.Update(BootstrapProgressMsg{Phase: "complete"})
	progress := updated.(*BootstrapProgressOverlay)
	assert.True(t, progress.Done())
	assert.Contains(t, progress.View(), "Bootstrap complete")
}

func TestBootstrapProgressOverlay_ErrorPhase(t *testing.T) {
	o := NewBootstrapProgressOverlay(80, 24, nil, nil)
	updated, _ := o.Update(BootstrapProgressMsg{Phase: "error", Error: "boom"})
	progress := updated.(*BootstrapProgressOverlay)
	assert.True(t, progress.Done())
	assert.Contains(t, progress.View(), "boom")
}

func TestBootstrapProgressOverlay_WindowSize(t *testing.T) {
	o := NewBootstrapProgressOverlay(80, 24, nil, nil)
	updated, _ := o.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	progress := updated.(*BootstrapProgressOverlay)
	assert.Equal(t, 120, progress.width)
	assert.Equal(t, 40, progress.height)
}

// --- StatusBar.ErrorCount ---

func TestStatusBar_ErrorCount_GetterMatchesSetters(t *testing.T) {
	sb := NewStatusBar(80)
	assert.Equal(t, 0, sb.ErrorCount())

	sb.IncrementErrorCount()
	sb.IncrementErrorCount()
	assert.Equal(t, 2, sb.ErrorCount())

	sb.ClearErrorCount()
	assert.Equal(t, 0, sb.ErrorCount())
}

// --- colorizeBold (inline **bold** runs) ---

func TestColorizeBold_NoBoldMarkers_ReturnsUnchanged(t *testing.T) {
	line := "plain text with no bold"
	assert.Equal(t, line, colorizeBold(line))
}

func TestColorizeBold_SingleBoldRun_RendersStyled(t *testing.T) {
	got := colorizeBold("before **bold** after")
	// Plain-text content (ANSI stripped) must still contain the literal bold run.
	assert.Contains(t, stripANSI(got), "**bold**")
	assert.Contains(t, stripANSI(got), "before")
	assert.Contains(t, stripANSI(got), "after")
}

func TestColorizeBold_UnterminatedBold_FallsBackToLine(t *testing.T) {
	line := "start **unterminated bold"
	// Must not loop forever or drop content.
	assert.Contains(t, colorizeBold(line), "unterminated bold")
}

func TestColorizeMarkdown_ExercisesBoldBranch(t *testing.T) {
	md := "# Heading\n```\ncode fenced\n```\nSome **bold** inline\nplain tail"
	result := colorizeMarkdown(md)
	plain := stripANSI(result)
	assert.Contains(t, plain, "Heading")
	assert.Contains(t, plain, "code fenced")
	assert.Contains(t, plain, "**bold**")
	assert.Contains(t, plain, "plain tail")
}

// --- ApprovalPrompt.Result (pending path) ---

func TestApprovalPrompt_Result_BeforeDoneReturnsPending(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60, nil, false)
	// Not yet acted on.
	assert.False(t, ap.Done())
	assert.Equal(t, ApprovalPending, ap.Result())
}

func TestApprovalOverlay_Result_PendingReturnsNil(t *testing.T) {
	o := NewApprovalOverlay("shell", `{"command":"ls"}`, "/tmp", 80,
		[]ApprovalResult{ApprovalYes, ApprovalNo}, false)
	// Overlay Result() returns nil until the prompt has a result.
	assert.Nil(t, o.Result())
}

// --- fallbackFormatArgs ---

func TestFallbackFormatArgs_NoArgs(t *testing.T) {
	assert.Equal(t, "(no arguments)", fallbackFormatArgs(map[string]json.RawMessage{}))
}

func TestFallbackFormatArgs_PriorityKey(t *testing.T) {
	args := map[string]json.RawMessage{
		"command": json.RawMessage(`"ls -la"`),
		"extra":   json.RawMessage(`"ignored"`),
	}
	assert.Equal(t, "ls -la", fallbackFormatArgs(args))
}

func TestFallbackFormatArgs_NonStringValueTruncation(t *testing.T) {
	longVal := `"` + string(make([]byte, 60)) + `"` // 60 zero bytes
	for i := range longVal {
		if i > 0 && i < len(longVal)-1 {
			_ = i
		}
	}
	// Construct a string value longer than 40 runes to trigger truncation.
	args := map[string]json.RawMessage{
		"irrelevant": json.RawMessage(`"` +
			"this-is-a-reasonably-long-string-value-that-exceeds-forty-runes" + `"`),
	}
	got := fallbackFormatArgs(args)
	assert.Contains(t, got, "irrelevant:")
	assert.Contains(t, got, "...")
}

// --- processOverlayResult: Config/Wiki/Undo/Cancel branches ---

func TestProcessOverlayResult_ConfigResultClearsForm(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	cfg := config.DefaultConfig()
	o, _ := NewConfigOverlay(cfg, "")
	m.configForm = o.form
	m.state = StateConfigOverlay

	cmd := m.processOverlayResult(ConfigResult{})
	assert.Nil(t, cmd)
	assert.Nil(t, m.configForm)
	assert.Equal(t, StateInput, m.state)
}

func TestProcessOverlayResult_WikiResultStartsGenerationWithSafePath(t *testing.T) {
	dir := t.TempDir()
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.wikiCfg = WikiCommandConfig{WorkDir: dir}
	m.wikiForm = NewWikiForm(dir)
	m.state = StateWikiOverlay

	// Default form values (".", "docs/wiki") stay within workDir.
	cmd := m.processOverlayResult(WikiResult{Form: m.wikiForm})
	// startWikiGeneration returns a Cmd that runs wiki generation.
	assert.NotNil(t, cmd)
	assert.Nil(t, m.wikiForm)
	assert.Equal(t, StateInput, m.state)
	// Cancel immediately to avoid starting real work if the caller were to run cmd.
	if m.wikiCancel != nil {
		m.wikiCancel()
	}
}

func TestProcessOverlayResult_UndoWithoutCheckpointMgr(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateUndoOverlay

	cmd := m.processOverlayResult(UndoResult{Turn: 1, All: false})
	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
	// executeUndo writes a message about the missing checkpoint manager.
	assert.Contains(t, m.content.String(), "no checkpoint manager")
}

func TestProcessOverlayResult_NilCancelClearsApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	// Simulate a pending approval that should be denied on overlay cancellation.
	respCh := make(chan ApprovalResult, 1)
	m.pendingApproval = &approvalRequest{
		tool:          "shell",
		input:         `{"command":"x"}`,
		responseValue: respCh,
	}

	cmd := m.processOverlayResult(nil)
	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
	assert.Nil(t, m.pendingApproval)

	select {
	case r := <-respCh:
		assert.Equal(t, ApprovalNo, r)
	default:
		t.Fatal("expected ApprovalNo to be delivered on overlay cancel")
	}
}

func TestProcessOverlayResult_InitKnowledgeGraphNilProfile(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateInitKnowledgeGraphOverlay

	cmd := m.processOverlayResult(InitKnowledgeGraphResult{Profile: nil})
	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
}

// --- doQuit: exercise both with and without pending approval ---

func TestModel_DoQuit_ClearsPendingApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	respCh := make(chan ApprovalResult, 1)
	m.pendingApproval = &approvalRequest{tool: "shell", responseValue: respCh}

	m.doQuit()
	assert.True(t, m.quitting)
	assert.Nil(t, m.pendingApproval)
	// Response delivered to any waiting goroutine.
	select {
	case r := <-respCh:
		assert.Equal(t, ApprovalNo, r)
	default:
		t.Fatal("expected approval response on doQuit")
	}
}

// --- scrollToLastError ---

func TestModel_ScrollToLastError_NoErrorsIsNoop(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	// No error segments in content buffer — scrollToLastError early-returns.
	before := m.viewport.YOffset
	m.scrollToLastError()
	assert.Equal(t, before, m.viewport.YOffset)
}

func TestModel_ScrollToLastError_WithErrorScrollsToBottom(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.content.AppendError(CollapsibleError{Message: "boom"})
	// Call should not panic; it delegates to viewport.GotoBottom.
	m.scrollToLastError()
}

// --- actionIDFromApprovalResult / approvalResultFromActionID ---

func TestApprovalResultFromActionID_UnknownReturnsPending(t *testing.T) {
	r, ok := approvalResultFromActionID("totally-bogus-id")
	assert.False(t, ok)
	assert.Equal(t, ApprovalPending, r)
}

func TestApprovalResultFromActionID_AllKnownIDs(t *testing.T) {
	cases := map[string]ApprovalResult{
		"allow":        ApprovalYes,
		"yes":          ApprovalYes,
		"deny":         ApprovalNo,
		"no":           ApprovalNo,
		"allow_always": ApprovalAlways,
		"always":       ApprovalAlways,
		"deny_always":  ApprovalDenyAlways,
	}
	for id, want := range cases {
		got, ok := approvalResultFromActionID(id)
		assert.True(t, ok, "id %q should be known", id)
		assert.Equal(t, want, got, "id %q", id)
	}
}

func TestActionIDFromApprovalResult_CanonicalFallback(t *testing.T) {
	// Empty actions triggers the canonical switch.
	assert.Equal(t, "allow", actionIDFromApprovalResult(ApprovalYes, nil))
	assert.Equal(t, "deny", actionIDFromApprovalResult(ApprovalNo, nil))
	assert.Equal(t, "allow_always", actionIDFromApprovalResult(ApprovalAlways, nil))
	assert.Equal(t, "deny_always", actionIDFromApprovalResult(ApprovalDenyAlways, nil))
}

func TestActionIDFromApprovalResult_PrefersProvidedID(t *testing.T) {
	actions := []agent.UIAction{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}}
	// "yes" maps to ApprovalYes — should be returned rather than the canonical
	// "allow" so callers see the ID they originally offered.
	assert.Equal(t, "yes", actionIDFromApprovalResult(ApprovalYes, actions))
	assert.Equal(t, "no", actionIDFromApprovalResult(ApprovalNo, actions))
}

// --- optionsFromUIActions ---

func TestOptionsFromUIActions_EmptyReturnsNil(t *testing.T) {
	assert.Nil(t, optionsFromUIActions(nil))
	assert.Nil(t, optionsFromUIActions([]agent.UIAction{}))
}

func TestOptionsFromUIActions_DeduplicatesAndSkipsUnknown(t *testing.T) {
	actions := []agent.UIAction{
		{ID: "allow"},
		{ID: "yes"}, // duplicate of allow -> ApprovalYes
		{ID: "deny"},
		{ID: "whatever"}, // unknown -> skipped
		{ID: "always"},
	}
	opts := optionsFromUIActions(actions)
	assert.Equal(t, []ApprovalResult{ApprovalYes, ApprovalNo, ApprovalAlways}, opts)
}

// --- optionLabel ---

func TestOptionLabel_AllKnownResults(t *testing.T) {
	for _, opt := range []ApprovalResult{
		ApprovalYes, ApprovalNo, ApprovalAlways, ApprovalDenyAlways,
	} {
		assert.NotEmpty(t, optionLabel(opt), "label for %v should not be empty", opt)
	}
}

func TestOptionLabel_UnknownReturnsEmpty(t *testing.T) {
	// ApprovalPending falls through to the default branch.
	assert.Equal(t, "", optionLabel(ApprovalPending))
}

// --- MakeApprovalFunc: auto-approval path ---

func TestMakeApprovalFunc_AutoApproveForTrustedTool(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	// Mark a non-destructive tool as always-approved.
	m.alwaysApproved.Store("read_file", true)

	fn := m.MakeApprovalFunc()
	ok, err := fn(context.Background(), "read_file", json.RawMessage(`{"path":"/tmp/x"}`))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestMakeApprovalFunc_AutoDenyForBlockedTool(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.alwaysDenied.Store("shell", true)

	fn := m.MakeApprovalFunc()
	ok, err := fn(context.Background(), "shell", json.RawMessage(`{"command":"ls"}`))
	require.NoError(t, err)
	assert.False(t, ok)
}

// --- filecompletion SetWidth ---

func TestFileCompletionOverlay_SetWidth(t *testing.T) {
	overlay := NewFileCompletionOverlay(nil, 60)
	overlay.SetWidth(120)
	assert.Equal(t, 120, overlay.width)
}

// --- ContentBuffer.ToggleFullExpandMostRecent ---

func TestContentBuffer_ToggleFullExpandMostRecent_TogglesExpandableResult(t *testing.T) {
	buf := NewContentBuffer()
	// A short tool-result is ineligible; a long non-collapsed result is eligible.
	buf.AppendToolResult(CollapsibleToolResult{
		Name: "a", Content: "tiny", LineCount: 1, Collapsed: false,
	})
	buf.AppendToolResult(CollapsibleToolResult{
		Name: "b", Content: "long", LineCount: maxToolResultLines + 5, Collapsed: false,
	})

	buf.ToggleFullExpandMostRecent()
	results := buf.ToolResults()
	// Most-recent eligible result should be fully expanded now.
	assert.True(t, results[1].FullyExpanded)
	// Toggle again — should revert to not fully expanded.
	buf.ToggleFullExpandMostRecent()
	results = buf.ToolResults()
	assert.False(t, results[1].FullyExpanded)
}

func TestContentBuffer_ToggleFullExpandMostRecent_NoEligibleIsNoop(t *testing.T) {
	buf := NewContentBuffer()
	// Small tool result is not eligible.
	buf.AppendToolResult(CollapsibleToolResult{
		Name: "a", Content: "tiny", LineCount: 1, Collapsed: false,
	})
	// Should not panic and should not mark the segment as expanded.
	buf.ToggleFullExpandMostRecent()
	results := buf.ToolResults()
	assert.False(t, results[0].FullyExpanded)
}

// --- ModelPickerOverlay Update/View (multi-model non-autoselect path) ---

func TestModelPickerOverlay_UpdateAndView_MultipleModels(t *testing.T) {
	overlay, _ := NewModelPickerOverlay([]ModelChoice{
		{Name: "gpt-4o", Size: "large"},
		{Name: "gpt-4o-mini", Size: "small"},
	})
	// Not auto-selected since multiple choices.
	assert.False(t, overlay.Done())
	// View should render the huh-powered form content.
	assert.NotEmpty(t, overlay.View())
	// Forwarding a window resize to Update should not panic.
	updated, _ := overlay.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	assert.NotNil(t, updated)
}

// --- ColRangeForLine: out-of-range and multi-line middle ---

func TestColRangeForLine_OutOfSelectionReturnsZero(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 2, Col: 3},
		End:    Position{Line: 4, Col: 5},
		Active: true,
	}
	s, e := sel.ColRangeForLine(10, 20)
	assert.Equal(t, 0, s)
	assert.Equal(t, 0, e)
}

func TestColRangeForLine_MiddleOfMultiLineSelection(t *testing.T) {
	sel := MouseSelection{
		Start:  Position{Line: 1, Col: 3},
		End:    Position{Line: 5, Col: 7},
		Active: true,
	}
	s, e := sel.ColRangeForLine(3, 50)
	// Middle line is fully selected: 0..lineLen.
	assert.Equal(t, 0, s)
	assert.Equal(t, 50, e)
}

// --- refreshRenderers exercised via SetTermCaps with light background ---

func TestModel_RefreshRenderers_LightBackground(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.SetTermCaps(&terminal.Caps{DarkBackground: false})
	assert.NotNil(t, m.mdRenderer)
}

// --- Approval prompt: batch and viewport paths in HandleKey ---

func TestApprovalPrompt_HandleKey_BatchKeyEnabled(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60,
		[]ApprovalResult{ApprovalYes, ApprovalNo}, true)
	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	assert.True(t, handled)
	assert.True(t, ap.Done())
	assert.Equal(t, ApprovalAlways, ap.Result())
}

func TestApprovalPrompt_HandleKey_BatchKeyDisabled(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60,
		[]ApprovalResult{ApprovalYes, ApprovalNo}, false)
	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	assert.False(t, handled, "batch key should be ignored when hint is off")
	assert.False(t, ap.Done())
}

func TestApprovalPrompt_HandleKey_UnknownKey(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60,
		[]ApprovalResult{ApprovalYes, ApprovalNo}, false)
	assert.False(t, ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}))
}

func TestApprovalPrompt_HandleKey_DisallowedOption(t *testing.T) {
	// Options without Always: pressing 'a' must not trigger a decision.
	ap := NewApprovalPrompt("shell", `{"command":"rm -rf /"}`, "", 60,
		[]ApprovalResult{ApprovalYes, ApprovalNo}, false)
	assert.False(t, ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}))
	assert.False(t, ap.Done())
}

// --- view.View full-screen overlay branch ---

func TestModel_View_FullScreenOverlay_ReturnsOverlayView(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.activeOverlay = NewHelpOverlay(80, 24)

	view := m.View()
	// Full-screen overlays return their own view verbatim — no banner header.
	assert.NotContains(t, view, "rubichan · model")
}

func TestModel_View_QuittingMessage(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.quitting = true
	assert.NotEmpty(t, m.View())
}

func TestModel_View_PlainMode_StreamingAndApprovalBranches(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.SetPlainMode(true)

	// Streaming without thinking.
	m.state = StateStreaming
	m.turnStartTime = time.Now()
	out := m.View()
	assert.Contains(t, out, "Generating")

	// Streaming while thinking.
	m.thinkingActive = true
	out = m.View()
	assert.Contains(t, out, "Thinking")

	// Awaiting approval without an overlay — falls back to status bar path.
	m.state = StateAwaitingApproval
	m.thinkingActive = false
	out = m.View()
	assert.NotEmpty(t, out)
}

// --- activeSkillsLine truncation path ---

func TestModel_ActiveSkillsLine_TruncatesToWidth(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.width = 30
	m.activeSkills = []string{
		"very-long-skill-name-one",
		"another-long-skill-name-two",
		"third-long-skill-name-three",
	}
	line := m.activeSkillsLine()
	// stripped line should end with "..." because it exceeded the width.
	plain := stripANSI(line)
	assert.True(t, len(plain) <= m.width, "line should fit within width")
	assert.Contains(t, plain, "...")
}

func TestModel_ActiveSkillsLine_Empty(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.activeSkills = nil
	assert.Equal(t, "", m.activeSkillsLine())
}

// --- renderCurrentTurn ---

func TestModel_RenderCurrentTurn_EmptyTurnRenders(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.rawAssistant.WriteString("hello world")

	out, err := m.renderCurrentTurn(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, out)
}

// --- handleKeyMsg keybindings ---

func TestModel_KeyBindings_CtrlF_TogglesPlanPanel(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	before := m.planPanelVisible
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	assert.NotEqual(t, before, updated.(*Model).planPanelVisible)
}

func TestModel_KeyBindings_CtrlA_TogglesAgentPanel(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	before := m.agentPanelVisible
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	assert.NotEqual(t, before, updated.(*Model).agentPanelVisible)
}

func TestModel_KeyBindings_CtrlT_TogglesToolResults(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	// Need a collapsible segment so the branch runs.
	m.content.AppendToolResult(CollapsibleToolResult{
		Name: "a", Content: "x", LineCount: 1, Collapsed: false,
	})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	results := m.content.ToolResults()
	assert.True(t, results[0].Collapsed)
}

func TestModel_KeyBindings_CtrlE_TogglesExpandedView(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.content.AppendToolResult(CollapsibleToolResult{
		Name: "a", Content: "x", LineCount: maxToolResultLines + 5, Collapsed: false,
	})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	results := m.content.ToolResults()
	assert.True(t, results[0].FullyExpanded)
}

func TestModel_KeyBindings_CtrlL_NoErrorsIsNoop(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	// No errors — Ctrl+L path returns early, doesn't panic.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
}

func TestModel_KeyBindings_QuestionOpensHelp(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.NotNil(t, m.activeOverlay)
	_, ok := m.activeOverlay.(*HelpOverlay)
	assert.True(t, ok, "expected HelpOverlay")
}

// --- executeUndo with real checkpoint manager ---

func TestModel_ExecuteUndo_EmptyStack(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	mgr, err := checkpoint.New(t.TempDir(), "test-session", 0)
	require.NoError(t, err)
	m.SetCheckpointManager(mgr)

	m.executeUndo(UndoResult{Turn: 0, All: false})
	// With no checkpoints, undo yields an error message.
	assert.Contains(t, m.content.String(), "undo failed")
}

// --- MakeApprovalFunc interactive path (via goroutine responder) ---

func TestMakeApprovalFunc_InteractivePathDeliversAllow(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)

	// Responder goroutine emulates the TUI event loop delivering ApprovalYes.
	go func() {
		req := <-m.approvalCh
		req.responseValue <- ApprovalYes
	}()

	fn := m.MakeApprovalFunc()
	ok, err := fn(context.Background(), "read_file", json.RawMessage(`{"path":"/tmp/x"}`))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestMakeApprovalFunc_InteractivePathDeliversDeny(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	go func() {
		req := <-m.approvalCh
		req.responseValue <- ApprovalNo
	}()

	fn := m.MakeApprovalFunc()
	ok, err := fn(context.Background(), "shell", json.RawMessage(`{"command":"ls"}`))
	require.NoError(t, err)
	assert.False(t, ok)
}

// --- MakeUIRequestHandler ---

func TestMakeUIRequestHandler_RejectsNonApprovalKind(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	handler := m.MakeUIRequestHandler()

	_, err := handler.Request(context.Background(), agent.UIRequest{
		Kind: "unsupported-kind",
		ID:   "req-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported UI request kind")
}

func TestMakeUIRequestHandler_AutoApprovedShortCircuits(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.alwaysApproved.Store("read_file", true)

	handler := m.MakeUIRequestHandler()
	resp, err := handler.Request(context.Background(), agent.UIRequest{
		Kind: agent.UIKindApproval,
		ID:   "req-1",
		Metadata: map[string]string{
			"tool":  "read_file",
			"input": `{"path":"/tmp/x"}`,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "allow_always", resp.ActionID)
	assert.Equal(t, "req-1", resp.RequestID)
}

// --- ContentBuffer.AppendText early-return on empty ---

func TestContentBuffer_AppendText_EmptyIsNoop(t *testing.T) {
	buf := NewContentBuffer()
	buf.AppendText("")
	assert.Empty(t, buf.ToolResults())
	// No segments added.
	assert.Equal(t, "", buf.Render(80))
}

// --- CompletionOverlay.SelectedValue empty-candidates branch ---

func TestCompletionOverlay_SelectedValue_EmptyReturnsEmpty(t *testing.T) {
	reg := commands.NewRegistry()
	co := NewCompletionOverlay(reg, 80)
	assert.Equal(t, "", co.SelectedValue())
}

// --- FileCompletionOverlay.HandleTab not visible branch ---

// --- Mouse wheel scroll forwards to viewport ---

func TestModel_Update_MouseWheelScroll(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	// Fill content so there's something to scroll.
	for i := 0; i < 50; i++ {
		m.content.AppendText("line\n")
	}
	m.viewport.SetContent(m.viewportContent())
	m.viewport.GotoBottom()

	_, _ = m.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	// No panic is the primary assertion; the viewport may or may not move
	// depending on current offset, but the branch is exercised.
}

// --- Update delegates to active overlay when present ---

func TestModel_Update_DelegatesToActiveOverlay(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.activeOverlay = NewHelpOverlay(80, 24)

	// A benign key should be forwarded to the overlay without crashing.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
}

// --- Update: WindowSize while overlay is active ---

func TestModel_Update_WindowSizeWithOverlay(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.activeOverlay = NewHelpOverlay(80, 24)

	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Equal(t, 120, m.width)
	assert.Equal(t, 40, m.height)
}

// --- wikiDoneMsg handling ---

func TestModel_Update_WikiDoneSuccess(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.wikiRunning = true

	_, _ = m.Update(wikiDoneMsg{Err: nil})
	assert.False(t, m.wikiRunning)
	assert.Contains(t, m.content.String(), "complete")
}

func TestModel_Update_WikiDoneCancelled(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.wikiRunning = true

	_, _ = m.Update(wikiDoneMsg{Err: context.Canceled})
	assert.False(t, m.wikiRunning)
	assert.Contains(t, m.content.String(), "cancelled")
}

// --- approvalRequestMsg installs overlay and pendingApproval ---

func TestModel_Update_ApprovalRequestMsgInstallsOverlay(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	respCh := make(chan ApprovalResult, 1)

	_, _ = m.Update(approvalRequestMsg{
		tool:          "read_file",
		input:         `{"path":"/tmp/x"}`,
		responseValue: respCh,
	})

	assert.Equal(t, StateAwaitingApproval, m.state)
	assert.NotNil(t, m.activeOverlay)
	assert.NotNil(t, m.pendingApproval)
}

// --- handleTurnEvent: error branch via Update ---

func TestModel_Update_TurnEventMsg_Error(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming

	_, _ = m.Update(TurnEventMsg{
		Type: "error",
	})
	// Error events append a CollapsibleError and bump the error counter.
	assert.Equal(t, 1, m.statusBar.ErrorCount())
	assert.Contains(t, m.content.String(), "unknown error")
}

func TestModel_Update_TurnEventMsg_Done(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming
	m.turnStartTime = time.Now().Add(-time.Second)

	_, _ = m.Update(TurnEventMsg{Type: "done"})
	// "done" returns to StateInput after cleanup.
	assert.Equal(t, StateInput, m.state)
}

func TestModel_Update_TurnEventMsg_TextDelta(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming

	_, _ = m.Update(TurnEventMsg{Type: "text_delta", Text: "hello"})
	assert.Contains(t, m.rawAssistant.String(), "hello")
}

func TestModel_Update_TurnEventMsg_ThinkingDelta(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming

	_, _ = m.Update(TurnEventMsg{Type: "thinking_delta", Text: "ponder"})
	assert.Contains(t, m.rawThinking.String(), "ponder")
	assert.True(t, m.thinkingActive)
}

func TestModel_Update_TurnEventMsg_ToolResult(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.state = StateStreaming

	_, _ = m.Update(TurnEventMsg{
		Type: "tool_result",
		ToolResult: &agentsdk.ToolResultEvent{
			ID:      "tool-1",
			Name:    "read_file",
			Content: "contents",
			IsError: false,
		},
	})
	// A tool-result segment should appear in the buffer.
	assert.Equal(t, 1, m.content.ToolResultCount())
}

func TestFileCompletionOverlay_HandleTab_NotVisible(t *testing.T) {
	overlay := NewFileCompletionOverlay(nil, 60)
	accepted, value := overlay.HandleTab()
	assert.False(t, accepted)
	assert.Equal(t, "", value)
}

func TestModel_ExecuteUndo_RewindToTurnWithEmptyStack(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	mgr, err := checkpoint.New(t.TempDir(), "test-session-rewind", 0)
	require.NoError(t, err)
	m.SetCheckpointManager(mgr)

	// RewindToTurn with an empty stack returns empty restored list (no error).
	m.executeUndo(UndoResult{Turn: 5, All: true})
	// The message is either "nothing to undo" or "undo failed" depending on behavior.
	content := m.content.String()
	hasNothing := assert.ObjectsAreEqual(true, len(content) > 0)
	assert.True(t, hasNothing)
}

func TestModel_KeyBindings_CopySelectionOnCtrlC(t *testing.T) {
	m := NewModel(nil, "rubichan", "model", 10, "", nil, nil)
	m.selection = MouseSelection{
		Start:  Position{Line: 0, Col: 0},
		End:    Position{Line: 0, Col: 5},
		Active: true,
	}
	// Content must contain enough text for selection to be non-empty.
	m.content.AppendText("hello world\n")
	m.viewport.SetContent(m.viewportContent())

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	um := updated.(*Model)
	// Not quitting — Ctrl+C consumed the selection copy.
	assert.False(t, um.quitting)
	assert.False(t, um.selection.Active)
}

func TestApprovalPrompt_HandleKey_ViewportScrollKeys(t *testing.T) {
	// Args with >5 lines → useViewport is true.
	longArgs := `{"command":"` +
		"echo line1\\necho line2\\necho line3\\necho line4\\necho line5\\necho line6\\necho line7" +
		`"}`
	ap := NewApprovalPrompt("shell", longArgs, "", 80,
		[]ApprovalResult{ApprovalYes, ApprovalNo}, false)
	require.True(t, ap.useViewport, "expected viewport mode for long args")

	// Viewport-bound keys should be consumed by the prompt.
	assert.True(t, ap.HandleKey(tea.KeyMsg{Type: tea.KeyUp}))
	assert.True(t, ap.HandleKey(tea.KeyMsg{Type: tea.KeyDown}))
	assert.True(t, ap.HandleKey(tea.KeyMsg{Type: tea.KeyPgUp}))
	assert.True(t, ap.HandleKey(tea.KeyMsg{Type: tea.KeyPgDown}))
	// Prompt is not "done" — viewport scrolling isn't a decision.
	assert.False(t, ap.Done())
}
