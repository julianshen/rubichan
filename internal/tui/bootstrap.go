package tui

import (
	"os"

	"github.com/charmbracelet/huh"
	"github.com/julianshen/rubichan/internal/config"
)

// NeedsBootstrap returns true if the config file doesn't exist or has no
// provider API key configured (and provider is not ollama).
func NeedsBootstrap(configPath string) bool {
	cfg, err := config.Load(configPath)
	if err != nil {
		return true
	}
	// Ollama doesn't need API key.
	if cfg.Provider.Default == "ollama" {
		return false
	}
	// Check Anthropic key.
	if cfg.Provider.Anthropic.APIKey != "" || os.Getenv("ANTHROPIC_API_KEY") != "" {
		return false
	}
	// Check any OpenAI-compatible provider.
	for _, oc := range cfg.Provider.OpenAI {
		if oc.APIKey != "" {
			return false
		}
		if oc.APIKeySource != "" && os.Getenv(oc.APIKeySource) != "" {
			return false
		}
	}
	return true
}

// BootstrapForm is a first-run setup wizard using Huh multi-step form.
type BootstrapForm struct {
	form      *huh.Form
	cfg       *config.Config
	savePath  string
	openaiKey string
}

// NewBootstrapForm creates a multi-step setup wizard.
func NewBootstrapForm(savePath string) *BootstrapForm {
	cfg := config.DefaultConfig()

	// openaiKey is a staging area. After the form completes, Save() copies
	// it into the correct OpenAI-compatible provider config entry.
	bf := &BootstrapForm{
		cfg:      cfg,
		savePath: savePath,
	}

	providerGroup := huh.NewGroup(
		huh.NewSelect[string]().
			Title("Choose your AI provider").
			Options(
				huh.NewOption("Anthropic (Claude)", "anthropic"),
				huh.NewOption("OpenAI Compatible", "openai"),
				huh.NewOption("Ollama (Local)", "ollama"),
			).
			Value(&cfg.Provider.Default),
	).Title("Welcome to Rubichan")

	anthropicKeyGroup := huh.NewGroup(
		huh.NewInput().
			Title("Anthropic API Key").
			Placeholder("sk-ant-...").
			Value(&cfg.Provider.Anthropic.APIKey).
			EchoMode(huh.EchoModePassword),
	).Title("Authentication").
		WithHideFunc(func() bool { return cfg.Provider.Default != "anthropic" })

	openaiKeyGroup := huh.NewGroup(
		huh.NewInput().
			Title("OpenAI API Key").
			Placeholder("sk-...").
			Value(&bf.openaiKey).
			EchoMode(huh.EchoModePassword),
	).Title("Authentication").
		WithHideFunc(func() bool { return cfg.Provider.Default != "openai" })

	modelGroup := huh.NewGroup(
		huh.NewInput().
			Title("Model").
			Placeholder("claude-sonnet-4-5").
			Value(&cfg.Provider.Model),
	).Title("Model").
		WithHideFunc(func() bool { return cfg.Provider.Default == "ollama" })

	bf.form = huh.NewForm(providerGroup, anthropicKeyGroup, openaiKeyGroup, modelGroup)
	return bf
}

// Form returns the underlying huh.Form for Bubble Tea embedding.
func (b *BootstrapForm) Form() *huh.Form { return b.form }

// SetForm replaces the underlying huh.Form (needed for Bubble Tea Update cycle).
func (b *BootstrapForm) SetForm(f *huh.Form) { b.form = f }

// Config returns the config populated by the wizard.
func (b *BootstrapForm) Config() *config.Config { return b.cfg }

// Save persists the config. It copies the OpenAI key into the correct config
// entry if the user selected the OpenAI provider.
func (b *BootstrapForm) Save() error {
	if b.cfg.Provider.Default == "openai" && b.openaiKey != "" {
		b.cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
			{Name: "default", APIKey: b.openaiKey},
		}
	}
	return config.Save(b.savePath, b.cfg)
}

// IsCompleted returns true if the form has been completed (submitted).
func (b *BootstrapForm) IsCompleted() bool { return b.form.State == huh.StateCompleted }

// IsAborted returns true if the form has been aborted (cancelled).
func (b *BootstrapForm) IsAborted() bool { return b.form.State == huh.StateAborted }
