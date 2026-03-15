package tui

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/session"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/julianshen/rubichan/internal/tools"
)

type mockSkillSummaryProvider struct {
	summaries []skills.SkillSummary
}

func (m *mockSkillSummaryProvider) GetAllSkillSummaries() []skills.SkillSummary {
	return m.summaries
}

func TestUIStateConstants(t *testing.T) {
	states := []UIState{
		StateInput,
		StateStreaming,
		StateAwaitingApproval,
		StateConfigOverlay,
		StateBootstrap,
		StateWikiOverlay,
	}
	seen := make(map[UIState]bool)
	for _, s := range states {
		assert.False(t, seen[s], "duplicate UIState value: %d", s)
		seen[s] = true
	}
}

func TestNewModel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	assert.Equal(t, StateInput, m.state)
	assert.Equal(t, "rubichan", m.appName)
	assert.Equal(t, "claude-3", m.modelName)
	assert.Equal(t, 80, m.width)
	assert.Equal(t, 24, m.height)
	assert.False(t, m.quitting)
	assert.NotNil(t, m.input)
	assert.NotNil(t, m.viewport)
	assert.NotNil(t, m.spinner)
}

func TestModelHandleSlashQuit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.handleCommand("/quit")

	require.NotNil(t, cmd, "handleCommand(/quit) should return a non-nil tea.Cmd")
	assert.True(t, m.quitting)

	// Verify it produces a tea.Quit message
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)
}

func TestModelHandleSlashExit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.handleCommand("/exit")

	require.NotNil(t, cmd, "handleCommand(/exit) should return a non-nil tea.Cmd")
	assert.True(t, m.quitting)
}

func TestModelHandleSlashClear(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	// Write some content first
	m.content.WriteString("some previous content")

	cmd := m.handleCommand("/clear")

	assert.Nil(t, cmd, "handleCommand(/clear) should return nil (doesn't quit)")
	assert.Equal(t, "", m.content.String())
}

func TestModelHandleSlashHelp(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.handleCommand("/help")

	assert.Nil(t, cmd, "handleCommand(/help) should return nil")
	content := m.content.String()
	assert.Contains(t, content, "/quit")
	assert.Contains(t, content, "/clear")
	assert.Contains(t, content, "/model")
	assert.Contains(t, content, "/help")
}

func TestModelHandleSlashModel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.handleCommand("/model gpt-4")

	assert.Nil(t, cmd, "handleCommand(/model) should return nil")
	assert.Equal(t, "gpt-4", m.modelName)
	assert.True(t, strings.Contains(m.content.String(), "Model switched"))
}

func TestModelHandleSlashModelNoArg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.handleCommand("/model")

	assert.Nil(t, cmd)
	assert.Equal(t, "claude-3", m.modelName, "model should not change without argument")
	assert.Contains(t, m.content.String(), "Pigi")
}

func TestModelHandleSlashRalphLoopParsesQuotedArgs(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)
	m.agent = &agent.Agent{}
	require.NoError(t, reg.Register(commands.NewRalphLoopCommand(m.StartRalphLoop)))
	require.NoError(t, reg.Register(commands.NewCancelRalphCommand(m.CancelRalphLoop)))

	cmd := m.handleCommand(`/ralph-loop "finish the feature" --completion-promise "ALL DONE" --max-iterations 3`)
	require.NotNil(t, cmd)
	require.NotNil(t, m.ralph)
	assert.Equal(t, "finish the feature", m.ralph.cfg.Prompt)
	assert.Equal(t, "ALL DONE", m.ralph.cfg.CompletionPromise)
	assert.Equal(t, 3, m.ralph.cfg.MaxIterations)
	assert.Equal(t, StateStreaming, m.state)
	assert.Contains(t, m.content.String(), "finish the feature")
}

func TestModelHandleCommandParseError(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	cmd := m.handleCommand(`/ralph-loop "unterminated`)
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "unterminated quoted string")
}

func TestModelHandleUnknownCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.handleCommand("/unknown")

	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "Unknown command")
}

func TestModelHandleSlashOnlyShowsHelp(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.handleCommand("/")

	assert.Nil(t, cmd)
	assert.NotContains(t, m.content.String(), "Unknown command")
	assert.Contains(t, m.content.String(), "Available commands:")
}

func TestModelHandleSlashOnlyWithoutHelpCommand(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)
	cmd := m.handleCommand("/")

	assert.Nil(t, cmd)
	assert.NotContains(t, m.content.String(), "Unknown command")
	assert.Contains(t, m.content.String(), "Type /help to show available commands.")
}

// --- Task 24 Tests ---

func TestModelInit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.Init()

	// Init should return the input area's init command (focus)
	assert.NotNil(t, cmd, "Init should return a non-nil tea.Cmd")
}

func TestModelView(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	view := m.View()

	assert.Contains(t, view, "rubichan")
	assert.Contains(t, view, "claude-3")
}

func TestModelViewShowsActiveSkills(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.SetSkillSummaryProvider(&mockSkillSummaryProvider{
		summaries: []skills.SkillSummary{
			{Name: "app-generation-workflow", State: skills.SkillStateActive},
			{Name: "writing-plans", State: skills.SkillStateInactive},
		},
	})

	view := m.View()
	assert.Contains(t, view, "Skills: app-generation-workflow")
	assert.NotContains(t, view, "writing-plans")
	assert.Contains(t, view, "Ruby")
}

func TestModelViewPlainModeOmitsHeaderChrome(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.SetPlainMode(true)

	view := m.View()
	assert.NotContains(t, view, "rubichan · claude-3")
	assert.NotContains(t, view, "━━━━━━━━")
	assert.Contains(t, view, "❯ ")
}

func TestModelSetPlainModeClearsBannerContent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	require.NotEmpty(t, m.content.String())

	m.SetPlainMode(true)

	assert.Equal(t, "", m.content.String())
}

func TestModelHandleCommandRefreshesActiveSkills(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	provider := &mockSkillSummaryProvider{
		summaries: []skills.SkillSummary{
			{Name: "app-generation-workflow", State: skills.SkillStateInactive},
		},
	}
	m.SetSkillSummaryProvider(provider)

	require.NoError(t, reg.Register(commands.NewClearCommand(func() {
		provider.summaries[0].State = skills.SkillStateActive
	})))

	cmd := m.handleCommand("/clear")
	assert.Nil(t, cmd)
	assert.Equal(t, []string{"app-generation-workflow"}, m.activeSkills)
	assert.Contains(t, m.statusBar.View(), "Skills:")
	assert.Contains(t, m.statusBar.View(), "app-generation-workflow")
	assert.Contains(t, m.content.String(), `Skill "app-generation-workflow" activated.`)
}

func TestSummarizeActiveSkills(t *testing.T) {
	assert.Equal(t, "", summarizeActiveSkills(nil))
	assert.Equal(t, "alpha", summarizeActiveSkills([]string{"alpha"}))
	assert.Equal(t, "alpha, beta", summarizeActiveSkills([]string{"alpha", "beta"}))
	assert.Equal(t, "3 active (alpha, beta, +1)", summarizeActiveSkills([]string{"alpha", "beta", "gamma"}))
}

func TestDiffActiveSkills(t *testing.T) {
	activated, deactivated := diffActiveSkills(
		[]string{"alpha", "beta"},
		[]string{"beta", "gamma"},
	)
	assert.Equal(t, []string{"gamma"}, activated)
	assert.Equal(t, []string{"alpha"}, deactivated)
}

func TestLogSlashCommand(t *testing.T) {
	var buf strings.Builder
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	logSlashCommand("/skill deactivate alpha", "Skill \"alpha\" deactivated.\n", nil, []string{"alpha"})

	out := buf.String()
	assert.Contains(t, out, "slash command: /skill deactivate alpha")
	assert.Contains(t, out, `output="Skill \"alpha\" deactivated."`)
	assert.Contains(t, out, "deactivated=alpha")
}

