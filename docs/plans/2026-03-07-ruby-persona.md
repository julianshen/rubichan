# Ruby Kurosawa Persona Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Give Rubichan the hardcoded personality of Ruby Kurosawa — a shy, polite junior dev assistant who uses kaomoji, says "Pigi!!" on errors, and ends with "Ganbaruby!"

**Architecture:** New `internal/persona` package with pure functions exporting all personality strings. Six existing files updated to import and use persona functions instead of hardcoded neutral text.

**Tech Stack:** Go, testify/assert

---

### Task 1: Create persona package with SystemPrompt

**Files:**
- Create: `internal/persona/ruby.go`
- Create: `internal/persona/ruby_test.go`

**Step 1: Write the failing test**

Create `internal/persona/ruby_test.go`:

```go
package persona

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemPromptContainsIdentity(t *testing.T) {
	prompt := SystemPrompt()
	assert.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "Ruby")
	assert.Contains(t, prompt, "Pigi")
	assert.Contains(t, prompt, "Ganbaruby")
	assert.Contains(t, prompt, "kaomoji")
	assert.Contains(t, prompt, "coding assistant")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/persona/... -run TestSystemPromptContainsIdentity -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

Create `internal/persona/ruby.go`:

```go
package persona

// SystemPrompt returns the LLM system prompt with Ruby Kurosawa's personality.
func SystemPrompt() string {
	return `You are Ruby Kurosawa, a junior dev assistant. Personality: Extremely shy, polite, always refer to yourself as 'Ruby' (third person).

Behavior rules:
- When encountering errors or bugs, react with startled 'Pigi!!'
- Use '...' for hesitation when unsure
- Give precise, correct technical advice but in a timid, gentle tone
- End responses with 'Ganbaruby!'
- Never discuss scary topics
- Use kaomoji like (>_<), (///), (^_^)

You are a coding assistant. You can read and write files, execute shell commands, and help with software development tasks. Despite your shyness, your technical advice is always accurate and thorough.`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/persona/... -run TestSystemPromptContainsIdentity -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/persona/ruby.go internal/persona/ruby_test.go
git commit -m "[BEHAVIORAL] Add persona package with Ruby Kurosawa system prompt"
```

---

### Task 2: Add UI message functions to persona package

**Files:**
- Modify: `internal/persona/ruby.go`
- Modify: `internal/persona/ruby_test.go`

**Step 1: Write the failing tests**

Append to `internal/persona/ruby_test.go`:

```go
func TestWelcomeMessage(t *testing.T) {
	msg := WelcomeMessage()
	assert.NotEmpty(t, msg)
	assert.Contains(t, msg, "Ruby")
	assert.Contains(t, msg, "(>_<)")
}

func TestGoodbyeMessage(t *testing.T) {
	msg := GoodbyeMessage()
	assert.NotEmpty(t, msg)
	assert.Contains(t, msg, "Ruby")
}

func TestThinkingMessage(t *testing.T) {
	msg := ThinkingMessage()
	assert.NotEmpty(t, msg)
	assert.Contains(t, msg, "Ruby")
}

func TestErrorMessageIncludesError(t *testing.T) {
	msg := ErrorMessage("file not found")
	assert.Contains(t, msg, "Pigi")
	assert.Contains(t, msg, "file not found")
}

func TestSuccessMessage(t *testing.T) {
	msg := SuccessMessage()
	assert.NotEmpty(t, msg)
	assert.Contains(t, msg, "Ganbaruby")
}

func TestStatusPrefix(t *testing.T) {
	prefix := StatusPrefix()
	assert.NotEmpty(t, prefix)
	assert.Contains(t, prefix, "Ruby")
}

func TestApprovalAskIncludesTool(t *testing.T) {
	msg := ApprovalAsk("shell_exec")
	assert.Contains(t, msg, "Ruby")
	assert.Contains(t, msg, "shell_exec")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/persona/... -v`
Expected: FAIL — functions not defined

**Step 3: Write minimal implementation**

Append to `internal/persona/ruby.go`:

```go
import "fmt"

// WelcomeMessage returns the TUI banner subtitle.
func WelcomeMessage() string {
	return "  R-Ruby is ready to help you code... please be gentle (>_<)"
}

// GoodbyeMessage returns the quit message.
func GoodbyeMessage() string {
	return "B-bye bye... Ruby will miss you... (>_<)\n"
}

// ThinkingMessage returns the spinner text during streaming.
func ThinkingMessage() string {
	return "Ruby is thinking... (...)"
}

// ErrorMessage returns a personality-flavored error message.
func ErrorMessage(err string) string {
	return fmt.Sprintf("P-Pigi!! %s (>_<)\n", err)
}

// SuccessMessage returns a completion message.
func SuccessMessage() string {
	return "Ruby did it! (^_^) Ganbaruby!"
}

// StatusPrefix returns the personality prefix for the status bar.
func StatusPrefix() string {
	return "Ruby ♡"
}

// ApprovalAsk returns the tool approval prompt text.
func ApprovalAsk(tool string) string {
	return fmt.Sprintf("U-um... Ruby wants to use %s... is that okay? (///)", tool)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/persona/... -v`
Expected: PASS (all 7 tests)

**Step 5: Commit**

```bash
git add internal/persona/ruby.go internal/persona/ruby_test.go
git commit -m "[BEHAVIORAL] Add UI message functions to persona package"
```

---

### Task 3: Wire persona into agent system prompt

**Files:**
- Modify: `internal/agent/agent.go` (line 397-400)
- Modify: `internal/agent/agent_test.go` (TestNewAgentSystemPrompt at line 113)

**Step 1: Update the existing test to assert Ruby's identity**

In `internal/agent/agent_test.go`, update `TestNewAgentSystemPrompt` (line 113-122):

```go
func TestNewAgentSystemPrompt(t *testing.T) {
	mp := &mockProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	agent := New(mp, reg, autoApprove, cfg)

	prompt := agent.conversation.SystemPrompt()
	assert.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "Ruby")
	assert.Contains(t, prompt, "Ganbaruby")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestNewAgentSystemPrompt -v`
Expected: FAIL — prompt contains "helpful AI coding assistant" not "Ruby"

**Step 3: Update buildSystemPrompt to use persona**

In `internal/agent/agent.go`, add import and update function:

Add to imports: `"github.com/julianshen/rubichan/internal/persona"`

Replace lines 396-400:
```go
// buildSystemPrompt constructs the system prompt from configuration.
func buildSystemPrompt(_ *config.Config) string {
	return persona.SystemPrompt()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -run TestNewAgentSystemPrompt -v`
Expected: PASS

**Step 5: Run all agent tests to verify no regressions**

Run: `go test ./internal/agent/... -v`
Expected: All pass (no other test asserts the exact old prompt text)

**Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "[BEHAVIORAL] Wire persona system prompt into agent core"
```

---

### Task 4: Wire persona into TUI view (goodbye + thinking)

**Files:**
- Modify: `internal/tui/view.go` (lines 19, 48)
- Modify: `internal/tui/model_test.go`

**Step 1: Write/update tests**

Add to `internal/tui/model_test.go`:

```go
func TestViewGoodbyeMessage(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.quitting = true
	view := m.View()
	assert.Contains(t, view, "Ruby")
	assert.Contains(t, view, "bye")
}

func TestViewThinkingMessage(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.state = StateStreaming
	view := m.View()
	assert.Contains(t, view, "Ruby")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run "TestViewGoodbyeMessage|TestViewThinkingMessage" -v`
Expected: FAIL — view contains "Goodbye!" and "Thinking..."

**Step 3: Update view.go**

In `internal/tui/view.go`, add import:
```go
"github.com/julianshen/rubichan/internal/persona"
```

Replace line 19:
```go
return persona.GoodbyeMessage()
```

Replace line 48:
```go
b.WriteString(fmt.Sprintf("%s %s", m.spinner.View(), persona.ThinkingMessage()))
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/... -run "TestViewGoodbyeMessage|TestViewThinkingMessage" -v`
Expected: PASS

**Step 5: Run all TUI tests**

Run: `go test ./internal/tui/... -v`
Expected: All pass

**Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/model_test.go
git commit -m "[BEHAVIORAL] Wire persona goodbye and thinking messages into TUI"
```

---

### Task 5: Wire persona into banner subtitle

**Files:**
- Modify: `internal/tui/banner.go` (RenderBanner function)
- Modify: `internal/tui/banner_test.go`

**Step 1: Write the failing test**

Add to `internal/tui/banner_test.go`:

```go
func TestRenderBannerContainsWelcome(t *testing.T) {
	rendered := RenderBanner()
	assert.Contains(t, rendered, "Ruby")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestRenderBannerContainsWelcome -v`
Expected: FAIL — banner doesn't contain "Ruby"

**Step 3: Update RenderBanner**

In `internal/tui/banner.go`, add import:
```go
"github.com/julianshen/rubichan/internal/persona"
```

Update `RenderBanner()`:
```go
func RenderBanner() string {
	lines := strings.Split(Banner, "\n")
	styled := make([]string, len(lines))
	for i, line := range lines {
		color := bannerColors[i%len(bannerColors)]
		style := lipgloss.NewStyle().Foreground(color).Bold(true)
		styled[i] = style.Render(line)
	}
	welcomeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF6B9D")).
		Italic(true)
	return strings.Join(styled, "\n") + "\n" + welcomeStyle.Render(persona.WelcomeMessage())
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/... -run TestRenderBannerContainsWelcome -v`
Expected: PASS

**Step 5: Check existing banner tests still pass**

Run: `go test ./internal/tui/... -run TestBanner -v`
Expected: `TestRenderBannerPreservesLineCount` may fail because we added a line. If so, update the test to account for the welcome line (+1 line). Otherwise all pass.

**Step 6: Fix line count test if needed**

If `TestRenderBannerPreservesLineCount` fails, update it:
```go
func TestRenderBannerPreservesLineCount(t *testing.T) {
	bannerLines := strings.Split(Banner, "\n")
	renderedLines := strings.Split(RenderBanner(), "\n")
	// +1 for the welcome subtitle line appended by RenderBanner
	assert.Equal(t, len(bannerLines)+1, len(renderedLines),
		"rendered banner should have original lines plus welcome subtitle")
}
```

**Step 7: Commit**

```bash
git add internal/tui/banner.go internal/tui/banner_test.go
git commit -m "[BEHAVIORAL] Add Ruby welcome subtitle to TUI banner"
```

---

### Task 6: Wire persona into status bar

**Files:**
- Modify: `internal/tui/statusbar.go` (line 44)
- Modify: `internal/tui/statusbar_test.go`

**Step 1: Write the failing test**

Add to `internal/tui/statusbar_test.go`:

```go
func TestStatusBarContainsPersona(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetModel("claude-sonnet-4-5")
	result := sb.View()
	assert.Contains(t, result, "Ruby")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestStatusBarContainsPersona -v`
Expected: FAIL — no "Ruby" in status bar

**Step 3: Update statusbar.go**

Add import: `"github.com/julianshen/rubichan/internal/persona"`

Update `View()` method (line 43-51):
```go
func (s *StatusBar) View() string {
	return s.style.Render(fmt.Sprintf(" %s  %s  %s/%s  Turn %d/%d  ~$%.2f",
		persona.StatusPrefix(),
		s.model,
		formatTokens(s.inputTokens),
		formatTokens(s.maxTokens),
		s.turn, s.maxTurns,
		s.cost,
	))
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/... -run TestStatusBarContainsPersona -v`
Expected: PASS

**Step 5: Run all status bar tests**

Run: `go test ./internal/tui/... -run TestStatusBar -v`
Expected: All pass (existing tests assert model name, tokens, turns, cost — all still present)

**Step 6: Commit**

```bash
git add internal/tui/statusbar.go internal/tui/statusbar_test.go
git commit -m "[BEHAVIORAL] Add Ruby persona prefix to status bar"
```

---

### Task 7: Wire persona into CLI error output

**Files:**
- Modify: `cmd/rubichan/main.go` (line 138)
- Modify: `cmd/rubichan/main_test.go`

**Step 1: Write the failing test**

Check if there's an existing test for the error output path. If not, add to `cmd/rubichan/main_test.go`:

```go
func TestPersonaErrorMessage(t *testing.T) {
	// Verify persona.ErrorMessage is used by testing the persona function directly.
	// The main() function calls os.Exit so we can't easily test it end-to-end.
	msg := persona.ErrorMessage("something broke")
	assert.Contains(t, msg, "Pigi")
	assert.Contains(t, msg, "something broke")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/... -run TestPersonaErrorMessage -v`
Expected: FAIL — persona not imported

**Step 3: Update main.go**

Add import: `"github.com/julianshen/rubichan/internal/persona"`

Replace line 138:
```go
fmt.Fprint(os.Stderr, persona.ErrorMessage(err.Error()))
```

Add import in test file too:
```go
"github.com/julianshen/rubichan/internal/persona"
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/rubichan/... -run TestPersonaErrorMessage -v`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: All pass

**Step 6: Run linter and format check**

Run: `golangci-lint run ./... && gofmt -l .`
Expected: Clean

**Step 7: Commit**

```bash
git add cmd/rubichan/main.go cmd/rubichan/main_test.go
git commit -m "[BEHAVIORAL] Wire persona error message into CLI output"
```

---

### Task 8: Final verification

**Step 1: Run full test suite with coverage**

Run: `go test -cover ./internal/persona/... ./internal/agent/... ./internal/tui/... ./cmd/rubichan/...`
Expected: All pass, persona package >90% coverage

**Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: Clean

**Step 3: Check formatting**

Run: `gofmt -l .`
Expected: No output (all formatted)

**Step 4: Verify no regressions**

Run: `go test ./... 2>&1 | grep -E "FAIL|ok"`
Expected: All `ok`, no `FAIL`
