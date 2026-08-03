# TUI Provider Config UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make three TUI provider-config surfaces (bootstrap wizard, `/config` overlay, `/model` picker) actually aware of which provider is selected, consuming the `internal/provider` `Registry` API instead of hardcoded/generic values — and fix a real bug along the way where `/config`'s API key field always writes to Anthropic's config regardless of the selected provider.

**Architecture:** Bootstrap's model field splits into three provider-gated `huh` groups (mirroring its existing auth-group pattern). `/config` is rebuilt to the same per-provider group shape. `/model`'s picker splits into an async, `Registry.ListModels`-backed flow for Ollama (new `ModelsFetchedMsg`/`StateFetchingModels`, mirroring the existing `wikiDoneMsg` async-result pattern) and a new free-text overlay for every other provider — both converging on the existing `ModelPickerResult` type, so `processOverlayResult` needs no changes.

**Tech Stack:** Go, `charmbracelet/huh` (forms), `charmbracelet/bubbletea` (TUI event loop), `internal/provider` (Registry), `testify` (assert/require), `internal/testutil.NewServer` (hermetic in-memory HTTP for the Ollama fetch test).

## Global Constraints

- TDD strictly: one test at a time, Red → Green → Refactor → Commit. Never write implementation before the test.
- Commit prefixes: `[STRUCTURAL]` (no behavior change) or `[BEHAVIORAL]` (new/changed behavior). Never mix both in one commit.
- Run `go build ./...`, `go test ./...`, `gofmt -l .`, `go vet ./...` after every task; all must be clean before moving on.
- No change to `internal/provider` — this plan consumes the existing `Registry`/`ProviderDef` API (`Registry.ListModels`, `ProviderDef.DefaultModel`) as-is (spec, Scope section).
- No live model listing added for Anthropic, Z.ai, or OpenAI-compatible — `Registry.ListModels` only has a real implementation for Ollama; their model fields stay free text (spec, Scope section).
- `/model` picker selections stay session-only (`agent.SetModel(name)`, no `config.Save`) — unchanged, existing behavior (spec, Scope section).
- Tests exercise real behavior — real `huh.Form`/`tea.Model` interaction, real HTTP via `testutil.NewServer` where network is involved — not mocks (spec, Testing section; matches this repo's established convention throughout `internal/tui` and `internal/provider`).

---

## Task 1: Bootstrap wizard — per-provider model groups

**Files:**
- Modify: `internal/tui/bootstrap.go:81-89` (replace the single `modelGroup` with three per-provider groups)
- Modify: `internal/tui/bootstrap_test.go` (add placeholder-mapping tests)

**Interfaces:**
- Produces: `defaultModelPlaceholder(providerName string) string` — pure function, unexported, package `tui`. Returns `"claude-sonnet-4-5"` for `"anthropic"`, `"glm-5"` for `"zai"`, `"gpt-4o"` for anything else (including `"openai"`).
- Consumes: nothing new — `huh.NewGroup`, `huh.NewInput`, `WithHideFunc` (already used elsewhere in this file).

**Why a separate function instead of inlining three literals:** `huh.Input.Placeholder(string)` bakes its argument in at construction time with no way to re-read it later, and this codebase's existing tests for `WithHideFunc`-gated content (see `bootstrap_test.go`, `configform_test.go`) never drive `huh.Form` navigation to inspect rendered/hidden-group content — they only test data through `Save()`. A placeholder has no data to assert on through `Save()`, so the provider→placeholder mapping needs to be extracted into a plain function to stay testable without inventing new, fragile navigation-driving test infrastructure this codebase doesn't otherwise use.

- [ ] **Step 1: Write the failing test for the placeholder mapping**

Add to `internal/tui/bootstrap_test.go` (package `tui`, already has the needed imports):

```go
func TestDefaultModelPlaceholder(t *testing.T) {
	assert.Equal(t, "claude-sonnet-4-5", defaultModelPlaceholder("anthropic"))
	assert.Equal(t, "glm-5", defaultModelPlaceholder("zai"))
	assert.Equal(t, "gpt-4o", defaultModelPlaceholder("openai"))
	assert.Equal(t, "gpt-4o", defaultModelPlaceholder("some-custom-openai-compatible-name"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestDefaultModelPlaceholder -v`
Expected: FAIL — `undefined: defaultModelPlaceholder`.

- [ ] **Step 3: Add `defaultModelPlaceholder` and replace the model group**

In `internal/tui/bootstrap.go`, add this function (e.g. just above `NewBootstrapForm`):

```go
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
```

Replace, in `NewBootstrapForm` (currently lines 81-89):

```go
	modelGroup := huh.NewGroup(
		huh.NewInput().
			Title("Model").
			Placeholder("claude-sonnet-4-5").
			Value(&cfg.Provider.Model),
	).Title("Model").
		WithHideFunc(func() bool { return cfg.Provider.Default == "ollama" })

	bf.form = huh.NewForm(providerGroup, anthropicKeyGroup, openaiGroup, zaiKeyGroup, modelGroup)
	return bf
}
```

with:

```go
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
```

(Ollama gets no model group at all, same as before — its absence from the `huh.NewForm(...)` group list is the only "hide" it needs, since there's no `WithHideFunc(provider == "ollama")` group to skip in the first place.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/... -run TestDefaultModelPlaceholder -v`
Expected: PASS

- [ ] **Step 5: Run the full bootstrap test file (regression check)**

Run: `go test ./internal/tui/... -run TestBootstrap -v` and `go test ./internal/tui/... -run TestNeedsBootstrap -v`
Expected: all PASS unchanged — every existing test sets `cfg.Provider.Default`/key fields directly and calls `Save()` or `NeedsBootstrap()`, neither of which depends on which `huh` group is currently displayed, so none of this task's changes should affect their outcomes.

- [ ] **Step 6: Format, vet, commit**

Run: `gofmt -l internal/tui/bootstrap.go internal/tui/bootstrap_test.go` (expect no output) and `go vet ./internal/tui/...` (expect no issues).

```bash
git add internal/tui/bootstrap.go internal/tui/bootstrap_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Give bootstrap's model field a provider-specific placeholder

Splits the single Model group into three WithHideFunc-gated groups (one
per provider that has a model field — Ollama still has none), each with
that provider's own default-model placeholder instead of one hardcoded
"claude-sonnet-4-5" shown regardless of what was just selected. The
provider->placeholder mapping is extracted into defaultModelPlaceholder()
so it stays unit-testable without driving huh's group navigation, which
this codebase's existing tests never do.
EOF
)"
```

---

## Task 2: `/config` overlay rebuild — per-provider groups, fixes the API-key bug

**Files:**
- Modify: `internal/tui/configform.go` (rebuild `NewConfigForm` and `Save`)
- Modify: `internal/tui/configform_test.go` (add per-provider field-binding regression tests)

**Interfaces:**
- Consumes: `config.OpenAICompatibleConfig{Name, BaseURL, APIKeySource, APIKey, ExtraHeaders string/map}` (existing, `internal/config/config.go:219-225`); `config.ZaiProviderConfig{APIKeySource, APIKey, BaseURL, Model string}` (existing, `:233-238`); `config.OllamaProviderConfig{BaseURL string}` (existing, `:228-230`).
- Produces: no new exported symbols — `ConfigForm`/`ConfigOverlay`'s existing public methods (`Form()`, `SetForm()`, `Save()`, `IsCompleted()`, `IsAborted()`, `GroupCount()`) keep their signatures.

This is the task that fixes the actual bug: today, `NewConfigForm`'s provider group unconditionally binds an API-key input to `cfg.Provider.Anthropic.APIKey`, so selecting Z.ai or OpenAI-compatible and typing a key silently writes it to the wrong struct field.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/configform_test.go` (package `tui`, already imports `config`, `assert`, `require`, `filepath`):

```go
func TestConfigFormSaveWritesAnthropicKeyToAnthropicField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Anthropic.APIKey = "sk-ant-test"

	form := NewConfigForm(cfg, path)
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-test", loaded.Provider.Anthropic.APIKey)
	assert.Equal(t, "config", loaded.Provider.Anthropic.APIKeySource)
}

func TestConfigFormSaveWritesZaiKeyToZaiFieldNotAnthropic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "zai"
	cfg.Provider.Zai.APIKey = "zai-test-key"

	form := NewConfigForm(cfg, path)
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "zai-test-key", loaded.Provider.Zai.APIKey)
	assert.Equal(t, "config", loaded.Provider.Zai.APIKeySource)
	assert.Empty(t, loaded.Provider.Anthropic.APIKey, "zai key must not leak into the anthropic field")
}

func TestConfigFormSaveZaiBaseURLOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "zai"
	cfg.Provider.Zai.BaseURL = "https://custom.zai.example.com"

	form := NewConfigForm(cfg, path)
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "https://custom.zai.example.com", loaded.Provider.Zai.BaseURL)
}

func TestConfigFormSaveOllamaBaseURLOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "ollama"
	cfg.Provider.Ollama.BaseURL = "http://custom-ollama:1234"

	form := NewConfigForm(cfg, path)
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "http://custom-ollama:1234", loaded.Provider.Ollama.BaseURL)
}

func TestConfigFormSaveOpenAINewEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"

	form := NewConfigForm(cfg, path)
	form.openaiKey = "sk-openai-test"
	form.openaiBaseURL = "https://openrouter.ai/api/v1"
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Provider.OpenAI, 1)
	assert.Equal(t, "openai", loaded.Provider.OpenAI[0].Name)
	assert.Equal(t, "https://openrouter.ai/api/v1", loaded.Provider.OpenAI[0].BaseURL)
	assert.Equal(t, "sk-openai-test", loaded.Provider.OpenAI[0].APIKey)
}

func TestConfigFormSaveOpenAIUpdatesExistingEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"
	// Simulate editing a config that already has an "openai" entry (unlike
	// bootstrap, which always starts from an empty slice) — /config must
	// update it in place, not append a duplicate.
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "sk-old"},
		{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: "sk-other"},
	}

	form := NewConfigForm(cfg, path)
	form.openaiKey = "sk-new"
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Provider.OpenAI, 2, "must update the existing openai entry, not append a duplicate")
	assert.Equal(t, "sk-new", loaded.Provider.OpenAI[0].APIKey)
	assert.Equal(t, "sk-other", loaded.Provider.OpenAI[1].APIKey, "the unrelated openrouter entry must be untouched")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestConfigFormSave -v`
Expected: FAIL. `TestConfigFormSaveWritesZaiKeyToZaiFieldNotAnthropic` and `TestConfigFormSaveZaiBaseURLOverride`/`TestConfigFormSaveOllamaBaseURLOverride` fail because the current form has no Zai/Ollama base-URL fields and the Zai key never gets to `cfg.Provider.Zai.APIKey` (the current form binds the key input to `cfg.Provider.Anthropic.APIKey` unconditionally — so `loaded.Provider.Zai.APIKey` stays empty). `TestConfigFormSaveOpenAINewEntry`/`TestConfigFormSaveOpenAIUpdatesExistingEntry` fail to compile — `form.openaiKey`/`form.openaiBaseURL` don't exist on the current `ConfigForm` struct.

- [ ] **Step 3: Rebuild `ConfigForm`**

Replace the entire contents of `internal/tui/configform.go` with:

```go
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
	if c.cfg.Provider.Default == "openai" && c.openaiKey != "" {
		baseURL := c.openaiBaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		entry := config.OpenAICompatibleConfig{Name: "openai", BaseURL: baseURL, APIKey: c.openaiKey, APIKeySource: "config"}
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
```

Note: `findOpenAICompatibleEntry` pre-fills `openaiKey`/`openaiBaseURL` from any existing `"openai"`-named entry when the form is constructed, so re-opening `/config` on an already-configured OpenAI-compatible setup shows the current values instead of blank fields — this wasn't in the spec's Component 2 description explicitly but is necessary for the form to actually be an *editor* (its stated purpose) rather than a write-only form for that one field, and follows directly from `GroupCount` needing to change anyway (3 → 8, once for every actual group now). `TestConfigFormGroupCount` (existing test, `configform_test.go:20-24`) needs updating in Step 3's companion edit — see Step 5.

- [ ] **Step 4: Run the new tests to verify they pass**

Run: `go test ./internal/tui/... -run TestConfigFormSave -v`
Expected: PASS

- [ ] **Step 5: Update `TestConfigFormGroupCount` for the new group count**

In `internal/tui/configform_test.go`, change:

```go
func TestConfigFormGroupCount(t *testing.T) {
	cfg := config.DefaultConfig()
	form := NewConfigForm(cfg, "/tmp/test-config.toml")
	assert.Equal(t, 3, form.GroupCount())
}
```

to:

```go
func TestConfigFormGroupCount(t *testing.T) {
	cfg := config.DefaultConfig()
	form := NewConfigForm(cfg, "/tmp/test-config.toml")
	assert.Equal(t, 8, form.GroupCount())
}
```

- [ ] **Step 6: Run the full configform test file (regression check)**

Run: `go test ./internal/tui/... -run TestConfigForm -v`
Expected: all PASS, including the pre-existing `TestConfigFormCreation`, `TestConfigFormIsCompletedAborted`, and the pre-existing `TestConfigFormSave` (the `for _, provider := range []string{"ollama", "zai"}` loop test) — none of these depend on group count or field bindings beyond `cfg.Provider.Default`, which is unaffected by this task's changes.

- [ ] **Step 7: Format, vet, commit**

Run: `gofmt -l internal/tui/configform.go internal/tui/configform_test.go` and `go vet ./internal/tui/...`.

```bash
git add internal/tui/configform.go internal/tui/configform_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Rebuild /config with per-provider groups; fix the API-key bug

The API Key field was unconditionally bound to cfg.Provider.Anthropic.APIKey
regardless of the selected provider — selecting Z.ai or OpenAI-compatible and
typing a key silently wrote it to the wrong config struct field. Rebuilt to
mirror bootstrap.go's WithHideFunc per-provider group pattern: each
provider's key/base-URL fields now bind to that provider's own config
struct. Also adds Z.ai and Ollama base-URL fields /config never had, and
pre-fills the OpenAI-compatible staging fields from any existing "openai"
entry so re-opening the form on an already-configured setup shows current
values instead of blanks.
EOF
)"
```

---

## Task 3: `ModelTextInputOverlay` — the `/model` fallback for non-Ollama providers

**Files:**
- Create: `internal/tui/modeltextinput.go`
- Create: `internal/tui/modeltextinput_test.go`

**Interfaces:**
- Consumes: `Overlay` interface (existing, `internal/tui/overlay.go:15-20`: `Update(tea.Msg) (Overlay, tea.Cmd)`, `View() string`, `Done() bool`, `Result() any`); `ModelPickerResult{ModelName string}` (existing, `internal/tui/modelpicker.go:100-102`).
- Produces: `ModelTextInputOverlay` type; `NewModelTextInputOverlay(currentModel string) (*ModelTextInputOverlay, tea.Cmd)`.

This task is fully self-contained — no other file changes yet. Task 5 wires it into `ActionOpenModelPicker`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/modeltextinput_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelTextInputOverlayImplementsOverlay(t *testing.T) {
	overlay, _ := NewModelTextInputOverlay("claude-sonnet-4-5")
	var _ Overlay = overlay
}

func TestModelTextInputOverlayPrefillsCurrentModel(t *testing.T) {
	overlay, _ := NewModelTextInputOverlay("claude-sonnet-4-5")
	view := overlay.View()
	assert.Contains(t, view, "claude-sonnet-4-5")
}

func TestModelTextInputOverlayNotDoneUntilSubmitted(t *testing.T) {
	overlay, _ := NewModelTextInputOverlay("claude-sonnet-4-5")
	assert.False(t, overlay.Done())
	assert.Nil(t, overlay.Result())
}

func TestModelTextInputOverlaySubmitProducesModelPickerResult(t *testing.T) {
	overlay, initCmd := NewModelTextInputOverlay("claude-sonnet-4-5")
	if initCmd != nil {
		initCmd()
	}

	// Simulate typing over the prefilled field, then submitting. Uses
	// assert.Contains below rather than Equal, since this doesn't assume
	// any specific clear/select-all keybinding for huh.Input (it wraps
	// bubbles/textinput, whose edit shortcuts aren't this test's concern) —
	// only that typed characters end up in the submitted result.
	var updated Overlay
	for _, r := range "claude-opus-4-8" {
		updated, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		overlay = updated.(*ModelTextInputOverlay)
	}
	updated, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyEnter})
	overlay = updated.(*ModelTextInputOverlay)

	require.True(t, overlay.Done())
	result := overlay.Result()
	require.NotNil(t, result)
	picked, ok := result.(ModelPickerResult)
	require.True(t, ok)
	assert.Contains(t, picked.ModelName, "claude-opus-4-8")
}

