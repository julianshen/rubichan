package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// InitKnowledgeGraphOverlay displays the knowledge graph bootstrap interface.
type InitKnowledgeGraphOverlay struct {
	cancelled bool
	width     int
	height    int
}

// NewInitKnowledgeGraphOverlay creates a new knowledge graph bootstrap overlay.
func NewInitKnowledgeGraphOverlay(width, height int) *InitKnowledgeGraphOverlay {
	return &InitKnowledgeGraphOverlay{
		cancelled: false,
		width:     width,
		height:    height,
	}
}

// Update handles input for the knowledge graph overlay.
func (i *InitKnowledgeGraphOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEscape:
			i.cancelled = true
			return i, nil
		}

		// Check for character keys
		switch msg.String() {
		case "q", "Q", "n", "N":
			i.cancelled = true
			return i, nil
		case "y", "Y":
			i.cancelled = true
			return i, nil
		}

	case tea.WindowSizeMsg:
		i.width = msg.Width
		i.height = msg.Height
	}
	return i, nil
}

// View renders the knowledge graph overlay.
func (i *InitKnowledgeGraphOverlay) View() string {
	content := `
╭────────────────────────────────────────────────────────────╮
│  Initialize Knowledge Graph                                │
╰────────────────────────────────────────────────────────────╯

This will bootstrap your project's knowledge graph through:

  1. Interactive questionnaire about your project
  2. Automatic analysis of your codebase
  3. Discovery of modules, decisions, and integrations
  4. Interactive refinement with the agent

The process will create entities in .knowledge/ and start an
agent session for further refinement.

` + styleKeyHint.Render("y/Y = start  ·  n/N or Esc = cancel") + `
`

	return content
}

// Done returns true when the overlay should be dismissed.
func (i *InitKnowledgeGraphOverlay) Done() bool {
	return i.cancelled
}

// Result returns the overlay result (always nil for init knowledge graph overlay).
func (i *InitKnowledgeGraphOverlay) Result() any {
	return nil
}