func TestModelViewQuitting(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.quitting = true
	view := m.View()

	assert.Contains(t, view, "bye")
	assert.Contains(t, view, "Ruby")
}

func TestModelUpdateCtrlC(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	um := updated.(*Model)
	assert.True(t, um.quitting)
	require.NotNil(t, cmd)
	// cmd should produce QuitMsg
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)
}

func TestModelUpdateWindowSize(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	oldRenderer := m.mdRenderer
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	um := updated.(*Model)
	assert.Equal(t, 120, um.width)
	assert.Equal(t, 40, um.height)
	assert.NotNil(t, um.mdRenderer)
	assert.NotSame(t, oldRenderer, um.mdRenderer)
	assert.Equal(t, 120, um.toolBox.width)
}

func TestModelUpdateEnterSlashCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.input.SetValue("/help")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "/quit")
}

func TestModelUpdateEnterInlineSkillDirectiveRunsSkillCommand(t *testing.T) {
	reg := commands.NewRegistry()
	cmd := &testutil.StubSlashCommand{CommandName: "skill", Output: "Skill \"brainstorming\" activated."}
	require.NoError(t, reg.Register(cmd))
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)
	m.input.SetValue(`__skill({"name":"brainstorming"})`)

	updated, teaCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Nil(t, teaCmd)
	assert.Equal(t, StateInput, um.state)
	assert.Equal(t, []string{"activate", "brainstorming"}, cmd.LastArgs)
	assert.Contains(t, um.content.String(), `Inline skill directive: activate "brainstorming"`)
	assert.Contains(t, um.content.String(), `Skill "brainstorming" activated.`)
}

func TestModelUpdateEnterInlineSkillDirectiveShowsParseError(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.input.SetValue(`__skill({"action":"activate"})`)

	updated, teaCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Nil(t, teaCmd)
	assert.Equal(t, StateInput, um.state)
	assert.Contains(t, um.content.String(), "skill directive name is required")
}

func TestModelAdvanceRalphLoopStopsOnCompletionPromise(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.ralph = &ralphLoopState{
		cfg: commands.RalphLoopConfig{
			Prompt:            "keep going",
			MaxIterations:     3,
			CompletionPromise: "ALL DONE",
		},
	}

	cmd := m.advanceRalphLoop("work finished ALL DONE")
	assert.Nil(t, cmd)
	assert.Nil(t, m.ralph)
	assert.Contains(t, m.content.String(), "Ralph loop complete")
}

func TestModelAdvanceRalphLoopSchedulesNextIteration(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.agent = &agent.Agent{}
	m.ralph = &ralphLoopState{
		cfg: commands.RalphLoopConfig{
			Prompt:            "keep going",
			MaxIterations:     3,
			CompletionPromise: "ALL DONE",
		},
	}

	cmd := m.advanceRalphLoop("still working")
	require.NotNil(t, cmd)
	assert.Equal(t, 1, m.ralph.iteration)
	assert.Equal(t, StateStreaming, m.state)
	assert.Contains(t, m.content.String(), "keep going")
}

func TestModelAdvanceRalphLoopClearsPriorDiffSummary(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.agent = &agent.Agent{}
	m.diffSummary = "## Turn Summary: 1 file(s) changed\n\n- **foo.txt** created"
	m.diffExpanded = true
	m.ralph = &ralphLoopState{
		cfg: commands.RalphLoopConfig{
			Prompt:            "keep going",
			MaxIterations:     3,
			CompletionPromise: "ALL DONE",
		},
	}

	cmd := m.advanceRalphLoop("still working")

	require.NotNil(t, cmd)
	assert.Empty(t, m.diffSummary)
	assert.False(t, m.diffExpanded)
	assert.NotContains(t, m.viewport.View(), "Turn changes")
}

func TestModelAdvanceRalphLoopStopsAtMaxIterations(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.ralph = &ralphLoopState{
		cfg: commands.RalphLoopConfig{
			Prompt:            "keep going",
			MaxIterations:     2,
			CompletionPromise: "ALL DONE",
		},
		iteration: 1,
	}

	cmd := m.advanceRalphLoop("still working")
	assert.Nil(t, cmd)
	assert.Nil(t, m.ralph)
	assert.Contains(t, m.content.String(), "without completion promise")
}

func TestModelCancelRalphLoopCancelsTurn(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cancelled := false
	m.ralph = &ralphLoopState{cfg: commands.RalphLoopConfig{Prompt: "keep going", MaxIterations: 2, CompletionPromise: "DONE"}}
	m.turnCancel = func() { cancelled = true }

	ok := m.CancelRalphLoop()
	assert.True(t, ok)
	assert.True(t, cancelled)
	assert.True(t, m.ralph.cancelled)
	assert.Nil(t, m.turnCancel)
}

func TestModelCancelRalphLoopNoActiveLoop(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	assert.False(t, m.CancelRalphLoop())
}

func TestModelStartRalphLoopRequiresIdleState(t *testing.T) {
	m := NewModel(&agent.Agent{}, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming

	err := m.StartRalphLoop(commands.RalphLoopConfig{
		Prompt:            "keep going",
		MaxIterations:     2,
		CompletionPromise: "DONE",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "idle")
}

func TestModelStartRalphLoopRequiresAgent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	err := m.StartRalphLoop(commands.RalphLoopConfig{
		Prompt:            "keep going",
		MaxIterations:     2,
		CompletionPromise: "DONE",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no agent")
}

func TestModelMaybeStartRalphLoopNoopWhenAlreadyIterating(t *testing.T) {
	m := NewModel(&agent.Agent{}, "rubichan", "claude-3", 50, "", nil, nil)
	m.ralph = &ralphLoopState{
		cfg: commands.RalphLoopConfig{
			Prompt:            "keep going",
			MaxIterations:     3,
			CompletionPromise: "DONE",
		},
		iteration: 1,
	}

	cmd := m.maybeStartRalphLoop()
	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
	assert.NotContains(t, m.content.String(), "keep going")
}

func TestModelUpdateEnterEmptyInput(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	contentBefore := m.content.String()
	m.input.SetValue("")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	// Empty input should not change state or add new content
	assert.Equal(t, StateInput, um.state)
	assert.Equal(t, contentBefore, um.content.String())
}

func TestModelHandleTurnEventTextDelta(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	// Provide event channel so waitForEvent returns a non-nil cmd
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "text_delta",
		Text: "hello world",
	})

	updated, cmd := m.Update(evt)

	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "hello world")
	// Should continue waiting for events
	assert.NotNil(t, cmd)
}

func TestModelHandleTurnEventToolCall(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	// Provide a channel so waitForEvent has something to read from
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_call",
		ToolCall: &agent.ToolCallEvent{
			ID:   "tool-1",
			Name: "read_file",
		},
	})

	updated, _ := m.Update(evt)

	um := updated.(*Model)
	content := um.content.String()
	// Should render in a bordered box with tool name
	assert.Contains(t, content, "read_file")
	assert.Contains(t, content, "\u256d") // rounded border top-left
	assert.Contains(t, content, "\u2570") // rounded border bottom-left
}

func TestModelHandleTurnEventToolResult(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{
			ID:      "tool-1",
			Name:    "read_file",
			Content: "file contents here",
		},
	})

	updated, _ := m.Update(evt)

	um := updated.(*Model)
	// Content buffer has a placeholder; viewportContent() renders it.
	vc := um.viewportContent()
	assert.Contains(t, vc, "file contents here")
	assert.Contains(t, vc, "\u256d") // rounded border top-left
}

func TestModelHandleTurnEventToolProgress(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_progress",
		ToolProgress: &agent.ToolProgressEvent{
			ID:      "tool-1",
			Name:    "shell",
			Stage:   tools.EventDelta,
			Content: "partial output",
		},
	})

	updated, _ := m.Update(evt)

	um := updated.(*Model)
	content := um.content.String()
	assert.Contains(t, content, "shell:delta")
	assert.Contains(t, content, "partial output")
}

