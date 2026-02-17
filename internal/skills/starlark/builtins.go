package starlark

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/skills"

	starlib "go.starlark.net/starlark"
)

// Safety limits for Starlark builtins.
const (
	maxReadFileSize = 10 << 20         // 10 MB max file size for read_file.
	execTimeout     = 30 * time.Second // 30s timeout for exec commands.
)

// resolveSandboxedPath resolves a path relative to the skill directory and
// validates it stays within the skill directory. Returns an error if the
// resolved path escapes the sandbox.
func (e *Engine) resolveSandboxedPath(path string) (string, error) {
	if e.skillDir == "" {
		return "", fmt.Errorf("skill directory not set; cannot sandbox path")
	}

	// Resolve relative paths against the skill directory.
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.skillDir, path)
	}

	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	absSkillDir, err := filepath.Abs(e.skillDir)
	if err != nil {
		return "", fmt.Errorf("resolve skill dir: %w", err)
	}

	// Ensure the resolved path is within the skill directory.
	if !strings.HasPrefix(resolved, absSkillDir+string(filepath.Separator)) && resolved != absSkillDir {
		return "", fmt.Errorf("path %q escapes skill directory %q", path, absSkillDir)
	}

	return resolved, nil
}

// builtinReadFile implements read_file(path) -> starlark.String.
// Requires the file:read permission.
func (e *Engine) builtinReadFile(
	_ *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var path string
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &path); err != nil {
		return nil, err
	}

	if err := e.checker.CheckPermission(skills.PermFileRead); err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}

	resolved, err := e.resolveSandboxedPath(path)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}

	// Check file size before reading to avoid OOM on large files.
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}
	if info.Size() > maxReadFileSize {
		return nil, fmt.Errorf("read_file: file %q exceeds maximum size (%d bytes)", path, maxReadFileSize)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}

	return starlib.String(data), nil
}

// builtinWriteFile implements write_file(path, content) -> starlark.None.
// Requires the file:write permission.
func (e *Engine) builtinWriteFile(
	_ *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var path string
	var content string
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 2, &path, &content); err != nil {
		return nil, err
	}

	if err := e.checker.CheckPermission(skills.PermFileWrite); err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}

	resolved, err := e.resolveSandboxedPath(path)
	if err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}

	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}

	return starlib.None, nil
}

// builtinListDir implements list_dir(path) -> starlark.List of strings.
// Requires the file:read permission.
func (e *Engine) builtinListDir(
	_ *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var path string
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &path); err != nil {
		return nil, err
	}

	if err := e.checker.CheckPermission(skills.PermFileRead); err != nil {
		return nil, fmt.Errorf("list_dir: %w", err)
	}

	resolved, err := e.resolveSandboxedPath(path)
	if err != nil {
		return nil, fmt.Errorf("list_dir: %w", err)
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("list_dir: %w", err)
	}

	elems := make([]starlib.Value, len(entries))
	for i, entry := range entries {
		elems[i] = starlib.String(entry.Name())
	}

	return starlib.NewList(elems), nil
}

// builtinSearchFiles implements search_files(pattern) -> starlark.List of strings.
// Uses filepath.Glob for pattern matching. Requires the file:read permission.
func (e *Engine) builtinSearchFiles(
	_ *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var pattern string
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &pattern); err != nil {
		return nil, err
	}

	if err := e.checker.CheckPermission(skills.PermFileRead); err != nil {
		return nil, fmt.Errorf("search_files: %w", err)
	}

	// Resolve glob pattern relative to skill directory. If the pattern is
	// not absolute, prefix it with the skill directory to confine results.
	if !filepath.IsAbs(pattern) && e.skillDir != "" {
		pattern = filepath.Join(e.skillDir, pattern)
	}

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("search_files: %w", err)
	}

	// Filter matches to only include paths within the skill directory.
	absSkillDir, _ := filepath.Abs(e.skillDir)
	var filtered []string
	for _, m := range matches {
		absM, _ := filepath.Abs(m)
		if strings.HasPrefix(absM, absSkillDir+string(filepath.Separator)) || absM == absSkillDir {
			filtered = append(filtered, m)
		}
	}

	elems := make([]starlib.Value, len(filtered))
	for i, m := range filtered {
		elems[i] = starlib.String(m)
	}

	return starlib.NewList(elems), nil
}

