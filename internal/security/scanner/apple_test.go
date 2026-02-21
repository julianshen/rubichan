package scanner

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppleScannerName(t *testing.T) {
	s := NewAppleScanner()
	assert.Equal(t, "apple-platform", s.Name())
}

func TestAppleScannerInterface(t *testing.T) {
	var _ security.StaticScanner = NewAppleScanner()
}

func TestAppleScannerDetectsATSBypass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>NSAppTransportSecurity</key>
	<dict>
		<key>NSAllowsArbitraryLoads</key>
		<true/>
	</dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
</dict>
</plist>
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	found := false
	for _, f := range findings {
		if f.Title == "App Transport Security bypass enabled" {
			found = true
			assert.Equal(t, security.SeverityHigh, f.Severity)
			assert.Equal(t, security.CategoryMisconfiguration, f.Category)
			assert.Equal(t, "apple-platform", f.Scanner)
			assert.Equal(t, "Info.plist", f.Location.File)
		}
	}
	assert.True(t, found, "expected an ATS bypass finding")
}

func TestAppleScannerDetectsExcessiveEntitlements(t *testing.T) {
	dir := t.TempDir()

	t.Run("disable library validation", func(t *testing.T) {
		writeFile(t, dir, "App.entitlements", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>com.apple.security.cs.disable-library-validation</key>
	<true/>
</dict>
</plist>
`)

		s := NewAppleScanner()
		findings, err := s.Scan(context.Background(), security.ScanTarget{
			RootDir: dir,
			Files:   []string{"App.entitlements"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, findings)

		found := false
		for _, f := range findings {
			if f.Title == "Excessive entitlement: library validation disabled" {
				found = true
				assert.Equal(t, security.SeverityHigh, f.Severity)
			}
		}
		assert.True(t, found, "expected a disable-library-validation finding")
	})

	t.Run("allow JIT", func(t *testing.T) {
		writeFile(t, dir, "JIT.entitlements", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>com.apple.security.cs.allow-jit</key>
	<true/>
</dict>
</plist>
`)

		s := NewAppleScanner()
		findings, err := s.Scan(context.Background(), security.ScanTarget{
			RootDir: dir,
			Files:   []string{"JIT.entitlements"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, findings)

		found := false
		for _, f := range findings {
			if f.Title == "JIT compilation entitlement enabled" {
				found = true
				assert.Equal(t, security.SeverityMedium, f.Severity)
			}
		}
		assert.True(t, found, "expected an allow-jit finding")
	})
}

func TestAppleScannerCleanPlist(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
	<key>CFBundleName</key>
	<string>MyApp</string>
	<key>NSAppTransportSecurity</key>
	<dict>
		<key>NSAllowsArbitraryLoads</key>
		<false/>
	</dict>
</dict>
</plist>
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings, "clean plist should produce no findings")
}

func TestAppleScannerNoAppleFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

func main() {}
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	assert.Empty(t, findings, "non-Apple project should produce no findings")
}

func TestAppleScannerContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
</dict>
</plist>
`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewAppleScanner()
	_, err := s.Scan(ctx, security.ScanTarget{RootDir: dir})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestAppleScannerPlistWithIntegerAndArray(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleVersion</key>
	<integer>42</integer>
	<key>UISupportedInterfaceOrientations</key>
	<array>
		<string>UIInterfaceOrientationPortrait</string>
	</array>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
</dict>
</plist>
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// No security findings expected, but the parser should handle integer and array types.
	assert.Empty(t, findings)
}

func TestAppleScannerPlistReadError(t *testing.T) {
	dir := t.TempDir()
	// Need at least one valid apple file so scanner doesn't return early.
	writeFile(t, dir, "AppDelegate.swift", `import UIKit
class AppDelegate {}
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{
		RootDir: dir,
		Files:   []string{"Missing.plist", "AppDelegate.swift"},
	})
	require.NoError(t, err)
	// Missing plist is skipped, no findings from clean swift file.
	assert.Empty(t, findings)
}

func TestAppleScannerPlistInvalidXML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Info.plist", `not valid xml at all {{{{`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// Invalid plist should be skipped, no findings.
	assert.Empty(t, findings)
}

func TestAppleScannerEntitlementInvalidXML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Bad.entitlements", `not valid xml {{{{`)
	writeFile(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
</dict>
</plist>
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// Invalid entitlements should be skipped.
	assert.Empty(t, findings)
}

func TestAppleScannerEntitlementNotTrue(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Safe.entitlements", `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>com.apple.security.cs.disable-library-validation</key>
	<false/>
</dict>
</plist>
`)
	// Need at least one apple file so the scanner processes the directory.
	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{
		RootDir: dir,
		Files:   []string{"Safe.entitlements"},
	})
	require.NoError(t, err)
	// Entitlement is set to false, so no findings.
	assert.Empty(t, findings)
}

func TestAppleScannerEntitlementReadError(t *testing.T) {
	dir := t.TempDir()
	// Provide an entitlements file that doesn't exist alongside a real plist.
	writeFile(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
</dict>
</plist>
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{
		RootDir: dir,
		Files:   []string{"Info.plist", "Missing.entitlements"},
	})
	require.NoError(t, err)
	// The missing entitlements file should be skipped, not cause error.
	// No ATS bypass in the plist, so no findings expected.
	assert.Empty(t, findings)
}

func TestAppleScannerSwiftFileReadError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
</dict>
</plist>
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{
		RootDir: dir,
		Files:   []string{"Info.plist", "Missing.swift"},
	})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestAppleScannerCleanSwiftFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
</dict>
</plist>
`)
	writeFile(t, dir, "AppDelegate.swift", `import UIKit

class AppDelegate: UIResponder, UIApplicationDelegate {
    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        return true
    }
}
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)
	// No sensitive UserDefaults usage, no ATS bypass.
	assert.Empty(t, findings)
}

func TestAppleScannerDetectsUserDefaultsSensitiveData(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
</dict>
</plist>
`)
	writeFile(t, dir, "Settings.swift", `import Foundation

class SettingsManager {
    func saveCredentials(password: String) {
        UserDefaults.standard.set(password, forKey: "password")
    }
}
`)

	s := NewAppleScanner()
	findings, err := s.Scan(context.Background(), security.ScanTarget{RootDir: dir})
	require.NoError(t, err)

	found := false
	for _, f := range findings {
		if f.Title == "Sensitive data stored in UserDefaults" {
			found = true
			assert.Equal(t, security.SeverityMedium, f.Severity)
			assert.Equal(t, security.CategoryDataExposure, f.Category)
		}
	}
	assert.True(t, found, "expected a UserDefaults sensitive data finding")
}