func TestModelTextInputOverlayAbortProducesNilResult(t *testing.T) {
	overlay, initCmd := NewModelTextInputOverlay("claude-sonnet-4-5")
	if initCmd != nil {
		initCmd()
	}

	updated, _ := overlay.Update(tea.KeyMsg{Type: tea.KeyEsc})
	overlay = updated.(*ModelTextInputOverlay)

	require.True(t, overlay.Done())
	assert.Nil(t, overlay.Result())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestModelTextInputOverlay -v`
Expected: FAIL — `undefined: NewModelTextInputOverlay` (compile error; the file doesn't exist yet).

- [ ] **Step 3: Create `modeltextinput.go`**

Create `internal/tui/modeltextinput.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// ModelTextInputOverlay is the /model picker's fallback for providers with
// no live listing capability — everything except Ollama, which has a real
// selection-list picker (ModelPickerOverlay) backed by Registry.ListModels.
// A single free-text field pre-filled with the current model, converging
// on the same ModelPickerResult every ModelPicker selection produces, so
// processOverlayResult needs no per-overlay-type branching for this case.
type ModelTextInputOverlay struct {
	form  *huh.Form
	value string
}

// NewModelTextInputOverlay creates the overlay pre-filled with currentModel
// and returns its init command.
func NewModelTextInputOverlay(currentModel string) (*ModelTextInputOverlay, tea.Cmd) {
	o := &ModelTextInputOverlay{value: currentModel}
	input := huh.NewInput().Title("Model name").Value(&o.value)
	o.form = huh.NewForm(huh.NewGroup(input))
	return o, o.form.Init()
}

// Update forwards the message to the underlying huh.Form.
func (o *ModelTextInputOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	form, cmd := o.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		o.form = f
	}
	return o, cmd
}

