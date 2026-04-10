# Ghostty Terminal Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a terminal capability detection layer and escape sequence emitters so Rubichan can leverage modern terminal features (progress bars, notifications, inline images, enhanced keyboard, light/dark mode) across all three modes.

**Architecture:** A new `internal/terminal/` package detects capabilities via hybrid strategy (TERM_PROGRAM fast path + query-based probing fallback), exposes a `Caps` struct, and provides stateless emitter functions. Each mode receives `*Caps` at startup and uses relevant capabilities. The existing `tui/hyperlink.go` migrates to use `Caps.Hyperlinks`.

**Tech Stack:** Go standard library, `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/glamour`, `mmdc` (Mermaid CLI, optional external dependency)

**Spec:** `docs/superpowers/specs/2026-04-10-ghostty-terminal-integration-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|----------------|
| `internal/terminal/caps.go` | `Caps` struct, `Detect()`, known terminal table, `DetectWithEnv()` for testing |
| `internal/terminal/caps_test.go` | Tests for capability detection (fast path + slow path) |
| `internal/terminal/probe.go` | Query-based probing: `probeBackground()`, `probeSyncRendering()`, `probeKittyKeyboard()` |
| `internal/terminal/probe_test.go` | Tests with mock terminal I/O |
| `internal/terminal/progress.go` | `SetProgress()`, `ClearProgress()` (OSC 9;4) |
| `internal/terminal/progress_test.go` | Verify exact escape sequences |
| `internal/terminal/notify.go` | `Notify()` (OSC 9) |
| `internal/terminal/notify_test.go` | Verify exact escape sequences |
| `internal/terminal/workdir.go` | `SetWorkingDirectory()` (OSC 7) |
| `internal/terminal/workdir_test.go` | Verify exact escape sequences |
| `internal/terminal/clipboard.go` | `CopyToClipboard()` (OSC 52) |
| `internal/terminal/clipboard_test.go` | Verify exact escape sequences |
| `internal/terminal/sync.go` | `BeginSync()`, `EndSync()` (Mode 2026) |
| `internal/terminal/sync_test.go` | Verify exact escape sequences |
| `internal/terminal/focus.go` | `EnableFocusEvents()`, `DisableFocusEvents()` (Mode 1004) |
| `internal/terminal/focus_test.go` | Verify exact escape sequences |
| `internal/terminal/kittyimg.go` | `KittyImage()` — Kitty graphics protocol chunked image transmission |
| `internal/terminal/kittyimg_test.go` | Test chunking, framing, small and large images |
| `internal/terminal/mermaid.go` | `RenderMermaid()`, `MmdcAvailable()` — Mermaid CLI integration |
| `internal/terminal/mermaid_test.go` | Test error handling, PATH detection |

### Modified Files

| File | Change |
|------|--------|
| `internal/tui/hyperlink.go` | Replace `supportedTerminals` map + `SupportsHyperlinks()` with `Caps.Hyperlinks` parameter |
| `internal/tui/hyperlink_test.go` | Update tests for new parameter-based API |
| `internal/tui/markdown.go` | Accept `darkBackground bool` parameter in `NewMarkdownRenderer()` |
| `internal/tui/model.go` | Accept `*terminal.Caps` in `NewModel()`, store as field, pass to sub-components |
| `cmd/rubichan/main.go` | Call `terminal.Detect()` in each mode entrypoint, thread through |

---

### Task 1: Caps Struct and Fast-Path Detection

**Files:**
- Create: `internal/terminal/caps.go`
- Create: `internal/terminal/caps_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/terminal/caps_test.go
package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectWithEnv_Ghostty(t *testing.T) {
	caps := DetectWithEnv("ghostty", nil)
	assert.True(t, caps.Hyperlinks)
	assert.True(t, caps.KittyGraphics)
	assert.True(t, caps.KittyKeyboard)
	assert.True(t, caps.ProgressBar)
	assert.True(t, caps.Notifications)
	assert.True(t, caps.SyncRendering)
	assert.True(t, caps.ClipboardAccess)
	assert.True(t, caps.FocusEvents)
}

func TestDetectWithEnv_Kitty(t *testing.T) {
	caps := DetectWithEnv("kitty", nil)
	assert.True(t, caps.Hyperlinks)
	assert.True(t, caps.KittyGraphics)
	assert.True(t, caps.KittyKeyboard)
	assert.False(t, caps.ProgressBar) // Kitty doesn't support OSC 9;4
	assert.True(t, caps.Notifications)
	assert.True(t, caps.SyncRendering)
	assert.True(t, caps.ClipboardAccess)
	assert.True(t, caps.FocusEvents)
}

func TestDetectWithEnv_AppleTerminal(t *testing.T) {
	caps := DetectWithEnv("Apple_Terminal", nil)
	assert.True(t, caps.Hyperlinks)
	assert.False(t, caps.KittyGraphics)
	assert.False(t, caps.KittyKeyboard)
	assert.False(t, caps.ProgressBar)
	assert.False(t, caps.Notifications)
	assert.False(t, caps.SyncRendering)
	assert.False(t, caps.ClipboardAccess)
	assert.False(t, caps.FocusEvents)
}

func TestDetectWithEnv_UnknownTerminal(t *testing.T) {
	// Unknown terminal with nil prober => all capabilities false
	caps := DetectWithEnv("unknown-terminal", nil)
	assert.False(t, caps.Hyperlinks)
	assert.False(t, caps.KittyGraphics)
	assert.False(t, caps.ProgressBar)
}

