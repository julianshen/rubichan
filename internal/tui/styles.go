package tui

import "github.com/charmbracelet/lipgloss"

// Pink theme color palette.
// These constants define a cohesive pink-themed color scheme inspired by
// Ruby's persona. The palette uses warm pinks, soft magentas, and
// complementary neutrals for a clean, readable terminal UI.
// Primary and accent colors use AdaptiveColor for proper light/dark contrast.
var (
	// Primary brand colors — used for headers, accents, active elements.
	// Light mode uses deeper pinks for contrast against white backgrounds.
	colorPrimary      = lipgloss.AdaptiveColor{Light: "#CC4477", Dark: "#FF6B9D"} // warm pink
	colorPrimaryBold  = lipgloss.AdaptiveColor{Light: "#CC1166", Dark: "#FF3385"} // hot pink — emphasis
	colorPrimaryLight = lipgloss.AdaptiveColor{Light: "#DD6699", Dark: "#FFB3D0"} // pastel pink — subtle accents
	colorPrimaryDim   = lipgloss.AdaptiveColor{Light: "#994466", Dark: "#CC5580"} // muted pink — secondary text

	// Accent colors — used for interactive highlights and selections.
	colorAccent     = lipgloss.AdaptiveColor{Light: "#CC5588", Dark: "#FF85B5"} // rose — selections, highlights
	colorAccentDim  = lipgloss.AdaptiveColor{Light: "#AA4477", Dark: "#D4609A"} // dusty rose — dimmed accents
	colorAccentGlow = lipgloss.AdaptiveColor{Light: "#DD7799", Dark: "#FF9EC7"} // light rose — hover/glow

	// Semantic colors — used for status indicators.
	colorSuccess = lipgloss.AdaptiveColor{Light: "#2D8B3D", Dark: "#7CDB8A"} // green — success, added lines
	colorWarning = lipgloss.AdaptiveColor{Light: "#CC8822", Dark: "#FFB347"} // amber — warnings, medium risk
	colorDanger  = lipgloss.AdaptiveColor{Light: "#CC3333", Dark: "#FF6B6B"} // red — errors, high risk, removed lines
	colorInfo    = lipgloss.AdaptiveColor{Light: "#3377AA", Dark: "#7CC4E8"} // blue — info, hunk headers

	// Neutral colors — used for text, borders, backgrounds.
	colorTextBright = lipgloss.AdaptiveColor{Light: "#1A1A2E", Dark: "#F0E6F0"}
	colorTextNormal = lipgloss.AdaptiveColor{Light: "#333344", Dark: "#D4C6D4"}
	colorTextDim    = lipgloss.AdaptiveColor{Light: "#777788", Dark: "#998899"}
	colorTextMuted  = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666677"}
	colorBorder     = lipgloss.AdaptiveColor{Light: "#CC7799", Dark: "#995577"}
	colorBorderDim  = lipgloss.AdaptiveColor{Light: "#AA8899", Dark: "#665566"}
	colorBgSubtle   = lipgloss.AdaptiveColor{Light: "#FFF0F5", Dark: "#2A1A24"}
	colorBgSelected = lipgloss.AdaptiveColor{Light: "#FFD6E8", Dark: "#4A2040"}

	// Banner gradient — pink spectrum from light to deep.
	// Non-adaptive because gradient ordering requires precise color steps;
	// dual-mode gradients would double the palette for diminishing returns.
	bannerGradient = []lipgloss.Color{
		"#FFB3D0", // pastel pink
		"#FF9EC7", // light rose
		"#FF85B5", // rose
		"#FF6B9D", // warm pink
		"#FF5291", // bright pink
		"#FF3385", // hot pink
		"#E8297A", // deep pink
		"#D4609A", // dusty rose
		"#CC5580", // muted pink
	}
)

// Reusable style building blocks.
var (
	// Header and title styles.
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	styleDivider = lipgloss.NewStyle().
			Foreground(colorPrimaryDim)

	// User message prefix.
	styleUserPrompt = lipgloss.NewStyle().
			Foreground(colorPrimaryBold).
			Bold(true)

	// Input area prompt.
	styleInputPrompt = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	// Status bar.
	styleStatusBar = lipgloss.NewStyle().
			Foreground(colorTextDim)

	styleStatusLabel = lipgloss.NewStyle().
				Foreground(colorPrimaryLight)

	styleStatusValue = lipgloss.NewStyle().
				Foreground(colorTextNormal)

	// Spinner / thinking indicator.
	styleSpinner = lipgloss.NewStyle().
			Foreground(colorAccentGlow)

	// Success and error message styles.
	styleSuccess = lipgloss.NewStyle().
			Foreground(colorSuccess)

	styleError = lipgloss.NewStyle().
			Foreground(colorDanger)

	// Welcome / banner subtitle.
	styleWelcome = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Italic(true)

	// Tool box borders.
	styleToolBoxBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder).
				Padding(0, 1)

	styleToolBoxErrorBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorDanger).
				Padding(0, 1)

	// Approval prompt border.
	styleApprovalBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorPrimary).
				Padding(0, 1)

	// Risk level indicators.
	styleRiskHigh = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	styleRiskMedium = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true)

	styleRiskLow = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	styleDestructiveWarning = lipgloss.NewStyle().
				Foreground(colorDanger)

	// Approval option key styling.
	styleApprovalKey = lipgloss.NewStyle().
				Foreground(colorPrimaryBold).
				Bold(true)

	styleApprovalLabel = lipgloss.NewStyle().
				Foreground(colorTextNormal)

	// Completion overlay styles.
	styleCompletionBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder)

	styleCompletionSelected = lipgloss.NewStyle().
				Background(colorBgSelected).
				Foreground(colorTextBright)

	styleCompletionDesc = lipgloss.NewStyle().
				Foreground(colorTextMuted)

	// Diff colorization.
	styleDiffAdded   = lipgloss.NewStyle().Foreground(colorSuccess)
	styleDiffRemoved = lipgloss.NewStyle().Foreground(colorDanger)
	styleDiffHunk    = lipgloss.NewStyle().Foreground(colorInfo)

	// Diff summary panel.
	styleDiffPanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderDim).
			Padding(0, 1)

	// Collapsible tool result header.
	styleToolResultHeader = lipgloss.NewStyle().
				Foreground(colorPrimaryLight)

	// Section labels (e.g., "args:", "Turn changes").
	styleSectionLabel = lipgloss.NewStyle().
				Foreground(colorTextDim)

	// Keyboard shortcut hints.
	styleKeyHint = lipgloss.NewStyle().
			Foreground(colorPrimaryDim)

	// Dim text for secondary information.
	styleTextDim = lipgloss.NewStyle().
			Foreground(colorTextDim)

	// Error display styles.
	styleErrorBadge = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	styleErrorIcon = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	// Plan panel border.
	stylePlanPanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorInfo).
			Padding(0, 1)

	// Text selection highlight — inverted colors with blue-tinted background.
	selectionStyle = lipgloss.NewStyle().
			Reverse(true).
			Background(lipgloss.AdaptiveColor{Light: "#4A90E2", Dark: "#1D5FAD"})
)
