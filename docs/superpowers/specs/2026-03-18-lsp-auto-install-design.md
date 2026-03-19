# LSP Auto-Install & Agent Wiring — Detailed Design

> **Version:** 1.0 · **Date:** 2026-03-18 · **Status:** Approved
> **FRs:** FR-1.6 (LSP integration)

---

## Overview

Wire the existing LSP package (`internal/tools/lsp/`) into the agent (currently dead code) and add auto-installation of language server binaries before first use. Uses a hybrid approach: language-specific installers (go install, npm, pip, cargo, gem) as primary, system package manager hints (brew, apt) as fallback.

## Existing Infrastructure

The `internal/tools/lsp/` package is a complete 4,000-line implementation:
- **Registry** — 17 pre-configured language servers with binary names, args, file extensions
- **Manager** — lazy server startup, per-language singletons, graceful shutdown
- **Client** — JSON-RPC 2.0 over stdin/stdout with LSP protocol framing
- **9 tools** — diagnostics, definition, references, hover, rename, completions, code actions, symbols, call hierarchy
- **Summarizer** — response truncation for token budget management

**What's missing:**
- Not wired into the agent (AllTools() never called, no import in main.go)
- No auto-install (assumes binaries are pre-installed, returns ErrServerNotInstalled)

## Components

### 1. InstallCmd in ServerConfig (`internal/tools/lsp/registry.go`)

Extend `ServerConfig` with install metadata:

```go
// InstallCmd describes one way to install a language server.
type InstallCmd struct {
    Method  string // prerequisite binary: "go", "npm", "pip", "cargo", "gem", "brew", "apt", "rustup", ""
    Command string // shell command to run (e.g., "go install golang.org/x/tools/gopls@latest")
    Hint    string // human-readable fallback message if command fails
}
```

Add `InstallCmds []InstallCmd` field to `ServerConfig`.

**Pre-populated install commands in `defaultConfigs()`:**

| Server | Primary | Fallback |
|--------|---------|----------|
| gopls | `go install golang.org/x/tools/gopls@latest` | — |
| typescript-language-server | `npm install -g typescript-language-server typescript` | — |
| pyright-langserver | `pip install pyright` | `npm install -g pyright` |
| rust-analyzer | `rustup component add rust-analyzer` | `brew install rust-analyzer` |
| clangd | `brew install llvm` | `apt install clangd` |
| solargraph | `gem install solargraph` | — |
| phpactor | `composer global require phpactor/phpactor` | — |
| sourcekit-lsp | — (bundled with Xcode) | hint: "Install Xcode Command Line Tools: xcode-select --install" |
| kotlin-language-server | `brew install kotlin-language-server` | manual |
| zls | `brew install zls` | manual |
| lua-language-server | `brew install lua-language-server` | manual |
| elixir-ls | `mix archive.install hex elixir_ls` | manual |
| haskell-language-server-wrapper | `ghcup install hls` | `brew install haskell-language-server` |
| OmniSharp | `brew install omnisharp` | `dotnet tool install -g omnisharp` |
| dart | — (bundled with Dart SDK) | hint: "Install Dart SDK: https://dart.dev/get-dart" |
| ocamllsp | `opam install ocaml-lsp-server` | — |
| jdtls | `brew install jdtls` | manual |

Servers without install commands (sourcekit-lsp, dart) provide hints only. The `Method` field names the prerequisite binary — if that binary isn't on PATH, the command is skipped. For example, phpactor's `InstallCmd` has `Method: "composer"`, solargraph has `Method: "gem"`, etc.

**Security note:** Install commands in `defaultConfigs` are hardcoded and trusted. User-provided `ServerConfig` overrides via `Register()` are also trusted (the user explicitly configured them). The `sh -c` execution is acceptable since all commands come from developer-controlled sources.

### 2. Installer (`internal/tools/lsp/installer.go`)

```go
// TryInstall attempts to install the LSP server for the given language.
// Tries each InstallCmd in order, skipping commands whose prerequisite
// binary is not on PATH. Returns nil on first success.
func (m *Manager) TryInstall(ctx context.Context, languageID string) error
```