func TestModelHandleTurnEventToolProgressNilIsNoOp(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	contentBefore := m.content.String()

	evt := TurnEventMsg(agent.TurnEvent{
		Type:         "tool_progress",
		ToolProgress: nil,
	})

	updated, cmd := m.Update(evt)

	um := updated.(*Model)
	assert.Equal(t, contentBefore, um.content.String())
	assert.NotNil(t, cmd)
}

func TestModelHandleTurnEventToolResultTruncation(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	// Create a long result with many lines that should be truncated by ToolBoxRenderer
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "line content here\n"
	}
	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{
			ID:      "tool-1",
			Name:    "read_file",
			Content: longContent,
		},
	})

	updated, _ := m.Update(evt)

	um := updated.(*Model)
	// Content buffer has a placeholder; viewportContent() renders it.
	vc := um.viewportContent()
	// ToolBoxRenderer truncates by line count and shows "[N more lines]"
	assert.Contains(t, vc, "more lines")
}

func TestModelHandleTurnEventError(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:  "error",
		Error: assert.AnError,
	})

	updated, _ := m.Update(evt)

	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "Pigi")
}

func TestModelHandleTurnEventDone(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{Type: "done"})

	updated, cmd := m.Update(evt)

	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, um.eventCh)
	assert.Nil(t, cmd)
}

func TestModelHandleTurnEventDoneRendersMarkdown(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming

	// Simulate user prompt written before streaming starts
	m.content.WriteString("> hello\n")
	m.assistantStartIdx = m.content.Len()

	// Simulate text_delta events with markdown content
	m.rawAssistant.WriteString("Hello **world**")
	m.content.WriteString("Hello **world**")

	ch := make(chan agent.TurnEvent)
	close(ch)
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{Type: "done"})
	updated, cmd := m.Update(evt)

	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, cmd)

	content := um.content.String()
	// The raw ** markers should have been replaced by Glamour rendering
	assert.NotContains(t, content, "**world**")
	// But the rendered text should still contain the word
	assert.Contains(t, content, "world")
	// The user prompt should still be present
	assert.Contains(t, content, "> hello")
}

func TestModelHandleTurnEventDoneShowsCollapsedDiffSummary(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming

	ch := make(chan agent.TurnEvent)
	close(ch)
	m.eventCh = ch

	diffSummary := "## Turn Summary: 2 file(s) changed\n\n- **foo.txt** created (via write_file)\n- **bar.txt** modified (via apply_patch)\n```diff\n+hello\n```\n"

	updated, _ := m.Update(TurnEventMsg(agent.TurnEvent{
		Type:        "done",
		DiffSummary: diffSummary,
	}))

	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
	assert.Equal(t, diffSummary, um.diffSummary)
	assert.False(t, um.diffExpanded)

	viewportContent := um.viewport.View()
	assert.Contains(t, viewportContent, "Turn changes")
	assert.Contains(t, viewportContent, "2 files changed")
	assert.Contains(t, viewportContent, "ctrl+g")
	assert.NotContains(t, viewportContent, "foo.txt")
	assert.NotContains(t, viewportContent, "+hello")
}

func TestModelToggleDiffSummaryExpansion(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.diffSummary = "## Turn Summary: 1 file(s) changed\n\n- **foo.txt** created (via write_file)\n```diff\n+hello\n```\n"
	m.viewport.SetContent(m.viewportContent())

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	um := updated.(*Model)
	assert.True(t, um.diffExpanded)
	assert.Nil(t, cmd)
	assert.Contains(t, um.viewport.View(), "foo.txt")
	assert.Contains(t, um.viewport.View(), "hello")

	updated, cmd = um.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	um = updated.(*Model)
	assert.False(t, um.diffExpanded)
	assert.Nil(t, cmd)
	assert.Contains(t, um.viewport.View(), "Turn changes")
	assert.NotContains(t, um.viewport.View(), "foo.txt")
}

func TestModelToggleDiffSummaryIgnoredWithoutSummary(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})

	um := updated.(*Model)
	assert.False(t, um.diffExpanded)
	assert.Nil(t, cmd)
}

func TestModelToggleDiffSummaryIgnoredWhileStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	m.diffSummary = "## Turn Summary: 1 file(s) changed\n\n- **foo.txt** created"

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})

	um := updated.(*Model)
	assert.False(t, um.diffExpanded)
	assert.Nil(t, cmd)
}

func TestModelToggleDiffSummaryPreservesScrollPosition(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.viewport.Width = 80
	m.viewport.Height = 5

	var lines strings.Builder
	for i := 0; i < 50; i++ {
		lines.WriteString("line content\n")
	}
	m.content.WriteString(lines.String())
	m.diffSummary = "## Turn Summary: 1 file(s) changed\n\n- **foo.txt** created"
	m.viewport.SetContent(m.viewportContent())
	m.viewport.GotoBottom()
	m.viewport.HalfPageUp()
	assert.False(t, m.viewport.AtBottom(), "precondition: should be scrolled up")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})

	um := updated.(*Model)
	assert.True(t, um.diffExpanded)
	assert.Nil(t, cmd)
	assert.False(t, um.viewport.AtBottom(), "toggle should preserve the user's scroll position")
}

func TestModelViewStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	m.thinkingMsg = persona.ThinkingMessage()
	view := m.View()

	// During streaming, should show the persona thinking message.
	// The message rotates, but all variants contain "Ruby".
	assert.Contains(t, view, "Ruby")
}

func TestModelViewAwaitingApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateAwaitingApproval
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 80, nil)
	view := m.View()

	assert.Contains(t, view, "Ruby")
	assert.Contains(t, view, "[Y]")
}

func TestModelUpdateEnterUserMessage(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.input.SetValue("hello agent")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
	assert.Contains(t, um.content.String(), "hello agent")
	// Should return a batch command (startTurn + spinner tick)
	assert.NotNil(t, cmd)
}

func TestModelUpdateEnterUserMessageClearsPriorDiffSummary(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.diffSummary = "## Turn Summary: 1 file(s) changed\n\n- **foo.txt** created"
	m.diffExpanded = true
	m.input.SetValue("hello agent")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Empty(t, um.diffSummary)
	assert.False(t, um.diffExpanded)
	assert.NotContains(t, um.viewport.View(), "Turn changes")
}

func TestModelStartTurnNilAgent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.startTurn(nil, "test")

	require.NotNil(t, cmd)
	msg := cmd()
	evt, ok := msg.(TurnEventMsg)
	require.True(t, ok)
	assert.Equal(t, "error", evt.Type)
	assert.NotNil(t, evt.Error)
	assert.Contains(t, evt.Error.Error(), "no agent configured")
}

func TestModelUpdateRegularKey(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	// Type a regular character
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	um := updated.(*Model)
	// Should remain in input state; cmd from input area update
	assert.Equal(t, StateInput, um.state)
	_ = cmd // input area may return a cursor cmd
}

func TestModelUpdateEnterWhileStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	m.input.SetValue("ignored")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, cmd)
}

func TestModelUpdateSpinnerTick(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming

	// Spinner tick should update the spinner
	tickCmd := m.spinner.Tick
	msg := tickCmd()

	updated, _ := m.Update(msg)
	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
}

func TestModelUpdateSpinnerTickNotStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateInput

	// Spinner tick while not streaming should be handled gracefully
	tickCmd := m.spinner.Tick
	msg := tickCmd()

	updated, _ := m.Update(msg)
	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
}

func TestModelWaitForEventClosedChannel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	ch := make(chan agent.TurnEvent)
	close(ch)
	m.eventCh = ch

	cmd := m.waitForEvent()
	require.NotNil(t, cmd)

	msg := cmd()
	evt, ok := msg.(TurnEventMsg)
	require.True(t, ok)
	assert.Equal(t, "done", evt.Type)
}

