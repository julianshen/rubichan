# Ollama Support Enhancement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add CLI model management, auto-detection, interactive model picker, and integration tests for Ollama.

**Architecture:** New `client.go` in the existing ollama package provides a thin HTTP client for the Ollama REST API (list/pull/delete/version). CLI subcommands in `cmd/rubichan/ollama.go` wrap the client. Auto-detection probes localhost:11434 during config loading. A Bubble Tea model picker component handles interactive model selection.

**Tech Stack:** Go stdlib `net/http`, `encoding/json`, `bufio`; Bubble Tea (`charmbracelet/bubbletea`, `charmbracelet/bubbles/list`); Cobra CLI; `httptest` for mocking.

---

## PR 1: Ollama API Client (S-M)

### Task 1: Client types and ListModels

**Files:**
- Create: `internal/provider/ollama/client.go`
- Create: `internal/provider/ollama/client_test.go`

**Step 1: Write the failing test for ListModels**

```go
// client_test.go
package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"models": [
				{
					"name": "llama3.2:latest",
					"size": 4294967296,
					"modified_at": "2026-02-25T10:00:00Z",
					"digest": "sha256:abc123"
				},
				{
					"name": "codellama:7b",
					"size": 3758096384,
					"modified_at": "2026-02-20T08:00:00Z",
					"digest": "sha256:def456"
				}
			]
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "llama3.2:latest", models[0].Name)
	assert.Equal(t, int64(4294967296), models[0].Size)
	assert.Equal(t, "codellama:7b", models[1].Name)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/ollama/... -run TestClient_ListModels -v`
Expected: FAIL — `NewClient` undefined

**Step 3: Write minimal implementation**

```go
// client.go
package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ModelInfo describes a locally available Ollama model.
type ModelInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	Digest     string    `json:"digest"`
}

// Client is a thin HTTP client for the Ollama REST API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a new Ollama API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// ListModels returns locally available models via GET /api/tags.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing models: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result.Models, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/ollama/... -run TestClient_ListModels -v`
Expected: PASS

**Step 5: Add edge case tests**

```go
func TestClient_ListModels_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models": []}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestClient_ListModels_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.ListModels(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestClient_ListModels_ConnectionError(t *testing.T) {
	client := NewClient("http://localhost:1") // nothing listening
	_, err := client.ListModels(context.Background())
	require.Error(t, err)
}
```

**Step 6: Run all tests, commit**

Run: `go test ./internal/provider/ollama/... -v`
Commit: `[BEHAVIORAL] Add Ollama API client with ListModels`

---

### Task 2: Version and IsRunning

**Files:**
- Modify: `internal/provider/ollama/client.go`
- Modify: `internal/provider/ollama/client_test.go`

**Step 1: Write failing tests**

```go
func TestClient_Version(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/version", r.URL.Path)
		w.Write([]byte(`{"version": "0.5.1"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	ver, err := client.Version(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "0.5.1", ver)
}

func TestClient_Version_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Version(context.Background())
	require.Error(t, err)
}

func TestClient_IsRunning_True(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version": "0.5.1"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	assert.True(t, client.IsRunning(context.Background()))
}

func TestClient_IsRunning_False(t *testing.T) {
	client := NewClient("http://localhost:1")
	assert.False(t, client.IsRunning(context.Background()))
}
```

**Step 2: Run to verify failure, then implement**

```go
// Version returns the Ollama server version via GET /api/version.
func (c *Client) Version(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/version", nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checking version: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding version: %w", err)
	}
	return result.Version, nil
}

