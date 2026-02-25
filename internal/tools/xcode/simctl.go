package xcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

type simctlMode string

const (
	simctlList       simctlMode = "list"
	simctlBoot       simctlMode = "boot"
	simctlShutdown   simctlMode = "shutdown"
	simctlInstall    simctlMode = "install"
	simctlLaunch     simctlMode = "launch"
	simctlScreenshot simctlMode = "screenshot"
)

type simctlInput struct {
	Device     string `json:"device,omitempty"`
	AppPath    string `json:"app_path,omitempty"`
	BundleID   string `json:"bundle_id,omitempty"`
	OutputPath string `json:"output_path,omitempty"`
}

// SimDevice represents a simulator device parsed from xcrun simctl output.
type SimDevice struct {
	Name        string `json:"name"`
	UDID        string `json:"udid"`
	State       string `json:"state"`
	IsAvailable bool   `json:"isAvailable"`
	Runtime     string `json:"runtime"`
}

// SimctlTool wraps xcrun simctl for a specific operation mode.
type SimctlTool struct {
	rootDir  string
	platform PlatformChecker
	mode     simctlMode
}

// NewSimListTool creates a tool that lists simulator devices.
func NewSimListTool(pc PlatformChecker) *SimctlTool {
	return &SimctlTool{platform: pc, mode: simctlList}
}

// NewSimBootTool creates a tool that boots a simulator device.
func NewSimBootTool(pc PlatformChecker) *SimctlTool {
	return &SimctlTool{platform: pc, mode: simctlBoot}
}

// NewSimShutdownTool creates a tool that shuts down a simulator device.
func NewSimShutdownTool(pc PlatformChecker) *SimctlTool {
	return &SimctlTool{platform: pc, mode: simctlShutdown}
}

// NewSimInstallTool creates a tool that installs an app on a simulator.
func NewSimInstallTool(rootDir string, pc PlatformChecker) *SimctlTool {
	return &SimctlTool{rootDir: rootDir, platform: pc, mode: simctlInstall}
}

// NewSimLaunchTool creates a tool that launches an app on a simulator.
func NewSimLaunchTool(pc PlatformChecker) *SimctlTool {
	return &SimctlTool{platform: pc, mode: simctlLaunch}
}

// NewSimScreenshotTool creates a tool that takes a screenshot of a simulator.
func NewSimScreenshotTool(rootDir string, pc PlatformChecker) *SimctlTool {
	return &SimctlTool{rootDir: rootDir, platform: pc, mode: simctlScreenshot}
}

// Name returns the tool name based on the mode.
func (s *SimctlTool) Name() string {
	return "sim_" + string(s.mode)
}

// Description returns a human-readable description of the tool.
func (s *SimctlTool) Description() string {
	switch s.mode {
	case simctlList:
		return "List available iOS Simulator devices with their state and runtime."
	case simctlBoot:
		return "Boot an iOS Simulator device by name or UDID."
	case simctlShutdown:
		return "Shut down a running iOS Simulator device."
	case simctlInstall:
		return "Install an app (.app bundle) on a simulator device."
	case simctlLaunch:
		return "Launch an installed app on a simulator device by bundle ID."
	case simctlScreenshot:
		return "Take a screenshot of a simulator device and save to output path."
	default:
		return "iOS Simulator operation."
	}
}

// InputSchema returns the JSON Schema for the tool input.
func (s *SimctlTool) InputSchema() json.RawMessage {
	switch s.mode {
	case simctlList:
		return json.RawMessage(`{
			"type": "object",
			"properties": {}
		}`)
	case simctlBoot, simctlShutdown:
		return json.RawMessage(`{
			"type": "object",
			"properties": {
				"device": {"type": "string", "description": "Simulator device name or UDID"}
			},
			"required": ["device"]
		}`)
	case simctlInstall:
		return json.RawMessage(`{
			"type": "object",
			"properties": {
				"device":   {"type": "string", "description": "Simulator device name or UDID"},
				"app_path": {"type": "string", "description": "Path to the .app bundle to install"}
			},
			"required": ["device", "app_path"]
		}`)
	case simctlLaunch:
		return json.RawMessage(`{
			"type": "object",
			"properties": {
				"device":    {"type": "string", "description": "Simulator device name or UDID"},
				"bundle_id": {"type": "string", "description": "App bundle identifier (e.g. com.example.app)"}
			},
			"required": ["device", "bundle_id"]
		}`)
	case simctlScreenshot:
		return json.RawMessage(`{
			"type": "object",
			"properties": {
				"device":      {"type": "string", "description": "Simulator device name or UDID"},
				"output_path": {"type": "string", "description": "File path to save the screenshot"}
			},
			"required": ["device", "output_path"]
		}`)
	default:
		return json.RawMessage(`{"type": "object", "properties": {}}`)
	}
}

