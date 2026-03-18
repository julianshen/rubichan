# LSP Auto-Install & Agent Wiring Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire existing LSP tools into the agent and add auto-installation of language server binaries on first use.

**Architecture:** Add `InstallCmds` to `ServerConfig`, create `installer.go` with `TryInstall()`, modify `Manager.ServerFor()` to auto-install before returning `ErrServerNotInstalled`, add `LSPConfig` to config, wire LSP tools in `wireExtendedTools()`.

**Tech Stack:** Go stdlib (`os/exec`), existing `internal/tools/lsp/` package, existing `internal/config/`.

**Spec:** `docs/superpowers/specs/2026-03-18-lsp-auto-install-design.md`

---

## File Structure

| File | Package | Responsibility |
|------|---------|---------------|
| `internal/tools/lsp/registry.go` | `lsp` | Add InstallCmd type, InstallCmds field, populate defaults |
| `internal/tools/lsp/installer.go` | `lsp` | TryInstall with prereq checks, caching, shell execution |
| `internal/tools/lsp/installer_test.go` | `lsp` | Installer tests |
| `internal/tools/lsp/manager.go` | `lsp` | Add autoInstall to NewManager, auto-install in ServerFor |
| `internal/config/config.go` | `config` | Add LSPConfig |
| `cmd/rubichan/main.go` | `main` | Wire LSP tools in wireExtendedTools, shutdown lifecycle |

---

## Chunk 1: Install Infrastructure

### Task 1: InstallCmd type and default install commands

**Files:**
- Modify: `internal/tools/lsp/registry.go`
- Test: `internal/tools/lsp/registry_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestDefaultConfigsHaveInstallCmds(t *testing.T) {
	r := NewRegistry()

	// Go should have an install command
	cfg, err := r.ConfigFor("go")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.InstallCmds, "go should have install commands")
	assert.Equal(t, "go", cfg.InstallCmds[0].Method)
	assert.Contains(t, cfg.InstallCmds[0].Command, "gopls")
}

func TestInstallCmdMethodField(t *testing.T) {
	r := NewRegistry()

	cfg, _ := r.ConfigFor("typescript")
	require.NotEmpty(t, cfg.InstallCmds)
	assert.Equal(t, "npm", cfg.InstallCmds[0].Method)

	cfg, _ = r.ConfigFor("python")
	require.NotEmpty(t, cfg.InstallCmds)
	assert.Equal(t, "pip", cfg.InstallCmds[0].Method)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/lsp/ -run "TestDefaultConfigsHaveInstall|TestInstallCmdMethod" -v`
Expected: FAIL — InstallCmds field doesn't exist

- [ ] **Step 3: Write implementation**

Add to `registry.go`:

```go
// InstallCmd describes one way to install a language server.
type InstallCmd struct {
	Method  string // prerequisite binary: "go", "npm", "pip", "cargo", "gem", "brew", "apt", "rustup", "composer", ""
	Command string // shell command (e.g., "go install golang.org/x/tools/gopls@latest")
	Hint    string // human-readable fallback if command fails or no auto method
}
```

Add `InstallCmds []InstallCmd` field to `ServerConfig`.

Update `defaultConfigs` to include install commands for each server:

```go
var defaultConfigs = []ServerConfig{
	{Language: "go", Command: "gopls", Args: []string{"serve"}, Extensions: []string{".go"},
		InstallCmds: []InstallCmd{{Method: "go", Command: "go install golang.org/x/tools/gopls@latest"}}},
	{Language: "typescript", Command: "typescript-language-server", Args: []string{"--stdio"}, Extensions: []string{".ts", ".tsx", ".js", ".jsx"},
		InstallCmds: []InstallCmd{{Method: "npm", Command: "npm install -g typescript-language-server typescript"}}},
	{Language: "python", Command: "pyright-langserver", Args: []string{"--stdio"}, Extensions: []string{".py"},
		InstallCmds: []InstallCmd{
			{Method: "pip", Command: "pip install pyright"},
			{Method: "npm", Command: "npm install -g pyright"},
		}},
	{Language: "rust", Command: "rust-analyzer", Extensions: []string{".rs"},
		InstallCmds: []InstallCmd{
			{Method: "rustup", Command: "rustup component add rust-analyzer"},
			{Method: "brew", Command: "brew install rust-analyzer"},
		}},
	{Language: "java", Command: "jdtls", Extensions: []string{".java"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install jdtls", Hint: "Install Eclipse JDT Language Server"}}},
	{Language: "c", Command: "clangd", Extensions: []string{".c", ".h", ".cpp", ".hpp", ".cc", ".cxx"},
		InstallCmds: []InstallCmd{
			{Method: "brew", Command: "brew install llvm"},
			{Method: "apt", Command: "sudo apt install -y clangd"},
		}},
	{Language: "ruby", Command: "solargraph", Args: []string{"stdio"}, Extensions: []string{".rb"},
		InstallCmds: []InstallCmd{{Method: "gem", Command: "gem install solargraph"}}},
	{Language: "php", Command: "phpactor", Args: []string{"language-server"}, Extensions: []string{".php"},
		InstallCmds: []InstallCmd{{Method: "composer", Command: "composer global require phpactor/phpactor"}}},
	{Language: "swift", Command: "sourcekit-lsp", Extensions: []string{".swift"},
		InstallCmds: []InstallCmd{{Hint: "Install Xcode Command Line Tools: xcode-select --install"}}},
	{Language: "kotlin", Command: "kotlin-language-server", Extensions: []string{".kt", ".kts"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install kotlin-language-server"}}},
	{Language: "zig", Command: "zls", Extensions: []string{".zig"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install zls"}}},
	{Language: "lua", Command: "lua-language-server", Extensions: []string{".lua"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install lua-language-server"}}},
	{Language: "elixir", Command: "elixir-ls", Extensions: []string{".ex", ".exs"},
		InstallCmds: []InstallCmd{{Method: "mix", Command: "mix archive.install hex elixir_ls"}}},
	{Language: "haskell", Command: "haskell-language-server-wrapper", Args: []string{"--lsp"}, Extensions: []string{".hs"},
		InstallCmds: []InstallCmd{
			{Method: "ghcup", Command: "ghcup install hls"},
			{Method: "brew", Command: "brew install haskell-language-server"},
		}},
	{Language: "csharp", Command: "OmniSharp", Args: []string{"--languageserver"}, Extensions: []string{".cs"},
		InstallCmds: []InstallCmd{{Method: "brew", Command: "brew install omnisharp"}}},
	{Language: "dart", Command: "dart", Args: []string{"language-server"}, Extensions: []string{".dart"},
		InstallCmds: []InstallCmd{{Hint: "Install Dart SDK: https://dart.dev/get-dart"}}},
	{Language: "ocaml", Command: "ocamllsp", Extensions: []string{".ml", ".mli"},
		InstallCmds: []InstallCmd{{Method: "opam", Command: "opam install ocaml-lsp-server"}}},
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/lsp/ -run "TestDefaultConfigsHaveInstall|TestInstallCmdMethod" -v`
Expected: PASS

- [ ] **Step 5: Run all LSP tests**

Run: `go test ./internal/tools/lsp/ -v`
Expected: ALL PASS (no regressions — InstallCmds is additive)

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add InstallCmd type and install commands for all language servers
```

---

### Task 2: TryInstall implementation

**Files:**
- Create: `internal/tools/lsp/installer.go`
- Create: `internal/tools/lsp/installer_test.go`

- [ ] **Step 1: Write failing tests**

```go
package lsp

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryInstallSkipsMissingPrereq(t *testing.T) {
	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)

	// Register a fake language with a prereq that doesn't exist
	r.Register(ServerConfig{
		Language:   "fake-lang",
		Command:    "fake-server",
		Extensions: []string{".fake"},
		InstallCmds: []InstallCmd{
			{Method: "nonexistent-tool-xyz", Command: "nonexistent-tool-xyz install fake-server"},
		},
	})

	err := m.TryInstall(context.Background(), "fake-lang")
	assert.Error(t, err, "should fail when prereq not on PATH")
}

func TestTryInstallNoCommands(t *testing.T) {
	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)

	r.Register(ServerConfig{
		Language: "no-install", Command: "no-server", Extensions: []string{".nope"},
	})

	err := m.TryInstall(context.Background(), "no-install")
	assert.Error(t, err)
}

func TestTryInstallWithHintOnly(t *testing.T) {
	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)

	r.Register(ServerConfig{
		Language: "hint-only", Command: "hint-server", Extensions: []string{".hint"},
		InstallCmds: []InstallCmd{{Hint: "Please install manually"}},
	})

	err := m.TryInstall(context.Background(), "hint-only")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Please install manually")
}

func TestTryInstallCachesAttempts(t *testing.T) {
	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)

	r.Register(ServerConfig{
		Language: "cached", Command: "cached-server", Extensions: []string{".cache"},
		InstallCmds: []InstallCmd{
			{Method: "nonexistent-xyz", Command: "nonexistent-xyz install cached-server"},
		},
	})

	// First attempt
	err1 := m.TryInstall(context.Background(), "cached")
	assert.Error(t, err1)

	// Second attempt should be cached (fast)
	err2 := m.TryInstall(context.Background(), "cached")
	assert.Error(t, err2)
	assert.Contains(t, err2.Error(), "already attempted")
}