// IsRunning probes the Ollama server with a short timeout.
func (c *Client) IsRunning(ctx context.Context) bool {
	probeClient := &http.Client{Timeout: 1 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/version", nil)
	if err != nil {
		return false
	}
	resp, err := probeClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
```

**Step 3: Run tests, commit**

Run: `go test ./internal/provider/ollama/... -v`
Commit: `[BEHAVIORAL] Add Version and IsRunning to Ollama client`

---

### Task 3: DeleteModel

**Files:**
- Modify: `internal/provider/ollama/client.go`
- Modify: `internal/provider/ollama/client_test.go`

**Step 1: Write failing tests**

```go
func TestClient_DeleteModel(t *testing.T) {
	var capturedName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/delete", r.URL.Path)
		assert.Equal(t, http.MethodDelete, r.Method)
		var body struct{ Name string `json:"name"` }
		json.NewDecoder(r.Body).Decode(&body)
		capturedName = body.Name
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	err := client.DeleteModel(context.Background(), "llama3.2:latest")
	require.NoError(t, err)
	assert.Equal(t, "llama3.2:latest", capturedName)
}

func TestClient_DeleteModel_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	err := client.DeleteModel(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}
```

**Step 2: Implement**

```go
// DeleteModel removes a model via DELETE /api/delete.
func (c *Client) DeleteModel(ctx context.Context, name string) error {
	body, _ := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/delete", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("deleting model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deleting model %q: HTTP %d", name, resp.StatusCode)
	}
	return nil
}
```

**Step 3: Run tests, commit**

Commit: `[BEHAVIORAL] Add DeleteModel to Ollama client`

---

### Task 4: PullModel (streaming)

**Files:**
- Modify: `internal/provider/ollama/client.go`
- Modify: `internal/provider/ollama/client_test.go`

**Step 1: Write failing tests**

```go
func TestClient_PullModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/pull", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		var body struct{ Name string `json:"name"` }
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "llama3.2", body.Name)

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{\"status\":\"pulling manifest\"}\n"))
		w.Write([]byte("{\"status\":\"downloading\",\"total\":1000,\"completed\":500}\n"))
		w.Write([]byte("{\"status\":\"success\"}\n"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	ch, err := client.PullModel(context.Background(), "llama3.2")
	require.NoError(t, err)

	var events []PullProgress
	for evt := range ch {
		events = append(events, evt)
	}
	require.Len(t, events, 3)
	assert.Equal(t, "pulling manifest", events[0].Status)
	assert.Equal(t, "downloading", events[1].Status)
	assert.Equal(t, int64(500), events[1].Completed)
	assert.Equal(t, "success", events[2].Status)
}

func TestClient_PullModel_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.PullModel(context.Background(), "nonexistent")
	require.Error(t, err)
}
```

**Step 2: Implement**

```go
// PullProgress describes a single progress event during model download.
type PullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
}

// PullModel downloads a model via POST /api/pull, returning a channel of
// progress events. The channel is closed when the pull completes or errors.
func (c *Client) PullModel(ctx context.Context, name string) (<-chan PullProgress, error) {
	body, _ := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pulling model: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("pulling model %q: HTTP %d", name, resp.StatusCode)
	}

	ch := make(chan PullProgress)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var progress PullProgress
			if err := json.Unmarshal(line, &progress); err != nil {
				continue
			}
			select {
			case ch <- progress:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
```

**Step 3: Run tests, commit**

Commit: `[BEHAVIORAL] Add PullModel streaming to Ollama client`

---

## PR 2: CLI Subcommands (S)

### Task 5: ollama list and status commands

**Files:**
- Create: `cmd/rubichan/ollama.go`
- Create: `cmd/rubichan/ollama_test.go`
- Modify: `cmd/rubichan/main.go` — add `rootCmd.AddCommand(ollamaCmd())`

**Step 1: Write failing tests**

```go
// ollama_test.go
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaCmd_Structure(t *testing.T) {
	cmd := ollamaCmd()
	assert.Equal(t, "ollama", cmd.Use)

	subcommands := map[string]bool{}
	for _, sub := range cmd.Commands() {
		subcommands[sub.Name()] = true
	}

	assert.True(t, subcommands["list"], "should have list subcommand")
	assert.True(t, subcommands["pull"], "should have pull subcommand")
	assert.True(t, subcommands["rm"], "should have rm subcommand")
	assert.True(t, subcommands["status"], "should have status subcommand")
}

func TestOllamaCmd_BaseURLFlag(t *testing.T) {
	cmd := ollamaCmd()
	flag := cmd.PersistentFlags().Lookup("base-url")
	require.NotNil(t, flag)
	assert.Equal(t, "", flag.DefValue) // empty = use config or default
}
```

**Step 2: Implement ollama.go**

Create the full Cobra subcommand tree. The `list` command calls `client.ListModels()` and formats a table. The `status` command calls `client.Version()` and `client.ListModels()`. The `pull` command streams `PullProgress` to stdout. The `rm` command calls `client.DeleteModel()`.

Each subcommand resolves the base URL from `--base-url` flag (if set), then config file, then default `http://localhost:11434`.

**Step 3: Wire into main.go**

Add `rootCmd.AddCommand(ollamaCmd())` alongside the existing `skillCmd()` and `wikiCmd()`.

**Step 4: Run tests, commit**

Commit: `[BEHAVIORAL] Add rubichan ollama CLI subcommands`

---

## PR 3: Auto-Detection (S)

### Task 6: Auto-detect Ollama provider

**Files:**
- Modify: `cmd/rubichan/main.go`
- Modify: `cmd/rubichan/main_test.go`

**Step 1: Write failing tests**

```go
func TestAutoDetectProvider_OllamaRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version": "0.5.1"}`))
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	// No ANTHROPIC_API_KEY set, Ollama is running
	detected := autoDetectProvider(cfg, "", srv.URL)
	assert.True(t, detected)
	assert.Equal(t, "ollama", cfg.Provider.Default)
}

