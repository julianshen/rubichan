package tui

import "github.com/charmbracelet/lipgloss"

// bannerStyle uses the same adaptive color scheme as the header for consistency.
var bannerStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#EEEEEE"}).
	Bold(true)

// Banner is the ASCII art displayed on TUI startup. It spells "RUBICHAN".
const Banner = ` _  .-')             .-. .-')                             ('-. .-.   ('-.         .-') _
( \( -O )            \  ( OO )                           ( OO )  /  ( OO ).-.    ( OO ) )
 ,------. ,--. ,--.   ;-----.\   ,-.-')          .-----. ,--. ,--.  / . --. /,--./ ,--,'
 |   /` + "`" + `. '|  | |  |   | .-.  |   |  |OO)        '  .--./ |  | |  |  | \-.  \ |   \ |  |\
 |  /  | ||  | | .-') | '-' /_)  |  |  \        |  |('-. |   .|  |.-'-'  |  ||    \|  | )
 |  |_.' ||  |_|( OO )| .-. ` + "`" + `.   |  |(_/       /_) |OO  )|       | \| |_.'  ||  .     |/
 |  .  '.'|  | | ` + "`" + `-' /| |  \  | ,|  |_.'       ||  |` + "`" + `-'| |  .-.  |  |  .-.  ||  |\    |
 |  |\  \('  '-'(_.-' | '--'  /(_|  |         (_'  '--'\ |  | |  |  |  | |  ||  | \   |
 ` + "`" + `--' '--' ` + "`" + `-----'    ` + "`" + `------'   ` + "`" + `--'            ` + "`" + `-----' ` + "`" + `--' ` + "`" + `--'  ` + "`" + `--' ` + "`" + `--'` + "`" + `--'  ` + "`" + `--'`
