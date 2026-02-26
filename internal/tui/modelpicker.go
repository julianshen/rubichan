package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// ModelChoice represents a selectable model option.
type ModelChoice struct {
	Name string
	Size string
}

// ModelPicker is a Bubble Tea component for selecting an Ollama model.
type ModelPicker struct {
	models    []ModelChoice
	cursor    int
	selected  string
	done      bool
	cancelled bool
}

// Ensure ModelPicker satisfies the tea.Model interface at compile time.
var _ tea.Model = (*ModelPicker)(nil)

// NewModelPicker creates a new ModelPicker with the given model choices.
// If exactly one model is provided, it is auto-selected.
func NewModelPicker(models []ModelChoice) *ModelPicker {
	p := &ModelPicker{models: models}
	if len(models) == 1 {
		p.selected = models[0].Name
		p.done = true
	}
	return p
}

// Selected returns the name of the selected model, or empty string if none.
func (p *ModelPicker) Selected() string { return p.selected }

// Done returns whether the selection is complete.
func (p *ModelPicker) Done() bool { return p.done }

// Cancelled returns whether the user cancelled the selection.
func (p *ModelPicker) Cancelled() bool { return p.cancelled }

// Init implements tea.Model. Returns nil (no initial command).
func (p *ModelPicker) Init() tea.Cmd { return nil }

// Update implements tea.Model. It handles keyboard input for navigating and
// selecting a model from the list.
func (p *ModelPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			if p.cursor > 0 {
				p.cursor--
			}
		case tea.KeyDown:
			if p.cursor < len(p.models)-1 {
				p.cursor++
			}
		case tea.KeyEnter:
			p.selected = p.models[p.cursor].Name
			p.done = true
			return p, tea.Quit
		case tea.KeyCtrlC, tea.KeyEsc:
			p.cancelled = true
			return p, tea.Quit
		}
	}
	return p, nil
}

// View implements tea.Model. It renders the model list with a cursor indicator.
func (p *ModelPicker) View() string {
	s := "Select an Ollama model:\n\n"
	for i, m := range p.models {
		cursor := "  "
		if i == p.cursor {
			cursor = "> "
		}
		s += fmt.Sprintf("%s%s (%s)\n", cursor, m.Name, m.Size)
	}
	s += "\n(↑/↓ to move, enter to select, esc to cancel)\n"
	return s
}
