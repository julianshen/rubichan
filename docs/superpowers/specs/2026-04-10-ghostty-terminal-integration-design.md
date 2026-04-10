# Ghostty Terminal Integration Design

## Goal

Add a terminal capability detection and integration layer to Rubichan that leverages modern terminal features — progress bars, desktop notifications, inline image rendering, enhanced keyboard input, light/dark mode detection, and more. While named after Ghostty, the design is protocol-based and works with any terminal that supports the relevant escape sequences (Kitty, WezTerm, iTerm2, etc.).

## Architecture

A new `internal/terminal/` package handles capability detection (hybrid: fast `TERM_PROGRAM` lookup + query-based probing fallback) and provides stateless emitter functions for each escape sequence. Each mode (interactive, headless, wiki) receives a `*Caps` struct at startup and uses only the capabilities relevant to its UI surface. No global state.

## Capability Detection

### Caps Struct

```go
// Caps represents what the host terminal supports.
type Caps struct {
    Hyperlinks      bool // OSC 8
    KittyGraphics   bool // Kitty graphics protocol
    KittyKeyboard   bool // Kitty keyboard protocol
    ProgressBar     bool // OSC 9;4 (ConEmu/Ghostty)
    Notifications   bool // OSC 9
    SyncRendering   bool // Mode 2026
    LightDarkMode   bool // Mode 2031 + OSC 10/11
    ClipboardAccess bool // OSC 52
    FocusEvents     bool // Mode 1004
    DarkBackground  bool // detected via OSC 11 query
}
```

### Hybrid Detection Strategy

1. **Fast path**: Check `TERM_PROGRAM` against a built-in table of known terminals and their capabilities. Ghostty, Kitty, WezTerm, iTerm2 all have well-documented feature sets. This covers ~95% of users with zero latency.

2. **Slow path**: For unknown terminals (or when `TERM_PROGRAM` is not set), probe using escape sequence queries with a short timeout (~100ms per probe):
   - Background color: `OSC 11 ; ? ST` — parse response to determine dark/light
   - Synchronized rendering: `CSI ? 2026 $ p` — check DECRPM response
   - Kitty keyboard: `CSI ? u` — check if terminal responds

3. **DarkBackground**: Always determined by probing (`OSC 11`), even on the fast path, because terminal theme is user-configurable and can't be inferred from `TERM_PROGRAM`. If the probe times out, defaults to `true` (dark).

4. **Caching**: `Detect()` is called once at startup. The returned `*Caps` is passed to all modes. No repeated probing.

### Known Terminal Table (Best-Effort — Validate During Implementation)

| Terminal | Hyperlinks | KittyGraphics | KittyKeyboard | ProgressBar | Notifications | SyncRendering | LightDarkMode | Clipboard | FocusEvents |
|----------|-----------|--------------|--------------|------------|--------------|--------------|--------------|-----------|------------|
| ghostty  | yes | yes | yes | yes | yes | yes | yes | yes | yes |
| kitty    | yes | yes | yes | no  | yes | yes | yes | yes | yes |
| WezTerm  | yes | yes | yes | yes | yes | yes | yes | yes | yes |
| iTerm.app| yes | no  | no  | no  | yes | no  | yes | yes | yes |
| vscode   | yes | no  | no  | no  | no  | no  | no  | yes | no  |
| alacritty| yes | no  | yes | no  | no  | yes | no  | no  | yes |
| Apple_Terminal | yes | no | no | no | no | no | no | no | no |

### Migration

`tui/hyperlink.go`'s `SupportsHyperlinks()` function and `supportedTerminals` map are replaced by reading `Caps.Hyperlinks`. The hyperlink linkification logic itself stays in `tui/hyperlink.go` but accepts a `bool` parameter instead of checking the environment directly.

## Escape Sequence Emitters

All emitter functions are stateless — they write escape sequences to an `io.Writer`. Callers check `Caps` before calling.

### Progress Bar (OSC 9;4)

```go
// ProgressState represents the state of a terminal progress bar.
type ProgressState int

const (
    ProgressHidden       ProgressState = 0 // clear the progress bar
    ProgressNormal       ProgressState = 1 // blue/default progress
    ProgressError        ProgressState = 2 // red/error progress
    ProgressIndeterminate ProgressState = 3 // spinning/indeterminate
    ProgressWarning      ProgressState = 4 // yellow/warning progress
)

// SetProgress writes an OSC 9;4 progress bar sequence.
// Sequence format: ESC ] 9 ; 4 ; <state> ; <percent> BEL
func SetProgress(w io.Writer, state ProgressState, percent int)

// ClearProgress resets the progress bar (state=0).
func ClearProgress(w io.Writer)
```

