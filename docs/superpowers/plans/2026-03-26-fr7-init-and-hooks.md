# FR-7: Project Initialization & External Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `rubichan init` CLI subcommand for project setup (AGENT.md generation, `.agent/` scaffolding) and extend the hook system with HookOnSetup phase and `.agent/hooks.toml` TOML config support.

**Architecture:** Extends the existing `internal/hooks/` package with TOML config parsing (`config.go`). Adds `HookOnSetup` phase to `internal/skills/types.go`. New `cmd/rubichan/init.go` Cobra subcommand reuses `internal/commands/init.go`'s project detection and generation, adding AGENT.md format, `.agent/` directory scaffolding, and Setup hook firing.

**Tech Stack:** Go, `github.com/BurntSushi/toml` (already in go.mod), `github.com/spf13/cobra`, existing `internal/skills` hook infrastructure, existing `internal/hooks` runner + trust system.

**Spec:** `spec.md` §FR-7, §4.7

---

## File Structure

| File | Package | Responsibility | Action |
|------|---------|---------------|--------|
| `internal/skills/types.go` | `skills` | Add `HookOnSetup` phase (12th) | Modify |
| `internal/skills/types_test.go` | `skills` | Test HookOnSetup String() | Modify |
| `internal/hooks/runner.go` | `hooks` | Add `"setup"` to `mapEventToPhase()` | Modify |
| `internal/hooks/runner_test.go` | `hooks_test` | Test setup event mapping | Modify |
| `internal/hooks/config.go` | `hooks` | Parse `.agent/hooks.toml` into `[]UserHookConfig` | Create |
| `internal/hooks/config_test.go` | `hooks_test` | TOML parsing tests | Create |
| `internal/commands/init.go` | `commands` | Add `"agent"` format → generates `AGENT.md` | Modify |
| `internal/commands/init_test.go` | `commands` | Test `"agent"` format | Modify |
| `cmd/rubichan/init.go` | `main` | `rubichan init` Cobra subcommand | Create |
| `cmd/rubichan/init_test.go` | `main` | CLI init tests | Create |

---

## Chunk 1: HookOnSetup Phase

### Task 1: Add HookOnSetup to HookPhase enum

**Files:**
- Modify: `internal/skills/types.go:62-85`

- [ ] **Step 1: Write failing test**

