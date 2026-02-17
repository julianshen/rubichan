// Package goplugin provides a Go plugin backend for the skill system. It loads
// native Go .so plugins using the plugin stdlib package and bridges the
// skillsdk.Context interface to the skill runtime's PermissionChecker.
//
// The backend uses a PluginLoader interface for testability: the real loader
// calls plugin.Open() and looks up the "NewSkill" symbol, while tests inject
// mock loaders that return fake SkillPlugin implementations.
package goplugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goplugin "plugin"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/skillsdk"
)

// PluginLoader abstracts the loading of Go plugins. The real implementation
// uses plugin.Open() and symbol lookup; tests can substitute a mock.
type PluginLoader interface {
	// Load opens the plugin at the given path and returns the SkillPlugin
	// created by the exported NewSkill() function.
	Load(path string) (skillsdk.SkillPlugin, error)
}

// ToolRegistrar allows plugins to register tools during Activate.
type ToolRegistrar interface {
	RegisterTool(tool tools.Tool)
}

// HookRegistrar allows plugins to register hooks during Activate.
type HookRegistrar interface {
	RegisterHook(phase skills.HookPhase, handler skills.HookHandler)
}

// Option configures a GoPluginBackend.
type Option func(*GoPluginBackend)

// WithPluginLoader sets the PluginLoader used to load .so files.
func WithPluginLoader(loader PluginLoader) Option {
	return func(b *GoPluginBackend) {
		b.loader = loader
	}
}

// WithSkillDir sets the skill directory for the plugin context.
func WithSkillDir(dir string) Option {
	return func(b *GoPluginBackend) {
		b.skillDir = dir
	}
}

// GoPluginBackend implements skills.SkillBackend for native Go plugins.
// It uses a PluginLoader to load .so files, creates a pluginContext that
// bridges skillsdk.Context to the permission checker, and manages the
// plugin lifecycle (Activate/Deactivate).
type GoPluginBackend struct {
	loader   PluginLoader
	plugin   skillsdk.SkillPlugin
	tools    []tools.Tool
	hooks    map[skills.HookPhase]skills.HookHandler
	ctx      *pluginContext
	skillDir string
}

// compile-time check: GoPluginBackend implements skills.SkillBackend.
var _ skills.SkillBackend = (*GoPluginBackend)(nil)

// compile-time check: GoPluginBackend implements ToolRegistrar.
var _ ToolRegistrar = (*GoPluginBackend)(nil)

// compile-time check: GoPluginBackend implements HookRegistrar.
var _ HookRegistrar = (*GoPluginBackend)(nil)

