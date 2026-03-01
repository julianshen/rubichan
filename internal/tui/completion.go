package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/julianshen/rubichan/internal/commands"
)

const maxVisibleCandidates = 8

// CompletionOverlay shows a dropdown of matching slash commands above the
// input area. It queries the command registry for prefix matches and
// supports keyboard navigation and tab-completion.
type CompletionOverlay struct {
	registry   *commands.Registry
	candidates []commands.Candidate
	selected   int
	visible    bool
	dismissed  bool // prevents re-show after Escape until input changes
	width      int
	lastPrefix string
}

// NewCompletionOverlay creates a new completion overlay backed by the
// given command registry. The width parameter controls the rendered width.
func NewCompletionOverlay(registry *commands.Registry, width int) *CompletionOverlay {
	return &CompletionOverlay{
		registry: registry,
		width:    width,
	}
}

// Update refreshes the candidate list based on the current input text.
// If input starts with "/", it extracts the prefix after "/" and queries
// the registry. If a space is found after the command name, the overlay
// hides (argument completion is deferred). The dismissed flag prevents
// re-showing after Escape until the input no longer starts with "/".
func (co *CompletionOverlay) Update(input string) {
	// If input doesn't start with /, clear everything and reset dismissed.
	if !strings.HasPrefix(input, "/") {
		co.visible = false
		co.candidates = nil
		co.dismissed = false
		co.lastPrefix = ""
		return
	}

	// If dismissed, stay hidden until input no longer starts with /.
	if co.dismissed {
		return
	}

	// Extract the text after "/".
	rest := input[1:]

	// If there's a space, we're in argument phase — hide overlay.
	if strings.Contains(rest, " ") {
		co.visible = false
		co.candidates = nil
		co.lastPrefix = ""
		return
	}

	// Query registry for matching commands.
	candidates := co.registry.Match(rest)

	// Reset selected if the prefix changed (filter changed).
	if rest != co.lastPrefix {
		co.selected = 0
	}
	co.lastPrefix = rest

	co.candidates = candidates
	if co.selected >= len(candidates) && len(candidates) > 0 {
		co.selected = len(candidates) - 1
	}
	co.visible = len(candidates) > 0
}

// HandleKey processes a keypress when the overlay is visible.
// Up/Down navigate candidates (with wrap-around), Escape dismisses.
// Returns true if the key was consumed by the overlay.
func (co *CompletionOverlay) HandleKey(msg tea.KeyMsg) bool {
	if !co.visible {
		return false
	}

	switch msg.Type {
	case tea.KeyUp:
		co.selected--
		if co.selected < 0 {
			co.selected = len(co.candidates) - 1
		}
		return true

	case tea.KeyDown:
		co.selected++
		if co.selected >= len(co.candidates) {
			co.selected = 0
		}
		return true

	case tea.KeyEscape:
		co.visible = false
		co.dismissed = true
		return true
	}

	return false
}

// HandleTab accepts the currently selected candidate and returns its value.
// Returns (false, "") if the overlay is not visible or has no candidates.
func (co *CompletionOverlay) HandleTab() (accepted bool, value string) {
	if !co.visible || len(co.candidates) == 0 {
		return false, ""
	}
	return true, co.candidates[co.selected].Value
}

// Visible returns whether the overlay should be rendered.
func (co *CompletionOverlay) Visible() bool {
	return co.visible
}

// Candidates returns the current list of completion candidates.
func (co *CompletionOverlay) Candidates() []commands.Candidate {
	return co.candidates
}

// Selected returns the index of the currently highlighted candidate.
func (co *CompletionOverlay) Selected() int {
	return co.selected
}

// SelectedValue returns the value of the currently selected candidate.
// Returns an empty string if no candidates are available.
func (co *CompletionOverlay) SelectedValue() string {
	if len(co.candidates) == 0 {
		return ""
	}
	return co.candidates[co.selected].Value
}

// SetWidth updates the render width.
func (co *CompletionOverlay) SetWidth(w int) {
	co.width = w
}

// View renders the completion overlay as a bordered box with candidate rows.
// Returns an empty string when not visible.
func (co *CompletionOverlay) View() string {
	if !co.visible || len(co.candidates) == 0 {
		return ""
	}

	boxWidth := co.width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#666666"}).
		Width(boxWidth)

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#5A56E0")).
		Foreground(lipgloss.Color("#FFFFFF"))

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})

	// Compute a scroll window that keeps the selected item visible.
	start := 0
	total := len(co.candidates)
	if total > maxVisibleCandidates {
		// Scroll so the selected item stays in view.
		if co.selected >= maxVisibleCandidates {
			start = co.selected - maxVisibleCandidates + 1
		}
		if start+maxVisibleCandidates > total {
			start = total - maxVisibleCandidates
		}
	}
	end := start + maxVisibleCandidates
	if end > total {
		end = total
	}
	visible := co.candidates[start:end]

	var rows []string
	for idx, c := range visible {
		i := start + idx // actual index in the full candidate list
		name := fmt.Sprintf("/%s", c.Value)
		desc := c.Description

		// Calculate spacing between name and description.
		innerWidth := boxWidth - 2 // account for border padding
		nameLen := len(name)
		descLen := len(desc)
		spacing := innerWidth - nameLen - descLen
		if spacing < 2 {
			spacing = 2
		}

		row := name + strings.Repeat(" ", spacing) + descStyle.Render(desc)

		if i == co.selected {
			// Apply selected style to the entire row content.
			row = selectedStyle.Render(fmt.Sprintf("/%s", c.Value) + strings.Repeat(" ", spacing) + desc)
		}

		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	return borderStyle.Render(content)
}