// builtinExec implements exec(command, *args) -> starlark dict with stdout/stderr/exit_code.
// Uses os/exec.Command which invokes the command directly without a shell,
// preventing shell injection. Requires the shell:exec permission.
func (e *Engine) builtinExec(
	_ *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("%s: requires at least 1 argument (command)", fn.Name())
	}

	command, ok := starlib.AsString(args[0])
	if !ok {
		return nil, fmt.Errorf("%s: command must be a string", fn.Name())
	}

	cmdArgs := make([]string, len(args)-1)
	for i := 1; i < len(args); i++ {
		s, ok := starlib.AsString(args[i])
		if !ok {
			return nil, fmt.Errorf("%s: argument %d must be a string", fn.Name(), i)
		}
		cmdArgs[i-1] = s
	}

	if err := e.checker.CheckPermission(skills.PermShellExec); err != nil {
		return nil, fmt.Errorf("%s: %w", fn.Name(), err)
	}

	execCtx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, command, cmdArgs...)
	stdout, err := cmd.Output()

	exitCode := 0
	stderr := ""
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			stderr = string(exitErr.Stderr)
		} else {
			return nil, fmt.Errorf("%s: %w", fn.Name(), err)
		}
	}

	dict := starlib.NewDict(3)
	_ = dict.SetKey(starlib.String("stdout"), starlib.String(string(stdout)))
	_ = dict.SetKey(starlib.String("stderr"), starlib.String(stderr))
	_ = dict.SetKey(starlib.String("exit_code"), starlib.MakeInt64(int64(exitCode)))

	return dict, nil
}

// builtinEnv implements env(key) -> starlark.String.
// Returns the value of the environment variable. Requires the env:read permission.
func (e *Engine) builtinEnv(
	_ *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var key string
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &key); err != nil {
		return nil, err
	}

	if err := e.checker.CheckPermission(skills.PermEnvRead); err != nil {
		return nil, fmt.Errorf("env: %w", err)
	}

	return starlib.String(os.Getenv(key)), nil
}

// builtinProjectRoot implements project_root() -> starlark.String.
// Returns the skill directory path. No permission required.
func (e *Engine) builtinProjectRoot(
	_ *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 0); err != nil {
		return nil, err
	}

	return starlib.String(e.skillDir), nil
}

// builtinLLMComplete implements llm_complete(prompt) -> starlark.String.
// Sends a prompt to the configured LLM provider and returns the response.
// Requires the llm:call permission.
func (e *Engine) builtinLLMComplete(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var prompt string
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &prompt); err != nil {
		return nil, err
	}

	if err := e.checker.CheckPermission(skills.PermLLMCall); err != nil {
		return nil, fmt.Errorf("llm_complete: %w", err)
	}

	if e.llmCompleter == nil {
		return nil, fmt.Errorf("llm_complete: no LLM completer configured")
	}

	result, err := e.llmCompleter.Complete(threadContext(thread), prompt)
	if err != nil {
		return nil, fmt.Errorf("llm_complete: %w", err)
	}

	return starlib.String(result), nil
}

// builtinFetch implements fetch(url) -> starlark.String.
// Fetches the given URL and returns the response body as a string.
// Requires the net:fetch permission.
func (e *Engine) builtinFetch(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	var url string
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &url); err != nil {
		return nil, err
	}

	if err := e.checker.CheckPermission(skills.PermNetFetch); err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	if e.httpFetcher == nil {
		return nil, fmt.Errorf("fetch: no HTTP fetcher configured")
	}

	result, err := e.httpFetcher.Fetch(threadContext(thread), url)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	return starlib.String(result), nil
}

// builtinGitDiff implements git_diff(*args) -> starlark.String.
// Runs git diff with the given arguments and returns the output.
// Requires the git:read permission.
func (e *Engine) builtinGitDiff(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	if err := e.checker.CheckPermission(skills.PermGitRead); err != nil {
		return nil, fmt.Errorf("git_diff: %w", err)
	}

	if e.gitRunner == nil {
		return nil, fmt.Errorf("git_diff: no git runner configured")
	}

	// Convert starlark args to strings.
	strArgs := make([]string, len(args))
	for i, a := range args {
		s, ok := starlib.AsString(a)
		if !ok {
			return nil, fmt.Errorf("git_diff: argument %d must be a string", i)
		}
		strArgs[i] = s
	}

	result, err := e.gitRunner.Diff(threadContext(thread), strArgs...)
	if err != nil {
		return nil, fmt.Errorf("git_diff: %w", err)
	}

	return starlib.String(result), nil
}

