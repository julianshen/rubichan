package tui

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/huh"
	"github.com/julianshen/rubichan/internal/config"
)

// ConfigForm wraps a Huh form for editing rubichan configuration.
type ConfigForm struct {
	form        *huh.Form
	cfg         *config.Config
	savePath    string
	maxTurnsStr string
}

// NewConfigForm creates a config editor form populated from the given config.
func NewConfigForm(cfg *config.Config, savePath string) *ConfigForm {
	cf := &ConfigForm{
		cfg:         cfg,
		savePath:    savePath,
		maxTurnsStr: fmt.Sprintf("%d", cfg.Agent.MaxTurns),
	}

	providerGroup := huh.NewGroup(
		huh.NewSelect[string]().
			Title("Provider").
			Options(
				huh.NewOption("Anthropic", "anthropic"),
				huh.NewOption("OpenAI Compatible", "openai"),
				huh.NewOption("Ollama", "ollama"),
			).
			Value(&cfg.Provider.Default),
		huh.NewInput().
			Title("Model").
			Value(&cfg.Provider.Model),
		huh.NewInput().
			Title("API Key").
			Value(&cfg.Provider.Anthropic.APIKey).
			EchoMode(huh.EchoModePassword),
	).Title("Provider")

	agentGroup := huh.NewGroup(
		huh.NewInput().
			Title("Max Turns").
			Placeholder("50").
			Value(&cf.maxTurnsStr),
		huh.NewSelect[string]().
			Title("Approval Mode").
			Options(
				huh.NewOption("Prompt", "prompt"),
				huh.NewOption("Auto", "auto"),
				huh.NewOption("Deny", "deny"),
			).
			Value(&cfg.Agent.ApprovalMode),
	).Title("Agent")

	securityGroup := huh.NewGroup(
		huh.NewSelect[string]().
			Title("Fail-On Severity").
			Options(
				huh.NewOption("Critical", "critical"),
				huh.NewOption("High", "high"),
				huh.NewOption("Medium", "medium"),
				huh.NewOption("Low", "low"),
			).
			Value(&cfg.Security.FailOn),
	).Title("Security")

	cf.form = huh.NewForm(providerGroup, agentGroup, securityGroup)

	return cf
}

// GroupCount returns the number of form groups.
func (c *ConfigForm) GroupCount() int { return 3 }

// Save persists the config to disk. It parses the maxTurns string back to int
// before saving.
func (c *ConfigForm) Save() error {
	if v, err := strconv.Atoi(c.maxTurnsStr); err == nil {
		c.cfg.Agent.MaxTurns = v
	}
	return config.Save(c.savePath, c.cfg)
}

// Form returns the underlying huh.Form for Bubble Tea embedding.
func (c *ConfigForm) Form() *huh.Form { return c.form }

// SetForm replaces the underlying huh.Form. This is used when the form's
// Update method returns a new Form instance.
func (c *ConfigForm) SetForm(f *huh.Form) { c.form = f }

// IsCompleted returns true if the form has been completed (submitted).
func (c *ConfigForm) IsCompleted() bool { return c.form.State == huh.StateCompleted }

// IsAborted returns true if the form has been aborted (cancelled).
func (c *ConfigForm) IsAborted() bool { return c.form.State == huh.StateAborted }
