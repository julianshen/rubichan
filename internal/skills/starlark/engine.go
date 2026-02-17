// Package starlark provides a Starlark-based skill backend. It embeds the
// go.starlark.net interpreter to run .star files in a sandboxed environment.
// Each skill gets its own Starlark thread with fresh global scope.
package starlark

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"

	starlib "go.starlark.net/starlark"
)

// LLMCompleter abstracts LLM completion for the llm_complete() built-in.
// Real implementations will be wired in Task 18; tests use mocks.
type LLMCompleter interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// HTTPFetcher abstracts HTTP fetching for the fetch() built-in.
// Real implementations will be wired in Task 18; tests use mocks.
type HTTPFetcher interface {
	Fetch(ctx context.Context, url string) (string, error)
}

// GitLogEntry represents a single git log entry returned by GitRunner.Log.
type GitLogEntry struct {
	Hash    string
	Author  string
	Message string
}

// GitStatusEntry represents a single file status returned by GitRunner.Status.
type GitStatusEntry struct {
	Path   string
	Status string
}

// GitRunner abstracts git operations for git_diff, git_log, git_status built-ins.
// Real implementations will be wired in Task 18; tests use mocks.
type GitRunner interface {
	Diff(ctx context.Context, args ...string) (string, error)
	Log(ctx context.Context, args ...string) ([]GitLogEntry, error)
	Status(ctx context.Context) ([]GitStatusEntry, error)
}

// SkillInvoker abstracts cross-skill invocation for the invoke_skill() built-in.
// Real implementations will be wired in Task 18; tests use mocks.
type SkillInvoker interface {
	Invoke(ctx context.Context, name string, input map[string]any) (map[string]any, error)
}

// WorkflowHandler is a function that executes a workflow with the given input
// and returns a string result. Starlark workflow handlers are wrapped into this type.
type WorkflowHandler func(ctx context.Context, input map[string]any) (string, error)

// ScannerHandler is a function that scans content and returns a list of findings.
// Starlark scanner handlers are wrapped into this type.
type ScannerHandler func(ctx context.Context, content string) ([]string, error)

// hookPhaseNames maps Starlark phase name strings to their HookPhase constants.
var hookPhaseNames = map[string]skills.HookPhase{
	"OnActivate":             skills.HookOnActivate,
	"OnDeactivate":           skills.HookOnDeactivate,
	"OnConversationStart":    skills.HookOnConversationStart,
	"OnBeforePromptBuild":    skills.HookOnBeforePromptBuild,
	"OnBeforeToolCall":       skills.HookOnBeforeToolCall,
	"OnAfterToolResult":      skills.HookOnAfterToolResult,
	"OnAfterResponse":        skills.HookOnAfterResponse,
	"OnBeforeWikiSection":    skills.HookOnBeforeWikiSection,
	"OnSecurityScanComplete": skills.HookOnSecurityScanComplete,
}

// Engine implements skills.SkillBackend using the go.starlark.net interpreter.
// Each Engine instance gets its own Starlark thread with a fresh global scope
// and injected SDK builtins (register_tool, register_hook, log).
type Engine struct {
	skillName    string
	skillDir     string
	checker      skills.PermissionChecker
	thread       *starlib.Thread
	globals      starlib.StringDict
	tools        []tools.Tool
	hooks        map[skills.HookPhase]skills.HookHandler
	workflows    map[string]WorkflowHandler
	scanners     map[string]ScannerHandler
	llmCompleter LLMCompleter
	httpFetcher  HTTPFetcher
	gitRunner    GitRunner
	skillInvoker SkillInvoker
}

// compile-time check: Engine implements skills.SkillBackend.
var _ skills.SkillBackend = (*Engine)(nil)