func TestTryInstallSucceedsWithEcho(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)

	r.Register(ServerConfig{
		Language: "echo-lang", Command: "echo-server", Extensions: []string{".echo"},
		InstallCmds: []InstallCmd{
			{Method: "", Command: "echo installed"},  // Method "" = no prereq check
		},
	})

	err := m.TryInstall(context.Background(), "echo-lang")
	assert.NoError(t, err, "echo should succeed as install command")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/lsp/ -run "TestTryInstall" -v`
Expected: FAIL — TryInstall not defined

- [ ] **Step 3: Write implementation**

Create `internal/tools/lsp/installer.go`:

```go
package lsp

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const installTimeout = 2 * time.Minute

// TryInstall attempts to install the LSP server for the given language.
// Tries each InstallCmd in order, skipping commands whose prerequisite
// binary is not on PATH. Returns nil on first success.
// Caches failed attempts to avoid repeated install spam.
func (m *Manager) TryInstall(ctx context.Context, languageID string) error {
	// Check cache
	if _, attempted := m.installAttempted.Load(languageID); attempted {
		return fmt.Errorf("install for %s already attempted this session", languageID)
	}

	cfg, err := m.registry.ConfigFor(languageID)
	if err != nil {
		return err
	}

	if len(cfg.InstallCmds) == 0 {
		m.installAttempted.Store(languageID, true)
		return fmt.Errorf("no install commands for %s", languageID)
	}

	var hints []string
	for _, ic := range cfg.InstallCmds {
		if ic.Hint != "" {
			hints = append(hints, ic.Hint)
		}
		if ic.Command == "" {
			continue // hint-only entry
		}

		// Check prerequisite
		if ic.Method != "" {
			if _, err := exec.LookPath(ic.Method); err != nil {
				continue // prereq not available, try next
			}
		}

		log.Printf("Installing LSP server for %s via: %s", languageID, ic.Command)

		installCtx, cancel := context.WithTimeout(ctx, installTimeout)
		cmd := exec.CommandContext(installCtx, "sh", "-c", ic.Command)
		output, err := cmd.CombinedOutput()
		cancel()

		if err == nil {
			log.Printf("LSP server for %s installed successfully", languageID)
			return nil
		}
		log.Printf("Install failed for %s: %s (output: %s)", languageID, err, strings.TrimSpace(string(output)))
	}

	m.installAttempted.Store(languageID, true)

	if len(hints) > 0 {
		return fmt.Errorf("could not install LSP server for %s. Hints: %s", languageID, strings.Join(hints, "; "))
	}
	return fmt.Errorf("all install methods failed for %s", languageID)
}
```

Add `installAttempted sync.Map` field to `Manager` struct in `manager.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/lsp/ -run "TestTryInstall" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add TryInstall with prereq checks and session caching
```

---

### Task 3: Manager auto-install integration + NewManager update

**Files:**
- Modify: `internal/tools/lsp/manager.go`

- [ ] **Step 1: Update NewManager signature**

Change `NewManager` to accept `autoInstall bool`:

```go
func NewManager(registry *Registry, rootDir string, autoInstall bool) *Manager {
	return &Manager{
		// ... existing fields ...
		autoInstall: autoInstall,
	}
}
```

Add `autoInstall bool` field to Manager struct.

- [ ] **Step 2: Add auto-install logic to ServerFor()**

In `ServerFor()`, replace the current `IsInstalled` check (line ~123):

```go
// BEFORE (current):
if !m.registry.IsInstalled(languageID) {
    m.mu.Unlock()
    return nil, nil, fmt.Errorf("%w: %s (%s not found on PATH)", ErrServerNotInstalled, languageID, cfg.Command)
}

