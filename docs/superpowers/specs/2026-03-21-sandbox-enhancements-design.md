# Sandbox Enhancements Design Spec

**Date:** 2026-03-21
**Status:** Draft
**Scope:** Network domain proxy, sandbox escape control, excluded commands

## Problem

Rubichan's shell sandbox uses the same OS primitives as Claude Code (Seatbelt on macOS, Bubblewrap on Linux) but has three gaps that limit adoption:

1. **Network is binary** — `AllowNetwork` is a bool. Tools like `npm install`, `kubectl`, or `cargo build` need specific hosts but not the full internet. Without per-domain control, users must either disable network entirely (breaking tools) or allow all traffic (defeating the sandbox).

2. **Silent unsandboxed fallback** — When the sandbox backend is unavailable or hits a setup error, commands silently run unsandboxed. There is no config to enforce sandboxing or even notify the user.

3. **No per-command exclusion** — Some commands (Docker, watchman) are incompatible with sandboxing. Users have no way to exempt specific commands while keeping the sandbox active for everything else.

## Design

### Config Schema

New `[sandbox]` section in `config.toml`:

```toml
[sandbox]
enabled = true
allow_unsandboxed_commands = true
excluded_commands = ["docker", "watchman"]

[sandbox.network]
allowed_domains = ["github.com", "registry.npmjs.org", "proxy.golang.org"]
proxy_port = 0  # 0 = auto-assign ephemeral port

[sandbox.filesystem]
allow_write = ["~/.kube", "/tmp/build"]
deny_read = ["~/.ssh"]
```

Go types in `internal/config/config.go`. The existing `Config` struct gains a new field:

```go
type Config struct {
    // ... existing fields ...
    Sandbox SandboxConfig `toml:"sandbox"`
}

type SandboxConfig struct {
    Enabled                  *bool    `toml:"enabled"`
    AllowUnsandboxedCommands *bool    `toml:"allow_unsandboxed_commands"`
    ExcludedCommands         []string `toml:"excluded_commands"`
    Network                  SandboxNetworkConfig    `toml:"network"`
    Filesystem               SandboxFilesystemConfig `toml:"filesystem"`
}

func (c SandboxConfig) IsEnabled() bool {
    if c.Enabled == nil { return false }
    return *c.Enabled
}

type SandboxNetworkConfig struct {
    AllowedDomains []string `toml:"allowed_domains"`
    ProxyPort      int      `toml:"proxy_port"`
}

type SandboxFilesystemConfig struct {
    AllowWrite []string `toml:"allow_write"`
    DenyRead   []string `toml:"deny_read"`
}
```

**Defaults:** `enabled = false` (opt-in), `allow_unsandboxed_commands = true` (backward compatible), `excluded_commands = []`, `allowed_domains = []`.

**Path resolution:** `/` = absolute, `~/` = home-relative, `./` = project-relative. Resolution happens at config load time (in `LoadConfig`), not at policy build time.

**Validation at startup:**
- `allow_write`/`deny_read` paths must use valid prefixes
- `excluded_commands` must be bare command names (no `/` or spaces)
- `allowed_domains` validated against glob syntax; entries with scheme prefixes (`https://`) or port suffixes (`:443`) are rejected with a helpful message ("use `github.com` not `https://github.com`")
- Overlapping `deny_read` + `allow_write` produces a warning
- Sandbox sub-fields configured (e.g., `excluded_commands`) but `enabled` not set → warning ("sandbox configuration present but sandbox is not enabled")

### Feature 1: Network Domain Proxy

#### Architecture

```
┌─────────────────────────────────────┐
│  Sandboxed Command (bwrap/seatbelt) │
│  HTTP_PROXY=127.0.0.1:PORT          │
│  HTTPS_PROXY=127.0.0.1:PORT         │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│  DomainProxy (outside sandbox)      │
│                                     │
│  HTTP:  parse Host header → check   │
│  HTTPS: parse CONNECT host → check  │
│                                     │
│  Allowed → tunnel through           │
│  Denied  → 403 + onBlocked callback │
└─────────────────────────────────────┘
```

The proxy runs outside the sandbox as a goroutine within the Rubichan process. Sandboxed commands can only reach `127.0.0.1:PORT` (enforced by Seatbelt/bwrap). All outbound traffic routes through the proxy, which filters by domain.

#### Domain Matching

- **Exact:** `github.com` matches `github.com`
- **Wildcard:** `*.npmjs.org` matches `registry.npmjs.org` but not `npmjs.org` itself
- No regex — glob-style only

