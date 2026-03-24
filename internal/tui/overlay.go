package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Overlay represents a modal UI surface that temporarily takes over
// keyboard input from the main Model. When Done() returns true, the
// Model processes Result() and clears the overlay.
type Overlay interface {
	Update(msg tea.Msg) (Overlay, tea.Cmd)
	View() string
	Done() bool
	Result() any
}

// ConfigResult carries saved config form values.
type ConfigResult struct{}

// WikiResult carries wiki generation parameters.
type WikiResult struct {
	Form *WikiForm
}

// UndoResult carries the user's undo selection.
type UndoResult struct {
	Turn int  // turn number of selected checkpoint
	All  bool // true = rewind all changes from that turn
}
