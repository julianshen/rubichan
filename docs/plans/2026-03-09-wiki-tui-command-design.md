# Wiki TUI Command Design

## Problem

The wiki generation feature is only accessible as an LLM tool (`generate_wiki`). Users should be able to trigger it directly via a `/wiki` slash command in the TUI with an interactive form for configuration.

## Design

### `/wiki` Command and Form

- User types `/wiki` → TUI opens a `huh` form overlay (same pattern as `/config`)
- Form has 4 fields:
  - **Path**: text input, defaults to `.` (current working directory)
  - **Format**: select from `raw-md`, `hugo`, `docusaurus`
  - **Output directory**: text input, defaults to `docs/wiki`
  - **Concurrency**: text input, defaults to `5`
- On submit → start wiki generation in background. On abort (Esc) → return to input.

### Background Execution and Progress

- Wiki runs in a goroutine after form submission, communicating via Bubble Tea messages
- `wikiProgressMsg` carries stage updates; `wikiDoneMsg` signals completion/error
- Goroutine calls `wiki.Run()` with a `ProgressFunc` callback that sends messages through a channel
- User can continue chatting while wiki runs — input stays in `StateInput`

### Progress Display

- **Content area**: Progress lines appended to viewport as they arrive
- **Status bar**: New field showing `Wiki: scanning...`, `Wiki: analyzing...`, etc. Cleared when done.
- Both streams coexist if user starts an agent turn during wiki generation.

### Wiki Progress Callback

- Add `ProgressFunc func(stage string, current, total int)` to `wiki.Config`
- Replace `fmt.Fprintf(os.Stderr, ...)` in `wiki.Run()` with calls to `ProgressFunc` when non-nil
- Falls back to stderr when nil (preserves headless behavior unchanged)
- 7 existing stages: scanning, chunking, analyzing, diagrams, assembling, rendering, done

## File Map

### New files
- `internal/tui/wiki_command.go` — command implementation, form, background runner, message types
- `internal/tui/wiki_command_test.go` — tests

### Modified files
- `internal/wiki/pipeline.go` — add `ProgressFunc` to Config, replace stderr writes
- `internal/wiki/pipeline_test.go` — test progress callback
- `internal/tui/model.go` — wiki fields (running flag), register command, pass wiki dependencies
- `internal/tui/update.go` — handle wikiProgressMsg/wikiDoneMsg
- `internal/tui/statusbar.go` — SetWikiProgress/ClearWikiProgress
- `cmd/rubichan/main.go` — pass wiki dependencies to TUI model
