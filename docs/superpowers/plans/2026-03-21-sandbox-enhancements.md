# Sandbox Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add network domain proxy, sandbox escape control, and excluded commands to the shell sandbox.

**Architecture:** Three features share a new `[sandbox]` config section. The domain proxy runs as an in-process HTTP/CONNECT proxy outside the sandbox. Escape control replaces the silent fallback with configurable hard-error-or-approval behavior. Excluded commands use existing shell parsing to bypass the sandbox for specific tools.

**Tech Stack:** Go stdlib `net/http`, Seatbelt SBPL, Bubblewrap, TOML config

**Spec:** `docs/superpowers/specs/2026-03-21-sandbox-enhancements-design.md`

---

### Task 1: Add SandboxConfig to config package

**Files:**
- Modify: `internal/config/config.go:12-23` (add Sandbox field to Config struct)
- Modify: `internal/config/config_test.go` (add config parsing tests)

- [ ] **Step 1: Write failing test — SandboxConfig defaults**

```go
func TestSandboxConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	assert.False(t, cfg.Sandbox.IsEnabled(), "sandbox disabled by default")
	assert.True(t, cfg.Sandbox.IsAllowUnsandboxedCommands(), "unsandboxed allowed by default")
	assert.Empty(t, cfg.Sandbox.ExcludedCommands)
	assert.Empty(t, cfg.Sandbox.Network.AllowedDomains)
	assert.Equal(t, 0, cfg.Sandbox.Network.ProxyPort)
	assert.Empty(t, cfg.Sandbox.Filesystem.AllowWrite)
	assert.Empty(t, cfg.Sandbox.Filesystem.DenyRead)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run TestSandboxConfigDefaults -v`
Expected: FAIL — `IsEnabled` and `IsAllowUnsandboxedCommands` not defined

- [ ] **Step 3: Implement SandboxConfig types**

Add to `internal/config/config.go`:

```go
// In Config struct:
Sandbox SandboxConfig `toml:"sandbox"`

// New types:
type SandboxConfig struct {
	Enabled                  *bool    `toml:"enabled"`
	AllowUnsandboxedCommands *bool    `toml:"allow_unsandboxed_commands"`
	ExcludedCommands         []string `toml:"excluded_commands"`
	Network                  SandboxNetworkConfig    `toml:"network"`
	Filesystem               SandboxFilesystemConfig `toml:"filesystem"`
}

func (c SandboxConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

func (c SandboxConfig) IsAllowUnsandboxedCommands() bool {
	if c.AllowUnsandboxedCommands == nil {
		return true
	}
	return *c.AllowUnsandboxedCommands
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -run TestSandboxConfigDefaults -v`
Expected: PASS

- [ ] **Step 5: Write failing test — TOML parsing**

```go
func TestSandboxConfigParsing(t *testing.T) {
	tomlContent := `
[sandbox]
enabled = true
allow_unsandboxed_commands = false
excluded_commands = ["docker", "watchman"]

[sandbox.network]
allowed_domains = ["github.com", "*.npmjs.org"]
proxy_port = 8080

[sandbox.filesystem]
allow_write = ["/tmp/build"]
deny_read = ["~/.ssh"]
`
	// Write to temp file and parse
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(tomlContent), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.Sandbox.IsEnabled())
	assert.False(t, cfg.Sandbox.IsAllowUnsandboxedCommands())
	assert.Equal(t, []string{"docker", "watchman"}, cfg.Sandbox.ExcludedCommands)
	assert.Equal(t, []string{"github.com", "*.npmjs.org"}, cfg.Sandbox.Network.AllowedDomains)
	assert.Equal(t, 8080, cfg.Sandbox.Network.ProxyPort)
	assert.Equal(t, []string{"/tmp/build"}, cfg.Sandbox.Filesystem.AllowWrite)
	assert.Equal(t, []string{"~/.ssh"}, cfg.Sandbox.Filesystem.DenyRead)
}
```

- [ ] **Step 6: Run test, verify it passes** (types already exist, TOML parsing is automatic)