func TestModelWaitForEventNilChannel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.eventCh = nil

	cmd := m.waitForEvent()
	assert.Nil(t, cmd)
}

func TestModelHandleTurnEventToolCallNilToolCall(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: nil,
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)
	// Nil tool_call should render a bordered box with empty name
	content := um.content.String()
	assert.Contains(t, content, "\u256d")
}

func TestModelHandleTurnEventToolResultNilToolResult(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: nil,
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)
	// Nil tool_result should render via collapsible result (expanded during streaming)
	vc := um.viewportContent()
	assert.Contains(t, vc, "\u256d")
}

func TestModelHandleTurnEventErrorNilError(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:  "error",
		Error: nil,
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "Pigi")
	assert.Contains(t, um.content.String(), "unknown error")
}

func TestModelHandleTurnEventErrorResetsRawAssistant(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch
	m.rawAssistant.WriteString("stale DONE content")

	updated, _ := m.Update(TurnEventMsg(agent.TurnEvent{
		Type:  "error",
		Error: assert.AnError,
	}))

	um := updated.(*Model)
	assert.Equal(t, "", um.rawAssistant.String())
	assert.Contains(t, um.content.String(), assert.AnError.Error())
}

func TestModelUpdateWindowSizeTiny(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 10, Height: 2})

	um := updated.(*Model)
	assert.Equal(t, 10, um.width)
	assert.Equal(t, 2, um.height)
}

func TestModelHandleCommandEmptyString(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.handleCommand("")
	assert.Nil(t, cmd)
}

func TestModelHandleTurnEventUnknownType(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "unknown_type",
	})

	updated, cmd := m.Update(evt)
	um := updated.(*Model)
	// Unknown types should still continue reading
	assert.Equal(t, StateStreaming, um.state)
	assert.NotNil(t, cmd)
}

func TestModelUpdateUnknownMsg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	// Send an unrecognized message type
	type customMsg struct{}
	updated, cmd := m.Update(customMsg{})

	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, cmd)
}

func TestModelUpdateKeyWhileStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming

	// Regular key press while streaming should be ignored (not forwarded to input area)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	um := updated.(*Model)
	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, cmd)
}

func TestModelStatusBarInView(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-sonnet-4-5", 50, "", nil, nil)
	m.state = StateInput
	view := m.View()
	assert.Contains(t, view, "claude-sonnet-4-5")
	assert.Contains(t, view, "Turn 0/50")
}

func TestModelStatusBarUpdatedOnDone(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-sonnet-4-5", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent)
	close(ch)
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:         "done",
		InputTokens:  1500,
		OutputTokens: 300,
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)

	assert.Equal(t, 1, um.turnCount)
	view := um.View()
	assert.Contains(t, view, "Turn 1/50")
	assert.Contains(t, view, "1.5k")
}

// --- Config overlay tests ---

func TestModelHandleSlashConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, "rubichan", "claude-3", 50, "/tmp/test-config.toml", cfg, nil)

	cmd := m.handleCommand("/config")

	assert.Equal(t, StateConfigOverlay, m.state)
	assert.NotNil(t, m.configForm)
	assert.NotNil(t, cmd, "/config should return an init cmd from the form")
}

func TestModelHandleSlashConfigNilCfg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	cmd := m.handleCommand("/config")

	assert.Equal(t, StateInput, m.state, "should remain in input state without config")
	assert.Nil(t, m.configForm)
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "No config available")
}

func TestModelConfigOverlayView(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, "rubichan", "claude-3", 50, "/tmp/test-config.toml", cfg, nil)

	m.handleCommand("/config")
	view := m.View()

	// The view should render the form, not the normal TUI
	assert.NotContains(t, view, "rubichan · claude-3", "config overlay should not show normal header")
}

func TestModelConfigOverlayCompleted(t *testing.T) {
	cfg := config.DefaultConfig()
	dir := t.TempDir()
	m := NewModel(nil, "rubichan", "claude-3", 50, dir+"/config.toml", cfg, nil)

	m.handleCommand("/config")
	assert.Equal(t, StateConfigOverlay, m.state)

	// Simulate form completion by setting state directly
	m.configForm.Form().State = huh.StateCompleted

	// Send any message to trigger the state check in Update
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(*Model)

	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, um.configForm)
}

func TestModelConfigOverlayAborted(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, "rubichan", "claude-3", 50, "/tmp/test-config.toml", cfg, nil)

	m.handleCommand("/config")
	assert.Equal(t, StateConfigOverlay, m.state)

	// Simulate form abort
	m.configForm.Form().State = huh.StateAborted

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	um := updated.(*Model)

	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, um.configForm)
}

func TestModelHelpIncludesConfig(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.handleCommand("/help")

	content := m.content.String()
	assert.Contains(t, content, "/config")
}

func TestModelConfigOverlayRoutesMessages(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, "rubichan", "claude-3", 50, "/tmp/test-config.toml", cfg, nil)

	m.handleCommand("/config")
	assert.Equal(t, StateConfigOverlay, m.state)

	// Regular key message while in config overlay should be routed to form, not handled by model
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	um := updated.(*Model)

	// Should still be in config overlay state (not input state)
	assert.Equal(t, StateConfigOverlay, um.state)
}

// --- Approval wiring tests ---

func TestModelApprovalChannelInitialized(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	assert.NotNil(t, m.approvalCh)
	// sync.Map is always usable at zero value — verify Store/Load works.
	m.alwaysApproved.Store("test", true)
	_, ok := m.alwaysApproved.Load("test")
	assert.True(t, ok)
}

func TestModelApprovalRequestMsg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming

	// Simulate an approval request arriving
	respCh := make(chan bool, 1)
	msg := approvalRequestMsg{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	updated, cmd := m.Update(msg)
	um := updated.(*Model)

	assert.Equal(t, StateAwaitingApproval, um.state)
	assert.NotNil(t, um.approvalPrompt)
	assert.NotNil(t, cmd, "should return a cmd to wait for next approval")
}

func TestModelApprovalKeyYes(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 60, nil)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	// Provide event channel so waitForEvent works
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	um := updated.(*Model)

	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, um.approvalPrompt)
	assert.NotNil(t, cmd, "should return waitForEvent cmd")

	// Check that response was sent
	select {
	case approved := <-respCh:
		assert.True(t, approved)
	default:
		t.Fatal("expected response on channel")
	}
}

func TestModelApprovalKeyNo(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"rm -rf /"}`, 60, nil)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"rm -rf /"}`,
		response: respCh,
	}

	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	um := updated.(*Model)

	assert.Equal(t, StateStreaming, um.state)
	assert.Nil(t, um.approvalPrompt)
	assert.NotNil(t, cmd)

	select {
	case approved := <-respCh:
		assert.False(t, approved)
	default:
		t.Fatal("expected response on channel")
	}
}

func TestModelApprovalKeyAlways(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 60, nil)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	um := updated.(*Model)

	assert.Equal(t, StateStreaming, um.state)
	_, alwaysOK := um.alwaysApproved.Load("shell")
	assert.True(t, alwaysOK)
	assert.NotNil(t, cmd)

	select {
	case approved := <-respCh:
		assert.True(t, approved)
	default:
		t.Fatal("expected response on channel")
	}
}

func TestModelApprovalUnhandledKey(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 60, nil)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	um := updated.(*Model)

	// Should remain in awaiting approval state
	assert.Equal(t, StateAwaitingApproval, um.state)
	assert.NotNil(t, um.approvalPrompt)
	assert.Nil(t, cmd)

	// No response should have been sent
	select {
	case <-respCh:
		t.Fatal("no response expected for unhandled key")
	default:
		// expected
	}
}

func TestModelApprovalViewShowsPrompt(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateAwaitingApproval
	m.approvalPrompt = NewApprovalPrompt("file", `"/etc/hosts"`, 60, nil)

	view := m.View()
	assert.Contains(t, view, "file")
	assert.Contains(t, view, "Ruby")
	assert.Contains(t, view, "[Y]")
}

