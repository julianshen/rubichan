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

// Detect probes the current terminal and returns its capabilities.
func Detect() *Caps {
	return DetectWithEnv(os.Getenv("TERM_PROGRAM"), nil)
}

// DetectWithProber probes the current terminal with a custom prober for the slow path.
func DetectWithProber(prober Prober) *Caps {
	return DetectWithEnv(os.Getenv("TERM_PROGRAM"), prober)
}

// DetectWithEnv is the testable core of Detect.
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
		return caps
	}

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