// NewEngine creates a new Starlark engine for the given skill. The skillDir
// is the directory containing the .star files. The checker is used for
// permission and rate-limit enforcement during execution.
func NewEngine(skillName, skillDir string, checker skills.PermissionChecker) *Engine {
	return &Engine{
		skillName: skillName,
		skillDir:  skillDir,
		checker:   checker,
		hooks:     make(map[skills.HookPhase]skills.HookHandler),
		workflows: make(map[string]WorkflowHandler),
		scanners:  make(map[string]ScannerHandler),
	}
}

// SetLLMCompleter sets the LLM completer used by the llm_complete() built-in.
func (e *Engine) SetLLMCompleter(c LLMCompleter) { e.llmCompleter = c }

// SetHTTPFetcher sets the HTTP fetcher used by the fetch() built-in.
func (e *Engine) SetHTTPFetcher(f HTTPFetcher) { e.httpFetcher = f }

// SetGitRunner sets the git runner used by git_diff, git_log, git_status built-ins.
func (e *Engine) SetGitRunner(r GitRunner) { e.gitRunner = r }

// SetSkillInvoker sets the skill invoker used by the invoke_skill() built-in.
func (e *Engine) SetSkillInvoker(i SkillInvoker) { e.skillInvoker = i }

// Load reads and executes the entrypoint .star file from the manifest. It
// injects the SDK builtins (register_tool, register_hook, log) into the
// Starlark global scope before execution.
func (e *Engine) Load(manifest skills.SkillManifest, checker skills.PermissionChecker) error {
	e.checker = checker

	entrypoint := manifest.Implementation.Entrypoint
	if entrypoint == "" {
		entrypoint = "main.star"
	}

	path := filepath.Join(e.skillDir, entrypoint)
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read starlark entrypoint %q: %w", path, err)
	}

	e.thread = &starlib.Thread{
		Name: e.skillName,
	}

	// Build the predeclared builtins available to all .star files.
	predeclared := starlib.StringDict{
		"register_tool":     starlib.NewBuiltin("register_tool", e.builtinRegisterTool),
		"register_hook":     starlib.NewBuiltin("register_hook", e.builtinRegisterHook),
		"log":               starlib.NewBuiltin("log", e.builtinLog),
		"read_file":         starlib.NewBuiltin("read_file", e.builtinReadFile),
		"write_file":        starlib.NewBuiltin("write_file", e.builtinWriteFile),
		"list_dir":          starlib.NewBuiltin("list_dir", e.builtinListDir),
		"search_files":      starlib.NewBuiltin("search_files", e.builtinSearchFiles),
		"exec":              starlib.NewBuiltin("exec", e.builtinExec),
		"env":               starlib.NewBuiltin("env", e.builtinEnv),
		"project_root":      starlib.NewBuiltin("project_root", e.builtinProjectRoot),
		"llm_complete":      starlib.NewBuiltin("llm_complete", e.builtinLLMComplete),
		"fetch":             starlib.NewBuiltin("fetch", e.builtinFetch),
		"git_diff":          starlib.NewBuiltin("git_diff", e.builtinGitDiff),
		"git_log":           starlib.NewBuiltin("git_log", e.builtinGitLog),
		"git_status":        starlib.NewBuiltin("git_status", e.builtinGitStatus),
		"invoke_skill":      starlib.NewBuiltin("invoke_skill", e.builtinInvokeSkill),
		"register_workflow": starlib.NewBuiltin("register_workflow", e.builtinRegisterWorkflow),
		"register_scanner":  starlib.NewBuiltin("register_scanner", e.builtinRegisterScanner),
	}

	globals, err := starlib.ExecFile(
		e.thread,
		path,
		src,
		predeclared,
	)
	if err != nil {
		return fmt.Errorf("execute starlark %q: %w", path, err)
	}

	e.globals = globals
	return nil
}

// Tools returns the tools registered by this skill via register_tool() calls.
func (e *Engine) Tools() []tools.Tool {
	return e.tools
}

// Hooks returns hook handlers registered by this skill, keyed by phase.
// This is populated by register_hook() calls in the Starlark code.
func (e *Engine) Hooks() map[skills.HookPhase]skills.HookHandler {
	return e.hooks
}