// Execute runs xcrun simctl with the configured mode and input parameters.
func (s *SimctlTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	if !s.platform.IsDarwin() {
		return tools.ToolResult{Content: "simctl requires macOS with Xcode installed", IsError: true}, nil
	}

	var in simctlInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if err := s.validate(in); err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	// Validate file path inputs for modes that accept host filesystem paths.
	if s.rootDir != "" {
		if s.mode == simctlInstall && in.AppPath != "" {
			if _, err := validatePath(s.rootDir, in.AppPath); err != nil {
				return tools.ToolResult{Content: err.Error(), IsError: true}, nil
			}
		}
		if s.mode == simctlScreenshot && in.OutputPath != "" {
			if _, err := validatePath(s.rootDir, in.OutputPath); err != nil {
				return tools.ToolResult{Content: err.Error(), IsError: true}, nil
			}
		}
	}

	if s.mode == simctlList {
		return s.executeList(ctx)
	}
	return s.executeCommand(ctx, in)
}

func (s *SimctlTool) validate(in simctlInput) error {
	switch s.mode {
	case simctlList:
		return nil
	case simctlBoot, simctlShutdown:
		if in.Device == "" {
			return fmt.Errorf("device is required")
		}
	case simctlInstall:
		if in.Device == "" {
			return fmt.Errorf("device is required")
		}
		if in.AppPath == "" {
			return fmt.Errorf("app_path is required")
		}
	case simctlLaunch:
		if in.Device == "" {
			return fmt.Errorf("device is required")
		}
		if in.BundleID == "" {
			return fmt.Errorf("bundle_id is required")
		}
	case simctlScreenshot:
		if in.Device == "" {
			return fmt.Errorf("device is required")
		}
		if in.OutputPath == "" {
			return fmt.Errorf("output_path is required")
		}
	}
	return nil
}

func (s *SimctlTool) executeList(ctx context.Context) (tools.ToolResult, error) {
	cmd := exec.CommandContext(ctx, "xcrun", "simctl", "list", "-j", "devices")
	out, err := cmd.Output()
	if err != nil {
		return tools.ToolResult{
			Content: fmt.Sprintf("simctl list failed: %s", err),
			IsError: true,
		}, nil
	}

	devices := ParseSimctlDevices(out)
	return tools.ToolResult{Content: FormatDeviceTable(devices)}, nil
}

func (s *SimctlTool) executeCommand(ctx context.Context, in simctlInput) (tools.ToolResult, error) {
	args := []string{"simctl", string(s.mode), in.Device}

	switch s.mode {
	case simctlInstall:
		args = append(args, in.AppPath)
	case simctlLaunch:
		args = append(args, in.BundleID)
	case simctlScreenshot:
		// simctl io <device> screenshot <path> is the correct form.
		args = []string{"simctl", "io", in.Device, "screenshot", in.OutputPath}
	}

	cmd := exec.CommandContext(ctx, "xcrun", args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if err != nil {
		if output != "" {
			return tools.ToolResult{Content: output, IsError: true}, nil
		}
		return tools.ToolResult{Content: fmt.Sprintf("simctl %s failed: %s", s.mode, err), IsError: true}, nil
	}

	if output == "" {
		return tools.ToolResult{Content: fmt.Sprintf("simctl %s succeeded", s.mode)}, nil
	}
	return tools.ToolResult{Content: output}, nil
}

// ParseSimctlDevices parses the nested JSON structure from xcrun simctl list -j devices.
func ParseSimctlDevices(data []byte) []SimDevice {
	var raw struct {
		Devices map[string][]SimDevice `json:"devices"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	// Sort runtimes for deterministic output.
	runtimes := make([]string, 0, len(raw.Devices))
	for runtime := range raw.Devices {
		runtimes = append(runtimes, runtime)
	}
	sort.Strings(runtimes)

	var devices []SimDevice
	for _, runtime := range runtimes {
		devs := raw.Devices[runtime]
		// Extract runtime name from key like "com.apple.CoreSimulator.SimRuntime.iOS-17-0".
		parts := strings.Split(runtime, ".")
		runtimeName := parts[len(parts)-1]
		for _, d := range devs {
			d.Runtime = runtimeName
			devices = append(devices, d)
		}
	}
	return devices
}

// FormatDeviceTable formats a slice of SimDevice as a readable table.
func FormatDeviceTable(devices []SimDevice) string {
	if len(devices) == 0 {
		return "No simulators found."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%-25s %-40s %-12s %-5s %s\n", "NAME", "UDID", "STATE", "AVAIL", "RUNTIME"))
	b.WriteString(strings.Repeat("-", 95))
	b.WriteString("\n")

	for _, d := range devices {
		avail := "no"
		if d.IsAvailable {
			avail = "yes"
		}
		b.WriteString(fmt.Sprintf("%-25s %-40s %-12s %-5s %s\n", d.Name, d.UDID, d.State, avail, d.Runtime))
	}

	return b.String()
}
