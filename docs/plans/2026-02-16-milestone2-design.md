# Milestone 2: Headless Mode + Output Formatters + Code Review

## Scope

**In scope:**
- Headless runner: `--headless` flag with `--prompt`, `--file`, stdin input
- Output formatters: JSON, Markdown (via `--output` flag, default: markdown)
- Code review mode: `--mode=code-review` with `--diff` flag
- Controls: `--max-turns`, `--timeout`, `--tools` whitelist
- Exit codes: 0 for success, 1 for errors

**Out of scope (deferred):**
- SARIF output, GitHub PR comment formatter
- Security scanners (secrets, SAST, deps, config)
- Log analysis pipeline
- Xcode/Apple tools
- Skill system integration

## Architecture

### New packages

```text
internal/runner/
    runner.go          # Runner interface + HeadlessRunner
    headless.go        # Headless execution: input → agent → output
    input.go           # Input resolution: --prompt, --file, stdin
internal/output/
    formatter.go       # Formatter interface + RunResult types
    json.go            # JSON output formatter
    markdown.go        # Markdown output formatter
internal/pipeline/
    codereview.go      # Code review pipeline: diff → prompt → findings
    diff.go            # Git diff extraction
```

### Modified files

- `cmd/rubichan/main.go` — Add headless flags, route to headless runner when `--headless` is set

### Data flow

```text
CLI flags/stdin
    ↓
InputResolver (--prompt | --file | stdin)
    ↓
HeadlessRunner
    ├── (generic mode) → Agent.Turn(prompt) → collect TurnEvents → Formatter → stdout
    └── (code-review)  → GitDiff(--diff) → build review prompt → Agent.Turn() → collect → Formatter → stdout
```

The agent loop runs with tools enabled (file, shell) so the LLM can inspect code during review. `--max-turns` and `--timeout` control execution bounds.

## Headless Runner

### CLI flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--headless` | bool | false | Run in non-interactive mode |
| `--prompt` | string | "" | Direct prompt text |
| `--file` | string | "" | Read prompt from file |
| `--mode` | string | "" | Specialized mode (e.g. `code-review`) |
| `--output` | string | "markdown" | Output format: `json`, `markdown` |
| `--diff` | string | "" | Git diff range for code-review (e.g. `HEAD~1..HEAD`) |
| `--max-turns` | int | from config | Override max agent turns |
| `--timeout` | duration | 120s | Overall execution timeout |
| `--tools` | string | "" | Comma-separated tool whitelist (empty = all) |

### Input priority

`--prompt` > `--file` > stdin. Error if none provided and stdin is a TTY.

### Execution

HeadlessRunner collects all TurnEvents from the agent channel, accumulates the text response, then calls the formatter. No Bubble Tea — writes directly to stdout.

### Exit codes

0 = success, 1 = error (agent failure, timeout, no input).

## Output Formatters

### Interface

```go
type Formatter interface {
    Format(result *RunResult) ([]byte, error)
}

type RunResult struct {
    Prompt     string
    Response   string
    ToolCalls  []ToolCallLog
    TurnCount  int
    DurationMs int64
    Mode       string
    Error      string
}

type ToolCallLog struct {
    ID      string
    Name    string
    Input   json.RawMessage
    Result  string
    IsError bool
}
```

### JSON formatter

Marshals RunResult as JSON to stdout.

### Markdown formatter

Renders human-readable output with response text, tool call summaries, and execution metadata.

## Code Review Pipeline

### Trigger

`--headless --mode=code-review --diff HEAD~1..HEAD`

### Flow

1. `diff.go` runs `git diff <range>` via `exec.Command`
2. `codereview.go` constructs a review prompt including the diff
3. Prompt is passed to `Agent.Turn()` — agent can use file/shell tools for context
4. Response piped through chosen output formatter

### Diff extraction

Shell out to `git diff`. If `--diff` is empty in code-review mode, default to `HEAD~1..HEAD`.

### No structured LLM output parsing

The LLM's text response is the review. Structured finding extraction deferred to security milestone.

## Testing

- **Unit**: Input resolution, headless runner with mock agent, formatters with known RunResult, diff extraction with temp git repo
- **Integration**: Build binary, run headless with real provider, verify stdout output
- **Tool whitelist**: Verify `--tools=file` prevents shell registration