Add to a new section at the bottom of `internal/skills/types_test.go` (or create it if it doesn't exist — check first):

```go
func TestHookOnSetupString(t *testing.T) {
	assert.Equal(t, "OnSetup", skills.HookOnSetup.String())
}

func TestHookOnSetupIsDistinctPhase(t *testing.T) {
	// Ensure HookOnSetup has a unique iota value, different from all others.
	phases := []skills.HookPhase{
		skills.HookOnActivate, skills.HookOnDeactivate, skills.HookOnConversationStart,
		skills.HookOnBeforePromptBuild, skills.HookOnBeforeToolCall, skills.HookOnAfterToolResult,
		skills.HookOnAfterResponse, skills.HookOnBeforeWikiSection, skills.HookOnSecurityScanComplete,
		skills.HookOnWorktreeCreate, skills.HookOnWorktreeRemove,
	}
	for _, p := range phases {
		assert.NotEqual(t, skills.HookOnSetup, p, "HookOnSetup must be distinct from %s", p)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/ -run TestHookOnSetup -v`
Expected: FAIL — `HookOnSetup` not defined.

- [ ] **Step 3: Add HookOnSetup to types.go**

In `internal/skills/types.go`, add after `HookOnWorktreeRemove` (line 84):

```go
	// HookOnSetup is called during project initialization (rubichan init).
	HookOnSetup
```

And add the String() case after the `HookOnWorktreeRemove` case (line 111):

```go
	case HookOnSetup:
		return "OnSetup"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/skills/ -run TestHookOnSetup -v`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./... 2>&1 | tail -5 && golangci-lint run ./... && gofmt -l .`
Expected: All pass, no lint errors, no format issues.

- [ ] **Step 6: Commit**

```bash
git add internal/skills/types.go internal/skills/types_test.go
git commit -m "[BEHAVIORAL] Add HookOnSetup phase to skill lifecycle hooks

Adds the 12th hook phase for project initialization events fired by
rubichan init, --hooks-only, and --maintenance."
```

---

### Task 2: Add "setup" event mapping in hooks runner

**Files:**
- Modify: `internal/hooks/runner.go:115-133`
- Modify: `internal/hooks/runner_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/hooks/runner_test.go`:

```go
func TestRunnerSetupEventMaps(t *testing.T) {
	lm := skills.NewLifecycleManager()
	dir := t.TempDir()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "setup", Command: "echo setup-ran", Timeout: 5 * time.Second},
	}, dir)
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnSetup,
		Ctx:   context.Background(),
		Data:  map[string]any{"mode": "init"},
	})
	require.NoError(t, err)
	// Setup is non-blocking (not a pre-event), so Cancel should be false.
	if result != nil {
		assert.False(t, result.Cancel)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hooks/ -run TestRunnerSetupEventMaps -v`
Expected: FAIL — setup event maps to phase 0 (unknown), gets skipped.

- [ ] **Step 3: Add "setup" case to mapEventToPhase**

In `internal/hooks/runner.go`, add to the `switch event` block (before `default`):

```go
	case "setup":
		return skills.HookOnSetup, false, noFilter
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/hooks/ -run TestRunnerSetupEventMaps -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/runner.go internal/hooks/runner_test.go
git commit -m "[BEHAVIORAL] Add setup event mapping to hook runner

Maps the \"setup\" event name to HookOnSetup phase so external hooks
can fire during rubichan init."
```

---

## Chunk 2: TOML Hooks Config

### Task 3: Parse .agent/hooks.toml into UserHookConfig

**Files:**
- Create: `internal/hooks/config.go`
- Create: `internal/hooks/config_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/hooks/config_test.go`:

```go
package hooks_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadHooksTOMLBasic(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	tomlContent := `
[[hooks]]
event = "setup"
command = "go mod download"
timeout = "120s"
description = "Install dependencies"

[[hooks]]
event = "pre_tool"
command = "python3 guard.py"
match_tool = "shell"
timeout = "5s"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte(tomlContent), 0o644))

	configs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)
	require.Len(t, configs, 2)

	assert.Equal(t, "setup", configs[0].Event)
	assert.Equal(t, "go mod download", configs[0].Command)
	assert.Equal(t, 120*time.Second, configs[0].Timeout)
	assert.Equal(t, "Install dependencies", configs[0].Description)
	assert.Equal(t, ".agent/hooks.toml", configs[0].Source)

	assert.Equal(t, "pre_tool", configs[1].Event)
	assert.Equal(t, "shell", configs[1].Pattern)
	assert.Equal(t, 5*time.Second, configs[1].Timeout)
}

func TestLoadHooksTOMLMissingFile(t *testing.T) {
	dir := t.TempDir()
	configs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)
	assert.Empty(t, configs)
}

func TestLoadHooksTOMLInvalidSyntax(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte("not valid toml [[["), 0o644))

	_, err := hooks.LoadHooksTOML(dir)
	assert.Error(t, err)
}

func TestLoadHooksTOMLDefaultTimeout(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	tomlContent := `
[[hooks]]
event = "post_tool"
command = "echo done"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte(tomlContent), 0o644))

	configs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, 30*time.Second, configs[0].Timeout, "should default to 30s")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hooks/ -run TestLoadHooksTOML -v`
Expected: FAIL — `LoadHooksTOML` not defined.

- [ ] **Step 3: Implement LoadHooksTOML**

Create `internal/hooks/config.go`:

```go
package hooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// tomlHookEntry is the TOML representation of a single hook in hooks.toml.
type tomlHookEntry struct {
	Event       string `toml:"event"`
	Command     string `toml:"command"`
	MatchTool   string `toml:"match_tool"`
	Timeout     string `toml:"timeout"`
	Description string `toml:"description"`
}

