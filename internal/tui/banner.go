package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/julianshen/rubichan/internal/persona"
)

// bannerColors is a pink gradient applied line-by-line to the banner.
var bannerColors = bannerGradient

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

// RenderBanner returns the full banner with a rainbow gradient applied per line.
func RenderBanner() string {
	lines := strings.Split(Banner, "\n")
	styled := make([]string, len(lines))
	for i, line := range lines {
		color := bannerColors[i%len(bannerColors)]
		style := lipgloss.NewStyle().Foreground(color).Bold(true)
		styled[i] = style.Render(line)
	}
	return strings.Join(styled, "\n") + "\n" + styleWelcome.Render(persona.WelcomeMessage())
}

// RenderCompactBanner returns a one-line banner for resume sessions or
// short terminals, preserving the pink identity with adaptive colors.
func RenderCompactBanner() string {
	name := lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render("rubichan")
	tag := lipgloss.NewStyle().Foreground(colorPrimaryDim).Render("何が好き？")
	return name + " " + tag + " " + styleWelcome.Render(persona.StatusPrefix()+" ready")
}
