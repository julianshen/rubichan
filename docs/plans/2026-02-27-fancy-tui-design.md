# Fancy TUI & Config TUI Design

**Date:** 2026-02-27
**Status:** Approved
**Milestone:** 7 — TUI Enhancement

## Context

Rubichan's current TUI is a bare scaffold: text input, viewport, spinner. No markdown rendering, no syntax highlighting, no interactive approval, no config editor. The spec (FR-1.1) requires rich TUI with syntax-highlighted code blocks and diff previews. Users currently configure via hand-editing TOML.

## Design Decisions

- **Style:** Claude Code — minimal chrome, inline tool results, streaming markdown
- **Markdown:** Glamour (Charm's renderer, spec section 7.2) with Chroma syntax highlighting
- **Forms:** Huh (Charm's form library) for all interactive components — target end-state is full Huh
- **Approval:** Inline y/n/always prompt within conversation flow
- **Config:** `/config` slash command opens Huh form overlay
- **Bootstrap:** First-run wizard when no config exists, using shared Huh form groups
- **Status bar:** Model name, tokens/budget, turn count/max, estimated cost

## Component Architecture

End-state component tree with Huh-powered sub-models:

```
Model (root)
├── ChatView         — conversation viewport + Glamour markdown rendering
├── InputArea        — huh.Text multi-line input
├── StatusBar        — tokens / turns / cost / model (Lipgloss styled)
├── ApprovalPrompt   — huh.Confirm inline when tool needs approval
├── ConfigOverlay    — huh.Form overlay triggered by /config
└── ModelPicker      — huh.Select for switching models
```

Migration path: new components use Huh from day one; existing components migrate to Huh when touched; ChatView and StatusBar stay custom (Glamour/Lipgloss).

Root Model manages active overlay state and routes key events.

## Bootstrap Setup Wizard

Triggers when:
- Config file doesn't exist
- Config exists but no provider API key resolvable
- `--headless` skips bootstrap (fails with clear error)

Flow (Huh multi-step form):
1. Provider selection — `huh.Select` (Anthropic / OpenAI-compatible / Ollama)
2. Provider-specific config (API key, base URL, etc.)
3. Model selection — `huh.Select` or `huh.Input`
4. Confirmation — summary + save to `~/.config/rubichan/config.toml`

After bootstrap, normal chat TUI launches.

## Fancy Chat TUI

### Markdown Rendering
- Glamour renders complete assistant messages after streaming settles
- During streaming: raw text displayed, re-rendered through Glamour on "done" event
- Syntax highlighting via Glamour's built-in Chroma
- Code blocks get bordered boxes with language label

### Tool Call/Result Display
```
╭─ file_read("src/main.go") ──────────────╮
│ package main                              │
│                                           │
│ func main() {                             │
│     fmt.Println("hello")                  │
│ }                                         │
╰───────────────────────────────────────────╯
```
- Bordered box with tool name + args in header
- Results syntax-highlighted if language detectable
- Large results truncated with "[N more lines]"
- Error results in red border

### Status Bar
```
 claude-sonnet-4-5  1.2k/100k  Turn 3/50  ~$0.02
```
- Updates after each turn
- Cost from simple per-model pricing lookup table

### Input Area
- `huh.Text` multi-line, shift+enter for newline, enter submits

### Approval Prompt (Inline)
```
╭─ shell("rm -rf /tmp/test") ──────────────╮
│ Allow?  (y)es  (n)o  (a)lways            │
╰───────────────────────────────────────────╯
```
- Single keypress, no Enter needed
- "always" auto-approves that tool name for the session
- Channel-based: agent sends request, TUI shows prompt, keypress sends response

### Slash Commands
- `/config` — config overlay (new)
- `/model` — Huh select picker (migrate existing)
- `/clear`, `/quit`, `/exit`, `/help` — preserved

## Config Overlay (`/config`)

Huh form overlay over the conversation:

**Form groups** (tab-navigable):
1. **Provider** — select provider, model input, API key (masked)
2. **Agent** — max turns, context budget, approval mode
3. **Security** — fail-on severity threshold

**Out of scope** (hand-edit TOML, hint shown):
- MCP server configuration
- Skills configuration
- OpenAI-compatible provider array

**Behavior:**
- Save writes to config.toml
- Provider/model changes take effect on next turn
- Cancel/Esc returns to chat unchanged
- Shares provider form group with bootstrap wizard

## State Machine

```
StateInput             — normal chat input
StateStreaming         — agent responding
StateAwaitingApproval  — inline y/n/a prompt
StateConfigOverlay     — /config form active
StateBootstrap         — first-run wizard
```

When overlay active, all keys route to overlay. Esc dismisses.

## New Dependencies

- `charmbracelet/huh` — form components
- `charmbracelet/glamour` — markdown rendering

## PR Breakdown

| PR | What | Size | Depends |
|----|------|------|---------|
| 1 | Add huh + glamour deps, extract view sub-models | S | — |
| 2 | Glamour markdown rendering for assistant messages | M | PR1 |
| 3 | Styled tool call/result boxes with syntax highlighting | M | PR1 |
| 4 | Rich status bar (tokens, turns, cost) | S | PR1 |
| 5 | Interactive approval UI with Huh confirm | M | PR3 |
| 6 | Multi-line input with Huh text | S | PR1 |
| 7 | /config overlay with Huh form | M | PR1 |
| 8 | Bootstrap setup wizard | M | PR7 |
| 9 | /model picker migration to Huh select | S | PR1 |

Execution order: PR1 → {PR2, PR3, PR4, PR6, PR7, PR9 in parallel} → PR5 (needs PR3) → PR8 (needs PR7).