// builtinGitLog implements git_log(*args) -> starlark.List of dicts.
// Each dict has keys "hash", "author", "message".
// Requires the git:read permission.
func (e *Engine) builtinGitLog(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	if err := e.checker.CheckPermission(skills.PermGitRead); err != nil {
		return nil, fmt.Errorf("git_log: %w", err)
	}

	if e.gitRunner == nil {
		return nil, fmt.Errorf("git_log: no git runner configured")
	}

	// Convert starlark args to strings.
	strArgs := make([]string, len(args))
	for i, a := range args {
		s, ok := starlib.AsString(a)
		if !ok {
			return nil, fmt.Errorf("git_log: argument %d must be a string", i)
		}
		strArgs[i] = s
	}

	entries, err := e.gitRunner.Log(threadContext(thread), strArgs...)
	if err != nil {
		return nil, fmt.Errorf("git_log: %w", err)
	}

	elems := make([]starlib.Value, len(entries))
	for i, entry := range entries {
		dict := starlib.NewDict(3)
		_ = dict.SetKey(starlib.String("hash"), starlib.String(entry.Hash))
		_ = dict.SetKey(starlib.String("author"), starlib.String(entry.Author))
		_ = dict.SetKey(starlib.String("message"), starlib.String(entry.Message))
		elems[i] = dict
	}

	return starlib.NewList(elems), nil
}

// builtinGitStatus implements git_status() -> starlark.List of dicts.
// Each dict has keys "path", "status".
// Requires the git:read permission.
func (e *Engine) builtinGitStatus(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	if err := starlib.UnpackPositionalArgs(fn.Name(), args, kwargs, 0); err != nil {
		return nil, err
	}

	if err := e.checker.CheckPermission(skills.PermGitRead); err != nil {
		return nil, fmt.Errorf("git_status: %w", err)
	}

	if e.gitRunner == nil {
		return nil, fmt.Errorf("git_status: no git runner configured")
	}

	entries, err := e.gitRunner.Status(threadContext(thread))
	if err != nil {
		return nil, fmt.Errorf("git_status: %w", err)
	}

	elems := make([]starlib.Value, len(entries))
	for i, entry := range entries {
		dict := starlib.NewDict(2)
		_ = dict.SetKey(starlib.String("path"), starlib.String(entry.Path))
		_ = dict.SetKey(starlib.String("status"), starlib.String(entry.Status))
		elems[i] = dict
	}

	return starlib.NewList(elems), nil
}

// builtinInvokeSkill implements invoke_skill(name, input_dict) -> starlark dict.
// Invokes another skill by name with the given input and returns the result.
// Requires the skill:invoke permission.
func (e *Engine) builtinInvokeSkill(
	thread *starlib.Thread,
	fn *starlib.Builtin,
	args starlib.Tuple,
	kwargs []starlib.Tuple,
) (starlib.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s: requires exactly 2 arguments (name, input_dict)", fn.Name())
	}

	name, ok := starlib.AsString(args[0])
	if !ok {
		return nil, fmt.Errorf("%s: name must be a string", fn.Name())
	}

	inputDict, ok := args[1].(*starlib.Dict)
	if !ok {
		return nil, fmt.Errorf("%s: input must be a dict", fn.Name())
	}

	if err := e.checker.CheckPermission(skills.PermSkillInvoke); err != nil {
		return nil, fmt.Errorf("invoke_skill: %w", err)
	}

	if e.skillInvoker == nil {
		return nil, fmt.Errorf("invoke_skill: no skill invoker configured")
	}

	// Convert Starlark dict to Go map.
	goInput := make(map[string]any)
	for _, item := range inputDict.Items() {
		key, ok := starlib.AsString(item[0])
		if !ok {
			return nil, fmt.Errorf("invoke_skill: dict key must be a string")
		}
		goInput[key] = starlarkValueToGo(item[1])
	}

	result, err := e.skillInvoker.Invoke(threadContext(thread), name, goInput)
	if err != nil {
		return nil, fmt.Errorf("invoke_skill: %w", err)
	}

	// Convert Go map result back to Starlark dict.
	resultDict, err := goMapToStarlarkDict(result)
	if err != nil {
		return nil, fmt.Errorf("invoke_skill: convert result: %w", err)
	}

	return resultDict, nil
}