// Workflows returns workflow handlers registered by this skill, keyed by name.
// This is populated by register_workflow() calls in the Starlark code.
func (e *Engine) Workflows() map[string]WorkflowHandler {
	return e.workflows
}

// Scanners returns scanner handlers registered by this skill, keyed by name.
// This is populated by register_scanner() calls in the Starlark code.
func (e *Engine) Scanners() map[string]ScannerHandler {
	return e.scanners
}

// Checker returns the engine's permission checker. This is used by tests
// to pass the checker back into Load().
func (e *Engine) Checker() skills.PermissionChecker {
	return e.checker
}

// Global returns the value of a Starlark global variable by name. For
// String values, the raw Go string is returned. For Int values, the int64
// is returned. For other types, the Starlark string representation is
// returned. Returns nil if the variable is not set.
func (e *Engine) Global(name string) any {
	v, ok := e.globals[name]
	if !ok {
		return nil
	}
	if s, ok := v.(starlib.String); ok {
		return string(s)
	}
	if i, ok := v.(starlib.Int); ok {
		if i64, ok := i.Int64(); ok {
			return i64
		}
	}
	return v.String()
}

// Unload releases resources held by the engine.
func (e *Engine) Unload() error {
	e.thread = nil
	e.globals = nil
	e.tools = nil
	e.hooks = make(map[skills.HookPhase]skills.HookHandler)
	e.workflows = make(map[string]WorkflowHandler)
	e.scanners = make(map[string]ScannerHandler)
	return nil
}

// builtinRegisterTool implements the register_tool(name, description, handler) builtin.
// It creates a starlarkTool wrapper and adds it to the engine's tool list.
func (e *Engine) builtinRegisterTool(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var name string
	var description string
	var handler starlib.Callable

	if err := starlib.UnpackArgs(fn.Name(), args, kwargs,
		"name", &name,
		"description", &description,
		"handler", &handler,
	); err != nil {
		return nil, err
	}

	tool := &starlarkTool{
		name:        name,
		description: description,
		handler:     handler,
		thread:      thread,
	}

	e.tools = append(e.tools, tool)
	return starlib.None, nil
}

// builtinRegisterHook implements register_hook(phase, handler). It validates
// the phase name against known HookPhase values and wraps the Starlark callable
// into a skills.HookHandler that converts HookEvent to Starlark args and
// HookResult back.
func (e *Engine) builtinRegisterHook(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var phaseName string
	var handler starlib.Callable

	if err := starlib.UnpackArgs(fn.Name(), args, kwargs,
		"phase", &phaseName,
		"handler", &handler,
	); err != nil {
		return nil, err
	}

	phase, ok := hookPhaseNames[phaseName]
	if !ok {
		return nil, fmt.Errorf("register_hook: unknown hook phase %q", phaseName)
	}

	// Capture the thread and handler in a closure that implements HookHandler.
	capturedThread := thread
	capturedHandler := handler

	e.hooks[phase] = func(event skills.HookEvent) (skills.HookResult, error) {
		// Convert HookEvent to a Starlark dict.
		eventDict := starlib.NewDict(4)

		_ = eventDict.SetKey(starlib.String("phase"), starlib.String(event.Phase.String()))
		_ = eventDict.SetKey(starlib.String("skill_name"), starlib.String(event.SkillName))

		dataDict, err := goMapToStarlarkDict(event.Data)
		if err != nil {
			return skills.HookResult{}, fmt.Errorf("convert hook event data: %w", err)
		}
		_ = eventDict.SetKey(starlib.String("data"), dataDict)

		// Call the Starlark handler with the event dict.
		result, err := starlib.Call(capturedThread, capturedHandler, starlib.Tuple{eventDict}, nil)
		if err != nil {
			return skills.HookResult{}, fmt.Errorf("call hook handler: %w", err)
		}

		// Convert the Starlark return value to a HookResult.
		resultDict, ok := result.(*starlib.Dict)
		if !ok {
			return skills.HookResult{}, fmt.Errorf("hook handler must return a dict, got %s", result.Type())
		}

		hookResult := skills.HookResult{}

		// Extract "modified" field.
		if modVal, found, _ := resultDict.Get(starlib.String("modified")); found {
			if modDict, ok := modVal.(*starlib.Dict); ok {
				goMap := make(map[string]any)
				for _, item := range modDict.Items() {
					key, ok := starlib.AsString(item[0])
					if !ok {
						key = item[0].String()
					}
					goMap[key] = starlarkValueToGo(item[1])
				}
				hookResult.Modified = goMap
			}
		}

		// Extract "cancel" field.
		if cancelVal, found, _ := resultDict.Get(starlib.String("cancel")); found {
			if b, ok := cancelVal.(starlib.Bool); ok {
				hookResult.Cancel = bool(b)
			}
		}

		return hookResult, nil
	}

	return starlib.None, nil
}

