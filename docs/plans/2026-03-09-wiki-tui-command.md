# Wiki TUI Command Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `/wiki` slash command to the TUI that opens a `huh` form for wiki configuration, then runs wiki generation in the background with progress in both the content area and status bar.

**Architecture:** Three layers: (1) `ProgressFunc` callback added to `wiki.Config` replacing stderr writes, (2) `/wiki` slash command with `huh` form and background goroutine, (3) TUI message types and Update handlers for progress/done events. The command returns a new `ActionOpenWiki` action, handled in `handleCommand` like `ActionOpenConfig`.

**Tech Stack:** Go, Bubble Tea (TUI), huh (forms), wiki pipeline, Glamour

---

### Task 1: Add ProgressFunc to wiki.Config — tests

**Files:**
- Modify: `internal/wiki/pipeline_test.go`

**Step 1: Write failing test**

```go
func TestRunCallsProgressFunc(t *testing.T) {
	var stages []string
	cfg := Config{
		Dir:         t.TempDir(),
		OutputDir:   filepath.Join(t.TempDir(), "out"),
		Format:      "raw-md",
		Concurrency: 1,
		ProgressFunc: func(stage string, current, total int) {
			stages = append(stages, stage)
		},
	}

	// Run will fail at scan (empty dir), but ProgressFunc should be called for stage 1.
	_ = Run(context.Background(), cfg, nil, nil)
	assert.Contains(t, stages, "scanning")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/wiki/ -run TestRunCallsProgressFunc -v`
Expected: FAIL — `ProgressFunc` field doesn't exist on Config

---

### Task 2: Add ProgressFunc to wiki.Config — implementation

**Files:**
- Modify: `internal/wiki/pipeline.go`

**Step 1: Add field to Config**

```go
type Config struct {
	Dir              string
	OutputDir        string
	Format           string
	DiagramFmt       string
	Concurrency      int
	SecurityFindings []security.Finding
	ProgressFunc     func(stage string, current, total int)
}
```

**Step 2: Add helper and replace stderr writes in Run()**

Add a helper at the top of the file:

```go
// progress calls the configured ProgressFunc if set, otherwise writes to stderr.
func (c *Config) progress(stage string, current, total int, fallbackMsg string) {
	if c.ProgressFunc != nil {
		c.ProgressFunc(stage, current, total)
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", fallbackMsg)
}
```

Replace each `fmt.Fprintf(os.Stderr, ...)` in Run() with a `cfg.progress(...)` call:

```go
func Run(ctx context.Context, cfg Config, llm LLMCompleter, p *parser.Parser) error {
	// Stage 1: Scan
	cfg.progress("scanning", 0, 0, fmt.Sprintf("wiki: scanning %s...", cfg.Dir))
	files, err := Scan(ctx, cfg.Dir, p)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	// Stage 2: Chunk
	cfg.progress("chunking", 0, len(files), fmt.Sprintf("wiki: chunking %d files...", len(files)))
	reader := &osSourceReader{baseDir: cfg.Dir}
	chunks, err := ChunkFiles(files, reader, DefaultChunkerConfig())
	if err != nil {
		return fmt.Errorf("chunk: %w", err)
	}

	// Stage 3: Analyze
	cfg.progress("analyzing", 0, len(chunks), fmt.Sprintf("wiki: analyzing %d chunks...", len(chunks)))
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	analyzerCfg := AnalyzerConfig{Concurrency: concurrency}
	analysis, err := Analyze(ctx, chunks, llm, analyzerCfg)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}

	// Stage 4: Diagrams
	cfg.progress("diagrams", 0, 0, "wiki: generating diagrams...")
	diagramFmt := cfg.DiagramFmt
	if diagramFmt == "" {
		diagramFmt = "mermaid"
	}
	diagrams, err := GenerateDiagrams(ctx, files, analysis, llm, DiagramConfig{Format: diagramFmt})
	if err != nil {
		return fmt.Errorf("diagrams: %w", err)
	}

	// Stage 5: Assemble
	cfg.progress("assembling", 0, 0, "wiki: assembling documents...")
	documents, err := Assemble(analysis, diagrams, nil, cfg.SecurityFindings)
	if err != nil {
		return fmt.Errorf("assemble: %w", err)
	}

	// Stage 6: Render
	cfg.progress("rendering", 0, len(documents), fmt.Sprintf("wiki: rendering %d documents to %s...", len(documents), cfg.OutputDir))
	if err := Render(documents, RendererConfig{Format: cfg.Format, OutputDir: cfg.OutputDir}); err != nil {
		return fmt.Errorf("render: %w", err)
	}

	cfg.progress("done", 0, 0, "wiki: done.")
	return nil
}
```