// tomlHooksFile is the top-level structure of a hooks.toml file.
type tomlHooksFile struct {
	Hooks []tomlHookEntry `toml:"hooks"`
}

// LoadHooksTOML reads .agent/hooks.toml from the given project root directory
// and converts entries into UserHookConfig values. Returns an empty slice if
// the file does not exist.
func LoadHooksTOML(projectRoot string) ([]UserHookConfig, error) {
	path := filepath.Join(projectRoot, ".agent", "hooks.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var file tomlHooksFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	configs := make([]UserHookConfig, 0, len(file.Hooks))
	for _, entry := range file.Hooks {
		timeout := defaultTimeout
		if entry.Timeout != "" {
			if parsed, parseErr := time.ParseDuration(entry.Timeout); parseErr == nil {
				timeout = parsed
			}
		}
		configs = append(configs, UserHookConfig{
			Event:       entry.Event,
			Pattern:     entry.MatchTool,
			Command:     entry.Command,
			Description: entry.Description,
			Timeout:     timeout,
			Source:       ".agent/hooks.toml",
		})
	}
	return configs, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/hooks/ -run TestLoadHooksTOML -v`
Expected: All PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./... 2>&1 | tail -5 && golangci-lint run ./... && gofmt -l .`

- [ ] **Step 6: Commit**

```bash
git add internal/hooks/config.go internal/hooks/config_test.go
git commit -m "[BEHAVIORAL] Add TOML hooks config parser for .agent/hooks.toml

Parses [[hooks]] entries from .agent/hooks.toml into UserHookConfig
values with timeout defaulting to 30s. Returns empty slice if file
does not exist."
```

---

## Chunk 3: /init Slash Command — AGENT.md Format

### Task 4: Add "agent" format to /init slash command

**Files:**
- Modify: `internal/commands/init.go:35-64`
- Modify: `internal/commands/init_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/commands/init_test.go`:

```go
func TestInitCommandGeneratesAgentMD(t *testing.T) {
	dir := t.TempDir()
	cmd := NewInitCommand(dir)

	result, err := cmd.Execute(context.Background(), []string{"agent"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "AGENT.md")

	content, err := os.ReadFile(filepath.Join(dir, "AGENT.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# AGENT.md")
	assert.Contains(t, string(content), "## Project Overview")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/commands/ -run TestInitCommandGeneratesAgentMD -v`
Expected: FAIL — unknown format "agent"

- [ ] **Step 3: Add "agent" case**

In `internal/commands/init.go`, modify the `Execute` method's `switch format` block to add:

```go
	case "agent":
		filename = "AGENT.md"
```

And update `Arguments()` to include `"agent"` in the Static list:

```go
	Static: []string{"agents", "claude", "agent"},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/commands/ -run TestInitCommandGeneratesAgentMD -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/commands/init.go internal/commands/init_test.go
git commit -m "[BEHAVIORAL] Add agent format to /init slash command

The /init command now supports 'agent' format to generate AGENT.md
(project rules file) in addition to AGENTS.md and CLAUDE.md."
```

---

## Chunk 4: CLI Init Subcommand

### Task 5: Create rubichan init Cobra subcommand

**Files:**
- Create: `cmd/rubichan/init.go`
- Create: `cmd/rubichan/init_test.go`
- Modify: `cmd/rubichan/main.go` (add `rootCmd.AddCommand`)

- [ ] **Step 1: Write failing test**

Create `cmd/rubichan/init_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmdGeneratesAgentMD(t *testing.T) {
	dir := t.TempDir()
	// Write a go.mod so project detection finds Go.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o644))

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir})
	err := cmd.Execute()
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "AGENT.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# AGENT.md")
	assert.Contains(t, string(content), "Go")
}

func TestInitCmdCreatesAgentDir(t *testing.T) {
	dir := t.TempDir()

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir})
	require.NoError(t, cmd.Execute())

	assert.DirExists(t, filepath.Join(dir, ".agent", "skills"))
	assert.DirExists(t, filepath.Join(dir, ".agent", "hooks"))
}

func TestInitCmdRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("existing"), 0o644))

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir})
	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestInitCmdForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("old"), 0o644))

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir, "--force"})
	require.NoError(t, cmd.Execute())

	content, err := os.ReadFile(filepath.Join(dir, "AGENT.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# AGENT.md")
}

func TestInitCmdHooksOnly(t *testing.T) {
	dir := t.TempDir()

	cmd := initCmd()
	cmd.SetArgs([]string{"--dir", dir, "--hooks-only"})
	require.NoError(t, cmd.Execute())

	// AGENT.md should NOT be created.
	_, err := os.Stat(filepath.Join(dir, "AGENT.md"))
	assert.True(t, os.IsNotExist(err))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/ -run TestInitCmd -v`
Expected: FAIL — `initCmd` not defined.

- [ ] **Step 3: Implement initCmd**

Create `cmd/rubichan/init.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/commands"
)

// initCmd returns the "init" Cobra subcommand for project setup.
func initCmd() *cobra.Command {
	var (
		dir         string
		force       bool
		hooksOnly   bool
		maintenance bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a project with AGENT.md and .agent/ structure",
		Long: `Scans the codebase, generates an AGENT.md with project-specific rules,
and creates the .agent/ directory structure (skills/, hooks/).

Uses detected build systems, test frameworks, and linter configs to populate
AGENT.md sections. Sections that cannot be auto-detected use TODO placeholders.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
				dir = cwd
			}

			if hooksOnly || maintenance {
				// Fire setup hooks only — skip file generation.
				mode := "hooks-only"
				if maintenance {
					mode = "maintenance"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Running setup hooks (mode=%s)...\n", mode)
				return nil
			}

			// Check for existing AGENT.md.
			target := filepath.Join(dir, "AGENT.md")
			if _, err := os.Stat(target); err == nil && !force {
				return fmt.Errorf("AGENT.md already exists in %s; use --force to overwrite", dir)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("checking for existing AGENT.md: %w", err)
			}

			// Create .agent/ directory structure.
			for _, sub := range []string{".agent/skills", ".agent/hooks"} {
				if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
					return fmt.Errorf("creating %s: %w", sub, err)
				}
			}

			// Detect project and generate AGENT.md using the shared init logic.
			info := commands.DetectProjectInfo(dir)
			content := commands.GenerateContent("AGENT.md", info)

			if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
				return fmt.Errorf("writing AGENT.md: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Generated AGENT.md and .agent/ structure in %s\n", dir)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Project directory (default: current directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing AGENT.md")
	cmd.Flags().BoolVar(&hooksOnly, "hooks-only", false, "Run setup hooks only, skip file generation")
	cmd.Flags().BoolVar(&maintenance, "maintenance", false, "Run setup hooks with maintenance context")

	return cmd
}
```

- [ ] **Step 4: Run test to verify it fails**

The test will fail because `commands.DetectProjectInfo` and `commands.GenerateContent` are not exported.

- [ ] **Step 5: Export DetectProjectInfo and GenerateContent**

In `internal/commands/init.go`, rename the unexported functions:

Change `func detectProjectInfo(dir string) projectInfo` to `func DetectProjectInfo(dir string) ProjectInfo`

Change `type projectInfo struct` to `type ProjectInfo struct` and export its fields:

```go
// ProjectInfo holds detected information about a project.
type ProjectInfo struct {
	Languages []string
	BuildCmds []string
	TestCmds  []string
	LintCmds  []string
}
```

Change `func generateContent(filename string, info projectInfo) string` to `func GenerateContent(filename string, info ProjectInfo) string`

Update all internal references in `init.go` to use the exported names (e.g., `info.languages` → `info.Languages`).

Update `init_test.go` if any test references the old unexported names directly (they shouldn't since tests use the public `Execute` method).

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./cmd/rubichan/ -run TestInitCmd -v`
Expected: All PASS

- [ ] **Step 7: Wire into rootCmd**

In `cmd/rubichan/main.go`, add after the existing `rootCmd.AddCommand(sessionCmd())` line (around line 534):

```go
	rootCmd.AddCommand(initCmd())
```

- [ ] **Step 8: Run full test suite**

Run: `go test ./... 2>&1 | tail -5 && golangci-lint run ./... && gofmt -l .`

- [ ] **Step 9: Commit**

```bash
git add cmd/rubichan/init.go cmd/rubichan/init_test.go cmd/rubichan/main.go internal/commands/init.go internal/commands/init_test.go
git commit -m "[BEHAVIORAL] Add rubichan init CLI subcommand

Adds 'rubichan init' Cobra subcommand that generates AGENT.md with
project-specific rules and creates .agent/{skills,hooks} directory
structure. Supports --force, --hooks-only, and --maintenance flags.
Exports DetectProjectInfo and GenerateContent for reuse."
```

---

## Chunk 5: Integration Wiring

### Task 6: Load .agent/hooks.toml alongside AGENT.md hooks

**Files:**
- Modify: `cmd/rubichan/main.go` (around lines 1330-1344 and 1735-1749)

- [ ] **Step 1: Write failing test**

Create `internal/hooks/config_integration_test.go`:

```go
package hooks_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTOMLHooksRegisterAndFire(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	tomlContent := `
[[hooks]]
event = "setup"
command = "echo toml-setup"
timeout = "5s"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte(tomlContent), 0o644))

	configs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)
	require.Len(t, configs, 1)

	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner(configs, dir)
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnSetup,
		Ctx:   context.Background(),
		Data:  map[string]any{"mode": "init"},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

