# Slash Command Completion Design

**Date:** 2026-03-01
**Status:** Approved

## Overview

Add a slash command completion system to Rubichan's TUI. When the user types `/`, a dropdown popup appears above the input area showing matching commands with descriptions. Commands filter as the user continues typing. Commands support argument-level completions (e.g., `/model` shows available model names). Skills can register their own slash commands via their manifest.

## Architecture

### Approach: Command Registry with Completion Provider Interface

A central `commands.Registry` where both built-in commands and skills register `SlashCommand` implementations. The TUI renders a `CompletionOverlay` Bubble Tea component that queries the registry for matches.

## Components

### 1. Command Registry (`internal/commands/`)

Core types:

```go
type SlashCommand interface {
    Name() string
    Description() string
    Arguments() []ArgumentDef
    Complete(ctx context.Context, args []string) []Candidate
    Execute(ctx context.Context, args []string) (Result, error)
}

type ArgumentDef struct {
    Name        string
    Description string
    Required    bool
    Static      []string // pre-known values (nil = use Complete() for dynamic)
}

type Candidate struct {
    Value       string
    Description string
}

type Result struct {
    Output  string   // text to display in viewport
    Command tea.Cmd  // optional Bubble Tea command (e.g., tea.Quit)
}
```

`Registry` holds commands in a `sync.RWMutex`-guarded map keyed by command name (without `/` prefix). Provides:
- `Register(cmd SlashCommand)` / `Unregister(name string)`
- `Match(prefix string) []Candidate` — case-insensitive prefix match on names
- `Get(name string) (SlashCommand, bool)` — lookup for execution
- `All() []Candidate` — all registered commands (for empty prefix)

Built-in commands registered at startup: `quit`, `exit`, `clear`, `model`, `config`, `help`.

### 2. Completion Overlay (`internal/tui/completion.go`)

A Bubble Tea sub-component composited with the input area.

**State:**
- `visible bool` — shown when input starts with `/`
- `candidates []Candidate` — filtered matches
- `selected int` — highlighted index
- `phase` — command name vs. argument completion

**Key handling (when visible):**
- `Up/Down` — navigate candidates
- `Tab` — accept selected candidate (fill input)
- `Enter` — accept and execute
- `Escape` — dismiss overlay
- Other keys pass through to input, then re-filter

**Rendering:** Bordered box above input area, up to 8 rows. Each row: `/name    description`. Selected row highlighted with accent color.

**Activation:** Triggers when `InputArea.Value()` starts with `/`. Deactivates on Escape, Enter, or when input no longer starts with `/`.

**Argument phase:** After a command name is accepted and a space is typed, the overlay switches to calling `SlashCommand.Complete(ctx, partialArgs)` for argument-level suggestions.

### 3. Skill-Contributed Commands

**Manifest extension:**
```yaml
commands:
  - name: pods
    description: "List Kubernetes pods"
    arguments:
      - name: namespace
        description: "Namespace to list"
        required: false
```

**Backend extension:** Add `Commands() []commands.SlashCommand` to the `SkillBackend` interface.

**Lifecycle:** Commands registered on skill activation, unregistered on deactivation. Only active skills contribute commands to the completion popup.

### 4. Data Flow

**Startup:**
1. `main.go` creates `commands.Registry`
2. Built-in commands registered
3. Registry passed to `tui.NewModel()` and `skills.Runtime`

**Typing `/mo`:**
1. Input area processes keystroke → value becomes `/mo`
2. Model sees input starts with `/`, updates `CompletionOverlay`
3. Overlay calls `Registry.Match("mo")` → `[{model, "Switch LLM model"}]`
4. Overlay renders filtered candidates above input

**Tab (accept) then typing argument:**
1. Tab fills input to `/model `, overlay switches to argument phase
2. Overlay calls `model.Complete(ctx, ["partial"])` → returns model names
3. Candidates update

**Enter (execute):**
1. Overlay fills final value, hides itself
2. Normal Enter path fires → `handleCommand()` delegates to `Registry.Execute()`

**Skill activation:**
1. `Runtime.Activate()` loads backend
2. `backend.Commands()` returns skill commands
3. Runtime registers with shared `commands.Registry`
4. Commands immediately appear in completion

## Refactoring

The existing `handleCommand()` switch in `model.go:182-243` is replaced by delegation to `Registry.Get(name).Execute()`. The five built-in commands become small structs implementing `SlashCommand`.

## Testing Strategy

- **Registry:** Unit tests for Register/Unregister/Match/Get, concurrent safety
- **Built-in commands:** Unit tests for each command's Execute and Complete
- **CompletionOverlay:** Unit tests for filtering, navigation, key handling, phase switching
- **Integration:** TUI tests verifying the popup appears/filters/executes correctly
- **Skill commands:** Tests for manifest parsing with `commands` field, lifecycle registration/unregistration
