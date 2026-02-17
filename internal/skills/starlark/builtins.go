package starlark

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/julianshen/rubichan/internal/skills"

	starlib "go.starlark.net/starlark"
)

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

	data, err := os.ReadFile(path)
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

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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

	entries, err := os.ReadDir(path)
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

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("search_files: %w", err)
	}

	elems := make([]starlib.Value, len(matches))
	for i, m := range matches {
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

	cmd := exec.Command(command, cmdArgs...)
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