**Step 3: Run tests**

Run: `go test ./internal/wiki/... -run TestRunCallsProgressFunc -v`
Expected: PASS

**Step 4: Run full wiki test suite**

Run: `go test ./internal/wiki/...`
Expected: PASS (existing tests use nil ProgressFunc, fall back to stderr)

**Step 5: Commit**

```bash
git add internal/wiki/pipeline.go internal/wiki/pipeline_test.go
git commit -m "[BEHAVIORAL] Add ProgressFunc callback to wiki.Config"
```

---

### Task 3: Add ActionOpenWiki to commands package

**Files:**
- Modify: `internal/commands/commands.go`

**Step 1: Add new Action constant**

In the Action const block (line 14-21), add:

```go
const (
	ActionNone Action = iota
	ActionQuit
	ActionOpenConfig
	ActionOpenWiki
)
```

**Step 2: Run tests**

Run: `go test ./internal/commands/...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/commands/commands.go
git commit -m "[STRUCTURAL] Add ActionOpenWiki action constant"
```

---

### Task 4: Wiki slash command — tests

**Files:**
- Create: `internal/tui/wiki_command_test.go`

**Step 1: Write failing tests**

```go
package tui

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWikiCommandName(t *testing.T) {
	cmd := NewWikiCommand(WikiCommandConfig{})
	assert.Equal(t, "wiki", cmd.Name())
}

func TestWikiCommandDescription(t *testing.T) {
	cmd := NewWikiCommand(WikiCommandConfig{})
	assert.Equal(t, "Generate project documentation wiki", cmd.Description())
}

func TestWikiCommandExecuteReturnsOpenWiki(t *testing.T) {
	cmd := NewWikiCommand(WikiCommandConfig{WorkDir: "/tmp"})
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, commands.ActionOpenWiki, result.Action)
}

func TestWikiCommandArguments(t *testing.T) {
	cmd := NewWikiCommand(WikiCommandConfig{})
	assert.Empty(t, cmd.Arguments())
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestWikiCommand -v`
Expected: FAIL — `NewWikiCommand` undefined

---

### Task 5: Wiki slash command — implementation

**Files:**
- Create: `internal/tui/wiki_command.go`

**Step 1: Implement the command**

```go
package tui

import (
	"context"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/wiki"
)

// WikiCommandConfig holds dependencies for the /wiki command.
type WikiCommandConfig struct {
	WorkDir string
	LLM     wiki.LLMCompleter
}

type wikiCommand struct {
	cfg WikiCommandConfig
}

// NewWikiCommand creates a /wiki slash command that opens the wiki form overlay.
func NewWikiCommand(cfg WikiCommandConfig) commands.SlashCommand {
	return &wikiCommand{cfg: cfg}
}

func (c *wikiCommand) Name() string        { return "wiki" }
func (c *wikiCommand) Description() string { return "Generate project documentation wiki" }
func (c *wikiCommand) Arguments() []commands.ArgumentDef { return nil }

func (c *wikiCommand) Complete(_ context.Context, _ []string) []commands.Candidate {
	return nil
}

func (c *wikiCommand) Execute(_ context.Context, _ []string) (commands.Result, error) {
	return commands.Result{Action: commands.ActionOpenWiki}, nil
}
```

**Step 2: Run tests**

Run: `go test ./internal/tui/ -run TestWikiCommand -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/tui/wiki_command.go internal/tui/wiki_command_test.go
git commit -m "[BEHAVIORAL] Add /wiki slash command with ActionOpenWiki"
```

---

### Task 6: Wiki form overlay — tests

**Files:**
- Modify: `internal/tui/wiki_command_test.go`

**Step 1: Add form tests**

```go
func TestNewWikiFormDefaults(t *testing.T) {
	wf := NewWikiForm("/tmp/project")
	assert.Equal(t, ".", wf.Path)
	assert.Equal(t, "raw-md", wf.Format)
	assert.Equal(t, "docs/wiki", wf.OutDir)
	assert.Equal(t, "5", wf.ConcurrencyStr)
}

func TestWikiFormForm(t *testing.T) {
	wf := NewWikiForm("/tmp/project")
	f := wf.Form()
	assert.NotNil(t, f)
}

func TestWikiFormConcurrency(t *testing.T) {
	wf := NewWikiForm("/tmp")
	wf.ConcurrencyStr = "10"
	assert.Equal(t, 10, wf.Concurrency())

	wf.ConcurrencyStr = "invalid"
	assert.Equal(t, 5, wf.Concurrency())
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestWikiForm -v`
Expected: FAIL — `NewWikiForm` undefined

