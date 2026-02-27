package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// ModelChoice represents a selectable model option.
type ModelChoice struct {
	Name string
	Size string
}

// ModelPicker is a Bubble Tea component for selecting a model using Huh.
type ModelPicker struct {
	form      *huh.Form
	selected  string
	done      bool
	cancelled bool
}

// Ensure ModelPicker satisfies the tea.Model interface at compile time.
var _ tea.Model = (*ModelPicker)(nil)

// NewModelPicker creates a new ModelPicker with the given model choices.
// If exactly one model is provided, it is auto-selected.
// If no models are provided, the picker is immediately done with no selection.
func NewModelPicker(models []ModelChoice) *ModelPicker {
	if len(models) == 1 {
		return &ModelPicker{selected: models[0].Name, done: true}
	}
	if len(models) == 0 {
		return &ModelPicker{done: true}
	}

	p := &ModelPicker{}
	opts := make([]huh.Option[string], len(models))
	for i, m := range models {
		opts[i] = huh.NewOption(fmt.Sprintf("%s (%s)", m.Name, m.Size), m.Name)
	}

	sel := huh.NewSelect[string]().
		Title("Select a model").
		Options(opts...).
		Value(&p.selected)

	p.form = huh.NewForm(huh.NewGroup(sel))
	return p
}

// Selected returns the name of the selected model, or empty string if none.
func (p *ModelPicker) Selected() string { return p.selected }

// Done returns whether the selection is complete.
func (p *ModelPicker) Done() bool { return p.done }

// Cancelled returns whether the user cancelled the selection.
func (p *ModelPicker) Cancelled() bool { return p.cancelled }

// Init implements tea.Model. Initializes the huh form if present.
func (p *ModelPicker) Init() tea.Cmd {
	if p.form == nil {
		return nil
	}
	return p.form.Init()
}

// Update implements tea.Model. Delegates to the huh form and checks for
// completion or abort.
func (p *ModelPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.form == nil {
		return p, nil
	}
	form, cmd := p.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		p.form = f
	}
	if p.form.State == huh.StateCompleted {
		p.done = true
		return p, tea.Quit
	}
	if p.form.State == huh.StateAborted {
		p.cancelled = true
		return p, tea.Quit
	}
	return p, cmd
}

// View implements tea.Model. Renders the huh form.
func (p *ModelPicker) View() string {
	if p.form == nil {
		return ""
	}
	return p.form.View()
}