// View renders the input form.
func (o *ModelTextInputOverlay) View() string {
	return o.form.View()
}

// Done returns true when the form has been submitted or cancelled.
func (o *ModelTextInputOverlay) Done() bool {
	return o.form.State == huh.StateCompleted || o.form.State == huh.StateAborted
}

// Result returns a ModelPickerResult when a non-empty model name was
// submitted, nil otherwise (cancelled, or submitted empty).
func (o *ModelTextInputOverlay) Result() any {
	if o.form.State == huh.StateCompleted && o.value != "" {
		return ModelPickerResult{ModelName: o.value}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/... -run TestModelTextInputOverlay -v`
Expected: PASS

- [ ] **Step 5: Run the full `internal/tui` package (regression check)**

Run: `go test ./internal/tui/...`
Expected: all PASS unchanged — this task adds two new files and touches nothing existing.

- [ ] **Step 6: Format, vet, commit**

Run: `gofmt -l internal/tui/modeltextinput.go internal/tui/modeltextinput_test.go` and `go vet ./internal/tui/...`.

```bash
git add internal/tui/modeltextinput.go internal/tui/modeltextinput_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Add ModelTextInputOverlay, the /model fallback for non-Ollama providers

Not wired in yet (Task 5 does that) — self-contained addition. Converges
on the existing ModelPickerResult type so processOverlayResult needs no
changes to handle it.
EOF
)"
```

---

## Task 4: Async Ollama model fetch — `ModelsFetchedMsg`, `StateFetchingModels`

**Files:**
- Modify: `internal/tui/model.go` (add `StateFetchingModels` to the state enum, line ~82-83)
- Modify: `internal/tui/modelpicker.go` (add `ModelsFetchedMsg` type and `fetchOllamaModels` method)
- Modify: `internal/tui/update.go` (add `case ModelsFetchedMsg:` to `Update`'s main switch; extend the `spinner.TickMsg` case's state gate)
- Modify: `internal/tui/view.go` (add `case StateFetchingModels:` to both `View()` switches — plain mode and full mode)
- Create: `internal/tui/modelsfetched_test.go`

**Interfaces:**
- Consumes: `provider.Registry.ListModels(ctx context.Context, providerID string, cfg *config.Config) ([]provider.Model, error)` (existing, `internal/provider/registry.go:158`); `provider.Default *provider.Registry` (existing package var); `provider.Model{ID, Name string}` (existing, `internal/provider/registry.go:32-35`); `NewModelPickerOverlay([]ModelChoice) (*ModelPickerOverlay, tea.Cmd)` (existing, `internal/tui/modelpicker.go:110`).
- Produces: `ModelsFetchedMsg{Models []provider.Model, Err error}`; `(m *Model) fetchOllamaModels() tea.Cmd`; `StateFetchingModels` (new `UIState` constant).

Not wired into `ActionOpenModelPicker` yet — Task 5 does that. This task is testable on its own by constructing `ModelsFetchedMsg` directly and feeding it through `Update()`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/modelsfetched_test.go`:

```go
package tui

import (
	"net/http"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	_ "github.com/julianshen/rubichan/internal/provider/ollama"
	"github.com/julianshen/rubichan/internal/testutil"
)

func TestModelsFetchedMsgSuccessOpensPicker(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", config.DefaultConfig(), nil)
	m.state = StateFetchingModels

	updated, cmd := m.Update(ModelsFetchedMsg{
		Models: []provider.Model{{ID: "llama3.2:latest", Name: "llama3.2:latest"}},
	})
	m = updated.(*Model)

	assert.Equal(t, StateModelPickerOverlay, m.state)
	require.NotNil(t, m.activeOverlay)
	assert.NotNil(t, cmd) // huh form Init returns a command
}

func TestModelsFetchedMsgEmptyListShowsMessage(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", config.DefaultConfig(), nil)
	m.state = StateFetchingModels

	before := m.content.String()
	updated, cmd := m.Update(ModelsFetchedMsg{Models: nil})
	m = updated.(*Model)

	assert.Equal(t, StateInput, m.state)
	assert.Nil(t, m.activeOverlay)
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String()[len(before):], "No models available")
}

func TestModelsFetchedMsgErrorShowsMessage(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", config.DefaultConfig(), nil)
	m.state = StateFetchingModels

	before := m.content.String()
	updated, cmd := m.Update(ModelsFetchedMsg{Err: assert.AnError})
	m = updated.(*Model)

	assert.Equal(t, StateInput, m.state)
	assert.Nil(t, m.activeOverlay)
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String()[len(before):], "Failed to list Ollama models")
}

func TestFetchOllamaModelsQueriesTheConfiguredServer(t *testing.T) {
	srv := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models": [{"name": "llama3.2:latest", "size": 1}]}`))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.Ollama.BaseURL = srv.URL

	m := NewModel(nil, "test", "model", 10, "", cfg, nil)
	cmd := m.fetchOllamaModels()
	require.NotNil(t, cmd)

	msg := cmd()
	fetched, ok := msg.(ModelsFetchedMsg)
	require.True(t, ok)
	require.NoError(t, fetched.Err)
	require.Len(t, fetched.Models, 1)
	assert.Equal(t, "llama3.2:latest", fetched.Models[0].ID)
}

