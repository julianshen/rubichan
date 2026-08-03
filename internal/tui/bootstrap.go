package tui

import (
	"github.com/charmbracelet/huh"
	"github.com/julianshen/rubichan/internal/config"
)

// defaultModelPlaceholder returns the hint text shown in the bootstrap
// wizard's model field for the given provider — the same literal each
// provider's own ProviderDef.DefaultModel resolves to (Anthropic:
// internal/provider/anthropic/provider.go; Z.ai: internal/provider/zai/provider.go).
// OpenAI-compatible has no fixed default since it's an arbitrary endpoint,
// so it gets a generic, well-known example instead.
func defaultModelPlaceholder(providerName string) string {
	switch providerName {
	case "anthropic":
		return "claude-sonnet-4-5"
	case "zai":
		return "glm-5"
	default:
		return "gpt-4o"
	}
}

// NeedsBootstrap returns true if the config file doesn't exist or has no
// provider API key configured (and provider is not ollama).
func NeedsBootstrap(configPath string) bool {
	cfg, err := config.Load(configPath)
	if err != nil {
		return true
	}
	return !config.HasUsableCredentialsForDefaultProvider(cfg)
}

// BootstrapForm is a first-run setup wizard using Huh multi-step form.
type BootstrapForm struct {
	form          *huh.Form
	cfg           *config.Config
	savePath      string
	openaiKey     string
	openaiBaseURL string

	// origAnthropicAPIKeySource/origZaiAPIKeySource capture each
	// provider's APIKeySource as cfg started (from config.DefaultConfig()
	// today, always "env" for Anthropic and unset for Z.ai — see the
	// matching field in ConfigForm for the full rationale). Kept here too
	// so Save()'s logic — and its "clear a config-sourced key resets the
	// source" behavior — stays identical between the first-run wizard and
	// the in-session /config editor, even though only the latter can load
	// an existing config-sourced key to begin with.
	origAnthropicAPIKeySource string
	origZaiAPIKeySource       string
}

// NewBootstrapForm creates a multi-step setup wizard.
func NewBootstrapForm(savePath string) *BootstrapForm {
	cfg := config.DefaultConfig()

	// openaiKey is a staging area. After the form completes, Save() copies
	// it into the correct OpenAI-compatible provider config entry.
	bf := &BootstrapForm{
		cfg:                       cfg,
		savePath:                  savePath,
		origAnthropicAPIKeySource: cfg.Provider.Anthropic.APIKeySource,
		origZaiAPIKeySource:       cfg.Provider.Zai.APIKeySource,
	}

	providerGroup := huh.NewGroup(
		huh.NewSelect[string]().
			Title("Choose your AI provider").
			Options(
				huh.NewOption("Anthropic (Claude)", "anthropic"),
				huh.NewOption("OpenAI Compatible", "openai"),
				huh.NewOption("Ollama (Local)", "ollama"),
				huh.NewOption("Z.ai (Zhipu)", "zai"),
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

	openaiGroup := huh.NewGroup(
		huh.NewInput().
			Title("Base URL").
			Description("Leave empty for https://api.openai.com/v1").
			Placeholder("https://api.openai.com/v1").
			Value(&bf.openaiBaseURL),
		huh.NewInput().
			Title("API Key").
			Placeholder("sk-...").
			Value(&bf.openaiKey).
			EchoMode(huh.EchoModePassword),
	).Title("OpenAI Compatible Provider").
		WithHideFunc(func() bool { return cfg.Provider.Default != "openai" })

	zaiKeyGroup := huh.NewGroup(
		huh.NewInput().
			Title("Z.ai API Key").
			Value(&cfg.Provider.Zai.APIKey).
			EchoMode(huh.EchoModePassword),
	).Title("Authentication").
		WithHideFunc(func() bool { return cfg.Provider.Default != "zai" })

	anthropicModelGroup := huh.NewGroup(
		huh.NewInput().
			Title("Model").
			Placeholder(defaultModelPlaceholder("anthropic")).
			Value(&cfg.Provider.Model),
	).Title("Model").
		WithHideFunc(func() bool { return cfg.Provider.Default != "anthropic" })

	openaiModelGroup := huh.NewGroup(
		huh.NewInput().
			Title("Model").
			Placeholder(defaultModelPlaceholder("openai")).
			Value(&cfg.Provider.Model),
	).Title("Model").
		WithHideFunc(func() bool { return cfg.Provider.Default != "openai" })

	zaiModelGroup := huh.NewGroup(
		huh.NewInput().
			Title("Model").
			Placeholder(defaultModelPlaceholder("zai")).
			Value(&cfg.Provider.Model),
	).Title("Model").
		WithHideFunc(func() bool { return cfg.Provider.Default != "zai" })

	bf.form = huh.NewForm(providerGroup, anthropicKeyGroup, openaiGroup, zaiKeyGroup, anthropicModelGroup, openaiModelGroup, zaiModelGroup)
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
	if b.cfg.Provider.Default == "anthropic" {
		switch {
		case b.cfg.Provider.Anthropic.APIKey != "":
			b.cfg.Provider.Anthropic.APIKeySource = "config"
		case b.origAnthropicAPIKeySource == "config":
			b.cfg.Provider.Anthropic.APIKeySource = "env"
		}
	}
	if b.cfg.Provider.Default == "zai" {
		switch {
		case b.cfg.Provider.Zai.APIKey != "":
			b.cfg.Provider.Zai.APIKeySource = "config"
		case b.origZaiAPIKeySource == "config":
			b.cfg.Provider.Zai.APIKeySource = "env"
		}
	}
	if b.cfg.Provider.Default == "openai" && b.openaiKey != "" {
		baseURL := b.openaiBaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		b.cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
			{Name: "openai", BaseURL: baseURL, APIKey: b.openaiKey, APIKeySource: "config"},
		}
	}
	return config.Save(b.savePath, b.cfg)
}

// IsCompleted returns true if the form has been completed (submitted).
func (b *BootstrapForm) IsCompleted() bool { return b.form.State == huh.StateCompleted }

// IsAborted returns true if the form has been aborted (cancelled).
func (b *BootstrapForm) IsAborted() bool { return b.form.State == huh.StateAborted }