// NewGoPluginBackend creates a new Go plugin backend with the given options.
// If no PluginLoader is provided, the system loader (using plugin.Open) is used.
func NewGoPluginBackend(opts ...Option) *GoPluginBackend {
	b := &GoPluginBackend{
		hooks: make(map[skills.HookPhase]skills.HookHandler),
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.loader == nil {
		b.loader = &systemPluginLoader{}
	}
	return b
}

// Load implements skills.SkillBackend. It loads the .so plugin using the
// configured PluginLoader, creates a pluginContext that bridges
// skillsdk.Context to the permission checker, and calls Activate on the plugin.
func (b *GoPluginBackend) Load(manifest skills.SkillManifest, checker skills.PermissionChecker) error {
	entrypoint := manifest.Implementation.Entrypoint
	if entrypoint == "" {
		return fmt.Errorf("load plugin: entrypoint is required")
	}

	plugin, err := b.loader.Load(entrypoint)
	if err != nil {
		return fmt.Errorf("load plugin %q: %w", entrypoint, err)
	}

	b.plugin = plugin
	b.ctx = newPluginContext(checker, b.skillDir)

	if err := b.plugin.Activate(b.ctx); err != nil {
		return fmt.Errorf("activate plugin %q: %w", manifest.Name, err)
	}

	return nil
}

// Tools implements skills.SkillBackend. Returns tools registered by the plugin.
func (b *GoPluginBackend) Tools() []tools.Tool {
	return b.tools
}

// Hooks implements skills.SkillBackend. Returns hooks registered by the plugin.
func (b *GoPluginBackend) Hooks() map[skills.HookPhase]skills.HookHandler {
	return b.hooks
}

// Unload implements skills.SkillBackend. Calls Deactivate on the plugin
// and releases all resources.
func (b *GoPluginBackend) Unload() error {
	if b.plugin == nil {
		return nil
	}

	if err := b.plugin.Deactivate(b.ctx); err != nil {
		return fmt.Errorf("deactivate plugin: %w", err)
	}

	b.plugin = nil
	b.ctx = nil
	b.tools = nil
	b.hooks = make(map[skills.HookPhase]skills.HookHandler)

	return nil
}

// RegisterTool implements ToolRegistrar. Plugins call this during Activate
// to register tools with the backend.
func (b *GoPluginBackend) RegisterTool(tool tools.Tool) {
	b.tools = append(b.tools, tool)
}

// RegisterHook implements HookRegistrar. Plugins call this during Activate
// to register hook handlers with the backend.
func (b *GoPluginBackend) RegisterHook(phase skills.HookPhase, handler skills.HookHandler) {
	b.hooks[phase] = handler
}

// --- systemPluginLoader ---

// systemPluginLoader uses the real plugin.Open() to load .so files.
// It looks up the "NewSkill" symbol and casts it to func() skillsdk.SkillPlugin.
type systemPluginLoader struct{}

// Load opens the plugin at the given path using plugin.Open, looks up the
// "NewSkill" symbol, and calls it to create a SkillPlugin.
func (s *systemPluginLoader) Load(path string) (skillsdk.SkillPlugin, error) {
	p, err := goplugin.Open(path)
	if err != nil {
		return nil, fmt.Errorf("plugin.Open %q: %w", path, err)
	}

	sym, err := p.Lookup("NewSkill")
	if err != nil {
		return nil, fmt.Errorf("plugin does not export symbol \"NewSkill\": %w", err)
	}

	newSkillFn, ok := sym.(func() skillsdk.SkillPlugin)
	if !ok {
		return nil, fmt.Errorf("symbol NewSkill has wrong type %T, expected func() skillsdk.SkillPlugin", sym)
	}

	return newSkillFn(), nil
}

// --- pluginContext ---

// pluginContext implements skillsdk.Context by bridging to real system operations
// while checking permissions via the PermissionChecker.
type pluginContext struct {
	checker  skills.PermissionChecker
	skillDir string
}

// newPluginContext creates a new pluginContext with the given permission checker.
func newPluginContext(checker skills.PermissionChecker, skillDir string) *pluginContext {
	return &pluginContext{
		checker:  checker,
		skillDir: skillDir,
	}
}

// ReadFile reads the contents of a file. Requires file:read permission.
func (c *pluginContext) ReadFile(path string) (string, error) {
	if err := c.checker.CheckPermission(skills.PermFileRead); err != nil {
		return "", fmt.Errorf("ReadFile: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("ReadFile: %w", err)
	}
	return string(data), nil
}

// WriteFile writes content to a file. Requires file:write permission.
func (c *pluginContext) WriteFile(path, content string) error {
	if err := c.checker.CheckPermission(skills.PermFileWrite); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}
	return nil
}

// ListDir lists directory entries. Requires file:read permission.
func (c *pluginContext) ListDir(path string) ([]skillsdk.FileInfo, error) {
	if err := c.checker.CheckPermission(skills.PermFileRead); err != nil {
		return nil, fmt.Errorf("ListDir: %w", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("ListDir: %w", err)
	}
	result := make([]skillsdk.FileInfo, len(entries))
	for i, e := range entries {
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("ListDir info %q: %w", e.Name(), err)
		}
		result[i] = skillsdk.FileInfo{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  info.Size(),
		}
	}
	return result, nil
}

// SearchFiles finds files matching a glob pattern. Requires file:read permission.
func (c *pluginContext) SearchFiles(pattern string) ([]string, error) {
	if err := c.checker.CheckPermission(skills.PermFileRead); err != nil {
		return nil, fmt.Errorf("SearchFiles: %w", err)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("SearchFiles: %w", err)
	}
	return matches, nil
}

// Exec runs an external command. Requires shell:exec permission.
func (c *pluginContext) Exec(command string, args ...string) (skillsdk.ExecResult, error) {
	if err := c.checker.CheckPermission(skills.PermShellExec); err != nil {
		return skillsdk.ExecResult{}, fmt.Errorf("Exec: %w", err)
	}
	cmd := exec.Command(command, args...)
	stdout, err := cmd.Output()

	result := skillsdk.ExecResult{}
	result.Stdout = string(stdout)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			result.Stderr = string(exitErr.Stderr)
		} else {
			return result, fmt.Errorf("Exec: %w", err)
		}
	}

	return result, nil
}

