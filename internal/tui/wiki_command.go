package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/parser"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/wiki"
)

// wikiDoneMsg signals wiki generation completion.
type wikiDoneMsg struct {
	Err error
}

// WikiCommandConfig holds dependencies for the /wiki command.
type WikiCommandConfig struct {
	WorkDir string
	LLM     wiki.LLMCompleter
}

type wikiCommand struct {
	cfg WikiCommandConfig
}

// NewWikiCommand creates a /wiki slash command that opens the wiki form overlay.
func NewWikiCommand(cfg WikiCommandConfig) commands.SlashCommand {
	return &wikiCommand{cfg: cfg}
}

func (c *wikiCommand) Name() string                      { return "wiki" }
func (c *wikiCommand) Description() string               { return "Generate project documentation wiki" }
func (c *wikiCommand) Arguments() []commands.ArgumentDef { return nil }

func (c *wikiCommand) Complete(_ context.Context, _ []string) []commands.Candidate {
	return nil
}

func (c *wikiCommand) Execute(_ context.Context, _ []string) (commands.Result, error) {
	return commands.Result{Action: commands.ActionOpenWiki}, nil
}

// WikiForm wraps a huh form for configuring wiki generation.
type WikiForm struct {
	form           *huh.Form
	Path           string
	Format         string
	OutDir         string
	ConcurrencyStr string
	workDir        string
}

// NewWikiForm creates a wiki configuration form with sensible defaults.
func NewWikiForm(workDir string) *WikiForm {
	wf := &WikiForm{
		Path:           ".",
		Format:         "raw-md",
		OutDir:         "docs/wiki",
		ConcurrencyStr: "5",
		workDir:        workDir,
	}

	group := huh.NewGroup(
		huh.NewInput().
			Title("Project Path").
			Value(&wf.Path),
		huh.NewSelect[string]().
			Title("Output Format").
			Options(
				huh.NewOption("Raw Markdown", "raw-md"),
				huh.NewOption("Hugo", "hugo"),
				huh.NewOption("Docusaurus", "docusaurus"),
			).
			Value(&wf.Format),
		huh.NewInput().
			Title("Output Directory").
			Value(&wf.OutDir),
		huh.NewInput().
			Title("Concurrency").
			Value(&wf.ConcurrencyStr),
	).Title("Wiki Generation")

	wf.form = huh.NewForm(group)
	return wf
}

// Form returns the underlying huh.Form.
func (wf *WikiForm) Form() *huh.Form { return wf.form }

// SetForm replaces the underlying huh.Form (used after Update returns a new form).
func (wf *WikiForm) SetForm(f *huh.Form) { wf.form = f }

// Concurrency parses the concurrency string, defaulting to 5.
func (wf *WikiForm) Concurrency() int {
	n, err := strconv.Atoi(wf.ConcurrencyStr)
	if err != nil || n <= 0 {
		return 5
	}
	return n
}

// startWikiGeneration launches wiki generation synchronously in a foreground tea.Cmd.
// The TUI blocks until generation completes; progress is written to stderr.
func (m *Model) startWikiGeneration(wf *WikiForm) tea.Cmd {
	m.wikiRunning = true
	m.content.WriteString(fmt.Sprintf("Wiki generation started (%s -> %s)\n", wf.Format, wf.OutDir))
	m.setContentAndAutoScroll()
	m.statusBar.SetWikiProgress("starting")

	dir := filepath.Clean(filepath.Join(m.wikiCfg.WorkDir, wf.Path))
	outDir := filepath.Clean(filepath.Join(m.wikiCfg.WorkDir, wf.OutDir))
	base := filepath.Clean(m.wikiCfg.WorkDir)
	if rel, err := filepath.Rel(base, dir); err != nil || strings.HasPrefix(rel, "..") {
		m.wikiRunning = false
		m.content.WriteString(persona.ErrorMessage("project path escapes working directory"))
		m.setContentAndAutoScroll()
		return nil
	}
	if rel, err := filepath.Rel(base, outDir); err != nil || strings.HasPrefix(rel, "..") {
		m.wikiRunning = false
		m.content.WriteString(persona.ErrorMessage("output directory escapes working directory"))
		m.setContentAndAutoScroll()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.wikiCancel = cancel

	cfg := wiki.Config{
		Dir:         dir,
		OutputDir:   outDir,
		Format:      wf.Format,
		Concurrency: wf.Concurrency(),
	}

	return func() tea.Msg {
		p := parser.NewParser()
		err := wiki.Run(ctx, cfg, m.wikiCfg.LLM, p)
		return wikiDoneMsg{Err: err}
	}
}
