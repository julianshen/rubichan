# TUI Provider Config UX Design

**Status:** Approved for implementation planning
**Date:** 2026-08-01

## Problem

The TUI has three surfaces where the user configures or selects an LLM provider/model, and none of them is actually aware of which provider is selected the way the backend (`internal/provider/registry.go`, merged in #332/#337) now is:

1. **First-run bootstrap wizard** (`internal/tui/bootstrap.go`) — the Model field is a single free-text input with a hardcoded `claude-sonnet-4-5` placeholder regardless of which provider was just selected.
2. **In-session `/config` overlay** (`internal/tui/configform.go`) — a real bug: the API Key input is unconditionally bound to `cfg.Provider.Anthropic.APIKey`, so selecting Z.ai or OpenAI-compatible and typing a key silently writes it to the wrong config field. There's no OpenAI base URL/key or Ollama base URL field at all, and the form doesn't hide/show fields per provider the way bootstrap already does.
3. **In-session `/model` picker** (`internal/tui/modelpicker.go`, `internal/tui/model.go`'s `availableModels()`) — returns a hardcoded static list mixing Claude and GPT model names together, with a comment reading *"In the future this could query the provider for available models."* That capability (`Registry.ListModels`) now exists and is unused.

## Scope

**In scope:** all three surfaces above, TUI-only changes.

**Out of scope:**
- Any change to `internal/provider` — this design consumes the existing `Registry`/`ProviderDef` API (`Registry.ListModels`, `ProviderDef.DefaultModel`) as-is.
- Live model listing for Anthropic, Z.ai, or OpenAI-compatible providers. `Registry.ListModels` only has a real implementation for Ollama today (the other three providers' `ProviderDef.ListModels` is `nil`, by design — none of them expose a listing API). This design does not add curated/static fallback lists for them; their model fields stay free text.
- Persisting `/model` picker selections to `config.toml`. `/model` has always been session-only (`agent.SetModel(name)`, no `config.Save`) — that's existing, deliberate behavior (`/config` is the persistence path), unchanged here.

## Architecture

Three independent surfaces, one shared principle — provider-config UI reflects what's actually selected/available, sourced from the `Registry` instead of hardcoded values:

| Surface | Change |
|---|---|
| Bootstrap wizard | Model field's placeholder becomes provider-specific (was: one hardcoded placeholder for all) |
| `/config` overlay | Full rebuild to mirror bootstrap's per-provider `WithHideFunc` group pattern; fixes the API-key-always-Anthropic bug |
| `/model` picker | Ollama: async fetch via `Registry.ListModels`, replacing the hardcoded list. Every other provider: a new free-text overlay, replacing the (meaningless, since there's nothing to list) selection picker |

No new packages. All changes live in `internal/tui/`.

## Components

### 1. Bootstrap wizard (`internal/tui/bootstrap.go`)

`Placeholder(string)` on a `huh.Input` evaluates its argument once, at construction time — `NewBootstrapForm` builds every group before the user has answered the provider-select group (the `Value(&cfg.Provider.Default)` binding is only written to later, during `Update()` calls). A single `modelGroup` with one placeholder computed from `cfg.Provider.Default` at construction would therefore always reflect the zero-value default provider, never the user's actual choice — not a real fix.

The existing `WithHideFunc(func() bool {...})` closures avoid exactly this problem by being *re-evaluated reactively* rather than read once. The fix reuses that same, already-proven pattern instead of relying on it for visibility alone: **split the single `modelGroup` into one group per provider**, mirroring how `anthropicKeyGroup`/`openaiGroup`/`zaiKeyGroup` already exist as separate groups rather than one dynamic auth group:

```go
anthropicModelGroup := huh.NewGroup(
	huh.NewInput().Title("Model").Placeholder("claude-sonnet-4-5").Value(&cfg.Provider.Model),
).Title("Model").
	WithHideFunc(func() bool { return cfg.Provider.Default != "anthropic" })

zaiModelGroup := huh.NewGroup(
	huh.NewInput().Title("Model").Placeholder("glm-5").Value(&cfg.Provider.Model),
).Title("Model").
	WithHideFunc(func() bool { return cfg.Provider.Default != "zai" })

openaiModelGroup := huh.NewGroup(
	huh.NewInput().Title("Model").Placeholder("gpt-4o").Value(&cfg.Provider.Model),
).Title("Model").
	WithHideFunc(func() bool { return cfg.Provider.Default != "openai" })
```

`huh.NewForm` takes all three (replacing the single `modelGroup` in the `huh.NewForm(...)` call). Each placeholder is a compile-time-known literal for its own group — the same literals as each provider's `ProviderDef.DefaultModel` (Anthropic: `internal/provider/anthropic/provider.go`; Z.ai: `internal/provider/zai/provider.go`) — with no dependency on any dynamic-placeholder capability from `huh`, since visibility (not content) is what needs to be reactive, and `WithHideFunc` already does that correctly.

Ollama gets no model group at all (as today) — `loadConfig()`'s `ResolveDefaultModel` already resolves Ollama's model via a live query at CLI startup.

### 2. `/config` overlay (`internal/tui/configform.go`)

Rebuilt to the same per-provider group shape as `bootstrap.go`:

```go
type ConfigForm struct {
	form          *huh.Form
	cfg           *config.Config
	savePath      string
	maxTurnsStr   string
	openaiKey     string // staging field, mirrors BootstrapForm
	openaiBaseURL string // staging field, mirrors BootstrapForm
}
```

Groups:
- **Provider** (unchanged): the `huh.NewSelect` bound to `cfg.Provider.Default`.
- **Anthropic auth** (`WithHideFunc`: provider != "anthropic"): API key input bound to `cfg.Provider.Anthropic.APIKey`.
- **OpenAI-compatible auth** (`WithHideFunc`: provider != "openai"): base URL + API key, staged in `openaiBaseURL`/`openaiKey` exactly like `BootstrapForm`, copied into `cfg.Provider.OpenAI` on `Save()` using the same logic as `BootstrapForm.Save()` (find-or-create the `"openai"`-named entry rather than always overwriting index 0, since `/config` may run against an existing config with a pre-populated `cfg.Provider.OpenAI` slice — bootstrap always starts from `config.DefaultConfig()` with an empty slice, so this is a real divergence bootstrap's `Save()` doesn't need to handle).
- **Z.ai auth** (`WithHideFunc`: provider != "zai"): API key bound to `cfg.Provider.Zai.APIKey`, plus a base URL input bound to `cfg.Provider.Zai.BaseURL` (optional — empty means "use the default", matching `providerDef()`'s `BaseURL` resolver behavior in `internal/provider/zai/provider.go`).
- **Ollama** (`WithHideFunc`: provider != "ollama"): base URL input bound to `cfg.Provider.Ollama.BaseURL` (optional, same "empty = default" semantics as `internal/provider/ollama/provider.go`'s `resolveBaseURL`).
- **Model** (unchanged position, always visible): free-text input bound to `cfg.Provider.Model`, pre-filled with its current value (already correct — no change needed here beyond keeping it).
- **Agent**, **Security** (unchanged).

`Save()` gains the same `APIKeySource = "config"` bookkeeping `BootstrapForm.Save()` does for Anthropic/Z.ai when a key was entered, plus the find-or-update logic for the OpenAI-compatible entry described above.

### 3. `/model` picker (`internal/tui/modelpicker.go`, `internal/tui/model.go`, `internal/tui/overlay.go`)

**New message type** (`internal/tui/model.go`, alongside `BootstrapProgressMsg`):

```go
// ModelsFetchedMsg carries the result of an async Registry.ListModels call,
// triggered when the model picker is opened for a provider that supports
// live listing (currently only Ollama).
type ModelsFetchedMsg struct {
	Models []provider.Model
	Err    error
}
```

**`ActionOpenModelPicker` handling** (`internal/tui/model.go`, replaces the current block at line 928):

```go
case commands.ActionOpenModelPicker:
	if m.cfg.Provider.Default == "ollama" {
		m.state = StateFetchingModels // reuses existing spinner rendering path
		return m.fetchOllamaModels()
	}
	overlay, initCmd := NewModelTextInputOverlay(m.cfg.Provider.Model)
	m.activeOverlay = overlay
	m.state = StateModelPickerOverlay
	return initCmd

// fetchOllamaModels returns a tea.Cmd that queries the Registry in the
// background, mirroring runBootstrap's async tea.Msg pattern (model.go)
// so the local Ollama HTTP call never blocks the event loop.
func (m *Model) fetchOllamaModels() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		models, err := provider.Default.ListModels(context.Background(), "ollama", cfg)
		return ModelsFetchedMsg{Models: models, Err: err}
	}
}
```

**New state** `StateFetchingModels` (alongside `StateModelPickerOverlay` in the state enum), rendered by the existing spinner/status-line path already used for other in-flight async operations (no new rendering code — reuses whatever `Update()`'s default `View()` branch already does for "busy" states; exact reuse point identified during implementation by following how e.g. wiki generation's in-flight state renders).

**New `Update()` case** (`internal/tui/update.go`, alongside the existing message-type switch):

```go
case ModelsFetchedMsg:
	if msg.Err != nil {
		m.content.WriteString(fmt.Sprintf("Failed to list Ollama models: %s\n", msg.Err))
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

(`ModelChoice.Size` is left empty for Registry-sourced models — `provider.Model` has no size field; `ModelPicker`'s existing `fmt.Sprintf("%s (%s)", m.Name, m.Size)` rendering degrades to `"name ()"` with an empty size, which is acceptable but noted as a follow-up polish item, not blocking.)

**New overlay type** (`internal/tui/modelpicker.go`, new file section or new file `modeltextinput.go` — file placement decided during implementation based on resulting size):

```go
// ModelTextInputOverlay is the /model picker's fallback for providers with
// no live listing capability (everything except Ollama): a single free-text
// field pre-filled with the current model, converging on the same
// ModelPickerResult every ModelPicker selection produces.
type ModelTextInputOverlay struct {
	form  *huh.Form
	value string
}

func NewModelTextInputOverlay(currentModel string) (*ModelTextInputOverlay, tea.Cmd) {
	o := &ModelTextInputOverlay{value: currentModel}
	input := huh.NewInput().Title("Model name").Value(&o.value)
	o.form = huh.NewForm(huh.NewGroup(input))
	return o, o.form.Init()
}

func (o *ModelTextInputOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	form, cmd := o.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		o.form = f
	}
	return o, cmd
}

func (o *ModelTextInputOverlay) View() string { return o.form.View() }

func (o *ModelTextInputOverlay) Done() bool {
	return o.form.State == huh.StateCompleted || o.form.State == huh.StateAborted
}

func (o *ModelTextInputOverlay) Result() any {
	if o.form.State == huh.StateCompleted && o.value != "" {
		return ModelPickerResult{ModelName: o.value}
	}
	return nil
}
```

No change needed to `processOverlayResult`'s existing `case ModelPickerResult:` branch (`internal/tui/overlay.go:91`) — both paths converge on the same result type, so that dispatch code is untouched.

`availableModels()` (`internal/tui/model.go:945`) is deleted — fully superseded by the two paths above.

## Testing

- **`bootstrap_test.go`**: extend existing per-provider group-visibility tests with a placeholder assertion per provider's model group (anthropic/zai/openai) — confirming each group's own literal placeholder (`"claude-sonnet-4-5"`, `"glm-5"`, `"gpt-4o"`) renders, and that switching the provider select correctly shows/hides the matching model group (same `WithHideFunc` assertion pattern already used for the auth groups).
- **`configform_test.go`**: rewritten to assert, per provider: (a) only that provider's auth group is visible (`WithHideFunc` parity with bootstrap), (b) entering a key writes to the correct config struct field (directly regression-tests the bug being fixed — e.g. selecting `"zai"`, entering a key, asserting `cfg.Provider.Zai.APIKey` is set and `cfg.Provider.Anthropic.APIKey` is untouched), (c) the OpenAI-compatible find-or-update `Save()` logic against both an empty and a pre-populated `cfg.Provider.OpenAI` slice.
- **`model_test.go`**: `ActionOpenModelPicker` dispatch test per provider — Ollama returns a `tea.Cmd` (state becomes `StateFetchingModels`); every other provider opens `ModelTextInputOverlay` directly (state becomes `StateModelPickerOverlay` immediately, no intermediate fetch state).
- **New test file** (`modeltextinput_test.go` or folded into `modelpicker_test.go`, decided at implementation time by resulting file size): `ModelTextInputOverlay` pre-fills the current model, produces `ModelPickerResult{ModelName: ...}` on submit, produces `nil` on abort/empty submit.
- **`ModelsFetchedMsg` handling** (`update_test.go` or `model_test.go`): success case (fake `Registry`/stubbed `ListModels`, or a real `httptest`-backed Ollama server per the pattern in `internal/provider/ollama/provider_test.go`) opens the picker with the real model list; error case (server unreachable) writes an error message to the transcript and returns to `StateInput`, matching the existing error-surfacing shape used by `ActionOpenWiki`/`ActionOpenUndo`.

All new/changed tests follow the codebase's existing convention of exercising real behavior (real `huh.Form` interaction via `Update()`, real HTTP via `testutil.NewServer` where network is involved) rather than mocks, matching this repo's established pattern throughout `internal/tui` and `internal/provider`.
