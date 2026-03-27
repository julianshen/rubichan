package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	inputMinHeight   = 3
	inputMaxHeight   = 8
	inputPromptWidth = 2 // width of the "❯ " prompt prefix
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
	ta.SetHeight(inputMinHeight)
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

// Reset clears the text content and shrinks back to minimum height.
func (ia *InputArea) Reset() {
	ia.textarea.Reset()
	ia.textarea.SetHeight(inputMinHeight)
}

// Init initializes the textarea and returns its initial command.
func (ia *InputArea) Init() tea.Cmd {
	return ia.textarea.Focus()
}

// Update delegates a message to the textarea, auto-grows height based on
// content line count (between inputMinHeight and inputMaxHeight), and
// returns any command.
func (ia *InputArea) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	ia.textarea, cmd = ia.textarea.Update(msg)
	ia.autoGrow()
	return cmd
}

// autoGrow adjusts the textarea height to fit the content line count,
// clamped between inputMinHeight and inputMaxHeight.
func (ia *InputArea) autoGrow() {
	lines := strings.Count(ia.textarea.Value(), "\n") + 1
	h := lines
	if h < inputMinHeight {
		h = inputMinHeight
	}
	if h > inputMaxHeight {
		h = inputMaxHeight
	}
	if h != ia.textarea.Height() {
		ia.textarea.SetHeight(h)
	}
}

// SetWidth sets the textarea width in columns.
func (ia *InputArea) SetWidth(w int) {
	ia.textarea.SetWidth(w)
}

// Height returns the current textarea height in rows.
func (ia *InputArea) Height() int {
	return ia.textarea.Height()
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
