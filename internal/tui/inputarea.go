package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// InputArea wraps a bubbles textarea.Model for multi-line input.
// It uses alt+enter/ctrl+j for newlines and delegates enter handling
// to the parent Model for submission.
type InputArea struct {
	textarea textarea.Model
}

// NewInputArea creates a new InputArea with a multi-line text area.
// Newlines are inserted with alt+enter or ctrl+j. The parent is
// responsible for handling plain enter as submit.
func NewInputArea() *InputArea {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetHeight(3)
	ta.CharLimit = 0

	// Remap InsertNewline from enter to alt+enter/ctrl+j so the parent
	// can intercept plain enter for submission.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter", "ctrl+j"),
		key.WithHelp("alt+enter", "new line"),
	)

	ta.FocusedStyle.CursorLine = ta.FocusedStyle.CursorLine.UnsetBackground()

	return &InputArea{textarea: ta}
}

// Value returns the current text content.
func (ia *InputArea) Value() string {
	return ia.textarea.Value()
}

// SetValue replaces the text content.
func (ia *InputArea) SetValue(s string) {
	ia.textarea.SetValue(s)
}

// Reset clears the text content.
func (ia *InputArea) Reset() {
	ia.textarea.Reset()
}

// Init initializes the textarea and returns its initial command.
func (ia *InputArea) Init() tea.Cmd {
	return ia.textarea.Focus()
}

// Update delegates a message to the textarea and returns any command.
func (ia *InputArea) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	ia.textarea, cmd = ia.textarea.Update(msg)
	return cmd
}

// View renders the textarea.
func (ia *InputArea) View() string {
	return ia.textarea.View()
}

// Focus gives the textarea focus.
func (ia *InputArea) Focus() tea.Cmd {
	return ia.textarea.Focus()
}

// Blur removes focus from the textarea.
func (ia *InputArea) Blur() {
	ia.textarea.Blur()
}