### Desktop Notifications (OSC 9)

```go
// Notify sends a desktop notification via OSC 9.
// Sequence format: ESC ] 9 ; <message> BEL
func Notify(w io.Writer, message string)
```

### Working Directory (OSC 7)

```go
// SetWorkingDirectory emits OSC 7 with the given absolute path.
// Sequence format: ESC ] 7 ; file://<hostname>/<path> BEL
func SetWorkingDirectory(w io.Writer, absPath string)
```

### Clipboard (OSC 52)

```go
// CopyToClipboard writes base64-encoded content to the system clipboard.
// Sequence format: ESC ] 52 ; c ; <base64> ST
func CopyToClipboard(w io.Writer, content string)
```

### Synchronized Rendering (Mode 2026)

```go
// BeginSync starts a synchronized update.
// Sequence: CSI ? 2026 h
func BeginSync(w io.Writer)

// EndSync ends a synchronized update.
// Sequence: CSI ? 2026 l
func EndSync(w io.Writer)
```

### Focus Events (Mode 1004)

```go
// EnableFocusEvents enables terminal focus/blur reporting.
// Sequence: CSI ? 1004 h
func EnableFocusEvents(w io.Writer)

// DisableFocusEvents disables terminal focus/blur reporting.
// Sequence: CSI ? 1004 l
func DisableFocusEvents(w io.Writer)
```

## Kitty Graphics Protocol — Inline Mermaid Diagrams

### Pipeline

1. **Mermaid to PNG**: Shell out to `mmdc` (Mermaid CLI):
   ```
   mmdc -i input.mmd -o output.png -t dark -b transparent -w 800
   ```
2. **PNG to Terminal**: Base64-encode the PNG, transmit via Kitty graphics protocol in 4096-byte chunks
3. **Fallback**: If `mmdc` is not installed or `Caps.KittyGraphics` is false, display the raw Mermaid text in a fenced code block (current behavior)

### Functions

```go
// KittyImage transmits a PNG image inline via the Kitty graphics protocol.
// Data is chunked into 4096-byte base64 segments.
func KittyImage(w io.Writer, pngData []byte)

// RenderMermaid converts Mermaid source to PNG using mmdc.
// Returns the PNG bytes, or an error if mmdc is not available.
// darkMode controls the Mermaid theme (dark vs default).
func RenderMermaid(ctx context.Context, mermaidSrc string, darkMode bool) ([]byte, error)

// MmdcAvailable returns true if the mmdc CLI is on PATH.
func MmdcAvailable() bool
```

### Where Used

- **Interactive mode**: When displaying wiki results or architecture info, render Mermaid diagrams inline instead of showing source text
- **Wiki overlay**: The existing `WikiOverlay` could show rendered diagrams
- **Not in headless/wiki output**: Those modes write Mermaid text to markdown files (unchanged)

## Kitty Keyboard Protocol

Bubble Tea already supports the Kitty keyboard protocol via `tea.WithKittyKeyboard()`. Integration is a wiring change — conditionally enable based on `Caps.KittyKeyboard`:

```go
opts := []tea.ProgramOption{tea.WithAltScreen()}
if caps.KittyKeyboard {
    opts = append(opts, tea.WithKittyKeyboard(tea.KittyDisambiguateEscapeCodes))
}
p := tea.NewProgram(model, opts...)
```

This unblocks future keybinding additions that rely on disambiguated key events (e.g., `Ctrl+I` vs `Tab`). No specific new bindings are in scope for this design.

## Mode Integration

### Interactive Mode

Receives `*terminal.Caps` at startup. Uses:

- **Light/dark mode**: `Caps.DarkBackground` selects glamour style (`"dark"` vs `"light"`)
- **Kitty keyboard**: Enable via `tea.WithKittyKeyboard()` conditionally
- **Synchronized rendering**: Enable via Bubble Tea program options conditionally
- **Focus events**: Enable Mode 1004 on start, disable on exit. On focus-lost, optionally throttle LLM streaming (stretch goal)
- **Inline diagrams**: Check `Caps.KittyGraphics && MmdcAvailable()` to render Mermaid inline
- **Notifications**: OSC 9 when tool approval is waiting
- **Working directory**: OSC 7 when agent's `cd` tool changes directory
- **Progress bar**: OSC 9;4 during knowledge graph bootstrap

### Headless Mode

Receives `*terminal.Caps` at startup. Uses:

- **Progress bar**: OSC 9;4 as files are analyzed (0→100%)
- **Notifications**: OSC 9 on completion and on high-severity security findings
- **Working directory**: OSC 7 at startup with the target directory