---

### Task 7: Wiki form overlay — implementation

**Files:**
- Modify: `internal/tui/wiki_command.go`

**Step 1: Add WikiForm struct**

```go
import (
	"strconv"

	"github.com/charmbracelet/huh"
)

// WikiForm wraps a huh form for configuring wiki generation.
type WikiForm struct {
	form           *huh.Form
	Path           string
	Format         string
	OutDir         string
	ConcurrencyStr string
	workDir        string
}

// NewWikiForm creates a wiki configuration form with sensible defaults.
func NewWikiForm(workDir string) *WikiForm {
	wf := &WikiForm{
		Path:           ".",
		Format:         "raw-md",
		OutDir:         "docs/wiki",
		ConcurrencyStr: "5",
		workDir:        workDir,
	}

	group := huh.NewGroup(
		huh.NewInput().
			Title("Project Path").
			Value(&wf.Path),
		huh.NewSelect[string]().
			Title("Output Format").
			Options(
				huh.NewOption("Raw Markdown", "raw-md"),
				huh.NewOption("Hugo", "hugo"),
				huh.NewOption("Docusaurus", "docusaurus"),
			).
			Value(&wf.Format),
		huh.NewInput().
			Title("Output Directory").
			Value(&wf.OutDir),
		huh.NewInput().
			Title("Concurrency").
			Value(&wf.ConcurrencyStr),
	).Title("Wiki Generation")

	wf.form = huh.NewForm(group)
	return wf
}

// Form returns the underlying huh.Form.
func (wf *WikiForm) Form() *huh.Form { return wf.form }

// Concurrency parses the concurrency string, defaulting to 5.
func (wf *WikiForm) Concurrency() int {
	n, err := strconv.Atoi(wf.ConcurrencyStr)
	if err != nil || n <= 0 {
		return 5
	}
	return n
}
```

**Step 2: Run tests**

Run: `go test ./internal/tui/ -run TestWikiForm -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/tui/wiki_command.go internal/tui/wiki_command_test.go
git commit -m "[BEHAVIORAL] Add WikiForm overlay with huh form for wiki configuration"
```

---

### Task 8: Wiki progress message types and status bar

**Files:**
- Modify: `internal/tui/wiki_command.go`
- Modify: `internal/tui/statusbar.go`
- Modify: `internal/tui/wiki_command_test.go`

**Step 1: Add message types to wiki_command.go**

```go
// wikiProgressMsg carries a progress update from the wiki goroutine.
type wikiProgressMsg struct {
	Stage   string
	Current int
	Total   int
}

// wikiDoneMsg signals wiki generation completion.
type wikiDoneMsg struct {
	Err error
}
```

**Step 2: Add status bar wiki methods**

In `statusbar.go`, add:

```go
// StatusBar struct — add field:
wikiStage string

// SetWikiProgress sets the wiki generation stage for display.
func (s *StatusBar) SetWikiProgress(stage string) { s.wikiStage = stage }

// ClearWikiProgress clears the wiki progress display.
func (s *StatusBar) ClearWikiProgress() { s.wikiStage = "" }
```

Update `View()` to include wiki progress when set:

```go
func (s *StatusBar) View() string {
	base := fmt.Sprintf(" %s  %s  %s/%s  Turn %d/%d  ~$%.2f",
		persona.StatusPrefix(),
		s.model,
		formatTokens(s.inputTokens),
		formatTokens(s.maxTokens),
		s.turn, s.maxTurns,
		s.cost,
	)
	if s.wikiStage != "" {
		base += fmt.Sprintf("  Wiki: %s", s.wikiStage)
	}
	return s.style.Render(base)
}
```

**Step 3: Add tests for status bar wiki methods**

In `internal/tui/wiki_command_test.go`:

```go
func TestStatusBarWikiProgress(t *testing.T) {
	sb := NewStatusBar(120)
	sb.SetWikiProgress("analyzing")
	view := sb.View()
	assert.Contains(t, view, "Wiki: analyzing")

	sb.ClearWikiProgress()
	view = sb.View()
	assert.NotContains(t, view, "Wiki:")
}
```

**Step 4: Run tests**