func TestSpinnerTicksDuringFetchingModels(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", config.DefaultConfig(), nil)
	m.state = StateFetchingModels

	updated, cmd := m.Update(m.spinner.Tick())
	m = updated.(*Model)
	assert.NotNil(t, cmd, "spinner must keep ticking while fetching models, or the animation freezes")
	_ = m
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run 'TestModelsFetchedMsg|TestFetchOllamaModels|TestSpinnerTicksDuringFetchingModels' -v`
Expected: FAIL — `undefined: ModelsFetchedMsg`, `undefined: StateFetchingModels`, `m.fetchOllamaModels undefined` (compile errors).

- [ ] **Step 3: Add `StateFetchingModels` to the state enum**

In `internal/tui/model.go`, the state enum block (currently lines 60-83), replace:

```go
	// StateModelPickerOverlay indicates the TUI is showing the model picker.
	StateModelPickerOverlay
)
```

with:

```go
	// StateModelPickerOverlay indicates the TUI is showing the model picker.
	StateModelPickerOverlay
	// StateFetchingModels indicates the TUI is querying a provider (Ollama)
	// for its available models before opening the picker.
	StateFetchingModels
)
```

- [ ] **Step 4: Add `ModelsFetchedMsg` and `fetchOllamaModels` to `modelpicker.go`**

In `internal/tui/modelpicker.go`, add `"context"` and `"github.com/julianshen/rubichan/internal/provider"` to the import block, and append at the end of the file:

```go
// ModelsFetchedMsg carries the result of an async Registry.ListModels call,
// triggered when the model picker is opened for a provider that supports
// live listing (currently only Ollama). Handled in Update (update.go).
type ModelsFetchedMsg struct {
	Models []provider.Model
	Err    error
}

