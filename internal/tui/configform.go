package tui

import (
	"fmt"
	"log"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/julianshen/rubichan/internal/config"
)

// ConfigForm wraps a Huh form for editing rubichan configuration.
type ConfigForm struct {
	form          *huh.Form
	cfg           *config.Config
	savePath      string
	maxTurnsStr   string
	openaiKey     string // staging field, mirrors BootstrapForm; copied into cfg.Provider.OpenAI on Save
	openaiBaseURL string // staging field, mirrors BootstrapForm
}

// NewConfigForm creates a config editor form populated from the given config.
func NewConfigForm(cfg *config.Config, savePath string) *ConfigForm {
	cf := &ConfigForm{
		cfg:         cfg,
		savePath:    savePath,
		maxTurnsStr: fmt.Sprintf("%d", cfg.Agent.MaxTurns),
	}

	if oc, ok := findOpenAICompatibleEntry(cfg, "openai"); ok {
		cf.openaiKey = oc.APIKey
		cf.openaiBaseURL = oc.BaseURL
	}

	providerGroup := huh.NewGroup(
		huh.NewSelect[string]().
			Title("Provider").
			Options(
				huh.NewOption("Anthropic", "anthropic"),
				huh.NewOption("OpenAI Compatible", "openai"),
				huh.NewOption("Ollama", "ollama"),
				huh.NewOption("Z.ai (Zhipu)", "zai"),
			).
			Value(&cfg.Provider.Default),
	).Title("Provider")

	anthropicGroup := huh.NewGroup(
		huh.NewInput().
			Title("API Key").
			Value(&cfg.Provider.Anthropic.APIKey).
			EchoMode(huh.EchoModePassword),
	).Title("Anthropic").
		WithHideFunc(func() bool { return cfg.Provider.Default != "anthropic" })

	openaiGroup := huh.NewGroup(
		huh.NewInput().
			Title("Base URL").
			Description("Leave empty for https://api.openai.com/v1").
			Placeholder("https://api.openai.com/v1").
			Value(&cf.openaiBaseURL),
		huh.NewInput().
			Title("API Key").
			Value(&cf.openaiKey).
			EchoMode(huh.EchoModePassword),
	).Title("OpenAI Compatible Provider").
		WithHideFunc(func() bool { return cfg.Provider.Default != "openai" })

	zaiGroup := huh.NewGroup(
		huh.NewInput().
			Title("API Key").
			Value(&cfg.Provider.Zai.APIKey).
			EchoMode(huh.EchoModePassword),
		huh.NewInput().
			Title("Base URL").
			Description("Leave empty for the default Z.ai endpoint").
			Value(&cfg.Provider.Zai.BaseURL),
	).Title("Z.ai").
		WithHideFunc(func() bool { return cfg.Provider.Default != "zai" })

	ollamaGroup := huh.NewGroup(
		huh.NewInput().
			Title("Base URL").
			Description("Leave empty for http://localhost:11434").
			Value(&cfg.Provider.Ollama.BaseURL),
	).Title("Ollama").
		WithHideFunc(func() bool { return cfg.Provider.Default != "ollama" })

	modelGroup := huh.NewGroup(
		huh.NewInput().
			Title("Model").
			Value(&cfg.Provider.Model),
	).Title("Model")

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

	cf.form = huh.NewForm(providerGroup, anthropicGroup, openaiGroup, zaiGroup, ollamaGroup, modelGroup, agentGroup, securityGroup)

	return cf
}

// GroupCount returns the number of form groups.
func (c *ConfigForm) GroupCount() int { return 8 }

// findOpenAICompatibleEntry returns the entry with the given name and
// whether it was found.
func findOpenAICompatibleEntry(cfg *config.Config, name string) (config.OpenAICompatibleConfig, bool) {
	for _, oc := range cfg.Provider.OpenAI {
		if oc.Name == name {
			return oc, true
		}
	}
	return config.OpenAICompatibleConfig{}, false
}

// Save persists the config to disk. It parses the maxTurns string back to int,
// records APIKeySource="config" for any provider whose key was entered
// through this form, and writes the OpenAI-compatible staging fields into
// cfg.Provider.OpenAI — updating an existing "openai" entry in place rather
// than appending a duplicate, since (unlike bootstrap) /config may be
// editing a config that already has one.
func (c *ConfigForm) Save() error {
	if v, err := strconv.Atoi(c.maxTurnsStr); err == nil {
		c.cfg.Agent.MaxTurns = v
	}

	if c.cfg.Provider.Default == "anthropic" && c.cfg.Provider.Anthropic.APIKey != "" {
		c.cfg.Provider.Anthropic.APIKeySource = "config"
	}
	if c.cfg.Provider.Default == "zai" && c.cfg.Provider.Zai.APIKey != "" {
		c.cfg.Provider.Zai.APIKeySource = "config"
	}
	if c.cfg.Provider.Default == "openai" {
		existing, found := findOpenAICompatibleEntry(c.cfg, "openai")
		baseURL := c.openaiBaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		entry := existing // preserves ExtraHeaders and any other fields this form doesn't expose
		entry.Name = "openai"
		entry.BaseURL = baseURL
		if c.openaiKey != "" {
			entry.APIKey = c.openaiKey
			entry.APIKeySource = "config"
		}
		if found || c.openaiKey != "" || c.openaiBaseURL != "" {
			updated := false
			for i, oc := range c.cfg.Provider.OpenAI {
				if oc.Name == "openai" {
					c.cfg.Provider.OpenAI[i] = entry
					updated = true
					break
				}
			}
			if !updated {
				c.cfg.Provider.OpenAI = append(c.cfg.Provider.OpenAI, entry)
			}
		}
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

// ConfigOverlay wraps ConfigForm as an Overlay.
type ConfigOverlay struct {
	form *ConfigForm
}

// NewConfigOverlay creates a ConfigOverlay and returns its init command.
func NewConfigOverlay(cfg *config.Config, savePath string) (*ConfigOverlay, tea.Cmd) {
	o := &ConfigOverlay{form: NewConfigForm(cfg, savePath)}
	return o, o.form.Form().Init()
}

// Update forwards the message to the underlying huh.Form and handles completion.
func (c *ConfigOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	model, cmd := c.form.Form().Update(msg)
	if f, ok := model.(*huh.Form); ok {
		c.form.SetForm(f)
	}
	if c.form.IsCompleted() {
		if err := c.form.Save(); err != nil {
			log.Printf("failed to save config: %v", err)
		}
	}
	return c, cmd
}

// View renders the config form.
func (c *ConfigOverlay) View() string {
	return c.form.Form().View()
}

// Done returns true when the form has been submitted or cancelled.
func (c *ConfigOverlay) Done() bool {
	return c.form.IsCompleted() || c.form.IsAborted()
}

// Result returns a ConfigResult when completed, nil otherwise.
func (c *ConfigOverlay) Result() any {
	if c.form.IsCompleted() {
		return ConfigResult{}
	}
	return nil
}