// Complete sends a prompt to the LLM. Requires llm:call permission.
// NOTE: LLM integration is wired in Task 18; this returns an error for now.
func (c *pluginContext) Complete(prompt string) (string, error) {
	if err := c.checker.CheckPermission(skills.PermLLMCall); err != nil {
		return "", fmt.Errorf("Complete: %w", err)
	}
	return "", fmt.Errorf("Complete: LLM completer not configured (wired in Task 18)")
}

// Fetch retrieves a URL's content. Requires net:fetch permission.
// NOTE: HTTP integration is wired in Task 18; this returns an error for now.
func (c *pluginContext) Fetch(url string) (string, error) {
	if err := c.checker.CheckPermission(skills.PermNetFetch); err != nil {
		return "", fmt.Errorf("Fetch: %w", err)
	}
	return "", fmt.Errorf("Fetch: HTTP fetcher not configured (wired in Task 18)")
}

// GitDiff runs git diff. Requires git:read permission.
func (c *pluginContext) GitDiff(args ...string) (string, error) {
	if err := c.checker.CheckPermission(skills.PermGitRead); err != nil {
		return "", fmt.Errorf("GitDiff: %w", err)
	}
	return "", fmt.Errorf("GitDiff: git runner not configured (wired in Task 18)")
}

// GitLog runs git log. Requires git:read permission.
func (c *pluginContext) GitLog(args ...string) ([]skillsdk.GitCommit, error) {
	if err := c.checker.CheckPermission(skills.PermGitRead); err != nil {
		return nil, fmt.Errorf("GitLog: %w", err)
	}
	return nil, fmt.Errorf("GitLog: git runner not configured (wired in Task 18)")
}

// GitStatus returns git working tree status. Requires git:read permission.
func (c *pluginContext) GitStatus() ([]skillsdk.GitFileStatus, error) {
	if err := c.checker.CheckPermission(skills.PermGitRead); err != nil {
		return nil, fmt.Errorf("GitStatus: %w", err)
	}
	return nil, fmt.Errorf("GitStatus: git runner not configured (wired in Task 18)")
}

// GetEnv returns an environment variable. Requires env:read permission.
func (c *pluginContext) GetEnv(key string) string {
	if err := c.checker.CheckPermission(skills.PermEnvRead); err != nil {
		return ""
	}
	return os.Getenv(key)
}

// ProjectRoot returns the skill directory path. No permission required.
func (c *pluginContext) ProjectRoot() string {
	return c.skillDir
}

// InvokeSkill calls another skill by name. Requires skill:invoke permission.
// NOTE: Cross-skill invocation is wired in Task 18; this returns an error for now.
func (c *pluginContext) InvokeSkill(name string, input map[string]any) (map[string]any, error) {
	if err := c.checker.CheckPermission(skills.PermSkillInvoke); err != nil {
		return nil, fmt.Errorf("InvokeSkill: %w", err)
	}
	return nil, fmt.Errorf("InvokeSkill: skill invoker not configured (wired in Task 18)")
}

// writeFileHelper is a helper for tests to write files.
func writeFileHelper(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
