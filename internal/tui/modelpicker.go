package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/julianshen/rubichan/internal/provider"
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
		return p, nil
	}
	if p.form.State == huh.StateAborted {
		p.cancelled = true
		return p, nil
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

// ModelPickerResult carries the selected model name from the overlay.
type ModelPickerResult struct {
	ModelName string
}

// ModelPickerOverlay adapts ModelPicker to the Overlay interface.
type ModelPickerOverlay struct {
	picker *ModelPicker
}

// NewModelPickerOverlay creates a model picker overlay and returns its init command.
func NewModelPickerOverlay(models []ModelChoice) (*ModelPickerOverlay, tea.Cmd) {
	p := NewModelPicker(models)
	o := &ModelPickerOverlay{picker: p}
	return o, p.Init()
}

func (o *ModelPickerOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	_, cmd := o.picker.Update(msg)
	return o, cmd
}

func (o *ModelPickerOverlay) View() string {
	return o.picker.View()
}

func (o *ModelPickerOverlay) Done() bool {
	return o.picker.Done() || o.picker.Cancelled()
}

func (o *ModelPickerOverlay) Result() any {
	if o.picker.Done() && !o.picker.Cancelled() && o.picker.Selected() != "" {
		return ModelPickerResult{ModelName: o.picker.Selected()}
	}
	return nil
}

// ModelsFetchedMsg carries the result of an async Registry.ListModels call,
// triggered when the model picker is opened for a provider that supports
// live listing (currently only Ollama). Handled in Update (update.go).
type ModelsFetchedMsg struct {
	Models []provider.Model
	Err    error
}

// fetchOllamaModelsTimeout bounds how long fetchOllamaModels waits for a
// response, in addition to (not instead of) ollama.Client's own 30s HTTP
// client timeout — this call lists installed models (metadata, not
// inference) and is expected to be near-instant, so a shorter ceiling
// gives better worst-case UX than waiting the client's full timeout
// before surfacing an error. Var, not const, so tests can shrink it.
var fetchOllamaModelsTimeout = 10 * time.Second

// fetchOllamaModels returns a tea.Cmd that queries the Registry in the
// background. Ollama's ListModels makes a real HTTP call to a local
// server — even though it's typically fast, it must never run inline in
// the synchronous command-dispatch path (ActionOpenModelPicker), which
// would block the whole TUI event loop on network I/O if the local
// server were slow or unresponsive. This mirrors the existing async
// tea.Cmd -> tea.Msg pattern used for wiki generation (wiki_command.go's
// wikiDoneMsg).
func (m *Model) fetchOllamaModels() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchOllamaModelsTimeout)
		defer cancel()
		models, err := provider.Default.ListModels(ctx, "ollama", cfg)
		return ModelsFetchedMsg{Models: models, Err: err}
	}
}