Run: `go test ./internal/config/... -run TestSandboxConfigParsing -v`
Expected: PASS

- [ ] **Step 7: Write failing test — config validation**

```go
func TestSandboxConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SandboxConfig
		wantErr string
	}{
		{
			name:    "excluded command with path",
			cfg:     SandboxConfig{ExcludedCommands: []string{"/usr/bin/docker"}},
			wantErr: "must be bare command names",
		},
		{
			name:    "excluded command with spaces",
			cfg:     SandboxConfig{ExcludedCommands: []string{"docker build"}},
			wantErr: "must be bare command names",
		},
		{
			name:    "domain with scheme",
			cfg:     SandboxConfig{Network: SandboxNetworkConfig{AllowedDomains: []string{"https://github.com"}}},
			wantErr: "use \"github.com\"",
		},
		{
			name:    "domain with port",
			cfg:     SandboxConfig{Network: SandboxNetworkConfig{AllowedDomains: []string{"github.com:443"}}},
			wantErr: "use \"github.com\"",
		},
		{
			name: "valid config",
			cfg:  SandboxConfig{ExcludedCommands: []string{"docker"}, Network: SandboxNetworkConfig{AllowedDomains: []string{"github.com", "*.npmjs.org"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
```

- [ ] **Step 8: Implement Validate() method**

```go
func (c SandboxConfig) Validate() error {
	for _, cmd := range c.ExcludedCommands {
		if strings.Contains(cmd, "/") || strings.Contains(cmd, " ") {
			return fmt.Errorf("excluded_commands: %q must be bare command names (no paths or spaces)", cmd)
		}
	}
	for _, p := range append(c.Filesystem.AllowWrite, c.Filesystem.DenyRead...) {
		if p == "" {
			return fmt.Errorf("filesystem paths must not be empty")
		}
		if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, "./") {
			return fmt.Errorf("filesystem path %q must start with /, ~/, or ./", p)
		}
	}
	for _, d := range c.Network.AllowedDomains {
		if strings.Contains(d, "://") {
			return fmt.Errorf("allowed_domains: %q has a scheme prefix — use %q not %q", d, strings.SplitN(d, "://", 2)[1], d)
		}
		if strings.Contains(d, ":") && !strings.HasPrefix(d, "*") {
			return fmt.Errorf("allowed_domains: %q has a port suffix — use the domain only", d)
		}
	}
	return nil
}
```

- [ ] **Step 9: Run all config tests**

Run: `go test ./internal/config/... -v`
Expected: ALL PASS

- [ ] **Step 10: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "[BEHAVIORAL] add SandboxConfig types with validation"
```

---

### Task 2: Add domain matching package

**Files:**
- Create: `internal/tools/sandbox/domain.go`
- Create: `internal/tools/sandbox/domain_test.go`

- [ ] **Step 1: Write failing test — exact domain match**

```go
// internal/tools/sandbox/domain_test.go
package sandbox_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/tools/sandbox"
	"github.com/stretchr/testify/assert"
)

