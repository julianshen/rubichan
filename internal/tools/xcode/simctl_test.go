package xcode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimctlTool_Names(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	assert.Equal(t, "sim_list", NewSimListTool(pc).Name())
	assert.Equal(t, "sim_boot", NewSimBootTool(pc).Name())
	assert.Equal(t, "sim_shutdown", NewSimShutdownTool(pc).Name())
	assert.Equal(t, "sim_install", NewSimInstallTool(pc).Name())
	assert.Equal(t, "sim_launch", NewSimLaunchTool(pc).Name())
	assert.Equal(t, "sim_screenshot", NewSimScreenshotTool(pc).Name())
}

func TestSimctlTool_NotDarwin(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: false}
	tool := NewSimListTool(pc)

	input, _ := json.Marshal(map[string]string{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "requires macOS")
}

func TestSimctlTool_BootMissingDevice(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimBootTool(pc)

	input, _ := json.Marshal(simctlInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "device is required")
}

func TestSimctlTool_ShutdownMissingDevice(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimShutdownTool(pc)

	input, _ := json.Marshal(simctlInput{})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "device is required")
}

func TestSimctlTool_InstallMissingDevice(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimInstallTool(pc)

	input, _ := json.Marshal(simctlInput{AppPath: "/path/to/app"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "device is required")
}

func TestSimctlTool_InstallMissingAppPath(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimInstallTool(pc)

	input, _ := json.Marshal(simctlInput{Device: "iPhone 15"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "app_path is required")
}

func TestSimctlTool_LaunchMissingDevice(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimLaunchTool(pc)

	input, _ := json.Marshal(simctlInput{BundleID: "com.example.app"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "device is required")
}

func TestSimctlTool_LaunchMissingBundleID(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimLaunchTool(pc)

	input, _ := json.Marshal(simctlInput{Device: "iPhone 15"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "bundle_id is required")
}

func TestSimctlTool_ScreenshotMissingDevice(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimScreenshotTool(pc)

	input, _ := json.Marshal(simctlInput{OutputPath: "/tmp/screenshot.png"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "device is required")
}

func TestSimctlTool_ScreenshotMissingOutputPath(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimScreenshotTool(pc)

	input, _ := json.Marshal(simctlInput{Device: "iPhone 15"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "output_path is required")
}

func TestSimctlTool_InvalidJSON(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	tool := NewSimBootTool(pc)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestSimctlTool_NotDarwinAllModes(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: false}
	input, _ := json.Marshal(simctlInput{Device: "iPhone 15", AppPath: "/app", BundleID: "com.x", OutputPath: "/tmp/s.png"})

	modes := []struct {
		name string
		tool *SimctlTool
	}{
		{"boot", NewSimBootTool(pc)},
		{"shutdown", NewSimShutdownTool(pc)},
		{"install", NewSimInstallTool(pc)},
		{"launch", NewSimLaunchTool(pc)},
		{"screenshot", NewSimScreenshotTool(pc)},
	}

	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			result, err := m.tool.Execute(context.Background(), input)
			require.NoError(t, err)
			assert.True(t, result.IsError)
			assert.Contains(t, result.Content, "requires macOS")
		})
	}
}

func TestParseSimctlDevices(t *testing.T) {
	jsonData := `{
		"devices": {
			"com.apple.CoreSimulator.SimRuntime.iOS-17-0": [
				{"name": "iPhone 15", "udid": "ABC-123", "state": "Shutdown", "isAvailable": true},
				{"name": "iPhone 15 Pro", "udid": "DEF-456", "state": "Booted", "isAvailable": true}
			]
		}
	}`
	devices := ParseSimctlDevices([]byte(jsonData))
	require.Len(t, devices, 2)
	assert.Equal(t, "iPhone 15", devices[0].Name)
	assert.Equal(t, "Shutdown", devices[0].State)
	assert.Equal(t, "iOS-17-0", devices[0].Runtime)
}

func TestParseSimctlDevices_MultipleRuntimes(t *testing.T) {
	jsonData := `{
		"devices": {
			"com.apple.CoreSimulator.SimRuntime.iOS-17-0": [
				{"name": "iPhone 15", "udid": "ABC-123", "state": "Shutdown", "isAvailable": true}
			],
			"com.apple.CoreSimulator.SimRuntime.watchOS-10-0": [
				{"name": "Apple Watch Series 9", "udid": "GHI-789", "state": "Shutdown", "isAvailable": true}
			]
		}
	}`
	devices := ParseSimctlDevices([]byte(jsonData))
	require.Len(t, devices, 2)

	// Collect runtimes (map order is nondeterministic).
	runtimes := map[string]bool{}
	for _, d := range devices {
		runtimes[d.Runtime] = true
	}
	assert.True(t, runtimes["iOS-17-0"])
	assert.True(t, runtimes["watchOS-10-0"])
}

func TestParseSimctlDevices_InvalidJSON(t *testing.T) {
	devices := ParseSimctlDevices([]byte(`{bad json`))
	assert.Nil(t, devices)
}

func TestParseSimctlDevices_EmptyDevices(t *testing.T) {
	jsonData := `{"devices": {}}`
	devices := ParseSimctlDevices([]byte(jsonData))
	assert.Empty(t, devices)
}

func TestSimctlTool_Description(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	assert.Contains(t, NewSimListTool(pc).Description(), "List")
	assert.Contains(t, NewSimBootTool(pc).Description(), "Boot")
	assert.Contains(t, NewSimShutdownTool(pc).Description(), "Shut")
	assert.Contains(t, NewSimInstallTool(pc).Description(), "Install")
	assert.Contains(t, NewSimLaunchTool(pc).Description(), "Launch")
	assert.Contains(t, NewSimScreenshotTool(pc).Description(), "screenshot")
}

func TestSimctlTool_InputSchema(t *testing.T) {
	pc := &MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}

	tools := []*SimctlTool{
		NewSimListTool(pc),
		NewSimBootTool(pc),
		NewSimShutdownTool(pc),
		NewSimInstallTool(pc),
		NewSimLaunchTool(pc),
		NewSimScreenshotTool(pc),
	}

	for _, tool := range tools {
		t.Run(tool.Name(), func(t *testing.T) {
			var schema map[string]any
			require.NoError(t, json.Unmarshal(tool.InputSchema(), &schema))
			assert.Equal(t, "object", schema["type"])
		})
	}
}

func TestFormatDeviceTable(t *testing.T) {
	devices := []SimDevice{
		{Name: "iPhone 15", UDID: "ABC-123", State: "Booted", IsAvailable: true, Runtime: "iOS-17-0"},
		{Name: "iPhone 15 Pro", UDID: "DEF-456", State: "Shutdown", IsAvailable: false, Runtime: "iOS-17-0"},
	}
	table := FormatDeviceTable(devices)
	assert.Contains(t, table, "iPhone 15")
	assert.Contains(t, table, "ABC-123")
	assert.Contains(t, table, "Booted")
	assert.Contains(t, table, "iOS-17-0")
	assert.Contains(t, table, "iPhone 15 Pro")
}

func TestFormatDeviceTable_Empty(t *testing.T) {
	table := FormatDeviceTable(nil)
	assert.Contains(t, table, "No simulators found")
}

// Verify interface compliance.
var _ tools.Tool = (*SimctlTool)(nil)