// builtinRegisterWorkflow implements register_workflow(name, handler). It stores
// the Starlark callable, exposed via the Workflows() method. The handler receives
// an input dict and returns a string result.
func (e *Engine) builtinRegisterWorkflow(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var name string
	var handler starlib.Callable

	if err := starlib.UnpackArgs(fn.Name(), args, kwargs,
		"name", &name,
		"handler", &handler,
	); err != nil {
		return nil, err
	}

	capturedThread := thread
	capturedHandler := handler

	e.workflows[name] = func(_ context.Context, input map[string]any) (string, error) {
		starDict, err := goMapToStarlarkDict(input)
		if err != nil {
			return "", fmt.Errorf("convert workflow input: %w", err)
		}

		result, err := starlib.Call(capturedThread, capturedHandler, starlib.Tuple{starDict}, nil)
		if err != nil {
			return "", fmt.Errorf("call workflow handler %q: %w", name, err)
		}

		return starlarkValueToString(result), nil
	}

	return starlib.None, nil
}

// builtinRegisterScanner implements register_scanner(name, handler). It stores
// the Starlark callable, exposed via the Scanners() method. The handler receives
// a content string and returns a list of finding strings.
func (e *Engine) builtinRegisterScanner(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var name string
	var handler starlib.Callable

	if err := starlib.UnpackArgs(fn.Name(), args, kwargs,
		"name", &name,
		"handler", &handler,
	); err != nil {
		return nil, err
	}

	capturedThread := thread
	capturedHandler := handler

	e.scanners[name] = func(_ context.Context, content string) ([]string, error) {
		result, err := starlib.Call(capturedThread, capturedHandler, starlib.Tuple{starlib.String(content)}, nil)
		if err != nil {
			return nil, fmt.Errorf("call scanner handler %q: %w", name, err)
		}

		// Convert the Starlark list to a Go string slice.
		resultList, ok := result.(*starlib.List)
		if !ok {
			return nil, fmt.Errorf("scanner handler %q must return a list, got %s", name, result.Type())
		}

		findings := make([]string, resultList.Len())
		for i := range resultList.Len() {
			findings[i] = starlarkValueToString(resultList.Index(i))
		}

		return findings, nil
	}

	return starlib.None, nil
}

// builtinLog implements the log(message) builtin, writing to Go's standard logger.
func (e *Engine) builtinLog(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var msg string
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &msg); err != nil {
		return nil, err
	}
	log.Printf("[skill:%s] %s", e.skillName, msg)
	return starlib.None, nil
}

// starlarkTool wraps a Starlark callable as a tools.Tool. When Execute is
// called, it converts the Go input JSON to a Starlark dict, calls the handler,
// and converts the return value back to a Go string.
type starlarkTool struct {
	name        string
	description string
	handler     starlib.Callable
	thread      *starlib.Thread
}

// compile-time check: starlarkTool implements tools.Tool.
var _ tools.Tool = (*starlarkTool)(nil)

func (st *starlarkTool) Name() string        { return st.name }
func (st *starlarkTool) Description() string { return st.description }

