package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/julianshen/rubichan/internal/persona"
)

// bannerColors is a rainbow gradient applied line-by-line to the banner.
var bannerColors = []lipgloss.Color{
	"#FF6B6B", // red
	"#FF8E53", // orange
	"#FFC857", // yellow
	"#A8E06C", // lime
	"#56D6A0", // green
	"#4ECDC4", // teal
	"#45B7D1", // cyan
	"#7C83FD", // blue
	"#B983FF", // purple
}

// Banner is the ASCII art displayed on TUI startup. It spells "RUBICHAN".
const Banner = ` _  .-')             .-. .-')                             ('-. .-.   ('-.         .-') _
( \( -O )            \  ( OO )                           ( OO )  /  ( OO ).-.    ( OO ) )
 ,------. ,--. ,--.   ;-----.\   ,-.-')          .-----. ,--. ,--.  / . --. /,--./ ,--,'
 |   /` + "`" + `. '|  | |  |   | .-.  |   |  |OO)        '  .--./ |  | |  |  | \-.  \ |   \ |  |\
 |  /  | ||  | | .-') | '-' /_)  |  |  \        |  |('-. |   .|  |.-'-'  |  ||    \|  | )
 |  |_.' ||  |_|( OO )| .-. ` + "`" + `.   |  |(_/       /_) |OO  )|       | \| |_.'  ||  .     |/
 |  .  '.'|  | | ` + "`" + `-' /| |  \  | ,|  |_.'       ||  |` + "`" + `-'| |  .-.  |  |  .-.  ||  |\    |
 |  |\  \('  '-'(_.-' | '--'  /(_|  |         (_'  '--'\ |  | |  |  |  | |  ||  | \   |
 ` + "`" + `--' '--' ` + "`" + `-----'    ` + "`" + `------'   ` + "`" + `--'            ` + "`" + `-----' ` + "`" + `--' ` + "`" + `--'  ` + "`" + `--' ` + "`" + `--'` + "`" + `--'  ` + "`" + `--'
                                     何が好き？`

// RenderBanner returns the banner with a rainbow gradient applied per line.
func RenderBanner() string {
	lines := strings.Split(Banner, "\n")
	styled := make([]string, len(lines))
	for i, line := range lines {
		color := bannerColors[i%len(bannerColors)]
		style := lipgloss.NewStyle().Foreground(color).Bold(true)
		styled[i] = style.Render(line)
	}
	welcomeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF6B9D")).
		Italic(true)
	return strings.Join(styled, "\n") + "\n" + welcomeStyle.Render(persona.WelcomeMessage())
}
