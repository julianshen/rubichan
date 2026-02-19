// Package goplugin provides a Go plugin backend for the skill system. It loads
// native Go .so plugins using the plugin stdlib package and bridges the
// skillsdk.Context interface to the skill runtime's PermissionChecker.
//
// The backend uses a PluginLoader interface for testability: the real loader
// calls plugin.Open() and looks up the "NewSkill" symbol, while tests inject
// mock loaders that return fake SkillPlugin implementations.
package goplugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	goplugin "plugin"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/skillsdk"
)

// Safety limits for plugin context operations.
const (
	maxPluginReadFileSize = 10 << 20         // 10 MB max file size for ReadFile.
	pluginExecTimeout     = 30 * time.Second // 30s timeout for Exec commands.
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

// PluginLLMCompleter abstracts LLM completion for the Go plugin context.
// The method signature intentionally omits context.Context to match
// skillsdk.Context.Complete.
type PluginLLMCompleter interface {
	Complete(prompt string) (string, error)
}

// PluginHTTPFetcher abstracts HTTP fetching for the Go plugin context.
// The method signature intentionally omits context.Context to match
// skillsdk.Context.Fetch.
type PluginHTTPFetcher interface {
	Fetch(url string) (string, error)
}

// PluginGitRunner abstracts git operations for the Go plugin context.
// The method signatures intentionally omit context.Context to match
// skillsdk.Context.GitDiff/GitLog/GitStatus.
type PluginGitRunner interface {
	Diff(args ...string) (string, error)
	Log(args ...string) ([]skillsdk.GitCommit, error)
	Status() ([]skillsdk.GitFileStatus, error)
}

// PluginSkillInvoker abstracts cross-skill invocation for the Go plugin context.
// The method signature intentionally omits context.Context to match
// skillsdk.Context.InvokeSkill.
type PluginSkillInvoker interface {
	Invoke(name string, input map[string]any) (map[string]any, error)
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

// WithLLMCompleter sets the LLM completer for the plugin context.
func WithLLMCompleter(c PluginLLMCompleter) Option {
	return func(b *GoPluginBackend) { b.llmCompleter = c }
}

// WithHTTPFetcher sets the HTTP fetcher for the plugin context.
func WithHTTPFetcher(f PluginHTTPFetcher) Option {
	return func(b *GoPluginBackend) { b.httpFetcher = f }
}

// WithGitRunner sets the git runner for the plugin context.
func WithGitRunner(r PluginGitRunner) Option {
	return func(b *GoPluginBackend) { b.gitRunner = r }
}

// WithSkillInvoker sets the skill invoker for the plugin context.
func WithSkillInvoker(i PluginSkillInvoker) Option {
	return func(b *GoPluginBackend) { b.skillInvoker = i }
}

// GoPluginBackend implements skills.SkillBackend for native Go plugins.
// It uses a PluginLoader to load .so files, creates a pluginContext that
// bridges skillsdk.Context to the permission checker, and manages the
// plugin lifecycle (Activate/Deactivate).
type GoPluginBackend struct {
	loader       PluginLoader
	plugin       skillsdk.SkillPlugin
	tools        []tools.Tool
	hooks        map[skills.HookPhase]skills.HookHandler
	ctx          *pluginContext
	skillDir     string
	llmCompleter PluginLLMCompleter
	httpFetcher  PluginHTTPFetcher
	gitRunner    PluginGitRunner
	skillInvoker PluginSkillInvoker
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
	b.ctx.llmCompleter = b.llmCompleter
	b.ctx.httpFetcher = b.httpFetcher
	b.ctx.gitRunner = b.gitRunner
	b.ctx.skillInvoker = b.skillInvoker

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
	checker      skills.PermissionChecker
	skillDir     string
	llmCompleter PluginLLMCompleter
	httpFetcher  PluginHTTPFetcher
	gitRunner    PluginGitRunner
	skillInvoker PluginSkillInvoker
}

// newPluginContext creates a new pluginContext with the given permission checker.
func newPluginContext(checker skills.PermissionChecker, skillDir string) *pluginContext {
	return &pluginContext{
		checker:  checker,
		skillDir: skillDir,
	}
}

// resolveSandboxedPath resolves a path relative to the skill directory and
// validates it stays within the skill directory. Returns an error if the
// resolved path escapes the sandbox.
func (c *pluginContext) resolveSandboxedPath(path string) (string, error) {
	if c.skillDir == "" {
		return "", fmt.Errorf("skill directory not set; cannot sandbox path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.skillDir, path)
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	absSkillDir, err := filepath.Abs(c.skillDir)
	if err != nil {
		return "", fmt.Errorf("resolve skill dir: %w", err)
	}
	if !strings.HasPrefix(resolved, absSkillDir+string(filepath.Separator)) && resolved != absSkillDir {
		return "", fmt.Errorf("path %q escapes skill directory %q", path, absSkillDir)
	}
	return resolved, nil
}

// ReadFile reads the contents of a file. Requires file:read permission.
// Path is sandboxed to the skill directory and file size is limited.
func (c *pluginContext) ReadFile(path string) (string, error) {
	if err := c.checker.CheckPermission(skills.PermFileRead); err != nil {
		return "", fmt.Errorf("ReadFile: %w", err)
	}
	resolved, err := c.resolveSandboxedPath(path)
	if err != nil {
		return "", fmt.Errorf("ReadFile: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("ReadFile: %w", err)
	}
	if info.Size() > maxPluginReadFileSize {
		return "", fmt.Errorf("ReadFile: file %q exceeds maximum size (%d bytes)", path, maxPluginReadFileSize)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("ReadFile: %w", err)
	}
	return string(data), nil
}

// WriteFile writes content to a file. Requires file:write permission.
// Path is sandboxed to the skill directory.
func (c *pluginContext) WriteFile(path, content string) error {
	if err := c.checker.CheckPermission(skills.PermFileWrite); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}
	resolved, err := c.resolveSandboxedPath(path)
	if err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return fmt.Errorf("WriteFile: %w", err)
	}
	return nil
}

// ListDir lists directory entries. Requires file:read permission.
// Path is sandboxed to the skill directory.
func (c *pluginContext) ListDir(path string) ([]skillsdk.FileInfo, error) {
	if err := c.checker.CheckPermission(skills.PermFileRead); err != nil {
		return nil, fmt.Errorf("ListDir: %w", err)
	}
	resolved, err := c.resolveSandboxedPath(path)
	if err != nil {
		return nil, fmt.Errorf("ListDir: %w", err)
	}
	entries, err := os.ReadDir(resolved)
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
// Pattern is resolved relative to the skill directory and results are filtered
// to only include paths within the skill directory.
func (c *pluginContext) SearchFiles(pattern string) ([]string, error) {
	if err := c.checker.CheckPermission(skills.PermFileRead); err != nil {
		return nil, fmt.Errorf("SearchFiles: %w", err)
	}
	if !filepath.IsAbs(pattern) && c.skillDir != "" {
		pattern = filepath.Join(c.skillDir, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("SearchFiles: %w", err)
	}
	if c.skillDir == "" {
		return matches, nil
	}
	absSkillDir, _ := filepath.Abs(c.skillDir)
	var filtered []string
	for _, m := range matches {
		absM, _ := filepath.Abs(m)
		if strings.HasPrefix(absM, absSkillDir+string(filepath.Separator)) || absM == absSkillDir {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

// Exec runs an external command. Requires shell:exec permission.
// Enforces a timeout to prevent runaway processes.
func (c *pluginContext) Exec(command string, args ...string) (skillsdk.ExecResult, error) {
	if err := c.checker.CheckPermission(skills.PermShellExec); err != nil {
		return skillsdk.ExecResult{}, fmt.Errorf("Exec: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), pluginExecTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
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
func (c *pluginContext) Complete(prompt string) (string, error) {
	if err := c.checker.CheckPermission(skills.PermLLMCall); err != nil {
		return "", fmt.Errorf("Complete: %w", err)
	}
	if c.llmCompleter == nil {
		return "", fmt.Errorf("Complete: LLM completer not configured")
	}
	return c.llmCompleter.Complete(prompt)
}

// Fetch retrieves a URL's content. Requires net:fetch permission.
func (c *pluginContext) Fetch(url string) (string, error) {
	if err := c.checker.CheckPermission(skills.PermNetFetch); err != nil {
		return "", fmt.Errorf("Fetch: %w", err)
	}
	if c.httpFetcher == nil {
		return "", fmt.Errorf("Fetch: HTTP fetcher not configured")
	}
	return c.httpFetcher.Fetch(url)
}

// GitDiff runs git diff. Requires git:read permission.
func (c *pluginContext) GitDiff(args ...string) (string, error) {
	if err := c.checker.CheckPermission(skills.PermGitRead); err != nil {
		return "", fmt.Errorf("GitDiff: %w", err)
	}
	if c.gitRunner == nil {
		return "", fmt.Errorf("GitDiff: git runner not configured")
	}
	return c.gitRunner.Diff(args...)
}

// GitLog runs git log. Requires git:read permission.
func (c *pluginContext) GitLog(args ...string) ([]skillsdk.GitCommit, error) {
	if err := c.checker.CheckPermission(skills.PermGitRead); err != nil {
		return nil, fmt.Errorf("GitLog: %w", err)
	}
	if c.gitRunner == nil {
		return nil, fmt.Errorf("GitLog: git runner not configured")
	}
	return c.gitRunner.Log(args...)
}

// GitStatus returns git working tree status. Requires git:read permission.
func (c *pluginContext) GitStatus() ([]skillsdk.GitFileStatus, error) {
	if err := c.checker.CheckPermission(skills.PermGitRead); err != nil {
		return nil, fmt.Errorf("GitStatus: %w", err)
	}
	if c.gitRunner == nil {
		return nil, fmt.Errorf("GitStatus: git runner not configured")
	}
	return c.gitRunner.Status()
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
func (c *pluginContext) InvokeSkill(name string, input map[string]any) (map[string]any, error) {
	if err := c.checker.CheckPermission(skills.PermSkillInvoke); err != nil {
		return nil, fmt.Errorf("InvokeSkill: %w", err)
	}
	if c.skillInvoker == nil {
		return nil, fmt.Errorf("InvokeSkill: skill invoker not configured")
	}
	return c.skillInvoker.Invoke(name, input)
}

// writeFileHelper is a helper for tests to write files.
func writeFileHelper(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
