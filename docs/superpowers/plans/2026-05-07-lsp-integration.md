# LSP (Language Server Protocol) Integration Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port Claude Code's LSP client to rubichan. Enables connecting to language servers for diagnostics, code completion, and symbol information.

**Architecture:** `LSPClient` manages JSON-RPC communication with language servers over stdio. `LSPDiagnostics` collects and formats diagnostics. Integrated as a tool that agents can call for code analysis.

**Tech Stack:** Go, existing `tools.Registry`, JSON-RPC over stdio.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/lsp.go` | `LSPConfig`, `LSPDiagnostic` SDK types |
| `internal/tools/lsp/client.go` | `LSPClient`, JSON-RPC communication |
| `internal/tools/lsp/diagnostics.go` | `LSPDiagnostics`, formatting |
| `internal/tools/lsp/client_test.go` | Tests for connection, requests |
| `internal/tools/lsp/tool.go` | `LSPDiagnosticsTool` for rubichan registry |

---

## Chunk 1: SDK Types

### Task 1: Define LSP SDK types

**Files:**
- Create: `pkg/agentsdk/lsp.go`

**Code:**

```go
package agentsdk

// LSPConfig defines a language server connection.
type LSPConfig struct {
	Name    string   `json:"name"`    // e.g. "gopls", "typescript-language-server"
	Command string   `json:"command"` // executable path
	Args    []string `json:"args"`
	RootURI string   `json:"root_uri"`
}

// LSPDiagnostic represents a single diagnostic from a language server.
type LSPDiagnostic struct {
	Severity    int    `json:"severity"` // 1=Error, 2=Warning, 3=Info, 4=Hint
	Message     string `json:"message"`
	Source      string `json:"source"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	Code        string `json:"code,omitempty"`
	FilePath    string `json:"file_path"`
}
```

**Test:**

```go
package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLSPConfig(t *testing.T) {
	cfg := LSPConfig{
		Name:    "gopls",
		Command: "gopls",
		Args:    []string{"serve"},
		RootURI: "file:///Users/julian/project",
	}
	require.Equal(t, "gopls", cfg.Name)
}

func TestLSPDiagnostic(t *testing.T) {
	d := LSPDiagnostic{
		Severity: 1,
		Message:  "undefined: foo",
		Source:   "compiler",
		Line:     42,
		Column:   10,
		FilePath: "/tmp/main.go",
	}
	require.Equal(t, "undefined: foo", d.Message)
	require.Equal(t, 42, d.Line)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestLSP -v
```

**Expected:** PASS.

---

## Chunk 2: LSP Client Core

### Task 2: Implement LSPClient

**Files:**
- Create: `internal/tools/lsp/client.go`

**Code:**

```go
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// LSPClient manages JSON-RPC communication with a language server.
type LSPClient struct {
	config *agentsdk.LSPConfig
	cmd    *exec.Cmd
	stdin  *json.Encoder
	stdout *json.Decoder
	nextID int
}

// NewClient creates an LSP client for the given config.
func NewClient(cfg *agentsdk.LSPConfig) *LSPClient {
	return &LSPClient{config: cfg}
}

// Connect starts the language server process.
func (c *LSPClient) Connect(ctx context.Context) error {
	c.cmd = exec.CommandContext(ctx, c.config.Command, c.config.Args...)
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("lsp stdin: %w", err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("lsp stdout: %w", err)
	}
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("lsp start: %w", err)
	}

	c.stdin = json.NewEncoder(stdin)
	c.stdout = json.NewDecoder(stdout)

	// Send initialize
	return c.initialize(ctx)
}

// Close shuts down the language server.
func (c *LSPClient) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return nil
}

// GetDiagnostics requests diagnostics for a file.
func (c *LSPClient) GetDiagnostics(ctx context.Context, filePath string) ([]agentsdk.LSPDiagnostic, error) {
	// Simplified: in real implementation, use textDocument/diagnostic or publishDiagnostics
	return nil, fmt.Errorf("not yet implemented")
}

func (c *LSPClient) initialize(ctx context.Context) error {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "initialize",
		"params": map[string]any{
			"processId": nil,
			"rootUri":   c.config.RootURI,
			"capabilities": map[string]any{},
		},
	}
	return c.stdin.Encode(req)
}

func (c *LSPClient) nextID() int {
	c.nextID++
	return c.nextID
}
```

**Test:**

```go
package lsp

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	cfg := &agentsdk.LSPConfig{Name: "test", Command: "echo"}
	c := NewClient(cfg)
	require.NotNil(t, c)
}
```

**Command:**
```bash
go test ./internal/tools/lsp/... -run TestNewClient -v
```

**Expected:** PASS.

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/tools/lsp/...
golangci-lint run ./internal/tools/lsp/...
gofmt -l .
```

---

## PR Description

**Title:** `[STRUCTURAL] LSP client for language server integration`

**Body:**
- `LSPClient` manages JSON-RPC over stdio with language servers
- `Connect()` starts server process and sends initialize
- `Close()` shuts down server
- `GetDiagnostics()` requests file diagnostics (stub)
- SDK types: `LSPConfig`, `LSPDiagnostic`
- Ports Claude Code's `lsp/LSPClient.ts` pattern to Go

**Commit prefix:** `[STRUCTURAL]`
