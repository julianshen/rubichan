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
		caps.Hyperlinks = kt.Hyperlinks
		caps.KittyGraphics = kt.KittyGraphics
		caps.KittyKeyboard = kt.KittyKeyboard
		caps.ProgressBar = kt.ProgressBar
		caps.Notifications = kt.Notifications
		caps.SyncRendering = kt.SyncRendering
		caps.LightDarkMode = kt.LightDarkMode
		caps.ClipboardAccess = kt.Clipboard
		caps.FocusEvents = kt.FocusEvents

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
