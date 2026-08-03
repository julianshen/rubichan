package tui

import (
	"strings"

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

// Update forwards every message to the underlying huh.Form first —
// including Escape and Enter — so any per-field logic huh runs
// synchronously (value sync, and validation if a .Validate() is ever
// added to this field) still executes. Overlay completion is then
// tracked with local flags rather than by reading o.form.State, because
// of two real huh v1.0.0 limitations, not a testing artifact:
//  1. huh's stock keymap never binds Escape to abort — o.form.State
//     would never become StateAborted no matter how this method is
//     driven, in tests or in production.
//  2. Submit's StateCompleted transition requires draining a 3-hop
//     Cmd -> Msg -> Update cascade (NextField -> blur -> nextGroup) that
//     a single Update() call — the shape every message arrives as in
//     this app — never completes on its own.
//
// Forwarding Enter before declaring done means the field's own
// validation (which huh runs synchronously within its Update handling,
// before the async NextField cmd is even constructed) still gets a
// chance to run. This field has no .Validate() today, so that's inert —
// but if one is ever added, its result isn't currently surfaced to the
// caller (this overlay declares "submitted" unconditionally on Enter,
// not conditionally on validation passing). A future validator would
// need this method to also inspect the field's error state before
// setting o.submitted, not just forward the message.
func (o *ModelTextInputOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	form, cmd := o.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		o.form = f
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
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
// submitted, nil otherwise (cancelled, or submitted empty/whitespace-only —
// trimmed before the check so a few stray spaces don't reach
// agent.SetModel as a garbage model name).
func (o *ModelTextInputOverlay) Result() any {
	trimmed := strings.TrimSpace(o.value)
	if o.submitted && trimmed != "" {
		return ModelPickerResult{ModelName: trimmed}
	}
	return nil
}