// fetchOllamaModels returns a tea.Cmd that queries the Registry in the
// background. Ollama's ListModels makes a real HTTP call to a local
// server — even though it's typically fast, it must never run inline in
// the synchronous command-dispatch path (ActionOpenModelPicker), which
// would block the whole TUI event loop on network I/O if the local
// server were slow or unresponsive. This mirrors the existing async
// tea.Cmd -> tea.Msg pattern used for wiki generation (wiki_command.go's
// wikiDoneMsg).
func (m *Model) fetchOllamaModels() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		models, err := provider.Default.ListModels(context.Background(), "ollama", cfg)
		return ModelsFetchedMsg{Models: models, Err: err}
	}
}
```

- [ ] **Step 5: Add the `Update()` case and extend the spinner-tick gate**

In `internal/tui/update.go`, add this case to the main `switch msg := msg.(type) {` block (currently starting at line 95), placed after the existing `case wikiDoneMsg:` block (currently ending at line 203, right before `case spinner.TickMsg:`):

```go
	case ModelsFetchedMsg:
		if msg.Err != nil {
			m.content.WriteString(fmt.Sprintf("Failed to list Ollama models: %s\n", msg.Err))
			m.setContentAndAutoScroll()
			m.state = StateInput
			return m, nil
		}
		if len(msg.Models) == 0 {
			m.content.WriteString("No models available.\n")
			m.setContentAndAutoScroll()
			m.state = StateInput
			return m, nil
		}
		choices := make([]ModelChoice, len(msg.Models))
		for i, mo := range msg.Models {
			choices[i] = ModelChoice{Name: mo.ID}
		}
		overlay, initCmd := NewModelPickerOverlay(choices)
		m.activeOverlay = overlay
		m.state = StateModelPickerOverlay
		return m, initCmd
```

Then extend the `spinner.TickMsg` case's gate (currently, `internal/tui/update.go:205-211`) from:

```go
	case spinner.TickMsg:
		if m.state == StateStreaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
```

to:

```go
	case spinner.TickMsg:
		if m.state == StateStreaming || m.state == StateFetchingModels {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
```

Without this second change, the spinner would render one static frame during `StateFetchingModels` and never advance — this gate is the only place `spinner.TickMsg` is consumed, and it's currently `StateStreaming`-only.

- [ ] **Step 6: Add `StateFetchingModels` rendering to `view.go`**

In `internal/tui/view.go`, in the plain-mode switch (currently lines 36-57), add a case after `StateStreaming` (before `case StateAwaitingApproval:`):

```go
		case StateFetchingModels:
			b.WriteString("Fetching models...")
```

In the full-mode switch (currently lines 98-119), add a case after `StateStreaming` (before `case StateAwaitingApproval:`):

```go
	case StateFetchingModels:
		b.WriteString(fmt.Sprintf("%s %s", m.spinner.View(), styleSpinner.Render("Fetching models...")))
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/tui/... -run 'TestModelsFetchedMsg|TestFetchOllamaModels|TestSpinnerTicksDuringFetchingModels' -v`
Expected: PASS

- [ ] **Step 8: Run the full `internal/tui` package and `internal/provider/ollama` (regression check)**

Run: `go test ./internal/tui/... ./internal/provider/...`
Expected: all PASS unchanged — this task adds a new state, a new message type, and new switch cases; nothing existing is removed or altered.

- [ ] **Step 9: Format, vet, commit**

Run: `gofmt -l internal/tui/model.go internal/tui/modelpicker.go internal/tui/update.go internal/tui/view.go internal/tui/modelsfetched_test.go` and `go vet ./internal/tui/...`.

```bash
git add internal/tui/model.go internal/tui/modelpicker.go internal/tui/update.go internal/tui/view.go internal/tui/modelsfetched_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Add async Ollama model fetch (ModelsFetchedMsg, StateFetchingModels)

Registry.ListModels makes a real HTTP call; this adds the async
tea.Cmd -> tea.Msg plumbing (mirroring wiki_command.go's wikiDoneMsg
pattern) so a slow/unresponsive local Ollama server can't block the TUI
event loop. Includes the spinner-tick gate extension StateFetchingModels
needs to actually animate, and both View() render paths (plain/full mode).
Not wired into ActionOpenModelPicker yet — Task 5 does that.
EOF
)"
```

---

## Task 5: Wire `ActionOpenModelPicker`, delete `availableModels()`

**Files:**
- Modify: `internal/tui/model.go` (rewrite the `ActionOpenModelPicker` case at lines 928-938; delete `availableModels()` at lines 944-955)
- Modify: `internal/tui/modelpicker_overlay_test.go` (update `TestModelCommandNoArgsOpensPickerOverlay` for the new nil-`cfg` guard and provider branching)

**Interfaces:**
- Consumes: `(m *Model) fetchOllamaModels() tea.Cmd` (Task 4); `NewModelTextInputOverlay(string) (*ModelTextInputOverlay, tea.Cmd)` (Task 3); `StateFetchingModels` (Task 4).
- Produces: nothing new — this is the wiring task, and the final task in this plan.

**Why `m.cfg == nil` must be checked first:** the existing test `TestModelCommandNoArgsOpensPickerOverlay` (`internal/tui/modelpicker_overlay_test.go:67-77`) constructs `NewModel(nil, "test", "model", 10, "", nil, reg)` — `cfg` is `nil`. The current `ActionOpenModelPicker` handler never touches `cfg` (its model list is hardcoded), so this works today. The new handler reads `m.cfg.Provider.Default` and, on the non-Ollama path, `m.cfg.Provider.Model` — both would nil-pointer-panic on this exact existing test without an explicit guard. `ActionOpenConfig`'s handler (`internal/tui/model.go:872-877`) already has this exact guard for the same reason; this task's guard mirrors it.

- [ ] **Step 1: Update the existing test for the new nil-`cfg` and provider-branching behavior**

In `internal/tui/modelpicker_overlay_test.go`, replace:

```go
func TestModelCommandNoArgsOpensPickerOverlay(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "test", "model", 10, "", nil, reg)

	require.NoError(t, reg.Register(commands.NewModelCommand(func(string) {})))

	cmd := m.handleCommand("/model")
	assert.NotNil(t, cmd) // huh form Init returns a command
	assert.Equal(t, StateModelPickerOverlay, m.state)
	assert.NotNil(t, m.activeOverlay)
}
```

with:

```go
func TestModelCommandNoArgsWithNilConfigShowsMessage(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "test", "model", 10, "", nil, reg)

	require.NoError(t, reg.Register(commands.NewModelCommand(func(string) {})))

	before := m.content.String()
	cmd := m.handleCommand("/model")
	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
	assert.Nil(t, m.activeOverlay)
	assert.Contains(t, m.content.String()[len(before):], "No config available")
}