func TestAutoDetectProvider_OllamaNotRunning(t *testing.T) {
	cfg := config.DefaultConfig()
	detected := autoDetectProvider(cfg, "", "http://localhost:1")
	assert.False(t, detected)
	assert.Equal(t, "anthropic", cfg.Provider.Default) // unchanged
}

func TestAutoDetectProvider_ExplicitProviderFlag(t *testing.T) {
	cfg := config.DefaultConfig()
	// When provider flag is set, skip auto-detection
	detected := autoDetectProvider(cfg, "openrouter", "http://localhost:11434")
	assert.False(t, detected)
	assert.Equal(t, "anthropic", cfg.Provider.Default) // unchanged
}

func TestAutoDetectProvider_APIKeyExists(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Anthropic.APIKey = "sk-test-key"
	detected := autoDetectProvider(cfg, "", "http://localhost:11434")
	assert.False(t, detected) // don't auto-detect when API key exists
}
```

**Step 2: Implement autoDetectProvider**

```go
// autoDetectProvider checks if Ollama should be auto-selected.
// Returns true if provider was switched to Ollama.
func autoDetectProvider(cfg *config.Config, providerFlagValue, ollamaURL string) bool {
	// Skip if provider explicitly set via flag.
	if providerFlagValue != "" {
		return false
	}
	// Only auto-detect when using default provider (anthropic).
	if cfg.Provider.Default != "anthropic" {
		return false
	}
	// Skip if Anthropic API key is configured.
	if cfg.Provider.Anthropic.APIKey != "" || os.Getenv("ANTHROPIC_API_KEY") != "" {
		return false
	}

	client := ollama.NewClient(ollamaURL)
	if client.IsRunning(context.Background()) {
		cfg.Provider.Default = "ollama"
		return true
	}
	return false
}
```

Call in `loadConfig()` after applying flag overrides:

```go
if ollamaURL := cfg.Provider.Ollama.BaseURL; ollamaURL == "" {
	ollamaURL = "http://localhost:11434"
}
if autoDetectProvider(cfg, providerFlag, ollamaURL) {
	fmt.Fprintln(os.Stderr, "Using local Ollama (no API key configured)")
}
```

**Step 3: Run tests, commit**

Commit: `[BEHAVIORAL] Auto-detect Ollama when no API key configured`

---

## PR 4: Model Picker (M)

### Task 7: Model picker TUI component

**Files:**
- Create: `internal/tui/modelpicker.go`
- Create: `internal/tui/modelpicker_test.go`

**Step 1: Write failing tests**

```go
// modelpicker_test.go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPicker_Init(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)
	require.NotNil(t, picker)
	assert.Equal(t, 2, len(picker.models))
	assert.Equal(t, "", picker.Selected()) // nothing selected yet
}

func TestModelPicker_SelectModel(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)

	// Simulate pressing Enter on first item
	updated, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p := updated.(*ModelPicker)
	assert.Equal(t, "llama3.2:latest", p.Selected())
	assert.True(t, p.Done())
	require.NotNil(t, cmd)
}

func TestModelPicker_SingleModel(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
	}
	picker := NewModelPicker(models)
	// With single model, should auto-select
	assert.Equal(t, "llama3.2:latest", picker.Selected())
	assert.True(t, picker.Done())
}

func TestModelPicker_Quit(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)

	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	p := updated.(*ModelPicker)
	assert.True(t, p.Cancelled())
}
```

**Step 2: Implement ModelPicker**

```go
// modelpicker.go
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// ModelChoice represents a selectable model option.
type ModelChoice struct {
	Name string
	Size string
}

// ModelPicker is a Bubble Tea component for selecting an Ollama model.
type ModelPicker struct {
	models    []ModelChoice
	cursor    int
	selected  string
	done      bool
	cancelled bool
}

// NewModelPicker creates a model picker. If only one model is available,
// it is auto-selected.
func NewModelPicker(models []ModelChoice) *ModelPicker {
	p := &ModelPicker{models: models}
	if len(models) == 1 {
		p.selected = models[0].Name
		p.done = true
	}
	return p
}

func (p *ModelPicker) Selected() string { return p.selected }
func (p *ModelPicker) Done() bool       { return p.done }
func (p *ModelPicker) Cancelled() bool  { return p.cancelled }

func (p *ModelPicker) Init() tea.Cmd { return nil }

func (p *ModelPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			if p.cursor > 0 {
				p.cursor--
			}
		case tea.KeyDown:
			if p.cursor < len(p.models)-1 {
				p.cursor++
			}
		case tea.KeyEnter:
			p.selected = p.models[p.cursor].Name
			p.done = true
			return p, tea.Quit
		case tea.KeyCtrlC, tea.KeyEsc:
			p.cancelled = true
			return p, tea.Quit
		}
	}
	return p, nil
}