func TestMatchDomainExact(t *testing.T) {
	assert.True(t, sandbox.MatchDomain("github.com", []string{"github.com"}))
	assert.False(t, sandbox.MatchDomain("evil.com", []string{"github.com"}))
	assert.False(t, sandbox.MatchDomain("github.com.evil.com", []string{"github.com"}))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/sandbox/... -run TestMatchDomainExact -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement MatchDomain with exact match**

```go
// internal/tools/sandbox/domain.go
package sandbox

import "strings"

// MatchDomain checks if domain matches any pattern in the allowlist.
// Patterns can be exact ("github.com") or wildcard ("*.npmjs.org").
func MatchDomain(domain string, patterns []string) bool {
	domain = strings.ToLower(domain)
	for _, p := range patterns {
		p = strings.ToLower(p)
		if domain == p {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/sandbox/... -run TestMatchDomainExact -v`
Expected: PASS

- [ ] **Step 5: Write failing test — wildcard match**

```go
func TestMatchDomainWildcard(t *testing.T) {
	patterns := []string{"*.npmjs.org"}
	assert.True(t, sandbox.MatchDomain("registry.npmjs.org", patterns))
	assert.True(t, sandbox.MatchDomain("www.npmjs.org", patterns))
	assert.False(t, sandbox.MatchDomain("npmjs.org", patterns), "bare domain should not match *.domain")
	assert.False(t, sandbox.MatchDomain("evil.org", patterns))
}
```

- [ ] **Step 6: Implement wildcard matching**

Add to `MatchDomain` loop:

```go
if strings.HasPrefix(p, "*.") {
	suffix := p[1:] // ".npmjs.org"
	if strings.HasSuffix(domain, suffix) && len(domain) > len(suffix) {
		return true
	}
}
```

- [ ] **Step 7: Write edge case tests**

```go
func TestMatchDomainEdgeCases(t *testing.T) {
	assert.False(t, sandbox.MatchDomain("", []string{"github.com"}), "empty domain")
	assert.False(t, sandbox.MatchDomain("github.com", nil), "nil patterns")
	assert.False(t, sandbox.MatchDomain("github.com", []string{}), "empty patterns")
	assert.True(t, sandbox.MatchDomain("GitHub.COM", []string{"github.com"}), "case insensitive")
}
```

- [ ] **Step 8: Run all domain tests**

Run: `go test ./internal/tools/sandbox/... -v`
Expected: ALL PASS

- [ ] **Step 9: Commit**

```bash
git add internal/tools/sandbox/
git commit -m "[BEHAVIORAL] add domain matching with exact and wildcard support"
```

---

### Task 3: Implement DomainProxy

**Files:**
- Create: `internal/tools/sandbox/proxy.go`
- Create: `internal/tools/sandbox/proxy_test.go`

- [ ] **Step 1: Write failing test — proxy blocks disallowed HTTP request**

```go
func TestProxyBlocksDisallowedHTTP(t *testing.T) {
	var blocked []string
	p := sandbox.NewDomainProxy(
		[]string{"allowed.com"},
		sandbox.WithOnBlocked(func(domain, cmd string) { blocked = append(blocked, domain) }),
	)
	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	proxyURL, _ := url.Parse("http://" + addr)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	resp, err := client.Get("http://evil.com/data")
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Contains(t, blocked, "evil.com")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/sandbox/... -run TestProxyBlocksDisallowedHTTP -v`
Expected: FAIL — `NewDomainProxy` not defined

- [ ] **Step 3: Implement DomainProxy — HTTP handling**

```go
// internal/tools/sandbox/proxy.go
package sandbox

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
)

type ProxyOption func(*DomainProxy)

func WithOnBlocked(fn func(domain, command string)) ProxyOption {
	return func(p *DomainProxy) { p.onBlocked = fn }
}

type DomainProxy struct {
	listener  net.Listener
	allowed   []string          // immutable after construction
	onBlocked func(string, string)
	mu        sync.RWMutex
	runtime   map[string]bool
	server    *http.Server
}

func NewDomainProxy(allowed []string, opts ...ProxyOption) *DomainProxy {
	p := &DomainProxy{
		allowed: allowed,
		runtime: make(map[string]bool),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *DomainProxy) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("proxy listen: %w", err)
	}
	p.listener = ln
	p.server = &http.Server{Handler: p}
	go p.server.Serve(ln)
	return ln.Addr().String(), nil
}

func (p *DomainProxy) Stop() error {
	if p.server != nil {
		return p.server.Close()
	}
	return nil
}

func (p *DomainProxy) Port() int {
	if p.listener == nil {
		return 0
	}
	return p.listener.Addr().(*net.TCPAddr).Port
}

func (p *DomainProxy) AllowDomain(domain string) {
	p.mu.Lock()
	p.runtime[domain] = true
	p.mu.Unlock()
}

func (p *DomainProxy) isAllowed(host string) bool {
	// Strip port if present
	domain := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		domain = h
	}
	// Check runtime additions
	p.mu.RLock()
	if p.runtime[domain] {
		p.mu.RUnlock()
		return true
	}
	p.mu.RUnlock()
	// Check static allowlist
	return MatchDomain(domain, p.allowed)
}

func (p *DomainProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

func (p *DomainProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if !p.isAllowed(host) {
		if p.onBlocked != nil {
			p.onBlocked(host, "")
		}
		http.Error(w, fmt.Sprintf("domain %q not in sandbox allowlist", host), http.StatusForbidden)
		return
	}
	// Forward the request — clear RequestURI (Go's RoundTrip rejects it)
	// and strip hop-by-hop headers (Proxy-Authorization, Proxy-Connection).
	r.RequestURI = ""
	r.Header.Del("Proxy-Authorization")
	r.Header.Del("Proxy-Connection")
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/sandbox/... -run TestProxyBlocksDisallowedHTTP -v`
Expected: PASS

- [ ] **Step 5: Write failing test — proxy allows CONNECT for allowed domain**

```go
func TestProxyAllowsCONNECT(t *testing.T) {
	// Use a local TCP server as the CONNECT target to avoid network dependency
	target := newLocalTCPServer(t) // helper: net.Listen, accept, echo, close
	defer target.Close()
	host, port, _ := net.SplitHostPort(target.Addr().String())

	p := sandbox.NewDomainProxy([]string{host})
	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	fmt.Fprintf(conn, "CONNECT %s:%s HTTP/1.1\r\nHost: %s:%s\r\n\r\n", host, port, host, port)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestProxyBlocksCONNECT(t *testing.T) {
	p := sandbox.NewDomainProxy([]string{"allowed.com"})
	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	fmt.Fprintf(conn, "CONNECT evil.com:443 HTTP/1.1\r\nHost: evil.com:443\r\n\r\n")
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}
```

- [ ] **Step 6: Implement CONNECT handler**

```go
func (p *DomainProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if !p.isAllowed(host) {
		if p.onBlocked != nil {
			domain := host
			if h, _, err := net.SplitHostPort(host); err == nil {
				domain = h
			}
			p.onBlocked(domain, "")
		}
		http.Error(w, fmt.Sprintf("domain %q not in sandbox allowlist", host), http.StatusForbidden)
		return
	}

	targetConn, err := net.DialTimeout("tcp", host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		targetConn.Close()
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		targetConn.Close()
		return
	}

	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	go func() {
		defer targetConn.Close()
		defer clientConn.Close()
		io.Copy(targetConn, clientConn)
	}()
	go func() {
		defer targetConn.Close()
		defer clientConn.Close()
		io.Copy(clientConn, targetConn)
	}()
}
```

- [ ] **Step 7: Write test — runtime AllowDomain**

```go
func TestProxyRuntimeAllow(t *testing.T) {
	// Use a local HTTP server as the target
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	targetHost := target.Listener.Addr().String()

	p := sandbox.NewDomainProxy([]string{})
	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	proxyURL, _ := url.Parse("http://" + addr)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	// Initially blocked
	resp, _ := client.Get("http://" + targetHost + "/")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Add the host at runtime
	host, _, _ := net.SplitHostPort(targetHost)
	p.AllowDomain(host)

	// Now should be allowed
	resp, err = client.Get("http://" + targetHost + "/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
```

- [ ] **Step 8: Write test — graceful shutdown**

```go
func TestProxyShutdown(t *testing.T) {
	p := sandbox.NewDomainProxy([]string{"example.com"})
	_, err := p.Start()
	require.NoError(t, err)

	err = p.Stop()
	assert.NoError(t, err)

	// Double-stop is safe
	err = p.Stop()
	assert.NoError(t, err)
}
```

- [ ] **Step 9: Run all proxy tests**

Run: `go test ./internal/tools/sandbox/... -v -race`
Expected: ALL PASS

- [ ] **Step 10: Commit**

```bash
git add internal/tools/sandbox/proxy.go internal/tools/sandbox/proxy_test.go
git commit -m "[BEHAVIORAL] add DomainProxy with HTTP and CONNECT filtering"
```

---

### Task 4: Update ShellSandboxPolicy and sandbox backends

**Files:**
- Modify: `internal/tools/shell_sandbox.go:24-30` (AllowNetwork → ProxyPort)
- Modify: `internal/tools/shell_sandbox.go:239` (buildSeatbeltProfile)
- Modify: `internal/tools/shell_sandbox.go:273` (sandboxCommandEnv)
- Modify: `internal/tools/shell_sandbox_test.go`

- [ ] **Step 1: Write failing test — ProxyPort in Seatbelt profile**

```go
func TestSeatbeltProfileWithProxyPort(t *testing.T) {
	policy := ShellSandboxPolicy{
		AllowedPaths:  []string{"/tmp/test"},
		WritablePaths: []string{"/tmp/test"},
		ProxyPort:     12345,
		AllowSubprocs: true,
	}
	profile := buildSeatbeltProfile(policy)
	// NOTE: The exact SBPL syntax for port-scoped network-outbound must be
	// verified against macOS sandbox profile documentation at implementation time.
	// The profile should contain an allow rule for 127.0.0.1 with the proxy port.
	assert.Contains(t, profile, "network-outbound")
	assert.Contains(t, profile, "127.0.0.1")
	assert.Contains(t, profile, "12345")
	assert.NotContains(t, profile, "(deny network*)")
}

func TestSeatbeltProfileNoNetwork(t *testing.T) {
	policy := ShellSandboxPolicy{
		AllowedPaths:  []string{"/tmp/test"},
		WritablePaths: []string{"/tmp/test"},
		ProxyPort:     0,
		AllowSubprocs: true,
	}
	profile := buildSeatbeltProfile(policy)
	assert.NotContains(t, profile, "(allow network-outbound")
	// (deny default) already blocks network; explicit deny is optional
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/... -run TestSeatbeltProfile -v`
Expected: FAIL — `ProxyPort` field does not exist

- [ ] **Step 3: Replace AllowNetwork with ProxyPort in ShellSandboxPolicy**

In `shell_sandbox.go`:

```go
type ShellSandboxPolicy struct {
	ProxyPort     int      // 0 = network blocked; >0 = allow outbound to 127.0.0.1:port
	AllowedPaths  []string
	WritablePaths []string
	DeniedPaths   []string
	AllowSubprocs bool
}
```

Update `DefaultShellSandboxPolicy` — remove `AllowNetwork: false` (ProxyPort defaults to 0).

Update `buildSeatbeltProfile`:

```go
if policy.ProxyPort > 0 {
	lines = append(lines, fmt.Sprintf(`(allow network-outbound (remote ip "127.0.0.1") (remote tcp-port %d))`, policy.ProxyPort))
}
// Remove old: if !policy.AllowNetwork { lines = append(lines, "(deny network*)") }
```

Update `bubblewrapArgs` — replace `if policy.AllowNetwork` with `if policy.ProxyPort > 0`:

```go
if policy.ProxyPort > 0 {
	args = append(args, "--share-net")
}
```

- [ ] **Step 4: Update sandboxCommandEnv to inject proxy vars**

```go
func sandboxCommandEnv(cmd *exec.Cmd, proxyPort int) []string {
	// ... existing HOME/XDG redirection ...
	if proxyPort > 0 {
		proxyAddr := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
		env = append(env,
			"HTTP_PROXY="+proxyAddr,
			"HTTPS_PROXY="+proxyAddr,
			"http_proxy="+proxyAddr,
			"https_proxy="+proxyAddr,
			"NO_PROXY=localhost,127.0.0.1",
			"no_proxy=localhost,127.0.0.1",
		)
	}
	return env
}
```

Update callers of `sandboxCommandEnv` to pass `policy.ProxyPort`. Both `seatbeltSandbox.Wrap()` and `bubblewrapSandbox.Wrap()` already store `policy` in their struct — they pass `s.policy.ProxyPort` to `sandboxCommandEnv`.

Also update `selectShellSandbox` and `NewDefaultShellSandbox` to accept a `ShellSandboxPolicy` parameter instead of constructing one internally from `DefaultShellSandboxPolicy`. This allows `BuildSandboxPolicy` (Task 5) to inject the config-derived policy at construction time rather than retrofitting it.

- [ ] **Step 5: Fix existing tests that reference AllowNetwork**

Search for `AllowNetwork` in test files and update to `ProxyPort: 0` (blocked) or `ProxyPort: PORT` (allowed).

- [ ] **Step 6: Run all sandbox tests**

Run: `go test ./internal/tools/... -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tools/shell_sandbox.go internal/tools/shell_sandbox_test.go
git commit -m "[BEHAVIORAL] replace AllowNetwork with ProxyPort in sandbox policy"
```

---

### Task 5: Add BuildSandboxPolicy from config

**Files:**
- Modify: `internal/tools/shell_sandbox.go` (add BuildSandboxPolicy)
- Modify: `internal/tools/shell_sandbox_test.go`

- [ ] **Step 1: Write failing test — config merges into policy**

```go
func TestBuildSandboxPolicyMergesConfig(t *testing.T) {
	cfg := config.SandboxConfig{
		Filesystem: config.SandboxFilesystemConfig{
			AllowWrite: []string{"/opt/custom"},
			DenyRead:   []string{"/etc/secrets"},
		},
		Network: config.SandboxNetworkConfig{ProxyPort: 9999},
	}
	policy := BuildSandboxPolicy("/project", cfg)

	assert.Contains(t, policy.WritablePaths, "/project")
	assert.Contains(t, policy.WritablePaths, "/opt/custom")
	assert.Contains(t, policy.DeniedPaths, "/etc/secrets")
	assert.Equal(t, 9999, policy.ProxyPort)
}

func TestBuildSandboxPolicyDefaults(t *testing.T) {
	policy := BuildSandboxPolicy("/project", config.SandboxConfig{})
	assert.Contains(t, policy.WritablePaths, "/project")
	assert.Equal(t, 0, policy.ProxyPort)
	assert.Empty(t, policy.DeniedPaths)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/... -run TestBuildSandboxPolicy -v`
Expected: FAIL — `BuildSandboxPolicy` not defined

- [ ] **Step 3: Implement BuildSandboxPolicy**

```go
func BuildSandboxPolicy(workDir string, cfg config.SandboxConfig) ShellSandboxPolicy {
	policy := DefaultShellSandboxPolicy(workDir)
	policy.ProxyPort = cfg.Network.ProxyPort
	policy.WritablePaths = append(policy.WritablePaths, normalizeSandboxPaths(cfg.Filesystem.AllowWrite)...)
	policy.DeniedPaths = append(policy.DeniedPaths, normalizeSandboxPaths(cfg.Filesystem.DenyRead)...)
	return policy
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tools/... -run TestBuildSandboxPolicy -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/shell_sandbox.go internal/tools/shell_sandbox_test.go
git commit -m "[BEHAVIORAL] add BuildSandboxPolicy for config-driven sandbox policy"
```

---

### Task 6: Add excluded commands and escape control to shell.go

**Files:**
- Modify: `internal/tools/shell.go:89-95` (ShellTool struct)
- Modify: `internal/tools/shell.go:165-296` (ExecuteStream decision flow)
- Modify: `internal/tools/shell_test.go`

- [ ] **Step 1: Write failing test — excluded command bypasses sandbox**

```go
func TestShellToolExcludedCommandBypassesSandbox(t *testing.T) {
	st := NewShellTool(t.TempDir(), 30*time.Second)
	recorder := &recordingSandbox{}
	st.SetSandbox(recorder)
	st.SetSandboxConfig(config.SandboxConfig{
		ExcludedCommands: []string{"docker"},
	}, nil)

	input, _ := json.Marshal(shellInput{Command: "docker --version"})
	result, err := st.ExecuteStream(context.Background(), input, nil)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.False(t, recorder.called, "sandbox should not wrap excluded commands")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/... -run TestShellToolExcludedCommand -v`
Expected: FAIL — `SetSandboxConfig` not defined

- [ ] **Step 3: Add config fields to ShellTool and SetSandboxConfig**

```go
type ShellTool struct {
	workDir     string
	timeout     time.Duration
	sandbox     ShellSandbox
	sandboxCfg  config.SandboxConfig
	domainProxy *sandbox.DomainProxy
	// ... existing fields ...
}

func (s *ShellTool) SetSandboxConfig(cfg config.SandboxConfig, proxy *sandbox.DomainProxy) {
	s.sandboxCfg = cfg
	s.domainProxy = proxy
}
```

- [ ] **Step 4: Add extractExecutableName and isExcludedFromSandbox**

```go
func extractExecutableName(segment string) string {
	name, args := parseCommandExecutable(segment)
	for isCommandPrefixWrapper(name) && len(args) > 0 {
		name, args = parseCommandExecutable(strings.Join(args, " "))
	}
	return filepath.Base(name)
}

func IsExcludedFromSandbox(fullCommand string, excluded []string) bool {
	if len(excluded) == 0 {
		return false
	}
	segments := splitAllShellSegments(fullCommand)
	if len(segments) == 0 {
		return false
	}
	exe := extractExecutableName(segments[0])
	for _, ex := range excluded {
		if exe == ex {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Modify ExecuteStream decision flow**

Replace the sandbox section at `shell.go:283-296`:

```go
if s.sandbox != nil && !IsExcludedFromSandbox(in.Command, s.sandboxCfg.ExcludedCommands) {
	if err := s.sandbox.Wrap(cmd); err != nil {
		if isSandboxUnavailableError(err) {
			if !s.sandboxCfg.IsAllowUnsandboxedCommands() {
				res := ToolResult{Content: "sandbox unavailable and unsandboxed execution is disabled", IsError: true}
				emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
				return res, nil
			}
			// Per-command fallback — do NOT set s.sandbox = nil
			// Command runs unsandboxed through normal approval flow
		} else {
			// Policy deny or other hard error
			res := withInterceptionWarnings(ToolResult{
				Content: fmt.Sprintf("tool execution error: sandbox: %s", err.Error()),
				IsError: true,
			}, interception.warnings)
			emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Display(), IsError: true})
			return res, nil
		}
	}
}
```

- [ ] **Step 6: Write failing test — allowUnsandboxedCommands=false blocks fallback**

```go
func TestShellToolHardLockdownBlocksFallback(t *testing.T) {
	st := NewShellTool(t.TempDir(), 30*time.Second)
	st.SetSandbox(&recordingSandbox{err: errors.New("sandbox unavailable")})

	f := false
	st.SetSandboxConfig(config.SandboxConfig{
		AllowUnsandboxedCommands: &f,
	}, nil)

	input, _ := json.Marshal(shellInput{Command: "echo hello"})
	result, err := st.ExecuteStream(context.Background(), input, nil)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unsandboxed execution is disabled")
}
```

- [ ] **Step 7: Write test — sudo excluded command**

```go
func TestShellToolExcludedSudoCommand(t *testing.T) {
	st := NewShellTool(t.TempDir(), 30*time.Second)
	recorder := &recordingSandbox{}
	st.SetSandbox(recorder)
	st.SetSandboxConfig(config.SandboxConfig{
		ExcludedCommands: []string{"docker"},
	}, nil)

	input, _ := json.Marshal(shellInput{Command: "sudo docker run nginx"})
	result, err := st.ExecuteStream(context.Background(), input, nil)
	require.NoError(t, err)
	assert.False(t, recorder.called, "sudo docker should match excluded 'docker'")
}
```

- [ ] **Step 8: Run all shell tests**

Run: `go test ./internal/tools/... -v`
Expected: ALL PASS

- [ ] **Step 9: Commit**

```bash
git add internal/tools/shell.go internal/tools/shell_test.go
git commit -m "[BEHAVIORAL] add excluded commands and sandbox escape control"
```

---

### Task 7: Wire everything into main.go

**Files:**
- Modify: `cmd/rubichan/main.go` (startup wiring)

- [ ] **Step 1: Add sandbox config validation at startup**

In both `runInteractive` and `runHeadless`, after `loadConfig`:

```go
if err := cfg.Sandbox.Validate(); err != nil {
	return fmt.Errorf("sandbox config: %w", err)
}
```

- [ ] **Step 2: Wire DomainProxy and sandbox config into shell tool**

```go
var domainProxy *sandbox.DomainProxy
if cfg.Sandbox.IsEnabled() && len(cfg.Sandbox.Network.AllowedDomains) > 0 {
	domainProxy = sandbox.NewDomainProxy(
		cfg.Sandbox.Network.AllowedDomains,
		sandbox.WithOnBlocked(func(domain, cmd string) {
			log.Printf("[sandbox] blocked connection to %s", domain)
		}),
	)
	if _, err := domainProxy.Start(); err != nil {
		log.Printf("warning: sandbox proxy failed to start: %v", err)
	} else {
		defer domainProxy.Stop()
	}
}

