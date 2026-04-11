# Plan: Next Priorities

Post-milestone polish and gap closure. Ordered by impact and risk.

---

## Priority 1: Wire ACP Approval to TUI Overlay (Security-Critical)

**Goal**: The interactive ACP client (`internal/modes/interactive/acp_client.go:292`) auto-approves all tool calls without user interaction. This is a security hole — the existing `ApprovalOverlay` in `internal/tui/approval.go` already handles the full approval UX (risk classification, destructive warnings, Y/N/A/D keys, viewport scrolling). The ACP client must delegate to it instead of returning `approve` unconditionally.

**Current state**: The TUI `Model.MakeApprovalFunc()` (`model.go:630`) correctly bridges agent approval to TUI via `approvalCh`. The ACP client bypasses this entirely.

### Tests

- [x] **1.1** `TestACPClientApprovalRequestDelegates` — `ACPClient.ApprovalRequest` sends a request to the provided approval callback instead of auto-approving. When callback returns `true`, the method returns `(true, nil)`.
- [x] **1.2** `TestACPClientApprovalRequestDenied` — When the approval callback returns `false`, `ApprovalRequest` returns `(false, nil)` and the ACP response carries `decision: "deny"`.
- [x] **1.3** `TestACPClientApprovalRequestCallbackError` — When the callback returns an error, `ApprovalRequest` propagates the error.
- [x] **1.4** `TestACPClientApprovalRequestPassesToolAndInput` — The callback receives the correct `tool` name and `input` JSON from the ACP request.
- [x] **1.5** `TestACPClientDefaultApprovalAutoApproves` — When no callback is configured (nil), the client falls back to auto-approve for backward compatibility.
- [x] **1.6** `TestNewACPClientWithApprovalFunc` — `NewACPClient` accepts an optional `ApprovalFunc` parameter and stores it for use by `ApprovalRequest`.

### Implementation

- Add `approvalFunc agent.ApprovalFunc` field to `ACPClient` struct
- Add `WithApprovalFunc(fn agent.ApprovalFunc)` option or constructor parameter
- Modify `ApprovalRequest` to call `approvalFunc` when non-nil, falling back to auto-approve when nil
- Wire `Model.MakeApprovalFunc()` into the ACP client at initialization in `cmd/rubichan/main.go`

---

## Priority 2: Wire Session Selector Overlay into Bubble Tea

**Goal**: The session selector overlay (`internal/modes/interactive/session_selector.go`) is constructed but never displayed (`ui.go:87`). Wire it into the TUI `Model` so `/resume` actually shows a selectable session list.

**Current state**: `InteractiveTUI.resumeSessionFlow()` creates a `SessionSelectorOverlay` and immediately discards it (`_ = overlay`). The `Model` already supports overlays via `activeOverlay` and `processOverlayResult()`.

### Tests

- [x] **2.1** `TestSessionResumeOverlayImplementsOverlay` — `SessionResumeOverlay` satisfies the `tui.Overlay` interface.
- [x] **2.2** `TestSessionResumeOverlayViewShowsSessions` — `View()` renders all session titles/dates.
- [x] **2.3** `TestSessionResumeOverlaySelectSession` — Arrow keys + Enter selects a session; `Done()` returns true and `Result()` carries the selected `SessionResumeResult`.
- [x] **2.4** `TestSessionResumeOverlayEscape` — Pressing Escape cancels; `Done()` returns true and `Result()` returns nil.
- [x] **2.5** `TestProcessOverlayResultSessionResume` — `processOverlayResult` for a `SessionResumeResult` transitions back to input state.
- [x] **2.6** `TestResumeCommandSetsOverlay` — The `/resume` command sets `activeOverlay` to a `SessionResumeOverlay` and transitions to overlay state.

### Status: Overlay UI complete, session restore NOT YET IMPLEMENTED

The overlay correctly collects the user's session selection and returns a `SessionResumeResult` with the session ID. However, `processOverlayResult` currently only prints "Resuming session..." — it does not load turns or restore conversation state.