func TestModelMakeApprovalFunc(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	fn := m.MakeApprovalFunc()
	assert.NotNil(t, fn)
}

func TestModelMakeUIRequestHandlerApprovalAlways(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	handler := m.MakeUIRequestHandler()

	resultCh := make(chan agent.UIResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := handler.Request(context.Background(), agent.UIRequest{
			ID:   "req-1",
			Kind: agent.UIKindApproval,
			Actions: []agent.UIAction{
				{ID: "allow", Label: "Allow"},
				{ID: "deny", Label: "Deny"},
				{ID: "allow_always", Label: "Always Allow"},
			},
			Metadata: map[string]string{
				"tool":  "shell",
				"input": `{"command":"ls"}`,
			},
		})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- resp
	}()

	approvalMsg := m.waitForApproval()()
	updated, _ := m.Update(approvalMsg)
	m = updated.(*Model)

	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(*Model)

	select {
	case err := <-errCh:
		t.Fatalf("unexpected UI handler error: %v", err)
	case resp := <-resultCh:
		assert.Equal(t, "req-1", resp.RequestID)
		assert.Equal(t, "allow_always", resp.ActionID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UI response")
	}

	_, ok := m.alwaysApproved.Load("shell")
	assert.True(t, ok, "always-approved cache should be set after 'a'")
}

func TestModelMakeUIRequestHandlerUsesAlwaysDenied(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.alwaysDenied.Store("shell", true)
	handler := m.MakeUIRequestHandler()

	resp, err := handler.Request(context.Background(), agent.UIRequest{
		ID:   "req-2",
		Kind: agent.UIKindApproval,
		Metadata: map[string]string{
			"tool": "shell",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "req-2", resp.RequestID)
	assert.Equal(t, "deny_always", resp.ActionID)
}

func TestModelIsAutoApproved(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	// Initially nothing is auto-approved.
	assert.False(t, m.IsAutoApproved("shell"))
	assert.False(t, m.IsAutoApproved("file"))

	// Mark shell as always-approved.
	m.alwaysApproved.Store("shell", true)
	assert.True(t, m.IsAutoApproved("shell"))
	assert.False(t, m.IsAutoApproved("file"), "unrelated tool should not be auto-approved")

	// Mark file as well.
	m.alwaysApproved.Store("file", true)
	assert.True(t, m.IsAutoApproved("file"))
}

func TestModelCheckApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	// Initially nothing is auto-approved.
	assert.Equal(t, agent.ApprovalRequired, m.CheckApproval("shell", json.RawMessage(`{}`)))

	// Mark shell as always-approved via session cache.
	m.alwaysApproved.Store("shell", true)
	assert.Equal(t, agent.AutoApproved, m.CheckApproval("shell", json.RawMessage(`{}`)))
	assert.Equal(t, agent.ApprovalRequired, m.CheckApproval("file", json.RawMessage(`{}`)))
}

func TestModelCheckApprovalDenyTakesPriority(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	// Mark shell as deny-always.
	m.alwaysDenied.Store("shell", true)
	assert.Equal(t, agent.AutoDenied, m.CheckApproval("shell", json.RawMessage(`{}`)))

	// Even if also in always-approved (shouldn't happen, but defensive), deny wins.
	m.alwaysApproved.Store("shell", true)
	assert.Equal(t, agent.AutoDenied, m.CheckApproval("shell", json.RawMessage(`{}`)))
}

func TestModelMakeApprovalFuncAutoDeny(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.alwaysDenied.Store("shell", true)

	fn := m.MakeApprovalFunc()

	done := make(chan struct{})
	go func() {
		defer close(done)
		approved, err := fn(context.Background(), "shell", json.RawMessage(`{}`))
		assert.NoError(t, err)
		assert.False(t, approved)
	}()

	select {
	case <-done:
		// success — auto-denied immediately
	case <-time.After(time.Second):
		t.Fatal("MakeApprovalFunc for deny-always tool should return immediately")
	}
}

func TestModelImplementsApprovalChecker(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	var _ agent.ApprovalChecker = m // compile-time check
}

func TestModelMakeApprovalFuncAlwaysApproved(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.alwaysApproved.Store("shell", true)

	fn := m.MakeApprovalFunc()

	// Should return true immediately for always-approved tools.
	// This runs in a goroutine to avoid blocking.
	done := make(chan struct{})
	go func() {
		defer close(done)
		approved, err := fn(context.Background(), "shell", json.RawMessage(`{}`))
		assert.NoError(t, err)
		assert.True(t, approved)
	}()

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Fatal("MakeApprovalFunc for always-approved tool should return immediately")
	}
}

func TestModelInitIncludesWaitForApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd := m.Init()
	// Init should return a batch that includes waitForApproval
	assert.NotNil(t, cmd)
}

// --- Viewport scrolling tests ---

func TestModelViewportScrollUpPreservesPosition(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	// Set a small viewport so content overflows.
	m.viewport.Width = 80
	m.viewport.Height = 5

	// Fill content so it overflows the viewport.
	var lines strings.Builder
	for i := 0; i < 50; i++ {
		lines.WriteString("line content\n")
	}
	m.content.WriteString(lines.String())
	m.viewport.SetContent(m.content.String())
	m.viewport.GotoBottom()

	// Scroll up via PageUp key.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	um := updated.(*Model)

	// After scrolling up, viewport should NOT be at the bottom.
	assert.False(t, um.viewport.AtBottom(), "viewport should not be at bottom after PageUp")
}

func TestModelAutoScrollWhenAtBottom(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	m.viewport.Width = 80
	m.viewport.Height = 5

	// Fill content and stay at bottom.
	var lines strings.Builder
	for i := 0; i < 50; i++ {
		lines.WriteString("line content\n")
	}
	m.content.WriteString(lines.String())
	m.viewport.SetContent(m.content.String())
	m.viewport.GotoBottom()

	// Provide event channel.
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	// Receive new content — should auto-scroll since we're at bottom.
	evt := TurnEventMsg(agent.TurnEvent{
		Type: "text_delta",
		Text: "new streaming text\n",
	})
	updated, _ := m.Update(evt)
	um := updated.(*Model)

	assert.True(t, um.viewport.AtBottom(), "should auto-scroll when already at bottom")
}

func TestModelNoAutoScrollWhenScrolledUp(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	m.viewport.Width = 80
	m.viewport.Height = 5

	// Fill content so it overflows.
	var lines strings.Builder
	for i := 0; i < 50; i++ {
		lines.WriteString("line content\n")
	}
	m.content.WriteString(lines.String())
	m.viewport.SetContent(m.content.String())
	m.viewport.GotoBottom()

	// Scroll up first.
	m.viewport.HalfPageUp()
	assert.False(t, m.viewport.AtBottom(), "precondition: should be scrolled up")

	// Provide event channel.
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	// Receive new content — should NOT auto-scroll since user scrolled up.
	evt := TurnEventMsg(agent.TurnEvent{
		Type: "text_delta",
		Text: "new streaming text\n",
	})
	updated, _ := m.Update(evt)
	um := updated.(*Model)

	assert.False(t, um.viewport.AtBottom(), "should NOT auto-scroll when user scrolled up")
}

func TestModelPageDownScrollsViewport(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.viewport.Width = 80
	m.viewport.Height = 5

	// Fill content.
	var lines strings.Builder
	for i := 0; i < 50; i++ {
		lines.WriteString("line content\n")
	}
	m.content.WriteString(lines.String())
	m.viewport.SetContent(m.content.String())
	// Start at top.
	m.viewport.GotoTop()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	um := updated.(*Model)

	// Should have scrolled down from top.
	assert.Greater(t, um.viewport.YOffset, 0, "viewport should scroll down on PageDown")
}