func TestModelCommandNoArgsOpensTextInputForNonOllama(t *testing.T) {
	reg := commands.NewRegistry()
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"
	m := NewModel(nil, "test", "model", 10, "", cfg, reg)

	require.NoError(t, reg.Register(commands.NewModelCommand(func(string) {})))

	cmd := m.handleCommand("/model")
	assert.NotNil(t, cmd) // huh form Init returns a command
	assert.Equal(t, StateModelPickerOverlay, m.state)
	require.NotNil(t, m.activeOverlay)
	_, ok := m.activeOverlay.(*ModelTextInputOverlay)
	assert.True(t, ok, "non-ollama providers get the text-input overlay, not the selection-list picker")
}

func TestModelCommandNoArgsFetchesForOllama(t *testing.T) {
	reg := commands.NewRegistry()
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "ollama"
	m := NewModel(nil, "test", "model", 10, "", cfg, reg)

	require.NoError(t, reg.Register(commands.NewModelCommand(func(string) {})))

	cmd := m.handleCommand("/model")
	assert.NotNil(t, cmd) // fetchOllamaModels + spinner.Tick, batched
	assert.Equal(t, StateFetchingModels, m.state)
	assert.Nil(t, m.activeOverlay, "overlay doesn't open until ModelsFetchedMsg arrives")
}
```

Add `"github.com/julianshen/rubichan/internal/config"` to this test file's import block if not already present (check first — the file currently imports `testing`, `assert`, `require`, `commands`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestModelCommand -v`
Expected: FAIL — `TestModelCommandNoArgsWithNilConfigShowsMessage` fails because the handler doesn't check `m.cfg == nil` yet (nil-pointer panic, or wrong assertions against current behavior); `TestModelCommandNoArgsOpensTextInputForNonOllama`/`TestModelCommandNoArgsFetchesForOllama` fail because `ActionOpenModelPicker` still calls the old `availableModels()` path.