### Follow-up: Complete session restore (Priority 2b)

Wire the selected session ID into the agent's session loading infrastructure so the conversation is actually restored.

#### Tests

- [ ] **2b.1** `TestProcessOverlayResultSessionResumeLoadsSession` — Selecting a session loads its turns from the store and restores them into the agent's conversation history.
- [ ] **2b.2** `TestProcessOverlayResultSessionResumeRendersTurns` — After resume, the previously stored turns are visible in the TUI content buffer.
- [ ] **2b.3** `TestProcessOverlayResultSessionResumeError` — When the session ID is not found in the store, an error message is shown and state returns to input.

#### Implementation

- In `processOverlayResult`, call `m.sessionStore.GetSession(r.SessionID)` + `m.sessionStore.GetMessages(r.SessionID)` to load the session data.
- Feed loaded messages into the agent via `agent.WithResumeSession` or equivalent.
- Render restored turns in the content buffer so the user sees conversation history.

---

## Priority 3: Test Coverage for Headless and Wiki Mode ACP Clients

**Goal**: `internal/modes/headless/` and `internal/modes/wiki/` have zero unit tests. Both ACP clients have testable logic (request construction, timeout handling, response parsing, progress tracking).

### Tests — Headless

- [ ] **3.1** `TestHeadlessACPClientRunCodeReview` — Constructs correct JSON-RPC request with method `agent/codeReview` and code input. (requires transport mock)
- [ ] **3.2** `TestHeadlessACPClientRunCodeReviewError` — Returns wrapped error when response contains error field. (requires transport mock)
- [ ] **3.3** `TestHeadlessACPClientRunSecurityScan` — Constructs correct request with method `security/scan` and unmarshals response. (requires transport mock)
- [x] **3.4** `TestHeadlessACPClientSetTimeout` — `SetTimeout`/`Timeout` round-trips correctly; default is 30s.
- [x] **3.5** `TestHeadlessACPClientGetNextID` — IDs increment monotonically; concurrent calls produce unique IDs.
- [x] **3.6** `TestHeadlessACPClientClose` — `Close` stops dispatcher without panic on nil.

### Tests — Wiki

- [ ] **3.7** `TestWikiACPClientGenerateDocs` — Constructs request with all `GenerateOptions` fields and method `wiki/generate`. (requires transport mock)
- [ ] **3.8** `TestWikiACPClientGenerateDocsError` — Returns wrapped error with code and message from response. (requires transport mock)
- [x] **3.9** `TestWikiACPClientProgress` — `SetProgress`/`Progress` round-trips; clamped to 0-100 range.
- [x] **3.10** `TestWikiACPClientProgressClamping` — Values > 100 clamp to 100; values < 0 clamp to 0.
- [ ] **3.11** `TestWikiACPClientGenerateDocsSetsProgress` — Progress is 0 before call, 100 after successful response. (requires transport mock)
- [x] **3.12** `TestWikiACPClientClose` — `Close` stops dispatcher without panic on nil.

---

## Priority 4: Wire Mermaid Inline Rendering into Viewport

**Goal**: `renderMermaidInline` (`internal/tui/diagrams.go:19`) exists but is never called. Wire it into the viewport rendering path so Mermaid code blocks in assistant output are replaced with inline Kitty graphics images when the terminal supports it.

**Current state**: The function checks `caps.KittyGraphics` and `terminal.MmdcAvailable()`, renders PNG via `terminal.RenderMermaid`, and transmits via `terminal.KittyImage`. The viewport renders markdown but doesn't intercept Mermaid blocks.

### Tests