Run: `go test ./internal/tui/ -run "TestStatusBarWiki|TestWiki" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tui/wiki_command.go internal/tui/wiki_command_test.go internal/tui/statusbar.go
git commit -m "[BEHAVIORAL] Add wiki progress message types and status bar wiki display"
```

---

### Task 9: Wire wiki form overlay and background execution into TUI model

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`

**Step 1: Add wiki fields to Model struct**

In `model.go`, add fields to Model:

```go
type Model struct {
	// ... existing fields ...
	wikiForm    *WikiForm
	wikiRunning bool
	wikiCfg     WikiCommandConfig
}
```

**Step 2: Update NewModel to accept WikiCommandConfig and register /wiki command**

Add `wikiCfg WikiCommandConfig` parameter to `NewModel` (or add a `SetWikiConfig` method to avoid breaking the signature). A setter is safer:

```go
// SetWikiConfig sets the wiki command configuration and registers the /wiki command.
func (m *Model) SetWikiConfig(cfg WikiCommandConfig) {
	m.wikiCfg = cfg
	_ = m.cmdRegistry.Register(NewWikiCommand(cfg))
}
```

**Step 3: Handle ActionOpenWiki in handleCommand**

In `handleCommand` (model.go ~line 281), add a case:

```go
case commands.ActionOpenWiki:
	if m.wikiRunning {
		m.content.WriteString("Wiki generation is already running.\n")
		m.viewport.SetContent(m.content.String())
		return nil
	}
	m.wikiForm = NewWikiForm(m.wikiCfg.WorkDir)
	m.state = StateWikiOverlay
	return m.wikiForm.Form().Init()
```

**Step 4: Add StateWikiOverlay to UIState**

In `model.go`, add to the const block:

```go
StateWikiOverlay
```

**Step 5: Handle wiki form overlay in Update**

In `update.go`, add wiki form routing near the config overlay handler (around line 37):

```go
if m.state == StateWikiOverlay && m.wikiForm != nil {
	form, cmd := m.wikiForm.Form().Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.wikiForm.form = f
	}
	switch m.wikiForm.Form().State {
	case huh.StateCompleted:
		m.state = StateInput
		wf := m.wikiForm
		m.wikiForm = nil
		return m, m.startWikiGeneration(wf)
	case huh.StateAborted:
		m.state = StateInput
		m.wikiForm = nil
	}
	return m, cmd
}
```

**Step 6: Add startWikiGeneration method**

In `wiki_command.go`, add the background runner:

```go
import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/julianshen/rubichan/internal/parser"
	"github.com/julianshen/rubichan/internal/wiki"
)

// startWikiGeneration launches wiki generation in a background goroutine.
func (m *Model) startWikiGeneration(wf *WikiForm) tea.Cmd {
	m.wikiRunning = true
	m.content.WriteString(fmt.Sprintf("📖 Wiki generation started (%s → %s)\n", wf.Format, wf.OutDir))
	m.setContentAndAutoScroll(m.content.String())
	m.statusBar.SetWikiProgress("starting")

	progressCh := make(chan wikiProgressMsg, 16)

	cfg := wiki.Config{
		Dir:         filepath.Join(m.wikiCfg.WorkDir, wf.Path),
		OutputDir:   filepath.Join(m.wikiCfg.WorkDir, wf.OutDir),
		Format:      wf.Format,
		Concurrency: wf.Concurrency(),
		ProgressFunc: func(stage string, current, total int) {
			progressCh <- wikiProgressMsg{Stage: stage, Current: current, Total: total}
		},
	}

	go func() {
		p, _ := parser.New()
		err := wiki.Run(context.Background(), cfg, m.wikiCfg.LLM, p)
		close(progressCh)
		// Send done after channel is drained by the listener.
		// The done message is sent via a separate mechanism below.
		_ = err // captured by done msg
		// We send done via the progress channel being closed.
	}()

	// Return a Cmd that listens for the first progress message.
	return m.waitForWikiProgress(progressCh)
}

// waitForWikiProgress returns a tea.Cmd that reads from the wiki progress channel.
func (m *Model) waitForWikiProgress(ch <-chan wikiProgressMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return wikiDoneMsg{}
		}
		return wikiListenMsg{progress: msg, ch: ch}
	}
}