- [ ] **Step 3: Rewrite `ActionOpenModelPicker` and delete `availableModels()`**

In `internal/tui/model.go`, replace (currently lines 928-955):

```go
	case commands.ActionOpenModelPicker:
		models := m.availableModels()
		if len(models) == 0 {
			m.content.WriteString("No models available.\n")
			m.setContentAndAutoScroll()
			return nil
		}
		overlay, initCmd := NewModelPickerOverlay(models)
		m.activeOverlay = overlay
		m.state = StateModelPickerOverlay
		return initCmd
	}

	return nil
}

// availableModels returns the model choices for the picker overlay.
func (m *Model) availableModels() []ModelChoice {
	// Provide a basic set of well-known models. In the future this
	// could query the provider for available models.
	return []ModelChoice{
		{Name: "claude-opus-4-6", Size: "large"},
		{Name: "claude-sonnet-4-6", Size: "medium"},
		{Name: "claude-haiku-4-5-20251001", Size: "small"},
		{Name: "gpt-4o", Size: "large"},
		{Name: "gpt-4o-mini", Size: "small"},
	}
}
```

with:

```go
	case commands.ActionOpenModelPicker:
		if m.cfg == nil {
			m.content.WriteString("No config available\n")
			m.setContentAndAutoScroll()
			return nil
		}
		if m.cfg.Provider.Default == "ollama" {
			m.state = StateFetchingModels
			return tea.Batch(m.fetchOllamaModels(), m.spinner.Tick)
		}
		overlay, initCmd := NewModelTextInputOverlay(m.cfg.Provider.Model)
		m.activeOverlay = overlay
		m.state = StateModelPickerOverlay
		return initCmd
	}

	return nil
}
```

