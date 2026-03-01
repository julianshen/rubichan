package builtin

import (
	"time"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
)

// defaultShellTimeout is the timeout applied to the shell tool.
const defaultShellTimeout = 30 * time.Second

// CoreToolsManifest returns the skill manifest for the built-in core-tools
// skill. The manifest is constructed directly (not parsed from YAML) because
// built-in skills have no on-disk representation.
func CoreToolsManifest() skills.SkillManifest {
	return skills.SkillManifest{
		Name:        "core-tools",
		Version:     "1.0.0",
		Description: "Built-in file and shell tools for the agent",
		Types:       []skills.SkillType{skills.SkillTypeTool},
	}
}

// CoreToolsBackend implements skills.SkillBackend for the built-in core-tools
// skill. It wraps the existing file and shell tools.
type CoreToolsBackend struct {
	// WorkDir is the working directory passed to file and shell tools.
	WorkDir string

	tools []tools.Tool
}

// Load creates the file and shell tools using the configured working directory.
func (b *CoreToolsBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	b.tools = []tools.Tool{
		tools.NewFileTool(b.WorkDir),
		tools.NewShellTool(b.WorkDir, defaultShellTimeout),
	}
	return nil
}

// Tools returns the file and shell tools created during Load.
func (b *CoreToolsBackend) Tools() []tools.Tool {
	return b.tools
}

// Hooks returns an empty map — core-tools does not register any hooks.
func (b *CoreToolsBackend) Hooks() map[skills.HookPhase]skills.HookHandler {
	return nil
}

// Commands returns nil — core-tools does not provide slash commands.
func (b *CoreToolsBackend) Commands() []commands.SlashCommand { return nil }

// Unload is a no-op for core-tools.
func (b *CoreToolsBackend) Unload() error {
	return nil
}