**Algorithm:**
1. Get `ServerConfig` via `m.registry.ConfigFor(languageID)`
2. If no `InstallCmds` → return error with hint if available
3. For each `InstallCmd`:
   a. If `Method != ""` → check `exec.LookPath(Method)`. If not found → skip
   b. Log: `"Installing LSP server for %s via %s..."`
   c. Execute `exec.CommandContext(ctx, "sh", "-c", cmd.Command)` with 2-minute timeout
   d. If exit code 0 → return nil (success)
   e. If fails → log error, continue to next command
4. Return error with all hints collected

**Caching:** A `sync.Map` of `languageID → bool` tracks which languages had install attempts this session, preventing repeated failed installs.

### 3. Manager Integration (`internal/tools/lsp/manager.go`)

In `ServerFor()`, after checking `IsInstalled()` returns false:

```go
if !m.registry.IsInstalled(languageID) {
    if !m.autoInstall {
        return nil, ErrServerNotInstalled
    }
    if err := m.TryInstall(ctx, languageID); err != nil {
        return nil, fmt.Errorf("LSP server %q not installed: %w", languageID, err)
    }
    // Re-check PATH after install
    if !m.registry.IsInstalled(languageID) {
        return nil, ErrServerNotInstalled
    }
}
```

Add `autoInstall bool` as a constructor parameter to `NewManager`:

```go
func NewManager(registry *Registry, rootDir string, autoInstall bool) *Manager
```

This avoids temporal coupling (no setter required after construction).

### 4. LSP Config (`internal/config/config.go`)

```go
type LSPConfig struct {
    Enabled     bool `toml:"enabled"`      // default true
    AutoInstall bool `toml:"auto_install"` // default true
}
```

Add `LSP LSPConfig` to `Config` struct. Default both to true (zero value is false in Go, so the defaults need to be applied in `Load()` or via a `DefaultConfig()`).

### 5. Agent Wiring (`cmd/rubichan/main.go`)

In `wireExtendedTools()` (or equivalent tool registration site), add:

```go
var lspManager *lsp.Manager
if cfg.LSP.Enabled {
    lspRegistry := lsp.NewRegistry()
    lspManager = lsp.NewManager(lspRegistry, cwd, cfg.LSP.AutoInstall)
    for _, tool := range lsp.AllTools(lspManager) {
        _ = registry.Register(tool)
    }
}
// ... later, after agent run completes:
if lspManager != nil {
    lspManager.Shutdown(context.Background())
}
```

**Shutdown wiring:** In `runInteractive()`, add `defer lspManager.Shutdown(ctx)` after manager creation (before the Bubble Tea `Run()` call). In `runHeadless()`, add the same defer after manager creation. This ensures LSP server processes are cleaned up on exit, not left as orphans.

## Scope Exclusions

- **No version management** — installs latest, doesn't track versions
- **No uninstall** — users manage their own tools
- **No `/lsp` TUI command** — LSP is transparent; auto-install handles setup
- **No remote LSP servers** — local process only
- **No user prompting before install** — the agent called the LSP tool, intent is clear

## File Summary

| File | Package | Change |
|------|---------|--------|
| `internal/tools/lsp/registry.go` | `lsp` | Add InstallCmd type, InstallCmds field, populate defaults |
| `internal/tools/lsp/registry_test.go` | `lsp` | Tests for install cmd configs |
| `internal/tools/lsp/installer.go` | `lsp` | TryInstall with method prereq checks, caching |
| `internal/tools/lsp/installer_test.go` | `lsp` | Installer tests (prereq skip, success, all-fail) |
| `internal/tools/lsp/manager.go` | `lsp` | Auto-install integration in ServerFor(), autoInstall field |
| `internal/config/config.go` | `config` | Add LSPConfig |
| `internal/config/config_test.go` | `config` | LSP config test |
| `cmd/rubichan/main.go` | `main` | Wire LSP tools, manager lifecycle |

## Dependencies

- No new external dependencies
- `os/exec` for install commands (already used by Manager for server spawning)