func TestModelCtrlCDuringApproval(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateAwaitingApproval

	respCh := make(chan bool, 1)
	m.approvalPrompt = NewApprovalPrompt("shell", `{"command":"ls"}`, 60, nil)
	m.pendingApproval = &approvalRequest{
		tool:     "shell",
		input:    `{"command":"ls"}`,
		response: respCh,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	um := updated.(*Model)

	assert.True(t, um.quitting)
	assert.Nil(t, um.pendingApproval, "pendingApproval should be cleared on Ctrl+C")
	require.NotNil(t, cmd)
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)

	// Verify the agent goroutine was unblocked with a denial.
	select {
	case approved := <-respCh:
		assert.False(t, approved, "Ctrl+C should deny the pending approval")
	default:
		t.Fatal("expected denial response on channel when Ctrl+C pressed during approval")
	}
}

// --- Command registry integration tests ---

func TestModelHandleCommandViaRegistry(t *testing.T) {
	reg := commands.NewRegistry()
	_ = reg.Register(commands.NewHelpCommand(reg))
	_ = reg.Register(commands.NewClearCommand(nil))
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	cmd := m.handleCommand("/help")
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "/help")
	assert.Contains(t, m.content.String(), "/clear")
}

func TestModelCompletionSyncOnInput(t *testing.T) {
	reg := commands.NewRegistry()
	_ = reg.Register(commands.NewHelpCommand(reg))
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	m.input.SetValue("/")
	m.syncCompletion()
	assert.True(t, m.completion.Visible())
}

func TestModelNewModelNilRegistryCreatesDefault(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	assert.NotNil(t, m.cmdRegistry)
	assert.NotNil(t, m.completion)
}

func TestModelCompletionOverlayInView(t *testing.T) {
	reg := commands.NewRegistry()
	_ = reg.Register(commands.NewHelpCommand(reg))
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	// Trigger completion
	m.input.SetValue("/h")
	m.syncCompletion()

	view := m.View()
	assert.Contains(t, view, "/help")
}

func TestModelSetAgent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	assert.Nil(t, m.GetAgent())
	// SetAgent is a no-op with nil, but verifies the method works.
	m.SetAgent(nil)
	assert.Nil(t, m.GetAgent())
}

func TestModelGetAgent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	assert.Nil(t, m.GetAgent())
}

func TestModelClearContent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.content.WriteString("some content")
	m.viewport.SetContent(m.content.String())

	m.ClearContent()
	assert.Equal(t, "", m.content.String())
}

func TestModelSwitchModel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.SwitchModel("gpt-4")
	assert.Equal(t, "gpt-4", m.modelName)
}

func TestModelMaybeStartRalphLoopClearsPriorDiffSummary(t *testing.T) {
	m := NewModel(&agent.Agent{}, "rubichan", "claude-3", 50, "", nil, nil)
	m.diffSummary = "## Turn Summary: 1 file(s) changed\n\n- **foo.txt** created"
	m.diffExpanded = true
	m.ralph = &ralphLoopState{
		cfg: commands.RalphLoopConfig{
			Prompt:            "continue",
			CompletionPromise: "done",
			MaxIterations:     2,
		},
	}

	cmd := m.maybeStartRalphLoop()

	require.NotNil(t, cmd)
	assert.Empty(t, m.diffSummary)
	assert.False(t, m.diffExpanded)
	assert.NotContains(t, m.viewport.View(), "Turn changes")
}

func TestModelCompletionTabAccept(t *testing.T) {
	reg := commands.NewRegistry()
	_ = reg.Register(commands.NewHelpCommand(reg))
	_ = reg.Register(commands.NewQuitCommand())
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	// Type "/" to show completion
	m.input.SetValue("/h")
	m.syncCompletion()
	assert.True(t, m.completion.Visible())

	// Press Tab to accept
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	um := updated.(*Model)
	assert.Contains(t, um.input.Value(), "/help ")
}

func TestModelCompletionEscapeDismisses(t *testing.T) {
	reg := commands.NewRegistry()
	_ = reg.Register(commands.NewHelpCommand(reg))
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	m.input.SetValue("/h")
	m.syncCompletion()
	assert.True(t, m.completion.Visible())

	// Press Escape
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	um := updated.(*Model)
	assert.False(t, um.completion.Visible())
}

func TestModelCompletionUpDownNavigate(t *testing.T) {
	reg := commands.NewRegistry()
	_ = reg.Register(commands.NewHelpCommand(reg))
	_ = reg.Register(commands.NewQuitCommand())
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	// Type "/" to show all completions
	m.input.SetValue("/")
	m.syncCompletion()
	assert.True(t, m.completion.Visible())

	// Press Down to move selection
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	um := updated.(*Model)
	assert.Equal(t, 1, um.completion.Selected())
}

func TestRenderAssistantMarkdown(t *testing.T) {
	m := &Model{}
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	m.mdRenderer = r

	// Simulate: user prompt is prefix, raw markdown is assistant text.
	m.content.WriteString("> hello\n")
	m.assistantStartIdx = m.content.Len()
	m.rawAssistant.WriteString("Hello **world**")
	m.content.WriteString("Hello **world**") // raw during streaming

	m.renderAssistantMarkdown()

	result := m.content.String()
	assert.True(t, strings.HasPrefix(result, "> hello\n"))
	// After rendering, raw ** markers should be stripped.
	assert.NotContains(t, result[m.assistantStartIdx:], "**world**")
	assert.Contains(t, result, "world")
}

func TestRenderAssistantMarkdownEmpty(t *testing.T) {
	m := &Model{}
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	m.mdRenderer = r

	m.content.WriteString("> hello\n")
	m.assistantStartIdx = m.content.Len()

	// rawAssistant is empty — should be a no-op.
	contentBefore := m.content.String()
	m.renderAssistantMarkdown()

	assert.Equal(t, contentBefore, m.content.String())
}

func TestRenderAssistantMarkdownSanitizesProtocolContent(t *testing.T) {
	m := &Model{}
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	m.mdRenderer = r

	m.content.WriteString("> hello\n")
	m.assistantStartIdx = m.content.Len()
	m.rawAssistant.WriteString("assistantanalysisThinking..." +
		"assistantcommentary to=functions.shell json{\"command\":\"ls\"}" +
		"assistantfinalHello **world**")
	m.content.WriteString(m.rawAssistant.String())

	m.renderAssistantMarkdown()

	result := m.content.String()
	assert.True(t, strings.HasPrefix(result, "> hello\n"))
	assert.NotContains(t, result, "assistantanalysis")
	assert.NotContains(t, result, "assistantcommentary")
	assert.NotContains(t, result, "to=functions.shell")
	assert.NotContains(t, result, "**world**")
	assert.Contains(t, result, "world")
}

func TestModelHandleTurnEventTextDeltaSanitizesStreamingContent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	m.content.WriteString("> hello\n")
	m.assistantStartIdx = m.content.Len()
	m.assistantEndIdx = m.assistantStartIdx

	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	updated, _ := m.Update(TurnEventMsg(agent.TurnEvent{
		Type: "text_delta",
		Text: "assistantanalysisThinking..." +
			"assistantcommentary to=functions.shell json{\"command\":\"ls\"}" +
			"assistantfinalHello there",
	}))
	um := updated.(*Model)

	assert.NotContains(t, um.content.String(), "assistantanalysis")
	assert.NotContains(t, um.content.String(), "assistantcommentary")
	assert.NotContains(t, um.content.String(), "to=functions.shell")
	assert.Contains(t, um.content.String(), "Hello there")
}

func TestModelHandleTurnEventTextDeltaPreservesToolBoxes(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	m.content.WriteString("> hello\n")
	m.assistantStartIdx = m.content.Len()
	m.assistantEndIdx = m.assistantStartIdx

	_, _ = m.handleTurnEvent(TurnEventMsg(agent.TurnEvent{
		Type: "text_delta",
		Text: "assistantfinalHello",
	}))
	_, _ = m.handleTurnEvent(TurnEventMsg(agent.TurnEvent{
		Type: "tool_call",
		ToolCall: &agent.ToolCallEvent{
			Name:  "shell",
			Input: json.RawMessage(`{"command":"ls"}`),
		},
	}))
	_, _ = m.handleTurnEvent(TurnEventMsg(agent.TurnEvent{
		Type: "text_delta",
		Text: "assistantfinalHello there",
	}))

	content := m.content.String()
	assert.Contains(t, content, "Hello there")
	assert.Contains(t, content, "shell({\"command\":\"ls\"})")
}