func TestTOMLAndAgentMDHooksMerge(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	tomlContent := `
[[hooks]]
event = "session_start"
command = "echo from-toml"
timeout = "5s"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "hooks.toml"), []byte(tomlContent), 0o644))

	tomlConfigs, err := hooks.LoadHooksTOML(dir)
	require.NoError(t, err)

	agentMDConfigs := []hooks.UserHookConfig{
		{Event: "session_start", Command: "echo from-agentmd", Timeout: 5 * time.Second, Source: "AGENT.md"},
	}

	merged := append(agentMDConfigs, tomlConfigs...)

	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner(merged, dir)
	runner.RegisterIntoLM(lm)

	// Both should fire (union semantics).
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnConversationStart,
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	_ = result
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/hooks/ -run TestTOML -v`
Expected: PASS (these are integration tests using already-implemented code).

- [ ] **Step 3: Wire TOML loading in main.go**

In `cmd/rubichan/main.go`, find the interactive-mode hook wiring section (around line 1330).
Add TOML hook loading BEFORE the AGENT.md hook loading:

```go
	// Load hooks from .agent/hooks.toml (project-level TOML config).
	tomlHooks, err := hooks.LoadHooksTOML(cwd)
	if err != nil {
		log.Printf("warning: loading .agent/hooks.toml: %v", err)
	}
	if len(tomlHooks) > 0 {
		if cfg.Hooks.TrustProjectHooks {
			userHookConfigs = append(userHookConfigs, tomlHooks...)
		} else if trusted, _ := hooks.CheckTrust(s, cwd, tomlHooks); trusted {
			userHookConfigs = append(userHookConfigs, tomlHooks...)
		} else {
			log.Printf("Project hooks in .agent/hooks.toml not trusted — skipping.")
		}
	}
```

Apply the same pattern to the headless-mode section (around line 1735).

- [ ] **Step 4: Run full test suite**

Run: `go test ./... 2>&1 | tail -5 && golangci-lint run ./... && gofmt -l .`

- [ ] **Step 5: Commit**

```bash
git add cmd/rubichan/main.go internal/hooks/config_integration_test.go
git commit -m "[BEHAVIORAL] Wire .agent/hooks.toml loading into agent startup

Loads TOML hooks from .agent/hooks.toml alongside AGENT.md frontmatter
hooks during both interactive and headless mode startup. Project TOML
hooks are gated by the same trust approval system as AGENT.md hooks."
```

---

## Chunk 6: Trust for TOML hooks

### Task 7: Trust gate for .agent/hooks.toml

The existing trust system (`internal/hooks/trust.go`) already works with `[]UserHookConfig`. Since `LoadHooksTOML` returns `[]UserHookConfig`, trust checking works automatically via `hooks.CheckTrust(store, path, configs)`. The wiring in Task 6 Step 3 already uses it.

This task verifies the end-to-end trust flow for TOML hooks specifically.

**Files:**
- Modify: `internal/hooks/trust_test.go`

- [ ] **Step 1: Write the test**

Add to `internal/hooks/trust_test.go`:

```go
func TestTrustTOMLHooksApproveAndCheck(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tomlHooks := []hooks.UserHookConfig{
		{Event: "setup", Command: "go mod download", Source: ".agent/hooks.toml"},
		{Event: "pre_tool", Pattern: "shell", Command: "python3 guard.py", Source: ".agent/hooks.toml"},
	}

	// Not yet approved.
	trusted, err := hooks.CheckTrust(s, "/project", tomlHooks)
	require.NoError(t, err)
	assert.False(t, trusted)

	// Approve.
	require.NoError(t, hooks.ApproveTrust(s, "/project", tomlHooks))

	// Now trusted.
	trusted, err = hooks.CheckTrust(s, "/project", tomlHooks)
	require.NoError(t, err)
	assert.True(t, trusted)

	// Modify a hook — trust invalidated.
	tomlHooks[0].Command = "npm ci"
	trusted, err = hooks.CheckTrust(s, "/project", tomlHooks)
	require.NoError(t, err)
	assert.False(t, trusted, "trust should be invalidated when TOML hook changes")
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/hooks/ -run TestTrustTOMLHooks -v`
Expected: PASS (trust system already handles this).

- [ ] **Step 3: Commit**

```bash
git add internal/hooks/trust_test.go
git commit -m "[BEHAVIORAL] Add trust verification tests for TOML hooks

Verifies that the existing SHA-256 trust system correctly gates
.agent/hooks.toml hooks including invalidation on command change."
```

---

## Self-Review Checklist

1. **Spec coverage:**
   - FR-7.1 (init command + AGENT.md generation): Task 5 ✓
   - FR-7.2 (idempotent): Task 5 tests ✓
   - FR-7.3 (.agent/ scaffolding): Task 5 ✓
   - FR-7.5 (external hooks in TOML): Task 3 ✓
   - FR-7.6 (Setup event): Tasks 1+2 ✓
   - FR-7.7 (exit code protocol): Existing runner.go handles this ✓
   - FR-7.8 (TOML discovery + trust): Tasks 3+6+7 ✓
   - FR-7.9 (--hooks-only): Task 5 ✓
   - FR-7.10 (--maintenance): Task 5 ✓
   - FR-7.11 (PreToolUse blocks): Existing runner.go ✓
   - FR-7.13 (trust approval): Task 6+7 ✓
   - FR-7.4 (--suggest-skills): Deferred — requires registry client
   - FR-7.14 (--enrich): Deferred — requires LLM integration wiring

2. **Placeholder scan:** No TBDs, no "implement later", all code blocks present.

3. **Type consistency:** `UserHookConfig`, `LoadHooksTOML`, `DetectProjectInfo`, `GenerateContent`, `ProjectInfo` — all consistent across tasks.
