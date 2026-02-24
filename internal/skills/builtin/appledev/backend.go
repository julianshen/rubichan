package appledev

import (
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tools/xcode"
)

// Manifest returns the skill manifest for apple-dev.
func Manifest() skills.SkillManifest {
	return skills.SkillManifest{
		Name:        "apple-dev",
		Version:     "1.0.0",
		Description: "Xcode CLI tools, Swift/iOS best practices, and Apple platform security scanning",
		Types:       []skills.SkillType{skills.SkillTypeTool, skills.SkillTypePrompt},
	}
}

// Backend implements skills.SkillBackend for apple-dev.
type Backend struct {
	WorkDir  string
	Platform xcode.PlatformChecker
	tools    []tools.Tool
}

// Load creates all Xcode tools, filtered by platform.
// Cross-platform tools (SPM, discover) are always registered.
// Darwin-only tools (xcodebuild, simctl, codesign, xcrun) are only registered on macOS.
func (b *Backend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	// Cross-platform tools (always registered)
	b.tools = append(b.tools, xcode.NewDiscoverTool(b.WorkDir))
	b.tools = append(b.tools, xcode.NewSwiftBuildTool(b.WorkDir))
	b.tools = append(b.tools, xcode.NewSwiftTestTool(b.WorkDir))
	b.tools = append(b.tools, xcode.NewSwiftResolveTool(b.WorkDir))
	b.tools = append(b.tools, xcode.NewSwiftAddDepTool(b.WorkDir))

	// Darwin-only tools
	if b.Platform.IsDarwin() {
		b.tools = append(b.tools, xcode.NewXcodeBuildTool(b.WorkDir, b.Platform))
		b.tools = append(b.tools, xcode.NewXcodeTestTool(b.WorkDir, b.Platform))
		b.tools = append(b.tools, xcode.NewXcodeArchiveTool(b.WorkDir, b.Platform))
		b.tools = append(b.tools, xcode.NewXcodeCleanTool(b.WorkDir, b.Platform))
		b.tools = append(b.tools, xcode.NewSimListTool(b.Platform))
		b.tools = append(b.tools, xcode.NewSimBootTool(b.Platform))
		b.tools = append(b.tools, xcode.NewSimShutdownTool(b.Platform))
		b.tools = append(b.tools, xcode.NewSimInstallTool(b.Platform))
		b.tools = append(b.tools, xcode.NewSimLaunchTool(b.Platform))
		b.tools = append(b.tools, xcode.NewSimScreenshotTool(b.Platform))
		b.tools = append(b.tools, xcode.NewCodesignInfoTool(b.Platform))
		b.tools = append(b.tools, xcode.NewCodesignVerifyTool(b.Platform))
		b.tools = append(b.tools, xcode.NewXcrunTool(b.Platform))
	}
	return nil
}

// Tools returns all registered tools.
func (b *Backend) Tools() []tools.Tool {
	return b.tools
}

// Hooks returns nil — apple-dev does not register any hooks.
func (b *Backend) Hooks() map[skills.HookPhase]skills.HookHandler {
	return nil
}

// Unload is a no-op for apple-dev.
func (b *Backend) Unload() error {
	return nil
}