// InputSchema returns a generic JSON schema accepting any object.
func (st *starlarkTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}

// Execute calls the Starlark handler with the given JSON input. The input
// is unmarshalled into a map[string]any, converted to a Starlark dict, and
// passed as the single positional argument. The handler's return value is
// converted to a string for the ToolResult.
func (st *starlarkTool) Execute(_ context.Context, input json.RawMessage) (tools.ToolResult, error) {
	// Unmarshal JSON input into a Go map.
	var goMap map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &goMap); err != nil {
			return tools.ToolResult{IsError: true, Content: err.Error()}, fmt.Errorf("unmarshal input: %w", err)
		}
	}
	if goMap == nil {
		goMap = make(map[string]any)
	}

	// Convert Go map to Starlark dict.
	starDict, err := goMapToStarlarkDict(goMap)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: err.Error()}, fmt.Errorf("convert input to starlark: %w", err)
	}

	// Call the Starlark handler.
	result, err := starlib.Call(st.thread, st.handler, starlib.Tuple{starDict}, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: err.Error()}, fmt.Errorf("call starlark handler %q: %w", st.name, err)
	}

	// Convert the return value to a Go string.
	content := starlarkValueToString(result)
	return tools.ToolResult{Content: content}, nil
}

// goMapToStarlarkDict converts a Go map[string]any to a Starlark *Dict.
func goMapToStarlarkDict(m map[string]any) (*starlib.Dict, error) {
	dict := starlib.NewDict(len(m))
	for k, v := range m {
		sv, err := goValueToStarlark(v)
		if err != nil {
			return nil, fmt.Errorf("convert key %q: %w", k, err)
		}
		if err := dict.SetKey(starlib.String(k), sv); err != nil {
			return nil, fmt.Errorf("set dict key %q: %w", k, err)
		}
	}
	return dict, nil
}

// goValueToStarlark converts a Go value to a Starlark value.
func goValueToStarlark(v any) (starlib.Value, error) {
	switch val := v.(type) {
	case nil:
		return starlib.None, nil
	case bool:
		return starlib.Bool(val), nil
	case string:
		return starlib.String(val), nil
	case float64:
		// JSON numbers are float64 by default.
		if val == float64(int64(val)) {
			return starlib.MakeInt64(int64(val)), nil
		}
		return starlib.Float(val), nil
	case map[string]any:
		return goMapToStarlarkDict(val)
	case []any:
		elems := make([]starlib.Value, len(val))
		for i, elem := range val {
			sv, err := goValueToStarlark(elem)
			if err != nil {
				return nil, err
			}
			elems[i] = sv
		}
		return starlib.NewList(elems), nil
	default:
		return starlib.String(fmt.Sprintf("%v", val)), nil
	}
}

// starlarkValueToString converts a Starlark value to a Go string. For
// String values, the raw string content is returned (without quotes).
// For other types, the Starlark string representation is used.
func starlarkValueToString(v starlib.Value) string {
	if s, ok := v.(starlib.String); ok {
		return string(s)
	}
	return v.String()
}

// starlarkValueToGo converts a Starlark value to a native Go value.
// Used for converting Starlark dicts to Go maps when invoking skills.
func starlarkValueToGo(v starlib.Value) any {
	switch val := v.(type) {
	case starlib.NoneType:
		return nil
	case starlib.Bool:
		return bool(val)
	case starlib.String:
		return string(val)
	case starlib.Int:
		if i64, ok := val.Int64(); ok {
			return i64
		}
		return val.String()
	case starlib.Float:
		return float64(val)
	case *starlib.List:
		result := make([]any, val.Len())
		for i := range val.Len() {
			result[i] = starlarkValueToGo(val.Index(i))
		}
		return result
	case *starlib.Dict:
		result := make(map[string]any)
		for _, item := range val.Items() {
			key, ok := starlib.AsString(item[0])
			if !ok {
				key = item[0].String()
			}
			result[key] = starlarkValueToGo(item[1])
		}
		return result
	default:
		return v.String()
	}
}