func TestDetectWithEnv_EmptyTermProgram(t *testing.T) {
	caps := DetectWithEnv("", nil)
	assert.False(t, caps.Hyperlinks)
	assert.False(t, caps.KittyGraphics)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/terminal/... -run TestDetectWithEnv -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write minimal implementation**

```go
// internal/terminal/caps.go
package terminal

import "os"

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
	DarkBackground  bool // detected via OSC 11 query (defaults true)
}

// knownTerminal defines the capability profile for a recognized terminal.
type knownTerminal struct {
	Hyperlinks    bool
	KittyGraphics bool
	KittyKeyboard bool
	ProgressBar   bool
	Notifications bool
	SyncRendering bool
	LightDarkMode bool
	Clipboard     bool
	FocusEvents   bool
}

// knownTerminals maps TERM_PROGRAM values to their capability profiles.
// This table is best-effort — validate entries against terminal documentation.
var knownTerminals = map[string]knownTerminal{
	"ghostty": {
		Hyperlinks: true, KittyGraphics: true, KittyKeyboard: true,
		ProgressBar: true, Notifications: true, SyncRendering: true,
		LightDarkMode: true, Clipboard: true, FocusEvents: true,
	},
	"kitty": {
		Hyperlinks: true, KittyGraphics: true, KittyKeyboard: true,
		ProgressBar: false, Notifications: true, SyncRendering: true,
		LightDarkMode: true, Clipboard: true, FocusEvents: true,
	},
	"WezTerm": {
		Hyperlinks: true, KittyGraphics: true, KittyKeyboard: true,
		ProgressBar: true, Notifications: true, SyncRendering: true,
		LightDarkMode: true, Clipboard: true, FocusEvents: true,
	},
	"iTerm.app": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: true, SyncRendering: false,
		LightDarkMode: true, Clipboard: true, FocusEvents: true,
	},
	"vscode": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, Clipboard: true, FocusEvents: false,
	},
	"alacritty": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: true,
		ProgressBar: false, Notifications: false, SyncRendering: true,
		LightDarkMode: false, Clipboard: false, FocusEvents: true,
	},
	"Apple_Terminal": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, Clipboard: false, FocusEvents: false,
	},
	"Hyper": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, Clipboard: false, FocusEvents: false,
	},
	"Tabby": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, Clipboard: false, FocusEvents: false,
	},
	"rio": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, Clipboard: false, FocusEvents: false,
	},
	"contour": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, Clipboard: false, FocusEvents: false,
	},
}

// Prober queries the terminal for capabilities not derivable from TERM_PROGRAM.
// Used as the slow-path fallback for unknown terminals.
type Prober interface {
	// ProbeBackground returns true if the terminal background is dark.
	// Returns (isDark, supported). If supported is false, the probe timed out.
	ProbeBackground() (isDark bool, supported bool)
	// ProbeSyncRendering returns true if Mode 2026 is supported.
	ProbeSyncRendering() bool
	// ProbeKittyKeyboard returns true if the Kitty keyboard protocol is supported.
	ProbeKittyKeyboard() bool
}

// Detect probes the current terminal and returns its capabilities.
// It reads TERM_PROGRAM from the environment for the fast path.
func Detect() *Caps {
	return DetectWithEnv(os.Getenv("TERM_PROGRAM"), nil)
}

// DetectWithProber probes the current terminal with a custom prober for the slow path.
func DetectWithProber(prober Prober) *Caps {
	return DetectWithEnv(os.Getenv("TERM_PROGRAM"), prober)
}

// DetectWithEnv is the testable core of Detect. It accepts the TERM_PROGRAM
// value and an optional Prober for the slow path.
func DetectWithEnv(termProgram string, prober Prober) *Caps {
	caps := &Caps{
		DarkBackground: true, // safe default
	}

	// Fast path: known terminal.
	if kt, ok := knownTerminals[termProgram]; ok {
		caps.Hyperlinks = kt.Hyperlinks
		caps.KittyGraphics = kt.KittyGraphics
		caps.KittyKeyboard = kt.KittyKeyboard
		caps.ProgressBar = kt.ProgressBar
		caps.Notifications = kt.Notifications
		caps.SyncRendering = kt.SyncRendering
		caps.LightDarkMode = kt.LightDarkMode
		caps.ClipboardAccess = kt.Clipboard
		caps.FocusEvents = kt.FocusEvents

		// Always probe background color — it's user-configurable.
		if prober != nil {
			if isDark, supported := prober.ProbeBackground(); supported {
				caps.DarkBackground = isDark
			}
		}

		return caps
	}

	// Slow path: unknown terminal — probe for capabilities.
	if prober == nil {
		return caps
	}

	if isDark, supported := prober.ProbeBackground(); supported {
		caps.DarkBackground = isDark
		caps.LightDarkMode = true
	}
	caps.SyncRendering = prober.ProbeSyncRendering()
	caps.KittyKeyboard = prober.ProbeKittyKeyboard()

	return caps
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/terminal/... -run TestDetectWithEnv -v`
Expected: PASS (all 5 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/terminal/caps.go internal/terminal/caps_test.go
git commit -m "[BEHAVIORAL] Add terminal capability detection with known terminal table"
```

---

### Task 2: Query-Based Terminal Probing

**Files:**
- Create: `internal/terminal/probe.go`
- Create: `internal/terminal/probe_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/terminal/probe_test.go
package terminal

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockTerminal simulates a terminal that responds to escape sequence queries.
type mockTerminal struct {
	responses map[string]string // query prefix -> response
	input     bytes.Buffer      // written queries accumulate here
}

func (m *mockTerminal) Read(p []byte) (int, error) {
	// Find first matching response for any query written so far.
	written := m.input.String()
	for prefix, resp := range m.responses {
		if bytes.Contains([]byte(written), []byte(prefix)) {
			return copy(p, []byte(resp)), nil
		}
	}
	// No response — simulate timeout by blocking until deadline.
	time.Sleep(200 * time.Millisecond)
	return 0, io.EOF
}

func (m *mockTerminal) Write(p []byte) (int, error) {
	return m.input.Write(p)
}

func TestStdioProber_ProbeBackground_Dark(t *testing.T) {
	mock := &mockTerminal{
		responses: map[string]string{
			"\x1b]11;?": "\x1b]11;rgb:1a1a/1a1a/1a1a\x1b\\", // dark background
		},
	}
	prober := NewStdioProber(mock, mock, 100*time.Millisecond)
	isDark, supported := prober.ProbeBackground()
	assert.True(t, supported)
	assert.True(t, isDark)
}

func TestStdioProber_ProbeBackground_Light(t *testing.T) {
	mock := &mockTerminal{
		responses: map[string]string{
			"\x1b]11;?": "\x1b]11;rgb:f0f0/f0f0/f0f0\x1b\\", // light background
		},
	}
	prober := NewStdioProber(mock, mock, 100*time.Millisecond)
	isDark, supported := prober.ProbeBackground()
	assert.True(t, supported)
	assert.False(t, isDark)
}

func TestStdioProber_ProbeBackground_Timeout(t *testing.T) {
	mock := &mockTerminal{responses: map[string]string{}} // no response
	prober := NewStdioProber(mock, mock, 50*time.Millisecond)
	_, supported := prober.ProbeBackground()
	assert.False(t, supported)
}

func TestDetectWithEnv_UnknownTerminal_WithProber(t *testing.T) {
	mock := &mockTerminal{
		responses: map[string]string{
			"\x1b]11;?": "\x1b]11;rgb:2020/2020/2020\x1b\\",
		},
	}
	prober := NewStdioProber(mock, mock, 100*time.Millisecond)
	caps := DetectWithEnv("unknown-terminal", prober)
	assert.True(t, caps.DarkBackground)
	assert.True(t, caps.LightDarkMode)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/terminal/... -run TestStdioProber -v`
Expected: FAIL — `NewStdioProber` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/terminal/probe.go
package terminal

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// StdioProber queries the terminal via escape sequences written to w,
// reading responses from r with the given timeout per probe.
type StdioProber struct {
	r       io.Reader
	w       io.Writer
	timeout time.Duration
}

// NewStdioProber creates a prober that communicates via the given reader/writer.
// timeout controls how long each probe waits for a response.
func NewStdioProber(r io.Reader, w io.Writer, timeout time.Duration) *StdioProber {
	return &StdioProber{r: r, w: w, timeout: timeout}
}

// ProbeBackground sends OSC 11 to query the terminal background color.
// Returns (isDark, true) on success, or (false, false) on timeout.
func (p *StdioProber) ProbeBackground() (isDark bool, supported bool) {
	// Send: OSC 11 ; ? ST
	fmt.Fprint(p.w, "\x1b]11;?\x1b\\")

	resp, err := p.readWithTimeout()
	if err != nil {
		return false, false
	}

	// Response format: OSC 11 ; rgb:RRRR/GGGG/BBBB ST
	return parseBackgroundResponse(resp), true
}

// ProbeSyncRendering sends a DECRPM query for Mode 2026.
// Returns true if the terminal reports the mode as recognized.
func (p *StdioProber) ProbeSyncRendering() bool {
	// Send: CSI ? 2026 $ p
	fmt.Fprint(p.w, "\x1b[?2026$p")

	resp, err := p.readWithTimeout()
	if err != nil {
		return false
	}

	// Response: CSI ? 2026 ; Ps $ y
	// Ps=1 (set), Ps=2 (reset), Ps=3 (permanently set), Ps=4 (permanently reset)
	// Any of these means the mode is recognized.
	return strings.Contains(resp, "2026") && strings.Contains(resp, "$y")
}

// ProbeKittyKeyboard sends a Kitty keyboard protocol query.
// Returns true if the terminal responds.
func (p *StdioProber) ProbeKittyKeyboard() bool {
	// Send: CSI ? u (query current keyboard flags)
	fmt.Fprint(p.w, "\x1b[?u")

	resp, err := p.readWithTimeout()
	if err != nil {
		return false
	}

	// Response: CSI ? <flags> u
	return strings.Contains(resp, "?") && strings.HasSuffix(strings.TrimSpace(resp), "u")
}

// readWithTimeout reads from the terminal with a deadline.
func (p *StdioProber) readWithTimeout() (string, error) {
	done := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		buf := make([]byte, 256)
		n, err := p.r.Read(buf)
		if err != nil {
			errCh <- err
			return
		}
		done <- string(buf[:n])
	}()

	select {
	case resp := <-done:
		return resp, nil
	case err := <-errCh:
		return "", err
	case <-time.After(p.timeout):
		return "", fmt.Errorf("probe timed out after %v", p.timeout)
	}
}

// parseBackgroundResponse extracts the brightness from an OSC 11 response.
// Format: ESC ] 11 ; rgb:RRRR/GGGG/BBBB ST
// Returns true if the color is considered dark (average intensity < 50%).
func parseBackgroundResponse(resp string) bool {
	// Find "rgb:" in response.
	idx := strings.Index(resp, "rgb:")
	if idx == -1 {
		return true // default to dark on parse failure
	}
	colorPart := resp[idx+4:]

	// Trim trailing ST (ESC \ or BEL).
	colorPart = strings.TrimRight(colorPart, "\x1b\\\x07")

	parts := strings.Split(colorPart, "/")
	if len(parts) != 3 {
		return true // default to dark
	}

	var total uint64
	for _, p := range parts {
		val, err := strconv.ParseUint(p, 16, 16)
		if err != nil {
			return true // default to dark
		}
		total += val
	}

	// Average of R, G, B. Max value is 0xFFFF per channel.
	avg := total / 3
	return avg < 0x8000 // < 50% brightness = dark
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/terminal/... -run "TestStdioProber|TestDetectWithEnv_UnknownTerminal_WithProber" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/terminal/probe.go internal/terminal/probe_test.go
git commit -m "[BEHAVIORAL] Add query-based terminal probing for unknown terminals"
```

---

### Task 3: Progress Bar Emitter (OSC 9;4)

**Files:**
- Create: `internal/terminal/progress.go`
- Create: `internal/terminal/progress_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/terminal/progress_test.go
package terminal

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetProgress_Normal(t *testing.T) {
	var buf bytes.Buffer
	SetProgress(&buf, ProgressNormal, 75)
	assert.Equal(t, "\x1b]9;4;1;75\x07", buf.String())
}

func TestSetProgress_Error(t *testing.T) {
	var buf bytes.Buffer
	SetProgress(&buf, ProgressError, 100)
	assert.Equal(t, "\x1b]9;4;2;100\x07", buf.String())
}

func TestSetProgress_Indeterminate(t *testing.T) {
	var buf bytes.Buffer
	SetProgress(&buf, ProgressIndeterminate, 0)
	assert.Equal(t, "\x1b]9;4;3;0\x07", buf.String())
}

func TestSetProgress_Warning(t *testing.T) {
	var buf bytes.Buffer
	SetProgress(&buf, ProgressWarning, 50)
	assert.Equal(t, "\x1b]9;4;4;50\x07", buf.String())
}

func TestClearProgress(t *testing.T) {
	var buf bytes.Buffer
	ClearProgress(&buf)
	assert.Equal(t, "\x1b]9;4;0;0\x07", buf.String())
}

func TestSetProgress_ClampsPercent(t *testing.T) {
	var buf bytes.Buffer
	SetProgress(&buf, ProgressNormal, 150)
	assert.Equal(t, "\x1b]9;4;1;100\x07", buf.String())

	buf.Reset()
	SetProgress(&buf, ProgressNormal, -10)
	assert.Equal(t, "\x1b]9;4;1;0\x07", buf.String())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/terminal/... -run TestSetProgress -v`
Expected: FAIL — `SetProgress` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/terminal/progress.go
package terminal

import (
	"fmt"
	"io"
)

// ProgressState represents the state of a terminal progress bar (OSC 9;4).
type ProgressState int

const (
	ProgressHidden        ProgressState = 0 // clear the progress bar
	ProgressNormal        ProgressState = 1 // blue/default progress
	ProgressError         ProgressState = 2 // red/error progress
	ProgressIndeterminate ProgressState = 3 // spinning/indeterminate
	ProgressWarning       ProgressState = 4 // yellow/warning progress
)

// SetProgress writes an OSC 9;4 progress bar sequence.
// Sequence format: ESC ] 9 ; 4 ; <state> ; <percent> BEL
// percent is clamped to 0-100.
func SetProgress(w io.Writer, state ProgressState, percent int) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	fmt.Fprintf(w, "\x1b]9;4;%d;%d\x07", state, percent)
}

// ClearProgress resets the progress bar.
func ClearProgress(w io.Writer) {
	SetProgress(w, ProgressHidden, 0)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/terminal/... -run "TestSetProgress|TestClearProgress" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/terminal/progress.go internal/terminal/progress_test.go
git commit -m "[BEHAVIORAL] Add OSC 9;4 progress bar emitter"
```

---

### Task 4: Notification Emitter (OSC 9)

**Files:**
- Create: `internal/terminal/notify.go`
- Create: `internal/terminal/notify_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/terminal/notify_test.go
package terminal

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNotify(t *testing.T) {
	var buf bytes.Buffer
	Notify(&buf, "Wiki generation complete")
	assert.Equal(t, "\x1b]9;Wiki generation complete\x07", buf.String())
}

func TestNotify_Empty(t *testing.T) {
	var buf bytes.Buffer
	Notify(&buf, "")
	assert.Equal(t, "\x1b]9;\x07", buf.String())
}

func TestNotify_SpecialChars(t *testing.T) {
	var buf bytes.Buffer
	Notify(&buf, "Found 3 high-severity findings!")
	assert.Equal(t, "\x1b]9;Found 3 high-severity findings!\x07", buf.String())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/terminal/... -run TestNotify -v`
Expected: FAIL — `Notify` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/terminal/notify.go
package terminal

import (
	"fmt"
	"io"
)

// Notify sends a desktop notification via OSC 9.
// Sequence format: ESC ] 9 ; <message> BEL
func Notify(w io.Writer, message string) {
	fmt.Fprintf(w, "\x1b]9;%s\x07", message)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/terminal/... -run TestNotify -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/terminal/notify.go internal/terminal/notify_test.go
git commit -m "[BEHAVIORAL] Add OSC 9 desktop notification emitter"
```

---

### Task 5: Working Directory, Clipboard, Sync, and Focus Emitters

**Files:**
- Create: `internal/terminal/workdir.go`
- Create: `internal/terminal/workdir_test.go`
- Create: `internal/terminal/clipboard.go`
- Create: `internal/terminal/clipboard_test.go`
- Create: `internal/terminal/sync.go`
- Create: `internal/terminal/sync_test.go`
- Create: `internal/terminal/focus.go`
- Create: `internal/terminal/focus_test.go`

These are all small, independent emitters grouped into one task for efficiency.

- [ ] **Step 1: Write the failing tests**

```go
// internal/terminal/workdir_test.go
package terminal

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetWorkingDirectory(t *testing.T) {
	var buf bytes.Buffer
	SetWorkingDirectory(&buf, "/Users/dev/project")
	assert.Equal(t, "\x1b]7;file:///Users/dev/project\x07", buf.String())
}

func TestSetWorkingDirectory_EncodesSpaces(t *testing.T) {
	var buf bytes.Buffer
	SetWorkingDirectory(&buf, "/Users/dev/my project")
	assert.Contains(t, buf.String(), "\x1b]7;file:///Users/dev/my%20project\x07")
}
```

```go
// internal/terminal/clipboard_test.go
package terminal

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyToClipboard(t *testing.T) {
	var buf bytes.Buffer
	CopyToClipboard(&buf, "hello world")
	encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
	assert.Equal(t, "\x1b]52;c;"+encoded+"\x1b\\", buf.String())
}

func TestCopyToClipboard_Empty(t *testing.T) {
	var buf bytes.Buffer
	CopyToClipboard(&buf, "")
	encoded := base64.StdEncoding.EncodeToString([]byte(""))
	assert.Equal(t, "\x1b]52;c;"+encoded+"\x1b\\", buf.String())
}
```

```go
// internal/terminal/sync_test.go
package terminal

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBeginSync(t *testing.T) {
	var buf bytes.Buffer
	BeginSync(&buf)
	assert.Equal(t, "\x1b[?2026h", buf.String())
}

func TestEndSync(t *testing.T) {
	var buf bytes.Buffer
	EndSync(&buf)
	assert.Equal(t, "\x1b[?2026l", buf.String())
}
```

```go
// internal/terminal/focus_test.go
package terminal

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnableFocusEvents(t *testing.T) {
	var buf bytes.Buffer
	EnableFocusEvents(&buf)
	assert.Equal(t, "\x1b[?1004h", buf.String())
}

func TestDisableFocusEvents(t *testing.T) {
	var buf bytes.Buffer
	DisableFocusEvents(&buf)
	assert.Equal(t, "\x1b[?1004l", buf.String())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/terminal/... -run "TestSetWorkingDirectory|TestCopyToClipboard|TestBeginSync|TestEndSync|TestEnableFocusEvents|TestDisableFocusEvents" -v`
Expected: FAIL — functions undefined

- [ ] **Step 3: Write minimal implementations**

```go
// internal/terminal/workdir.go
package terminal

import (
	"fmt"
	"io"
	"net/url"
)

// SetWorkingDirectory emits OSC 7 with the given absolute path.
// Sequence format: ESC ] 7 ; file:///<path> BEL
func SetWorkingDirectory(w io.Writer, absPath string) {
	u := &url.URL{Scheme: "file", Path: absPath}
	fmt.Fprintf(w, "\x1b]7;%s\x07", u.String())
}
```

```go
// internal/terminal/clipboard.go
package terminal

import (
	"encoding/base64"
	"fmt"
	"io"
)

// CopyToClipboard writes base64-encoded content to the system clipboard via OSC 52.
// Sequence format: ESC ] 52 ; c ; <base64> ST
func CopyToClipboard(w io.Writer, content string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	fmt.Fprintf(w, "\x1b]52;c;%s\x1b\\", encoded)
}
```

```go
// internal/terminal/sync.go
package terminal

import (
	"fmt"
	"io"
)

// BeginSync starts a synchronized update (Mode 2026).
// Sequence: CSI ? 2026 h
func BeginSync(w io.Writer) {
	fmt.Fprint(w, "\x1b[?2026h")
}

// EndSync ends a synchronized update (Mode 2026).
// Sequence: CSI ? 2026 l
func EndSync(w io.Writer) {
	fmt.Fprint(w, "\x1b[?2026l")
}
```

```go
// internal/terminal/focus.go
package terminal

import (
	"fmt"
	"io"
)

// EnableFocusEvents enables terminal focus/blur reporting (Mode 1004).
// Sequence: CSI ? 1004 h
func EnableFocusEvents(w io.Writer) {
	fmt.Fprint(w, "\x1b[?1004h")
}

// DisableFocusEvents disables terminal focus/blur reporting (Mode 1004).
// Sequence: CSI ? 1004 l
func DisableFocusEvents(w io.Writer) {
	fmt.Fprint(w, "\x1b[?1004l")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/terminal/... -v`
Expected: PASS (all tests in package)

- [ ] **Step 5: Commit**

```bash
git add internal/terminal/workdir.go internal/terminal/workdir_test.go \
       internal/terminal/clipboard.go internal/terminal/clipboard_test.go \
       internal/terminal/sync.go internal/terminal/sync_test.go \
       internal/terminal/focus.go internal/terminal/focus_test.go
git commit -m "[BEHAVIORAL] Add OSC 7, OSC 52, Mode 2026, and Mode 1004 emitters"
```

---

### Task 6: Kitty Graphics Protocol Image Transmission

**Files:**
- Create: `internal/terminal/kittyimg.go`
- Create: `internal/terminal/kittyimg_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/terminal/kittyimg_test.go
package terminal

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKittyImage_SmallImage(t *testing.T) {
	// A tiny PNG-like payload (< 4096 bytes) should be sent in one chunk.
	data := []byte("tiny-png-data")
	var buf bytes.Buffer
	KittyImage(&buf, data)

	output := buf.String()
	encoded := base64.StdEncoding.EncodeToString(data)

	// Single chunk: a=T,f=100,m=0;<base64>\x1b\\
	assert.Contains(t, output, "a=T,f=100,m=0;")
	assert.Contains(t, output, encoded)
	assert.True(t, strings.HasPrefix(output, "\x1b_G"))
	assert.True(t, strings.HasSuffix(output, "\x1b\\"))
}

func TestKittyImage_LargeImage(t *testing.T) {
	// A payload that exceeds 4096 base64 bytes should be split into chunks.
	data := bytes.Repeat([]byte("x"), 4096) // will produce >4096 base64 chars
	var buf bytes.Buffer
	KittyImage(&buf, data)

	output := buf.String()

	// First chunk should have m=1 (more chunks follow).
	assert.Contains(t, output, "a=T,f=100,m=1;")
	// Last chunk should have m=0 (final).
	// Count number of \x1b_G occurrences — should be > 1.
	chunks := strings.Count(output, "\x1b_G")
	require.Greater(t, chunks, 1, "large image should be split into multiple chunks")
}

func TestKittyImage_Empty(t *testing.T) {
	var buf bytes.Buffer
	KittyImage(&buf, nil)
	assert.Empty(t, buf.String())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/terminal/... -run TestKittyImage -v`
Expected: FAIL — `KittyImage` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/terminal/kittyimg.go
package terminal

import (
	"encoding/base64"
	"fmt"
	"io"
)

const kittyChunkSize = 4096 // max base64 bytes per chunk

// KittyImage transmits a PNG image inline via the Kitty graphics protocol.
// Data is base64-encoded and sent in chunks of up to 4096 bytes.
// Does nothing if data is nil or empty.
func KittyImage(w io.Writer, pngData []byte) {
	if len(pngData) == 0 {
		return
	}

	encoded := base64.StdEncoding.EncodeToString(pngData)

	if len(encoded) <= kittyChunkSize {
		// Single chunk — m=0 means no more chunks.
		fmt.Fprintf(w, "\x1b_Ga=T,f=100,m=0;%s\x1b\\", encoded)
		return
	}

	// Multiple chunks.
	for i := 0; i < len(encoded); i += kittyChunkSize {
		end := i + kittyChunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		chunk := encoded[i:end]
		more := 1
		if end == len(encoded) {
			more = 0
		}

		if i == 0 {
			// First chunk includes the action and format.
			fmt.Fprintf(w, "\x1b_Ga=T,f=100,m=%d;%s\x1b\\", more, chunk)
		} else {
			// Continuation chunks.
			fmt.Fprintf(w, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/terminal/... -run TestKittyImage -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/terminal/kittyimg.go internal/terminal/kittyimg_test.go
git commit -m "[BEHAVIORAL] Add Kitty graphics protocol image transmission"
```

---

### Task 7: Mermaid CLI Integration

**Files:**
- Create: `internal/terminal/mermaid.go`
- Create: `internal/terminal/mermaid_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/terminal/mermaid_test.go
package terminal

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMmdcAvailable_WhenNotOnPath(t *testing.T) {
	// Save and clear PATH to simulate mmdc not being available.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent")
	defer os.Setenv("PATH", origPath)

	assert.False(t, MmdcAvailable())
}

func TestMmdcAvailable_WhenOnPath(t *testing.T) {
	// Only run if mmdc is actually installed.
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed, skipping")
	}
	assert.True(t, MmdcAvailable())
}

func TestRenderMermaid_MmdcNotFound(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent")
	defer os.Setenv("PATH", origPath)

	_, err := RenderMermaid(context.Background(), "graph TD\n    A --> B", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mmdc")
}

func TestRenderMermaid_Success(t *testing.T) {
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed, skipping")
	}

	src := "graph TD\n    A[Start] --> B[End]"
	data, err := RenderMermaid(context.Background(), src, true)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// PNG magic bytes.
	assert.Equal(t, byte(0x89), data[0])
	assert.Equal(t, byte('P'), data[1])
	assert.Equal(t, byte('N'), data[2])
	assert.Equal(t, byte('G'), data[3])
}

func TestRenderMermaid_CancelledContext(t *testing.T) {
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed, skipping")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RenderMermaid(ctx, "graph TD\n    A --> B", true)
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/terminal/... -run "TestMmdc|TestRenderMermaid" -v`
Expected: FAIL — `MmdcAvailable`, `RenderMermaid` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/terminal/mermaid.go
package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// MmdcAvailable returns true if the mmdc (Mermaid CLI) is on PATH.
func MmdcAvailable() bool {
	_, err := exec.LookPath("mmdc")
	return err == nil
}

// RenderMermaid converts Mermaid source to PNG using the mmdc CLI.
// darkMode selects the "dark" theme; otherwise "default" is used.
// Returns the PNG bytes, or an error if mmdc is not available or fails.
func RenderMermaid(ctx context.Context, mermaidSrc string, darkMode bool) ([]byte, error) {
	if !MmdcAvailable() {
		return nil, fmt.Errorf("mmdc (Mermaid CLI) not found on PATH")
	}

	tmpDir, err := os.MkdirTemp("", "rubichan-mermaid-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input.mmd")
	outputPath := filepath.Join(tmpDir, "output.png")

	if err := os.WriteFile(inputPath, []byte(mermaidSrc), 0o600); err != nil {
		return nil, fmt.Errorf("writing mermaid input: %w", err)
	}

	theme := "default"
	if darkMode {
		theme = "dark"
	}

	cmd := exec.CommandContext(ctx, "mmdc",
		"-i", inputPath,
		"-o", outputPath,
		"-t", theme,
		"-b", "transparent",
		"-w", "800",
	)
	cmd.Stderr = nil // suppress mmdc stderr noise

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("mmdc execution: %w", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("reading mmdc output: %w", err)
	}

	return data, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/terminal/... -run "TestMmdc|TestRenderMermaid" -v`
Expected: PASS (tests requiring mmdc will skip if not installed)

- [ ] **Step 5: Commit**

```bash
git add internal/terminal/mermaid.go internal/terminal/mermaid_test.go
git commit -m "[BEHAVIORAL] Add Mermaid CLI integration for diagram rendering"
```

---

### Task 8: Migrate Hyperlink Detection to Caps

**Files:**
- Modify: `internal/tui/hyperlink.go`
- Modify: `internal/tui/hyperlink_test.go`

- [ ] **Step 1: Read current hyperlink test file**

Run: `cat internal/tui/hyperlink_test.go` (or use Read tool)

Understand current test structure before modifying.

- [ ] **Step 2: Update hyperlink.go — remove supportedTerminals, accept bool parameter**

Replace the contents of `internal/tui/hyperlink.go`:

```go
package tui

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// filePathPattern matches file paths that look like:
// - Absolute paths: /foo/bar.go
// - Relative paths: ./foo/bar.go, ../foo/bar.go
// - Directory paths: dir/subdir/file.ext (must contain / and end with extension)
var filePathPattern = regexp.MustCompile(`(?:^|[\s:])(/[^\s:]+\.[a-zA-Z0-9]+|\.\.?/[^\s:]+\.[a-zA-Z0-9]+|[a-zA-Z0-9_][a-zA-Z0-9_./+-]*\.[a-zA-Z0-9]+)`)

// LinkifyFilePaths wraps recognized file paths in OSC 8 hyperlinks.
// Only activates when hyperlinkSupported is true.
func LinkifyFilePaths(text string, workDir string, hyperlinkSupported bool) string {
	if !hyperlinkSupported {
		return text
	}

	return filePathPattern.ReplaceAllStringFunc(text, func(match string) string {
		// Preserve leading whitespace/colon
		prefix := ""
		path := match
		if len(path) > 0 && (path[0] == ' ' || path[0] == '\t' || path[0] == ':') {
			prefix = string(path[0])
			path = path[1:]
		}

		// Only linkify paths that contain a slash (to avoid matching random words.ext)
		if !strings.Contains(path, "/") {
			return match
		}

		absPath := path
		if !filepath.IsAbs(path) {
			cleaned := strings.TrimPrefix(path, "./")
			absPath = filepath.Join(workDir, cleaned)
		}

		// Reject paths containing control characters to prevent terminal injection.
		if strings.ContainsAny(absPath, "\x1b\x00\x07") {
			return match
		}

		fileURL := (&url.URL{Scheme: "file", Path: absPath}).String()
		link := "\x1b]8;;" + fileURL + "\x1b\\" + path + "\x1b]8;;\x1b\\"
		return prefix + link
	})
}
```

- [ ] **Step 3: Update hyperlink_test.go — pass bool instead of setting TERM_PROGRAM**

Update all test calls from `LinkifyFilePaths(text, workDir)` to `LinkifyFilePaths(text, workDir, true)` for tests that expect linkification, and `LinkifyFilePaths(text, workDir, false)` for tests that expect no linkification. Remove any `TERM_PROGRAM` env manipulation in tests.

- [ ] **Step 4: Update all callers of LinkifyFilePaths and SupportsHyperlinks**

Search codebase for calls to `LinkifyFilePaths` and `SupportsHyperlinks()`. Update each call site to pass the hyperlink capability from `Caps`. If `Caps` is not yet threaded to that call site, pass `true` as a temporary value with a `// TODO: wire from Caps` comment — this will be resolved in Task 10 when Caps is threaded through the TUI model.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/hyperlink.go internal/tui/hyperlink_test.go
git commit -m "[STRUCTURAL] Migrate hyperlink detection from TERM_PROGRAM to parameter-based API"
```

---

### Task 9: Light/Dark Mode in Markdown Renderer

**Files:**
- Modify: `internal/tui/markdown.go`
- Modify: `internal/tui/model.go` (only the `NewMarkdownRenderer` call)

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/tui/markdown_test.go (or create if it doesn't exist)

func TestNewMarkdownRenderer_DarkMode(t *testing.T) {
	r, err := NewMarkdownRenderer(80, true)
	require.NoError(t, err)
	require.NotNil(t, r)

	// Render something and verify it works (exact style is internal to glamour).
	output, err := r.Render("**bold**")
	require.NoError(t, err)
	assert.NotEmpty(t, output)
}

func TestNewMarkdownRenderer_LightMode(t *testing.T) {
	r, err := NewMarkdownRenderer(80, false)
	require.NoError(t, err)
	require.NotNil(t, r)

	output, err := r.Render("**bold**")
	require.NoError(t, err)
	assert.NotEmpty(t, output)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestNewMarkdownRenderer -v`
Expected: FAIL — `NewMarkdownRenderer` takes wrong number of arguments

- [ ] **Step 3: Update NewMarkdownRenderer to accept darkBackground parameter**

In `internal/tui/markdown.go`, change the constructor:

```go
// NewMarkdownRenderer creates a MarkdownRenderer with the appropriate style
// for the given background brightness and the given word wrap width.
func NewMarkdownRenderer(width int, darkBackground bool) (*MarkdownRenderer, error) {
	style := "light"
	if darkBackground {
		style = "dark"
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, fmt.Errorf("creating glamour renderer: %w", err)
	}
	return &MarkdownRenderer{renderer: r}, nil
}
```

- [ ] **Step 4: Update the call site in model.go**

In `internal/tui/model.go`, find the `NewMarkdownRenderer(80)` call (around line 185) and change it to `NewMarkdownRenderer(80, true)`. This preserves current behavior (dark mode default). It will be wired to `Caps.DarkBackground` in Task 10.

- [ ] **Step 5: Update any other callers**

Search for all `NewMarkdownRenderer(` calls and update them to pass the second parameter.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/markdown.go internal/tui/markdown_test.go internal/tui/model.go
git commit -m "[STRUCTURAL] Add darkBackground parameter to NewMarkdownRenderer"
```

---

### Task 10: Thread Caps Through TUI Model and Interactive Mode

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `cmd/rubichan/main.go` (interactive entrypoint)

This task wires `*terminal.Caps` into the TUI model and Bubble Tea program options.

- [ ] **Step 1: Add Caps field to Model struct**

In `internal/tui/model.go`, add to the `Model` struct:

```go
import "github.com/julianshen/rubichan/internal/terminal"

// In Model struct:
termCaps *terminal.Caps
```

- [ ] **Step 2: Update NewModel to accept *terminal.Caps**

Change the `NewModel` signature to add a `caps *terminal.Caps` parameter. Use `caps.DarkBackground` when calling `NewMarkdownRenderer`:

```go
func NewModel(a *agent.Agent, appName, modelName string, maxTurns int,
              configPath string, cfg *config.Config,
              cmdRegistry *commands.Registry, caps *terminal.Caps) *Model {
    // ...
    darkBg := true
    if caps != nil {
        darkBg = caps.DarkBackground
    }
    mdRenderer, err := NewMarkdownRenderer(80, darkBg)
    // ...
    m := &Model{
        // ... existing fields ...
        termCaps: caps,
    }
    // ...
}
```

- [ ] **Step 3: Add TermCaps() accessor**

```go
// TermCaps returns the terminal capabilities, or nil if not detected.
func (m *Model) TermCaps() *terminal.Caps {
	return m.termCaps
}
```

- [ ] **Step 4: Update cmd/rubichan/main.go — runInteractive()**

At the top of `runInteractive()` (around line 1370), add:

```go
caps := terminal.Detect()
```

Update the `tui.NewModel(...)` call (around line 1679) to pass `caps`:

```go
model := tui.NewModel(nil, "rubichan", cfg.Provider.Model, cfg.Agent.MaxTurns, cfgPath, cfg, cmdRegistry, caps)
```

Update Bubble Tea program options (around line 1862) to conditionally enable Kitty keyboard:

```go
programOpts := []tea.ProgramOption{
    tea.WithContext(runCtx),
}
if !noMouse {
    programOpts = append(programOpts, tea.WithMouseCellMotion())
}
if !noAltScreen {
    programOpts = append(programOpts, tea.WithAltScreen())
}
if caps.KittyKeyboard {
    programOpts = append(programOpts, tea.WithKittyKeyboard(tea.KittyDisambiguateEscapeCodes))
}
prog := tea.NewProgram(model, programOpts...)
```

- [ ] **Step 5: Update all other NewModel call sites**

Search for `tui.NewModel(` calls. Update each to pass the new `caps` parameter. For test files, pass `nil` (capabilities not needed in unit tests).

- [ ] **Step 6: Wire LinkifyFilePaths to use Caps**

Find all calls to `LinkifyFilePaths` and replace any temporary `true` values with `m.termCaps != nil && m.termCaps.Hyperlinks`.

- [ ] **Step 7: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: PASS (all packages)

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Thread terminal Caps through TUI model and interactive mode"
```

---

### Task 11: Wire Progress Bar and Notifications into Wiki Mode

**Files:**
- Modify: `cmd/rubichan/main.go` (wiki entrypoint, around line 3087)

- [ ] **Step 1: Update runWikiHeadless to detect caps and emit progress**

In `runWikiHeadless()` (line 3087), add capability detection and update the ProgressFunc:

```go
func runWikiHeadless(cfg *config.Config, cwd, outDir, format string, concurrency int) error {
	caps := terminal.Detect()

	// ... existing provider/llm/parser setup ...

	wikiCfg := wiki.Config{
		Dir:         cwd,
		OutputDir:   outDir,
		Format:      format,
		Concurrency: concurrency,
		ProgressFunc: func(stage string, current, total int) {
			if total > 0 {
				fmt.Fprintf(os.Stderr, "[%s] %d/%d\n", stage, current, total)
				if caps.ProgressBar {
					percent := current * 100 / total
					terminal.SetProgress(os.Stderr, terminal.ProgressNormal, percent)
				}
			} else {
				fmt.Fprintf(os.Stderr, "[%s]\n", stage)
				if caps.ProgressBar {
					terminal.SetProgress(os.Stderr, terminal.ProgressIndeterminate, 0)
				}
			}
		},
	}

	result, err := wiki.Run(context.Background(), wikiCfg, llm, par)

	// Clear progress bar on completion.
	if caps.ProgressBar {
		terminal.ClearProgress(os.Stderr)
	}

	if err != nil {
		if caps.Notifications {
			terminal.Notify(os.Stderr, "Wiki generation failed")
		}
		return err
	}

	if caps.Notifications {
		terminal.Notify(os.Stderr, fmt.Sprintf("Wiki complete — %d documents rendered", result.Documents))
	}

	// ... existing output handling ...
}
```

- [ ] **Step 2: Run wiki-related tests**

Run: `go test ./internal/wiki/... -v`
Run: `go build ./cmd/rubichan/`
Expected: PASS and clean build

- [ ] **Step 3: Commit**

```bash
git add cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Wire progress bar and notifications into wiki mode"
```

---

### Task 12: Wire Progress Bar and Notifications into Headless Mode

**Files:**
- Modify: `cmd/rubichan/main.go` (headless entrypoint, around line 1890)

- [ ] **Step 1: Update runHeadless to detect caps and emit notifications**

At the top of `runHeadless()`, add capability detection:

```go
func runHeadless() error {
	caps := terminal.Detect()
	// ... existing setup ...
```

After the agent loop completes (find the result handling section), add notifications:

```go
	// After code review completes:
	if caps.Notifications {
		terminal.Notify(os.Stderr, "Code review complete")
	}
```

For security scan results, add notification on high-severity findings:

```go
	// After security findings are processed:
	if caps.Notifications && highSeverityCount > 0 {
		terminal.Notify(os.Stderr, fmt.Sprintf("%d high-severity security findings detected", highSeverityCount))
	}
```

Emit OSC 7 at startup:

```go
	if caps.Notifications {
		terminal.SetWorkingDirectory(os.Stderr, cwd)
	}
```

Note: The exact integration points depend on the headless mode's result handling code. The implementer should read the full `runHeadless()` function to find where results are processed and add notifications at the appropriate points.

- [ ] **Step 2: Run build and existing tests**

Run: `go build ./cmd/rubichan/`
Run: `go test ./internal/modes/headless/... -v`
Expected: Clean build and PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Wire notifications and working directory into headless mode"
```

---

### Task 13: Wire Notifications and Progress into Interactive Mode

**Files:**
- Modify: `internal/tui/model.go` (Update method)
- Modify: `cmd/rubichan/main.go` (interactive entrypoint)

- [ ] **Step 1: Add notification helper to Model**

In `internal/tui/model.go`, add a helper that emits notifications when supported:

```go
// notifyIfSupported sends a desktop notification if the terminal supports it.
func (m *Model) notifyIfSupported(message string) {
	if m.termCaps != nil && m.termCaps.Notifications {
		terminal.Notify(os.Stderr, message)
	}
}
```

- [ ] **Step 2: Wire notifications into approval waiting**

Find the code path where the TUI shows an approval prompt (look for `pendingApproval` or `ApprovalPrompt`). When an approval prompt is displayed, call:

```go
m.notifyIfSupported("Rubichan needs approval to proceed")
```

- [ ] **Step 3: Wire OSC 7 for directory changes**

Find where the `cd` tool result is processed. After a successful directory change, emit:

```go
if m.termCaps != nil && m.termCaps.Notifications {
	terminal.SetWorkingDirectory(os.Stderr, newWorkDir)
}
```

Note: The exact location depends on how tool results flow back to the TUI. The implementer should trace the `cd` tool's result path.

- [ ] **Step 4: Wire progress bar for knowledge graph bootstrap**

Find `BootstrapProgressOverlay` or `BootstrapProgressMsg` handling. When bootstrap progress updates arrive, emit:

```go
if m.termCaps != nil && m.termCaps.ProgressBar {
	terminal.SetProgress(os.Stderr, terminal.ProgressNormal, progressPercent)
}
// On completion:
if m.termCaps != nil && m.termCaps.ProgressBar {
	terminal.ClearProgress(os.Stderr)
}
```

- [ ] **Step 5: Run all tests**

Run: `go test ./internal/tui/... -v`
Run: `go test ./... 2>&1 | tail -5`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Wire notifications, progress bar, and OSC 7 into interactive mode"
```

---

### Task 14: Inline Mermaid Diagram Rendering in TUI

**Files:**
- Modify: `internal/tui/model.go` or create `internal/tui/diagrams.go`

This task adds inline Mermaid diagram rendering when viewing wiki output in the interactive TUI.

- [ ] **Step 1: Create diagram rendering helper**

Create `internal/tui/diagrams.go`:

```go
package tui

import (
	"bytes"
	"context"
	"os"
	"time"

	"github.com/julianshen/rubichan/internal/terminal"
)

// renderMermaidInline attempts to render a Mermaid diagram as an inline image.
// Returns true and writes the image to stderr if successful.
// Returns false if rendering is not available (no mmdc, no Kitty graphics).
func renderMermaidInline(caps *terminal.Caps, mermaidSrc string) bool {
	if caps == nil || !caps.KittyGraphics || !terminal.MmdcAvailable() {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pngData, err := terminal.RenderMermaid(ctx, mermaidSrc, caps.DarkBackground)
	if err != nil {
		return false
	}

	var buf bytes.Buffer
	terminal.KittyImage(&buf, pngData)
	buf.WriteTo(os.Stderr)
	return true
}
```

- [ ] **Step 2: Write test**

```go
// internal/tui/diagrams_test.go
package tui

import (
	"testing"

	"github.com/julianshen/rubichan/internal/terminal"
	"github.com/stretchr/testify/assert"
)

func TestRenderMermaidInline_NilCaps(t *testing.T) {
	assert.False(t, renderMermaidInline(nil, "graph TD\n    A-->B"))
}

func TestRenderMermaidInline_NoKittyGraphics(t *testing.T) {
	caps := &terminal.Caps{KittyGraphics: false}
	assert.False(t, renderMermaidInline(caps, "graph TD\n    A-->B"))
}
```

- [ ] **Step 3: Wire into wiki overlay or wiki output display**

Find where wiki diagrams are displayed in the TUI (look for `WikiOverlay` or diagram content rendering). Before rendering Mermaid source as a code block, attempt inline rendering:

```go
// If Mermaid source is detected in output:
if !renderMermaidInline(m.termCaps, mermaidSrc) {
    // Fall back to text-based code block display.
    // ... existing rendering ...
}
```

The exact integration point depends on how wiki results flow to the TUI view. The implementer should trace the wiki overlay's view rendering.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diagrams.go internal/tui/diagrams_test.go
git commit -m "[BEHAVIORAL] Add inline Mermaid diagram rendering via Kitty graphics"
```

---

### Task 15: Final Integration Test and Cleanup

**Files:**
- Verify: all modified files compile and tests pass

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1 2>&1 | tail -30`
Expected: All packages PASS

- [ ] **Step 2: Run build**

Run: `go build ./cmd/rubichan/`
Expected: Clean build

- [ ] **Step 3: Run linter**

Run: `golangci-lint run ./...`
Expected: No errors

- [ ] **Step 4: Check coverage for new package**

Run: `go test ./internal/terminal/... -cover`
Expected: >90% coverage

- [ ] **Step 5: Verify gofmt**

Run: `gofmt -l internal/terminal/ internal/tui/`
Expected: No output (all files formatted)

- [ ] **Step 6: Commit any final fixes**

```bash
git add -A
git commit -m "[STRUCTURAL] Final cleanup for terminal integration"
```