// After shell tool creation:
shellTool.SetSandboxConfig(cfg.Sandbox, domainProxy)
```

- [ ] **Step 3: Update sandbox policy construction**

Where `NewDefaultShellSandbox` is called, pass the proxy port:

```go
if cfg.Sandbox.IsEnabled() {
	policy := BuildSandboxPolicy(cwd, cfg.Sandbox)
	if domainProxy != nil {
		policy.ProxyPort = domainProxy.Port()
	}
	// Use policy in sandbox construction
}
```

- [ ] **Step 4: Add startup check for hard lockdown without backend**

```go
if cfg.Sandbox.IsEnabled() && !cfg.Sandbox.IsAllowUnsandboxedCommands() && shellTool.Sandbox() == nil {
	return fmt.Errorf("sandbox enabled with allow_unsandboxed_commands=false but no sandbox backend available")
}
```

- [ ] **Step 5: Run full test suite**

Run: `go test ./... -timeout=120s`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] wire sandbox config, proxy, and escape control into main"
```

---

### Task 8: Integration tests

**Files:**
- Create: `internal/tools/sandbox/integration_test.go` (build-tagged)

- [ ] **Step 1: Write integration test — proxy with curl**

```go
//go:build integration

func TestProxyIntegrationCurl(t *testing.T) {
	p := sandbox.NewDomainProxy([]string{"httpbin.org"})
	addr, err := p.Start()
	require.NoError(t, err)
	defer p.Stop()

	// Allowed domain
	cmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		"--proxy", "http://"+addr, "http://httpbin.org/get")
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "200", strings.TrimSpace(string(out)))

	// Blocked domain
	cmd = exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		"--proxy", "http://"+addr, "http://evil.com/")
	out, _ = cmd.Output()
	assert.Equal(t, "403", strings.TrimSpace(string(out)))
}
```

- [ ] **Step 2: Write integration test — excluded command detection**

```go
func TestExcludedCommandIntegration(t *testing.T) {
	tests := []struct {
		command  string
		excluded []string
		want     bool
	}{
		{"docker build .", []string{"docker"}, true},
		{"sudo docker run nginx", []string{"docker"}, true},
		{"env FOO=bar docker ps", []string{"docker"}, true},
		{"dockerfile-lint .", []string{"docker"}, false},
		{"echo hello", []string{"docker"}, false},
		{"docker build . | grep error", []string{"docker"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := tools.IsExcludedFromSandbox(tt.command, tt.excluded)
			assert.Equal(t, tt.want, got)
		})
	}
}
```

- [ ] **Step 3: Run integration tests**

Run: `go test ./internal/tools/sandbox/... -tags=integration -v`
Expected: ALL PASS (requires network access)

- [ ] **Step 4: Commit**

```bash
git add internal/tools/sandbox/integration_test.go
git commit -m "[BEHAVIORAL] add sandbox integration tests"
```