- [ ] **4.1** `TestRenderMermaidInlineReturnsFalseWhenNoCaps` — Returns false when caps is nil.
- [ ] **4.2** `TestRenderMermaidInlineReturnsFalseWhenNoKitty` — Returns false when `KittyGraphics` is false.
- [ ] **4.3** `TestDetectMermaidCodeBlock` — Helper function correctly identifies fenced Mermaid blocks in markdown output.
- [ ] **4.4** `TestReplaceMermaidBlocksNoOp` — When Kitty is unavailable, markdown passes through unchanged.
- [ ] **4.5** `TestReplaceMermaidBlocksInsertsPlaceholder` — When Kitty is available and mmdc is present, Mermaid blocks are replaced with rendered image output.
- [ ] **4.6** `TestTurnRendererCallsMermaidReplace` — `TurnRenderer` invokes Mermaid replacement during assistant content rendering.

### Implementation

- Add `detectMermaidBlocks(content string) []mermaidBlock` helper
- Add `replaceMermaidBlocks(content string, caps *terminal.Caps) string` that calls `renderMermaidInline` per block
- Call from `TurnRenderer` or `MarkdownRenderer` when rendering assistant content

---

## Priority 5: Consume `ui_update` Payloads

**Goal**: The `ui_update` event type (`internal/tui/update.go:689`) is received but only logged in debug mode. Wire it to update a progress indicator (e.g., status bar or inline text) for long-running operations like wiki generation or security scans.

### Tests

- [ ] **5.1** `TestUIUpdateSetsStatusBarProgress` — A `ui_update` event with `status=running` and a percentage updates the status bar progress.
- [ ] **5.2** `TestUIUpdateCompleteClearsProgress` — A `ui_update` event with `status=complete` clears the progress indicator.
- [ ] **5.3** `TestUIUpdateWritesInlineText` — When `ui_update` carries a `message` field, it is appended to the content buffer.

---

## Priority 6: Model Picker Integration

**Goal**: Wire `tui.ModelPicker` (`internal/tui/modelpicker.go`) into the interactive mode so users can switch LLM models at runtime (`cmd/rubichan/main.go:1252`).

### Tests

- [ ] **6.1** `TestModelPickerOverlayImplementsOverlay` — Satisfies the `Overlay` interface.
- [ ] **6.2** `TestModelPickerSelectModel` — Selecting a model returns it as `Result()`.
- [ ] **6.3** `TestModelPickerEscape` — Escape cancels without changing model.
- [ ] **6.4** `TestProcessOverlayResultModelPicker` — `processOverlayResult` for a `ModelPickerResult` updates the agent's provider.
- [ ] **6.5** `TestSlashModelCommand` — The `/model` command triggers the model picker overlay.

---

## Priority 7: Windows Compatibility for Checkpoint Recovery

**Goal**: `internal/checkpoint/recovery.go:156` uses `unix.Kill(pid, 0)` to check process liveness, which doesn't work on Windows. Add a platform-abstracted process check.

### Tests

- [ ] **7.1** `TestProcessAliveCurrentProcess` — `processAlive(os.Getpid())` returns true.
- [ ] **7.2** `TestProcessAliveDeadProcess` — `processAlive(999999999)` returns false.
- [ ] **7.3** `TestProcessAliveNegativePID` — `processAlive(-1)` returns false.

### Implementation

- Extract `processAlive(pid int) bool` into `recovery_unix.go` (build tag `//go:build !windows`) and `recovery_windows.go` (using `os.FindProcess` + `process.Signal(nil)`).

---

## Deferred (P2 from spec, not urgent)

These are spec P2 items to address after the above are complete:

- **FR-1.6** LSP integration (diagnostics, go-to-definition, completions)
- **FR-3.7** Incremental wiki regeneration (only re-analyze changed modules)
- **FR-4.11** License compliance checking
- **FR-5.13** MCP servers auto-discovered as skills
- **FR-6.7** Asset catalog management (Xcode)
- **FR-6.9** App distribution support (Xcode)
- **FR-6.11** SwiftUI/UIKit-aware code analysis
- **FR-6.13** CoreData/SwiftData model introspection

---

## Execution Order

Work these in priority order (1 through 7). Each priority is a standalone PR. Follow the TDD rhythm from CLAUDE.md: Red -> Green -> Refactor -> Commit -> Repeat.