func TestModelWikiOverlayRoutesMessages(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.SetWikiConfig(WikiCommandConfig{WorkDir: "/tmp"})
	m.handleCommand("/wiki")
	assert.Equal(t, StateWikiOverlay, m.state)

	// Regular key message while in wiki overlay should be routed to form
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	um := updated.(*Model)
	assert.Equal(t, StateWikiOverlay, um.state)
}

func TestModelWikiOverlayAborted(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.SetWikiConfig(WikiCommandConfig{WorkDir: "/tmp"})
	m.handleCommand("/wiki")
	assert.Equal(t, StateWikiOverlay, m.state)

	// Simulate form abort
	m.wikiForm.Form().State = huh.StateAborted
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	um := updated.(*Model)
	assert.Equal(t, StateInput, um.state)
	assert.Nil(t, um.wikiForm)
}

func TestModelCtrlCCancelsWiki(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cancelled := false
	m.wikiCancel = func() { cancelled = true }
	m.wikiRunning = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	um := updated.(*Model)
	assert.True(t, um.quitting)
	assert.True(t, cancelled)
	require.NotNil(t, cmd)
}

func TestModelWindowSizeUpdatesCompletionWidth(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	// Completion width should be updated (we can't directly assert the width
	// field, but we verify no panic and that the width was passed through).
	assert.Equal(t, 120, m.width)
}

func TestModelToolResultsCollapsedOnDone(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 2)
	m.eventCh = ch

	// Simulate tool_result event
	ch <- agent.TurnEvent{Type: "done"}
	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{
			ID:      "t1",
			Name:    "read_file",
			Content: "file contents",
		},
	})
	updated, _ := m.Update(evt)
	um := updated.(*Model)
	assert.Len(t, um.toolResults, 1)
	assert.False(t, um.toolResults[0].Collapsed, "result should be expanded during streaming")

	// Now handle "done" event
	ch2 := make(chan agent.TurnEvent)
	close(ch2)
	um.eventCh = ch2
	doneEvt := TurnEventMsg(agent.TurnEvent{Type: "done"})
	updated2, _ := um.Update(doneEvt)
	um2 := updated2.(*Model)
	assert.True(t, um2.toolResults[0].Collapsed, "result should be collapsed after done")
}

func TestModelToolResultsExpandedDuringStreaming(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{
			ID:      "t1",
			Name:    "read_file",
			Content: "file contents",
		},
	})
	updated, _ := m.Update(evt)
	um := updated.(*Model)
	assert.Len(t, um.toolResults, 1)
	assert.False(t, um.toolResults[0].Collapsed)
	// Viewport content should contain the file contents (expanded)
	vc := um.viewportContent()
	assert.Contains(t, vc, "file contents")
}

func TestModelCtrlTTogglesToolResults(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateInput
	// Add some collapsed tool results
	m.toolResults = []CollapsibleToolResult{
		{ID: 0, Name: "file", Args: "a.go", Content: "content a", LineCount: 1, Collapsed: true},
		{ID: 1, Name: "file", Args: "b.go", Content: "content b", LineCount: 1, Collapsed: true},
	}

	// Ctrl+T should expand all
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	um := updated.(*Model)
	assert.False(t, um.toolResults[0].Collapsed)
	assert.False(t, um.toolResults[1].Collapsed)

	// Ctrl+T again should collapse all
	updated2, _ := um.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	um2 := updated2.(*Model)
	assert.True(t, um2.toolResults[0].Collapsed)
	assert.True(t, um2.toolResults[1].Collapsed)
}

func TestModelToolCallArgsCachedForResults(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 2)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	// First send a tool_call to cache args
	callEvt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_call",
		ToolCall: &agent.ToolCallEvent{
			ID:    "t1",
			Name:  "file_read",
			Input: []byte(`{"path":"src/main.go"}`),
		},
	})
	updated, _ := m.Update(callEvt)
	um := updated.(*Model)

	// Then send the tool_result — args should come from the cached call
	resultEvt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{
			ID:      "t1",
			Name:    "file_read",
			Content: "package main",
		},
	})
	updated2, _ := um.Update(resultEvt)
	um2 := updated2.(*Model)
	assert.Len(t, um2.toolResults, 1)
	assert.Equal(t, `{"path":"src/main.go"}`, um2.toolResults[0].Args)
}

func TestModelClearContentResetsToolResults(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.toolResults = []CollapsibleToolResult{
		{ID: 0, Name: "file", Args: "a.go", Content: "x", LineCount: 1},
	}
	m.nextToolResultID = 1
	m.toolCallArgs = map[string]string{"t1": "args"}

	m.ClearContent()
	assert.Nil(t, m.toolResults)
	assert.Equal(t, 0, m.nextToolResultID)
	assert.Nil(t, m.toolCallArgs)
}

func TestModelToolResultEmptyContentLineCount(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{
			ID:      "t1",
			Name:    "shell",
			Content: "",
		},
	})
	updated, _ := m.Update(evt)
	um := updated.(*Model)
	assert.Equal(t, 0, um.toolResults[0].LineCount)
}

func TestBuildVerificationSnapshotPassed(t *testing.T) {
	summary := buildVerificationSnapshot(
		"Create a backend-only todo API using Node.js and SQLite",
		[]CollapsibleToolResult{
			{Name: "shell", Args: `{"command":"npm install express better-sqlite3"}`, Content: "added 10 packages"},
			{Name: "file", Args: `{"operation":"write","path":"schema.sql"}`, Content: "CREATE TABLE todos (id integer primary key, title text, completed integer)"},
			{Name: "process", Args: `{"operation":"exec","command":"node index.js"}`, Content: "Todo API server listening on http://localhost:3000"},
			{Name: "shell", Args: `{"command":"curl -s -X POST http://localhost:3000/todos && curl -s http://localhost:3000/stats"}`, Content: `{"id":1,"title":"Test Todo","completed":false}
{"total":1,"completed":0,"pending":1}`},
		},
	)

	assert.Contains(t, summary, "verdict: passed")
	assert.Contains(t, summary, "api round-trip: true")
}

func TestBuildVerificationSnapshotInvalidatedByLaterEdit(t *testing.T) {
	summary := buildVerificationSnapshot(
		"Create a backend-only todo API using Node.js and SQLite",
		[]CollapsibleToolResult{
			{Name: "shell", Args: `{"command":"npm install express better-sqlite3"}`, Content: "added 10 packages"},
			{Name: "file", Args: `{"operation":"write","path":"schema.sql"}`, Content: "CREATE TABLE todos (id integer primary key, title text, completed integer)"},
			{Name: "process", Args: `{"operation":"exec","command":"node index.js"}`, Content: "Todo API server listening on http://localhost:3000"},
			{Name: "shell", Args: `{"command":"curl -s -X POST http://localhost:3000/todos && curl -s http://localhost:3000/todos"}`, Content: `{"id":1,"title":"Test Todo","completed":false}
[{"id":1,"title":"Test Todo","completed":false}]`},
			{Name: "file", Args: `{"operation":"patch","path":"index.js"}`, Content: "patched index.js"},
		},
	)

	assert.Contains(t, summary, "verdict: failed")
	assert.Contains(t, summary, "invalidated by later edits")
}

func TestModelRegistersDebugVerificationSnapshotCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	cmd, ok := m.cmdRegistry.Get("debug-verification-snapshot")
	require.True(t, ok)

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "unavailable")
}