#### HTTPS Handling

No TLS interception. For HTTPS, clients send a `CONNECT host:port` request. The proxy reads the host from the CONNECT line, checks the allowlist, then either tunnels raw TCP bytes or returns `403 Forbidden`. The proxy never sees plaintext HTTPS content.

#### Blocked Domain Flow

When a domain is blocked, the `onBlocked` callback notifies the agent, which emits a `tool_progress` event. The TUI shows `[sandbox] blocked connection to evil.com`. The user can update their config to add the domain permanently. Runtime additions via `proxy.AllowDomain(domain)` are session-only (not persisted).

#### When No Domains Configured

If `allowed_domains` is empty, the proxy is not started. Network remains fully blocked (`AllowNetwork=false`). This preserves current behavior.

#### OS Sandbox Adjustments When Proxy Active

**Seatbelt (macOS):**

The existing profile uses `(deny default)` which blocks all operations including network. When `AllowNetwork=false`, the code adds a redundant `(deny network*)`. When the proxy is active, we need an explicit allow for outbound connections to the proxy port. The `(deny default)` base already blocks everything else.

```scheme
;; Added to profile when proxy is active (before deny default takes effect):
(allow network-outbound (remote ip "127.0.0.1") (remote tcp-port PORT))
;; No (deny network*) needed — (deny default) already blocks other network
```

The port value is interpolated as an integer literal in the profile string by `buildSeatbeltProfile`. The existing `ShellSandboxPolicy` struct changes:

```go
type ShellSandboxPolicy struct {
-   AllowNetwork  bool       // removed
+   ProxyPort     int        // 0 = network blocked; >0 = allow outbound to 127.0.0.1:port only
    AllowedPaths  []string
    WritablePaths []string
    DeniedPaths   []string
    AllowSubprocs bool
}
```

In `buildSeatbeltProfile`, the check `if !policy.AllowNetwork` becomes `if policy.ProxyPort > 0 { emit allow-outbound rule } // else (deny default) blocks all network`. In `bubblewrapArgs`, `policy.ProxyPort > 0` adds `--share-net`.

**Bubblewrap (Linux):**

```
--share-net  # allow networking — proxy filtering is advisory (see Security Model)
```

**Security Model — Platform Differences:**

On **macOS**, Seatbelt enforces that the sandboxed process can only connect to `127.0.0.1:PORT`. Direct internet access is blocked at the kernel level. The proxy is a true security boundary.

On **Linux**, bubblewrap's `--share-net` gives the sandboxed process full network access. The proxy is **advisory** — programs that ignore `HTTP_PROXY`/`HTTPS_PROXY` (raw socket connections, `curl --noproxy '*'`, custom transports) bypass filtering entirely. This is a defense-in-depth layer, not an enforced boundary. For true network enforcement on Linux, future work could use network namespaces with a veth pair routed through the proxy, but this requires root privileges and is out of scope for this design.

**Env vars** `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY=localhost,127.0.0.1` are injected into the command environment by `sandboxCommandEnv()` in `shell_sandbox.go` (line 273), which already sets `HOME` and `XDG_*` vars. The proxy port flows through `ShellSandboxPolicy.ProxyPort` → `sandboxCommandEnv` → `cmd.Env`.

#### Key Type

```go
// internal/tools/sandbox/proxy.go
type DomainProxy struct {
    listener  net.Listener
    allowed   []string
    onBlocked func(domain, command string) // which command hit which domain
    mu        sync.RWMutex
    runtime   map[string]bool  // session-only additions
}

func (p *DomainProxy) Start() (addr string, err error)
func (p *DomainProxy) Stop() error
func (p *DomainProxy) AllowDomain(domain string)  // session-only
```

### Feature 2: Sandbox Escape Control

#### Decision Flow

```
Command arrives
  │
  ├─ Is command in excludedCommands? ──yes──→ Run unsandboxed (approval flow)
  │
  ├─ Sandbox backend available? ──no──→ allowUnsandboxedCommands?
  │                                        ├─ true  → Run unsandboxed (approval flow)
  │                                        └─ false → Hard error
  │
  ├─ Run sandboxed
  │    ├─ Success → done
  │    └─ Sandbox setup error (not policy deny)
  │         ├─ allowUnsandboxedCommands=true  → Run unsandboxed (approval flow)
  │         └─ allowUnsandboxedCommands=false → Hard error
  │
  └─ Sandbox policy deny → Hard error always
```