func (p *ModelPicker) View() string {
	s := "Select an Ollama model:\n\n"
	for i, m := range p.models {
		cursor := "  "
		if i == p.cursor {
			cursor = "> "
		}
		s += fmt.Sprintf("%s%s (%s)\n", cursor, m.Name, m.Size)
	}
	s += "\n(↑/↓ to move, enter to select, esc to cancel)\n"
	return s
}
```

**Step 3: Run tests, commit**

Commit: `[BEHAVIORAL] Add Bubble Tea model picker component`

---

### Task 8: Wire model picker into runInteractive

**Files:**
- Modify: `cmd/rubichan/main.go`
- Modify: `cmd/rubichan/main_test.go`

**Step 1: Write failing tests**

```go
func TestResolveOllamaModel_SingleModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models": [{"name": "llama3.2:latest", "size": 4294967296}]}`))
	}))
	defer srv.Close()

	model, err := resolveOllamaModel(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "llama3.2:latest", model)
}

func TestResolveOllamaModel_NoModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models": []}`))
	}))
	defer srv.Close()

	_, err := resolveOllamaModel(srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no models found")
}
```

**Step 2: Implement resolveOllamaModel helper**

This helper is called in `runInteractive()` when `cfg.Provider.Default == "ollama"` and `cfg.Provider.Model == ""`. For a single model it returns immediately; for multiple models it runs the model picker TUI; for zero models it returns an error with a helpful message.

**Step 3: Run tests, commit**

Commit: `[BEHAVIORAL] Wire model picker into interactive mode`

---

## PR 5: Integration Tests (S)

### Task 9: Integration tests with build tag

**Files:**
- Create: `internal/provider/ollama/integration_test.go`

**Step 1: Write integration tests**

```go
//go:build integration

package ollama

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testBaseURL = "http://localhost:11434"

func skipIfOllamaNotRunning(t *testing.T) {
	t.Helper()
	client := NewClient(testBaseURL)
	if !client.IsRunning(context.Background()) {
		t.Skip("Ollama is not running at " + testBaseURL)
	}
}

func TestIntegration_Version(t *testing.T) {
	skipIfOllamaNotRunning(t)
	client := NewClient(testBaseURL)
	ver, err := client.Version(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, ver)
}

func TestIntegration_ListModels(t *testing.T) {
	skipIfOllamaNotRunning(t)
	client := NewClient(testBaseURL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	// Just verify it returns without error; models may or may not exist.
	t.Logf("Found %d models", len(models))
	for _, m := range models {
		t.Logf("  %s (%d bytes)", m.Name, m.Size)
	}
}

func TestIntegration_StreamCompletion(t *testing.T) {
	skipIfOllamaNotRunning(t)
	client := NewClient(testBaseURL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	if len(models) == 0 {
		t.Skip("No models available for streaming test")
	}

	p := New(testBaseURL)
	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Model:     models[0].Name,
		Messages:  []provider.Message{{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "Say hello in exactly 3 words."}}}},
		MaxTokens: 20,
	})
	require.NoError(t, err)

	var gotText bool
	for evt := range ch {
		if evt.Type == "text_delta" {
			gotText = true
		}
	}
	assert.True(t, gotText, "should have received text events")
}
```

**Step 2: Verify tests skip without Ollama**

Run: `go test ./internal/provider/ollama/... -tags integration -v`
Expected: Tests skip with "Ollama is not running" if no local Ollama

**Step 3: Commit**

Commit: `[BEHAVIORAL] Add Ollama integration tests behind build tag`

---

## Dependency Graph

```
PR1 (Client)  ─── Tasks 1-4  ─┐
PR2 (CLI)     ─── Task 5      ─┼─ PR2 depends on PR1 (uses Client)
PR3 (Auto-detect) ─ Task 6    ─┤  PR3 depends on PR1 (uses IsRunning)
PR4 (Picker)  ─── Tasks 7-8   ─┤  PR4 depends on PR1 (uses ListModels)
PR5 (Integration) ─ Task 9    ─┘  PR5 depends on PR1 (uses Client)
```

PR1 first, then PRs 2-5 can be done in any order (all depend only on PR1).

## Verification

After all PRs:
1. `go test ./... -cover` — all packages >90%
2. `rubichan ollama status` — shows version or "not running"
3. `rubichan ollama list` — shows available models
4. `rubichan --provider ollama --model llama3.2` — works end-to-end
5. Start Ollama, unset `ANTHROPIC_API_KEY`, run `rubichan` — auto-detects Ollama
6. `go test -tags integration ./internal/provider/ollama/...` — passes against running Ollama