func TestModelHandleCommandEmitsSessionEvent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	var events []session.Event
	m.eventSink = session.SinkFunc(func(evt session.Event) {
		events = append(events, evt)
	})

	m.handleCommand("/help")

	require.Len(t, events, 1)
	assert.Equal(t, session.EventTypeCommandResult, events[0].Type)
	require.NotNil(t, events[0].Command)
	assert.Equal(t, "/help", events[0].Command.Command)
	assert.Contains(t, events[0].Command.Output, "Available commands:")
}

func TestModelDoneEmitsVerificationSnapshotEvent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	var events []session.Event
	m.eventSink = session.SinkFunc(func(evt session.Event) {
		events = append(events, evt)
	})
	m.sessionState.ResetForPrompt("Create a backend-only todo API using Node.js and SQLite")
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "1", Name: "shell", Input: json.RawMessage(`{"command":"npm install express better-sqlite3"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "1", Name: "shell", Content: "added 10 packages"},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "2", Name: "file", Input: json.RawMessage(`{"operation":"write","path":"schema.sql"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "2", Name: "file", Content: "CREATE TABLE todos (id integer primary key, title text, completed integer)"},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "3", Name: "process", Input: json.RawMessage(`{"operation":"exec","command":"node index.js"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "3", Name: "process", Content: "Todo API server listening on http://localhost:3000"},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "4", Name: "shell", Input: json.RawMessage(`{"command":"curl -s -X POST http://localhost:3000/todos && curl -s http://localhost:3000/todos"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "4", Name: "shell", Content: `{"id":1,"title":"Test Todo","completed":false}
[{"id":1,"title":"Test Todo","completed":false}]`},
	})

	updated, _ := m.Update(TurnEventMsg(agent.TurnEvent{Type: "done"}))
	m = updated.(*Model)

	require.NotEmpty(t, events)
	var verification *session.Event
	for i := range events {
		if events[i].Type == session.EventTypeVerificationSnapshot {
			verification = &events[i]
		}
	}
	require.NotNil(t, verification)
	require.NotNil(t, verification.Verification)
	assert.Equal(t, "passed", verification.Verification.Verdict)
	assert.Contains(t, verification.Verification.Snapshot, "api round-trip: true")
	assert.NotContains(t, m.content.String(), "Verification snapshot:")
}

func TestModelDoneShowsVerificationSnapshotInDebugMode(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.SetDebug(true)
	m.state = StateStreaming
	m.sessionState.ResetForPrompt("Create a backend-only todo API using Node.js and SQLite")
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "1", Name: "shell", Input: json.RawMessage(`{"command":"npm install express better-sqlite3"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "1", Name: "shell", Content: "added 10 packages"},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "2", Name: "process", Input: json.RawMessage(`{"operation":"exec","command":"node index.js"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "2", Name: "process", Content: "Todo API server listening on http://localhost:3000"},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "3", Name: "shell", Input: json.RawMessage(`{"command":"curl -s http://localhost:3000/todos"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "3", Name: "shell", Content: `[{"id":1,"title":"Test Todo","completed":false}]`},
	})

	updated, _ := m.Update(TurnEventMsg(agent.TurnEvent{Type: "done"}))

	assert.Contains(t, updated.(*Model).content.String(), "Verification snapshot:")
}

func TestModelDoneEmitsGateFailedAndPlanUpdatedWhenVerificationInvalidated(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	var events []session.Event
	m.eventSink = session.SinkFunc(func(evt session.Event) {
		events = append(events, evt)
	})
	m.sessionState.ResetForPrompt("Create a backend-only todo API using Node.js and SQLite")
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "1", Name: "shell", Input: json.RawMessage(`{"command":"npm install"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "1", Name: "shell", Content: "added 10 packages"},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "2", Name: "file", Input: json.RawMessage(`{"operation":"write","path":"schema.sql"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "2", Name: "file", Content: "CREATE TABLE todos (id integer primary key, title text, completed integer)"},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "3", Name: "process", Input: json.RawMessage(`{"operation":"exec","command":"node index.js"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "3", Name: "process", Content: "Todo API server listening on http://localhost:3000"},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "4", Name: "shell", Input: json.RawMessage(`{"command":"curl -s -X POST http://localhost:3000/todos && curl -s http://localhost:3000/todos"}`)},
	})
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type: "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "4", Name: "shell", Content: `{"id":1,"title":"Test Todo","completed":false}
[{"id":1,"title":"Test Todo","completed":false}]`},
	})
	// Edit after verification success should force re-verify.
	m.sessionState.ApplyEvent(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "5", Name: "file", Input: json.RawMessage(`{"operation":"patch","path":"index.js"}`)},
	})

	updated, _ := m.Update(TurnEventMsg(agent.TurnEvent{Type: "done"}))
	m = updated.(*Model)

	var sawPlan, sawGate bool
	for i := range events {
		if events[i].Type == session.EventTypePlanUpdated {
			sawPlan = true
			require.NotNil(t, events[i].Plan)
			require.NotEmpty(t, events[i].Plan.Steps)
			assert.Equal(t, "reverify_required", events[i].Plan.Steps[0].Status)
		}
		if events[i].Type == session.EventTypeGateFailed {
			sawGate = true
			require.NotNil(t, events[i].Gate)
			assert.Equal(t, "verification", events[i].Gate.Name)
			assert.Contains(t, events[i].Gate.Reason, "invalidated")
		}
	}
	assert.True(t, sawPlan)
	assert.True(t, sawGate)
}

func TestModelDoneEmitsAssistantFinalEvent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	var events []session.Event
	m.eventSink = session.SinkFunc(func(evt session.Event) {
		events = append(events, evt)
	})
	m.rawAssistant.WriteString("assistantfinalHello there")

	updated, _ := m.Update(TurnEventMsg(agent.TurnEvent{Type: "done"}))
	m = updated.(*Model)

	var assistant *session.Event
	for i := range events {
		if events[i].Type == session.EventTypeAssistantFinal {
			assistant = &events[i]
		}
	}
	require.NotNil(t, assistant)
	require.NotNil(t, assistant.Assistant)
	assert.Equal(t, "Hello there", assistant.Assistant.Content)
}

func TestModelTurnLifecycleEmitsSessionEvents(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	var events []session.Event
	m.eventSink = session.SinkFunc(func(evt session.Event) {
		events = append(events, evt)
	})

	m.input.SetValue("Create a backend-only todo API using Node.js and SQLite")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	require.NotNil(t, cmd)
	require.NotEmpty(t, events)
	assert.Equal(t, session.EventTypeTurnStarted, events[0].Type)
	require.NotNil(t, events[0].Turn)
	assert.Contains(t, events[0].Turn.Prompt, "backend-only todo API")
	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, session.EventTypeCheckpointCreated, events[1].Type)

	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	updated, _ = m.Update(TurnEventMsg(agent.TurnEvent{
		Type:     "tool_call",
		ToolCall: &agent.ToolCallEvent{ID: "1", Name: "shell", Input: json.RawMessage(`{"command":"npm install"}`)},
	}))
	m = updated.(*Model)
	assert.Equal(t, session.EventTypeToolCall, events[len(events)-1].Type)

	updated, _ = m.Update(TurnEventMsg(agent.TurnEvent{
		Type:       "tool_result",
		ToolResult: &agent.ToolResultEvent{ID: "1", Name: "shell", Content: "added 10 packages"},
	}))
	m = updated.(*Model)
	assert.Equal(t, session.EventTypeToolResult, events[len(events)-1].Type)
}

func TestHandleSubagentDoneRendersNotification(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "subagent_done",
		Text: "[Background task \"bg-42\" completed (agent: helper)]\nresults here",
		SubagentResult: &agent.SubagentResult{
			Name:   "helper",
			Output: "results here",
		},
	})
	updated, _ := m.Update(evt)
	um := updated.(*Model)
	assert.Contains(t, um.content.String(), "bg-42")
	assert.Contains(t, um.content.String(), "helper")
}