**Key distinction:** Setup errors (seatbelt permission denied, bwrap missing) are recoverable via fallback. Policy denials (sandbox intentionally blocked the access) are never recoverable regardless of config.

**Startup behavior:**
- `enabled=true` + no backend + `allow_unsandboxed_commands=false` → hard error at startup (refuse to run)
- `enabled=true` + no backend + `allow_unsandboxed_commands=true` → log warning, commands run unsandboxed through approval

**Unsandboxed commands always go through the approval flow.** The user sees a prompt indicating the command will run outside the sandbox. No silent execution.

**Per-command fallback, not permanent nullification:** The current code sets `s.sandbox = nil` on the first setup error, permanently disabling the sandbox for the session. The new behavior retries sandboxing on each command. If the sandbox fails again, the same per-command fallback-or-error logic applies. This prevents a transient error from permanently degrading the session.

### Feature 3: Excluded Commands

#### Matching Logic

Uses `parseCommandExecutable()` from `shell.go` (line 817), which parses shell words, skips env-var assignments (`FOO=bar`), and returns the executable name. For compound commands (pipes), the first segment is extracted by splitting on `|` before parsing.

The implementation composes existing shell parsing functions rather than duplicating logic:

```go
func isExcludedFromSandbox(fullCommand string, excluded []string) bool {
    // splitAllShellSegments (shell.go:758) splits on |, ;, &&, ||
    // with correct quoting. Take the first segment to find the lead command.
    segments := splitAllShellSegments(fullCommand)
    if len(segments) == 0 {
        return false
    }
    // isSegmentReadOnly (shell.go:502) demonstrates the pattern:
    // parseCommandExecutable → isCommandPrefixWrapper loop → base name.
    // We reuse the same approach here.
    exe := extractExecutableName(segments[0]) // see below
    for _, ex := range excluded {
        if exe == ex {
            return true
        }
    }
    return false
}

// extractExecutableName returns the base command name from a shell segment,
// stripping env-var assignments and command wrappers (env, sudo, command).
// Follows the same pattern as isSegmentReadOnly in shell.go:502-528.
func extractExecutableName(segment string) string {
    name, args := parseCommandExecutable(segment) // shell.go:817
    for isCommandPrefixWrapper(name) && len(args) > 0 {
        name, args = parseCommandExecutable(strings.Join(args, " "))
    }
    return filepath.Base(name) // /usr/bin/docker → docker
}
```

Key functions referenced (all in `shell.go`):
- `splitAllShellSegments` (line 758): splits on `|`, `;`, `&&`, `||` with quoting
- `parseCommandExecutable` (line 817): parses words, skips `FOO=bar` assignments
- `isCommandPrefixWrapper` (line 889): returns true for `env`, `command`, `sudo`

Examples:
- `docker build .` → exe = `docker` → excluded
- `sudo docker run nginx` → exe = `docker` → excluded
- `dockerfile-lint .` → exe = `dockerfile-lint` → not excluded
- `docker build . | grep error` → first segment exe = `docker` → entire pipeline excluded

#### Relationship to Escape Control

`excludedCommands` and `allowUnsandboxedCommands` are complementary:
- `excludedCommands` = "these specific commands bypass the sandbox"
- `allowUnsandboxedCommands` = "can ANY command fall back to unsandboxed on failure"

When `allowUnsandboxedCommands=false`, excluded commands still run unsandboxed (they are explicitly opted out). The flag only controls the *fallback* path, not explicit exclusions.

### Integration Points

#### Shell Tool Changes

`ShellTool` gains sandbox config and proxy reference:

```go
type ShellTool struct {
    workDir     string
    timeout     time.Duration
    sandbox     ShellSandbox
    sandboxCfg  config.SandboxConfig
    domainProxy *sandbox.DomainProxy  // nil when not configured
}
```

#### Startup Wiring

In `cmd/rubichan/main.go` (`runInteractive`/`runHeadless`):

```go
var domainProxy *sandbox.DomainProxy
if cfg.Sandbox.IsEnabled() && len(cfg.Sandbox.Network.AllowedDomains) > 0 {
    domainProxy = sandbox.NewDomainProxy(cfg.Sandbox.Network.AllowedDomains)
    addr, err := domainProxy.Start()
    defer domainProxy.Stop()
}

shellTool := tools.NewShellTool(cwd, shellTimeout)
shellTool.SetSandboxConfig(cfg.Sandbox, domainProxy)
```

#### Policy Construction

`DefaultShellSandboxPolicy` becomes `BuildSandboxPolicy(workDir string, cfg SandboxConfig)`:

- **Appends** `cfg.Filesystem.AllowWrite` to the default `WritablePaths` (CWD + tmp)
- **Appends** `cfg.Filesystem.DenyRead` to `DeniedPaths` (empty by default)
- Config paths do not replace defaults — they extend them
- If a `DenyRead` path overlaps a default `AllowedPath` (e.g., `/usr`), the deny takes precedence (deny-wins)
- Sets `AllowNetwork = true` when proxy is active (replaced by `ProxyPort > 0`)
- Sets `AllowNetwork = false` when no proxy

#### Package Layout

```
internal/tools/
  shell.go                 # decision flow changes
  shell_sandbox.go         # policy construction, Seatbelt/bwrap backends
internal/tools/sandbox/
  proxy.go                 # DomainProxy
  proxy_test.go
  domain.go                # domain matching (exact + glob)
  domain_test.go
internal/config/
  config.go                # SandboxConfig types
```

### Error Handling

#### Proxy Failures

| Failure | Handling |
|---------|----------|
| Port in use (non-zero `proxy_port`) | Startup error — user-specified port is unavailable. When `proxy_port = 0` (default), the OS assigns an ephemeral port and port-in-use cannot occur |
| Proxy crashes mid-command | Command gets connection refused; error surfaces through stderr |
| Proxy start fails | Fall back to `AllowNetwork=false`; log warning |

#### Config Errors

| Error | Handling |
|-------|----------|
| Invalid path prefix in `allow_write` | Startup error with message |
| `excluded_commands` entry contains `/` or spaces | Startup error |
| Invalid glob in `allowed_domains` | Startup error |
| `deny_read` overlaps `allow_write` | Warning, both applied |

### Testing Strategy

#### Unit Tests

| Component | Coverage |
|-----------|----------|
| `domain.go` | Exact match, glob match, no-match, empty, edge cases |
| `proxy.go` | HTTP allowed/blocked, CONNECT allowed/blocked, `onBlocked` callback, concurrent connections, shutdown |
| `config.go` | Defaults, TOML parsing, path resolution, validation errors |
| `shell.go` flow | Excluded bypasses sandbox, `allowUnsandboxedCommands=false` blocks fallback, setup error fallback, policy deny is always hard |
| `shell_sandbox.go` | `BuildSandboxPolicy` merges config, proxy env injected, Seatbelt profile emits correct `(allow network-outbound ...)` with port literal, bwrap gets `--share-net` |

#### Integration Tests (`//go:build integration`)

| Test | Validates |
|------|-----------|
| Proxy + `curl` | Allowed domain succeeds, blocked domain gets 403 |
| Excluded command | Runs unsandboxed when listed |
| Hard lockdown | `allowUnsandboxedCommands=false` + no backend = error |
| Platform sandbox + proxy | macOS: sandboxed process reaches proxy but not direct internet |
| Excluded + intercepted | Command in both `excludedCommands` and blocked by shell interceptor — interceptor wins (security) |
| Proxy shutdown mid-tunnel | Active CONNECT tunnel closed cleanly when proxy stops |
| Non-zero proxy port | User-specified port binding, port-in-use error |

### Backward Compatibility

- No `[sandbox]` section = current behavior unchanged
- `enabled = false` (default) = sandbox auto-detected but config enhancements inactive
- Existing `ShellSandbox` interface unchanged
- `ShellSandboxPolicy` struct unchanged (populated differently)

### Known Limitations

- **Linux network enforcement is advisory:** On Linux, bubblewrap's `--share-net` gives full network access. The proxy filters traffic only for programs that honor `HTTP_PROXY`/`HTTPS_PROXY`. Programs using raw sockets, `--noproxy`, or custom transports bypass filtering. On macOS, Seatbelt provides true kernel-level enforcement to localhost:PORT only.
- **Domain fronting:** The proxy filters on SNI/Host header. A CONNECT to `allowed.com` that domain-fronts to `evil.com` at the CDN level bypasses filtering. This is a known limitation shared with Claude Code.
- **Hardcoded IPs:** Commands connecting to raw IPs bypass domain filtering. The proxy only sees IPs, not domains. Mitigation: the OS sandbox still blocks network when no proxy is configured.
- **Unix sockets:** Not filtered by the proxy. If a sandboxed command accesses Docker socket or similar, it could bypass restrictions. Future work: `deny_unix_sockets` config option.
- **Performance:** The proxy adds one hop for every HTTP(S) connection. For typical development workflows (package downloads, API calls) this is negligible.