This function's containing switch (`handleCommandParts`) already returns `tea.Cmd` and already imports `tea` and `commands` — no import changes needed in `model.go` for this step. Note the return type here is a single `tea.Cmd`, not `(tea.Model, tea.Cmd)` — matching every other case in this switch (`handleCommandParts` returns one `tea.Cmd`, not a `tea.Model` pair — its caller wraps it), unlike Task 4's `Update()` case which returns both.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/... -run TestModelCommand -v`
Expected: PASS

- [ ] **Step 5: Run the full `internal/tui` package (regression check)**

Run: `go test ./internal/tui/...`
Expected: all PASS, including every other `TestModelPickerOverlay*`/`TestProcessOverlayResultModelPicker*` test in `modelpicker_overlay_test.go` (unmodified by this task — they exercise `NewModelPickerOverlay` and `processOverlayResult` directly, neither of which changed) and every test from Tasks 1-4.

- [ ] **Step 6: Format, vet, commit**

Run: `gofmt -l internal/tui/model.go internal/tui/modelpicker_overlay_test.go` and `go vet ./internal/tui/...`.

```bash
git add internal/tui/model.go internal/tui/modelpicker_overlay_test.go
git commit -m "$(cat <<'EOF'
[BEHAVIORAL] Wire ActionOpenModelPicker to the provider-aware /model flow

Ollama triggers the async Registry.ListModels fetch (Task 4); every other
provider opens ModelTextInputOverlay (Task 3) pre-filled with the current
model, replacing the hardcoded static list that mixed Claude and GPT
names together regardless of the selected provider. availableModels() is
deleted, fully superseded. Guards m.cfg == nil first, matching
ActionOpenConfig's existing guard — the pre-existing test that
constructed a Model with a nil config exercised this exact path.
EOF
)"
```

---

## Final verification (after Task 5)

- [ ] Run: `go build ./...` — expect success.
- [ ] Run: `gofmt -l .` — expect no output.
- [ ] Run: `go vet ./...` — expect no issues.
- [ ] Run: `go test ./internal/tui/... ./internal/provider/...` — expect all passing.
- [ ] Run: `grep -rn "availableModels" internal/tui/` — expect zero matches (fully removed, including its old tests if any referenced it directly — none did, per the spec's testing section covering only the new `ActionOpenModelPicker` dispatch behavior).
- [ ] Manually confirm (via `/config`, `/model`, and a fresh bootstrap run in a real terminal — this plan's tests cover data/state correctness, not visual rendering) that: switching providers in `/config` shows the right auth fields; typing a Z.ai key and saving doesn't touch the Anthropic field in the saved `config.toml`; `/model` on a non-Ollama provider opens a text field pre-filled with the current model; `/model` on Ollama (with a real local Ollama server running) shows a brief "Fetching models..." spinner then the real installed-model list.
