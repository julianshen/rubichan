package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// AboutOverlay displays project branding and information in a scrollable overlay.
type AboutOverlay struct {
	viewport  viewport.Model
	cancelled bool
	width     int
	height    int
}

// NewAboutOverlay creates a new about overlay with the given dimensions.
func NewAboutOverlay(width, height int) *AboutOverlay {
	vp := viewport.New(width-4, height-6)
	vp.SetContent(aboutContent())

	return &AboutOverlay{
		viewport:  vp,
		cancelled: false,
		width:     width,
		height:    height,
	}
}

// aboutContent generates the static about content with logo and project info.
func aboutContent() string {
	return `                         __,,,,__
              _╓╖╖,  ▄@╬ÑÜ▒▒▒▒▒▒É╠╬K▄  ,▄╖╓_
             ÆÑÜÜ╠╬▓Ñ▒▒▒É╬▒▒▒▒▒╠╣▒▒▒Ü╬▓╬╩╚Ü╚▀_
            ▓▒Ü░▄▄Ñ░░Ä░▄▓╟Ü▒ÜÜ╚▒▌▓▄░╠░░╬▌▄░▒Ü▓
           !Ñ▒▒▒╫▌▓▒╫▒▌` + "`" + `▌╫▒▒û░╟▒▌╫` + "`" + `▓▒▌▒▓╫╬▒▒▒╟H
            ▓▒▒▒╢╬▌▒▓▓▄▄▓▓▀█║▒▓▌▓▓▄▄██▒╫╣▌▒▒▒╫
            ╙▒▒▒╫╣▌▒██▒▓╬█▌` + "`" + `▀▀` + "`" + `Φ█J▓╬██▒╫╬▌▒Ü║▌
             ▓▒▓▓╫Ñ▒▌▀╬,╔▀      ▀▄,▄▀▓▒╢▌█▓▒▓
             ▐╬██╫▒▒▓                ▓▒╠▌╫█╬▌
             ╫▓ ▓╫▒Ü╫▄     ~..⌐     ▄▌╫╟▌▓ ▓▌
             ▓  ╙▓▓╫▌█╬▓▓╗▄,,,,▄╗φ▓╬▓╢Ñ▓▓Γ  ▀
                   ╣╣▓█╬▀` + "`" + `"╗ÄW▄^` + "`" + `▀╣▓▓Ñ▓
                    '▀ Å"% ╙▓▓▀ A^╚ ▀"
                      Å  ╟ ╟»»▌ ▌  Φ


Rubichan — An AI Coding Agent
何が好き？ — What do you love?

A terminal-first CLI tool for interactive code analysis, documentation
generation, and skill-based automation. Built with Go, Bubble Tea, and
Claude AI.

Key Features:
  • Interactive TUI with code review and approval workflows
  • Headless mode for CI/CD integration
  • Wiki generator for batch documentation
  • Extensible skill system with Starlark sandboxing
  • Multi-language AST parsing with tree-sitter
  • Knowledge graph for contextual code understanding

Architecture Highlights:
  • Provider Layer: Custom HTTP+SSE for LLM providers (Anthropic, OpenAI, Ollama)
  • Tool System: Interface-based registry with sandbox permissions
  • Security Engine: Static scanners + LLM-powered analysis
  • Wiki Pipeline: Batch documentation with Mermaid diagrams
  • Conversation Persistence: SQLite-backed session management

Built with:
  • Go 1.21+ for performance and simplicity
  • Charm TUI libraries for terminal UI
  • Tree-sitter for multi-language AST parsing
  • Starlark for skill scripting sandboxing
  • SQLite for persistent storage

Learn More:
  • GitHub: github.com/julianshen/rubichan
  • Spec: See spec.md for full architecture details

Press Esc or 'q' to close this screen.
`
}

// Update handles input for the about overlay.
func (a *AboutOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEscape:
			a.cancelled = true
			return a, nil
		case tea.KeyUp:
			a.viewport.ScrollUp(1)
			return a, nil
		case tea.KeyDown:
			a.viewport.ScrollDown(1)
			return a, nil
		case tea.KeyPgUp:
			a.viewport.HalfPageUp()
			return a, nil
		case tea.KeyPgDown:
			a.viewport.HalfPageDown()
			return a, nil
		case tea.KeyHome:
			a.viewport.GotoTop()
			return a, nil
		case tea.KeyEnd:
			a.viewport.GotoBottom()
			return a, nil
		}

		// Check for character keys q/Q.
		switch msg.String() {
		case "q", "Q":
			a.cancelled = true
			return a, nil
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.viewport.Width = msg.Width - 4
		a.viewport.Height = msg.Height - 6
	}
	return a, nil
}

// View renders the about overlay.
func (a *AboutOverlay) View() string {
	// Border and title
	border := styleApprovalBorder.Width(a.width - 4)
	content := border.Render(a.viewport.View())

	// Hints at bottom
	hints := styleTextDim.Render("↑↓/jk = scroll · q/Esc = close")
	about := fmt.Sprintf("%s\n%s\n", content, styleKeyHint.Render(hints))

	return about
}

// Done returns true when the overlay should be dismissed.
func (a *AboutOverlay) Done() bool {
	return a.cancelled
}

// Result returns the overlay result (always nil for about overlay).
func (a *AboutOverlay) Result() any {
	return nil
}
