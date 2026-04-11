package terminal

import (
	"encoding/json"
	"net"
	"os"
	"time"
)

// Caps represents what the host terminal supports.
type Caps struct {
	Hyperlinks      bool // OSC 8
	KittyGraphics   bool // Kitty graphics protocol
	KittyKeyboard   bool // Kitty keyboard protocol
	ProgressBar     bool // OSC 9;4 (ConEmu/Ghostty)
	Notifications   bool // OSC 9
	SyncRendering   bool // Mode 2026
	LightDarkMode   bool // OSC 11 background color query
	ClipboardAccess bool // OSC 52
	FocusEvents     bool // Mode 1004
	DarkBackground  bool // detected via OSC 11 query (defaults true)
	CmuxSocket      bool // cmux Unix socket API available
}

var knownTerminals = map[string]Caps{
	"ghostty": {
		Hyperlinks: true, KittyGraphics: true, KittyKeyboard: true,
		ProgressBar: true, Notifications: true, SyncRendering: true,
		LightDarkMode: true, ClipboardAccess: true, FocusEvents: true,
	},
	"kitty": {
		Hyperlinks: true, KittyGraphics: true, KittyKeyboard: true,
		ProgressBar: false, Notifications: true, SyncRendering: true,
		LightDarkMode: true, ClipboardAccess: true, FocusEvents: true,
	},
	"WezTerm": {
		Hyperlinks: true, KittyGraphics: true, KittyKeyboard: true,
		ProgressBar: true, Notifications: true, SyncRendering: true,
		LightDarkMode: true, ClipboardAccess: true, FocusEvents: true,
	},
	"iTerm.app": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: true, SyncRendering: false,
		LightDarkMode: true, ClipboardAccess: true, FocusEvents: true,
	},
	"vscode": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, ClipboardAccess: true, FocusEvents: false,
	},
	"alacritty": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: true,
		ProgressBar: false, Notifications: false, SyncRendering: true,
		LightDarkMode: false, ClipboardAccess: false, FocusEvents: true,
	},
	"Apple_Terminal": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, ClipboardAccess: false, FocusEvents: false,
	},
	"Hyper": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, ClipboardAccess: false, FocusEvents: false,
	},
	"Tabby": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, ClipboardAccess: false, FocusEvents: false,
	},
	"rio": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, ClipboardAccess: false, FocusEvents: false,
	},
	"contour": {
		Hyperlinks: true, KittyGraphics: false, KittyKeyboard: false,
		ProgressBar: false, Notifications: false, SyncRendering: false,
		LightDarkMode: false, ClipboardAccess: false, FocusEvents: false,
	},
}

// Prober queries the terminal for capabilities not derivable from TERM_PROGRAM.
type Prober interface {
	ProbeBackground() (isDark bool, supported bool)
	ProbeSyncRendering() bool
	ProbeKittyKeyboard() bool
}

// Detect returns terminal capabilities using the TERM_PROGRAM fast path only.
// Runtime probing (StdioProber) is not wired here because it requires raw
// terminal I/O that conflicts with Bubble Tea's terminal management. Use
// DetectWithProber for environments where stdin/stdout are available for probing.
func Detect() *Caps {
	return DetectWithEnv(os.Getenv("TERM_PROGRAM"), nil)
}

// DetectWithProber probes the current terminal with a custom prober for the slow path.
func DetectWithProber(prober Prober) *Caps {
	return DetectWithEnv(os.Getenv("TERM_PROGRAM"), prober)
}

// DetectWithEnv detects terminal capabilities using a two-phase strategy: first
// a fast lookup by termProgram against the known-terminals table, then optional
// runtime probing via prober for capabilities that vary per user config (e.g.,
// background color). Pass nil for prober to skip runtime probing.
func DetectWithEnv(termProgram string, prober Prober) *Caps {
	caps := &Caps{
		DarkBackground: true,
	}

	if kt, ok := knownTerminals[termProgram]; ok {
		*caps = kt
		caps.DarkBackground = true
		if prober != nil {
			if isDark, supported := prober.ProbeBackground(); supported {
				caps.DarkBackground = isDark
			}
		}
		caps.CmuxSocket = detectCmuxSocket()
		return caps
	}

	if prober == nil {
		caps.CmuxSocket = detectCmuxSocket()
		return caps
	}

	if isDark, supported := prober.ProbeBackground(); supported {
		caps.DarkBackground = isDark
		caps.LightDarkMode = true
	}
	caps.SyncRendering = prober.ProbeSyncRendering()
	caps.KittyKeyboard = prober.ProbeKittyKeyboard()
	caps.CmuxSocket = detectCmuxSocket()

	return caps
}

func detectCmuxSocket() bool {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		return false
	}
	socketPath := os.Getenv("CMUX_SOCKET_PATH")
	if socketPath == "" {
		socketPath = "/tmp/cmux.sock"
	}
	return pingCmuxSocket(socketPath)
}

func pingCmuxSocket(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		return false
	}
	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	if err := enc.Encode(map[string]any{
		"id": "detect-1", "method": "system.ping", "params": struct{}{},
	}); err != nil {
		return false
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := dec.Decode(&resp); err != nil {
		return false
	}
	return resp.OK
}