### Wiki Mode

Receives `*terminal.Caps` at startup. Uses:

- **Progress bar**: Existing `ProgressFunc(stage, current, total)` gets a new implementation that emits OSC 9;4. ~10 pipeline stages map to 0→100%
- **Notifications**: OSC 9 on completion (success or error)

### Threading Pattern

```go
// In entrypoint:
caps := terminal.Detect()
// Pass to mode:
interactive.Run(ctx, caps, ...)
headless.Run(ctx, caps, ...)
wiki.Run(ctx, caps, ...)
```

## File Structure

### New Files

```
internal/terminal/
├── caps.go          # Caps struct, Detect(), known terminal table
├── caps_test.go
├── probe.go         # Query-based probing (OSC 11, DECRPM, etc.)
├── probe_test.go
├── progress.go      # SetProgress, ClearProgress
├── progress_test.go
├── notify.go        # Notify (OSC 9)
├── notify_test.go
├── workdir.go       # SetWorkingDirectory (OSC 7)
├── workdir_test.go
├── clipboard.go     # CopyToClipboard (OSC 52)
├── clipboard_test.go
├── sync.go          # BeginSync, EndSync (Mode 2026)
├── sync_test.go
├── focus.go         # EnableFocusEvents, DisableFocusEvents (Mode 1004)
├── focus_test.go
├── kittyimg.go      # KittyImage (Kitty graphics protocol)
├── kittyimg_test.go
├── mermaid.go       # RenderMermaid, MmdcAvailable
└── mermaid_test.go
```

### Modified Files

```
internal/tui/hyperlink.go          # Replace supportedTerminals with Caps.Hyperlinks
internal/tui/model.go              # Accept *terminal.Caps, wire Bubble Tea options
internal/tui/markdown.go           # Use Caps.DarkBackground for glamour style
internal/modes/interactive/*.go    # Thread Caps, emit notifications, inline diagrams
internal/modes/headless/*.go       # Thread Caps, emit progress bar + notifications
internal/modes/wiki/acp_client.go  # Wire ProgressFunc to OSC 9;4
cmd/agent/main.go                  # Call terminal.Detect(), pass to modes
```

### Not Modified

- `internal/wiki/` — Wiki pipeline stays unchanged. Diagram rendering is a TUI concern.
- `internal/agent/` — Agent core is mode-agnostic.

## Testing Strategy

### Capability Detection

Mock terminal probing by injecting an `io.ReadWriter` that simulates terminal responses. The `Detect()` function is the public API; internally it calls `DetectWithReader(rw)` for testability.

- **Fast path**: Set `TERM_PROGRAM`, verify correct capabilities
- **Slow path**: Mock terminal responds to specific probes
- **Timeout**: Mock terminal never responds, verify defaults to false
- **Unknown terminal**: No `TERM_PROGRAM`, no probe responses — all capabilities false

### Emitter Functions

Capture output to `bytes.Buffer`, verify exact escape sequences:

```go
var buf bytes.Buffer
SetProgress(&buf, StateNormal, 75)
assert.Equal(t, "\x1b]9;4;1;75\x07", buf.String())
```

### Mermaid Rendering

- Test `MmdcAvailable()` with PATH manipulation
- Test `RenderMermaid` error handling when mmdc is not found
- Test `KittyImage` chunking — verify chunk boundaries and protocol framing

### Mode Integration

Construct `Caps` structs directly in mode tests (no real terminal needed):

```go
caps := &terminal.Caps{ProgressBar: true, Notifications: true}
```

### Out of Scope for Testing

- Actual terminal rendering (Ghostty's responsibility)
- `mmdc` output correctness (Mermaid CLI's responsibility)
- Bubble Tea's Kitty keyboard handling (Bubble Tea's responsibility)

## Notification Events

The following events trigger desktop notifications (OSC 9):

| Event | Mode | Message |
|-------|------|---------|
| Wiki generation complete | Wiki, Interactive | "Wiki generation complete — N documents rendered" |
| Headless review complete | Headless | "Code review complete — N findings" |
| Tool approval waiting | Interactive | "Rubichan needs approval to proceed" |
| Security findings detected | Headless, Interactive | "N high-severity security findings detected" |

## Non-Goals

- **Ghostty plugin system integration**: Plugin API doesn't exist yet. Revisit when shipped.
- **New keybindings**: Kitty keyboard protocol is enabled but no new bindings are designed.
- **Focus-based streaming throttle**: Marked as stretch goal, not required for initial implementation.
- **Sixel or iTerm2 inline image protocols**: Kitty graphics protocol only (covers Ghostty, Kitty, WezTerm).