// wikiListenMsg carries both a progress update and the channel for continued listening.
type wikiListenMsg struct {
	progress wikiProgressMsg
	ch       <-chan wikiProgressMsg
}
```

Wait — this approach has an issue: we need to capture the error from `wiki.Run()`. Let me revise. Use a done channel:

```go
func (m *Model) startWikiGeneration(wf *WikiForm) tea.Cmd {
	m.wikiRunning = true
	m.content.WriteString(fmt.Sprintf("📖 Wiki generation started (%s → %s)\n", wf.Format, wf.OutDir))
	m.setContentAndAutoScroll(m.content.String())
	m.statusBar.SetWikiProgress("starting")

	progressCh := make(chan wikiProgressMsg, 16)
	doneCh := make(chan error, 1)

	cfg := wiki.Config{
		Dir:         filepath.Join(m.wikiCfg.WorkDir, wf.Path),
		OutputDir:   filepath.Join(m.wikiCfg.WorkDir, wf.OutDir),
		Format:      wf.Format,
		Concurrency: wf.Concurrency(),
		ProgressFunc: func(stage string, current, total int) {
			progressCh <- wikiProgressMsg{Stage: stage, Current: current, Total: total}
		},
	}

	go func() {
		p, _ := parser.New()
		err := wiki.Run(context.Background(), cfg, m.wikiCfg.LLM, p)
		close(progressCh)
		doneCh <- err
	}()

	return m.waitForWikiEvent(progressCh, doneCh)
}

// waitForWikiEvent returns a tea.Cmd that reads either a progress or done event.
func (m *Model) waitForWikiEvent(progressCh <-chan wikiProgressMsg, doneCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-progressCh:
			if !ok {
				// Progress channel closed — read the done error.
				err := <-doneCh
				return wikiDoneMsg{Err: err}
			}
			return wikiEventMsg{progress: &msg, progressCh: progressCh, doneCh: doneCh}
		case err := <-doneCh:
			return wikiDoneMsg{Err: err}
		}
	}
}

// wikiEventMsg carries a progress update and channels for continued listening.
type wikiEventMsg struct {
	progress   *wikiProgressMsg
	progressCh <-chan wikiProgressMsg
	doneCh     <-chan error
}
```

**Step 7: Handle wiki messages in Update**

In `update.go`, add cases to the main switch:

```go
case wikiEventMsg:
	if msg.progress != nil {
		stage := msg.progress.Stage
		m.statusBar.SetWikiProgress(stage)
		detail := fmt.Sprintf("  Wiki: %s", stage)
		if msg.progress.Total > 0 {
			detail = fmt.Sprintf("  Wiki: %s (%d items)", stage, msg.progress.Total)
		}
		m.content.WriteString(detail + "\n")
		m.setContentAndAutoScroll(m.content.String())
	}
	return m, m.waitForWikiEvent(msg.progressCh, msg.doneCh)

case wikiDoneMsg:
	m.wikiRunning = false
	m.statusBar.ClearWikiProgress()
	if msg.Err != nil {
		m.content.WriteString(persona.ErrorMessage(fmt.Sprintf("Wiki generation failed: %s", msg.Err)))
	} else {
		m.content.WriteString("📖 Wiki generation complete!\n")
	}
	m.setContentAndAutoScroll(m.content.String())
	return m, nil
```

**Step 8: Run full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS

**Step 9: Commit**

```bash
git add internal/tui/model.go internal/tui/update.go internal/tui/wiki_command.go
git commit -m "[BEHAVIORAL] Wire wiki form overlay and background execution into TUI"
```

---

### Task 10: Wire wiki dependencies in main.go

**Files:**
- Modify: `cmd/rubichan/main.go`

**Step 1: Pass WikiCommandConfig to TUI model**

Find where the TUI model is created and `SetAgent` is called. After the `wireWiki(...)` call, add:

```go
m.SetWikiConfig(tui.WikiCommandConfig{
	WorkDir: cwd,
	LLM:     llmCompleter,
})
```

Where `m` is the TUI Model and `llmCompleter` is the existing `integrations.NewLLMCompleter(...)`.

**Step 2: Verify build**

Run: `go build ./cmd/rubichan/`
Expected: SUCCESS

**Step 3: Run tests**

Run: `go test ./cmd/rubichan/...`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Wire wiki command dependencies into TUI model"
```

---

### Task 11: Coverage check and final verification

**Step 1: Check coverage**

Run: `go test -cover ./internal/tui/ ./internal/wiki/ ./internal/commands/`
Expected: All packages >90%

**Step 2: Run full test suite**

Run: `go test ./...`
Expected: PASS

**Step 3: Check formatting**

Run: `gofmt -l .`
Expected: Clean

**Step 4: Add any needed coverage tests and commit**

```bash
git commit -m "[BEHAVIORAL] Boost test coverage for wiki TUI command"
```
