package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// ModelTextInputOverlay is the /model picker's fallback for providers with
// no live listing capability — everything except Ollama, which has a real
// selection-list picker (ModelPickerOverlay) backed by Registry.ListModels.
// A single free-text field pre-filled with the current model, converging
// on the same ModelPickerResult every ModelPicker selection produces, so
// processOverlayResult needs no per-overlay-type branching for this case.
type ModelTextInputOverlay struct {
	form      *huh.Form
	value     string
	done      bool // true when user submitted or cancelled
	submitted bool // true when user pressed Enter (distinguishes from cancelled)
	cancelled bool // true when user pressed Escape
}

// NewModelTextInputOverlay creates the overlay pre-filled with currentModel
// and returns its init command.
func NewModelTextInputOverlay(currentModel string) (*ModelTextInputOverlay, tea.Cmd) {
	o := &ModelTextInputOverlay{value: currentModel}
	input := huh.NewInput().Title("Model name").Value(&o.value)
	o.form = huh.NewForm(huh.NewGroup(input))
	return o, o.form.Init()
}

// Update forwards the message to the underlying huh.Form, intercepting
// Escape and Enter keys to manage overlay completion.
func (o *ModelTextInputOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEscape:
			o.done = true
			o.cancelled = true
			o.submitted = false
			return o, nil
		case tea.KeyEnter:
			o.done = true
			o.submitted = true
			o.cancelled = false
			return o, nil
		}
	}

	form, cmd := o.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		o.form = f
	}
	return o, cmd
}

// View renders the input form.
func (o *ModelTextInputOverlay) View() string {
	return o.form.View()
}

// Done returns true when the form has been submitted or cancelled.
func (o *ModelTextInputOverlay) Done() bool {
	return o.done
}

// Result returns a ModelPickerResult when a non-empty model name was
// submitted, nil otherwise (cancelled, or submitted empty).
func (o *ModelTextInputOverlay) Result() any {
	if o.submitted && o.value != "" {
		return ModelPickerResult{ModelName: o.value}
	}
	return nil
}