// AFTER (new):
if !m.registry.IsInstalled(languageID) {
    m.mu.Unlock()
    if !m.autoInstall {
        return nil, nil, fmt.Errorf("%w: %s (%s not found on PATH)", ErrServerNotInstalled, languageID, cfg.Command)
    }
    if err := m.TryInstall(ctx, languageID); err != nil {
        return nil, nil, fmt.Errorf("%w: %s: %v", ErrServerNotInstalled, languageID, err)
    }
    if !m.registry.IsInstalled(languageID) {
        return nil, nil, fmt.Errorf("%w: %s (install succeeded but binary still not on PATH)", ErrServerNotInstalled, languageID)
    }
    m.mu.Lock() // re-acquire for the server init section below
    if m.closed {
        m.mu.Unlock()
        return nil, nil, ErrManagerShutdown
    }
}
```

**IMPORTANT:** After install + re-check, we need to re-acquire `m.mu` because the rest of the function expects it held. Also need to re-check `m.closed` and `m.servers` in case something changed during the install.

- [ ] **Step 3: Fix existing tests that call NewManager**

Search for all `NewManager(` calls in test files and add the `autoInstall` parameter (use `false` for existing tests to preserve behavior):

Run: `grep -rn "NewManager(" internal/tools/lsp/ --include="*_test.go"`

Update each call: `NewManager(r, dir)` → `NewManager(r, dir, false)`

- [ ] **Step 4: Run all LSP tests**

Run: `go test ./internal/tools/lsp/ -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] integrate auto-install into Manager.ServerFor
```

---

## Chunk 2: Config + Wiring

### Task 4: LSPConfig in config

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestConfigLSPSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"

[lsp]
enabled = false
auto_install = false
`), 0644)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.False(t, cfg.LSP.Enabled)
	assert.False(t, cfg.LSP.AutoInstall)
}

func TestConfigLSPDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"
`), 0644)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	// Defaults should be true
	assert.True(t, cfg.LSP.Enabled)
	assert.True(t, cfg.LSP.AutoInstall)
}
```

- [ ] **Step 2: Write implementation**

Add to `internal/config/config.go`:

```go
// In Config struct:
LSP LSPConfig `toml:"lsp"`

// New type:
type LSPConfig struct {
	Enabled     bool `toml:"enabled"`
	AutoInstall bool `toml:"auto_install"`
}
```

In `Load()` or `DefaultConfig()`, set defaults:
```go
// After TOML decode, apply defaults for zero-value bools:
if !hasExplicitLSP {
    cfg.LSP.Enabled = true
    cfg.LSP.AutoInstall = true
}
```

Note: TOML bool zero-value is false. Need a way to distinguish "user set false" from "not set". The simplest approach: use a sentinel — check if the `[lsp]` section exists in the raw TOML. Or use pointer bools. Or just default in the struct tag. Check existing patterns in the config package.

Actually the simplest: apply defaults after Load() for the specific fields:

```go
// In Load(), after toml.DecodeFile:
if !hasKey(metadata, "lsp", "enabled") {
    cfg.LSP.Enabled = true
}
if !hasKey(metadata, "lsp", "auto_install") {
    cfg.LSP.AutoInstall = true
}
```

Where `metadata` is the `toml.MetaData` returned by `toml.DecodeFile`. Check if `Load()` already uses `toml.MetaData`.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/config/ -run "TestConfigLSP" -v`
Expected: PASS

- [ ] **Step 4: Commit**

```
[BEHAVIORAL] add LSPConfig with enabled and auto_install defaults
```

---

### Task 5: Wire LSP tools into wireExtendedTools

**Files:**
- Modify: `cmd/rubichan/main.go`

- [ ] **Step 1: Add LSP wiring to wireExtendedTools**

Add import: `"github.com/julianshen/rubichan/internal/tools/lsp"`

In `wireExtendedTools()`, after the browser tools section (line ~2184), add:

```go
// LSP tools
if cfg.LSP.Enabled {
    lspRegistry := lsp.NewRegistry()
    lspManager := lsp.NewManager(lspRegistry, cwd, cfg.LSP.AutoInstall)
    for _, tool := range lsp.AllTools(lspManager) {
        if toolsCfg.ShouldEnable(tool.Name()) {
            if err := registry.Register(tool); err != nil {
                return fmt.Errorf("registering LSP tool %s: %w", tool.Name(), err)
            }
        }
    }
    // Manager shutdown handled by OS process cleanup — LSP servers are child
    // processes that terminate when parent exits. For explicit cleanup, the
    // manager can be stored and Shutdown() called on application exit.
}
```

**Note on shutdown:** `wireExtendedTools` returns `error`, not a cleanup func. The simplest approach: LSP servers are child processes — they die when the parent process exits. If explicit cleanup is needed, refactor `wireExtendedTools` to return a cleanup func (bigger change, deferred).

- [ ] **Step 2: Verify build**

Run: `go build ./cmd/rubichan/`
Expected: BUILD OK

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```
[BEHAVIORAL] wire LSP tools into agent via wireExtendedTools
```

---

### Task 6: Final integration — tests + lint

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Check formatting**

Run: `gofmt -l .`
Expected: No files

- [ ] **Step 3: Verify LSP tools registered**

Run: `go build ./cmd/rubichan/ && echo "BUILD OK"`
Expected: BUILD OK

- [ ] **Step 4: Commit any fixes**

```
[STRUCTURAL] fix lint and formatting
```
