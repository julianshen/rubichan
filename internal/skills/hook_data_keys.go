package skills

// Hook event data keys. HookEvent.Data is a map[string]any populated at
// dispatch sites and read by skill handlers. These constants are the
// stable surface for that contract: dispatch sites must use them when
// writing keys, handlers should use them when reading. Adding a new
// dispatch-site field requires adding a constant here.

// Turn lifecycle data keys.
const (
	HookDataUserMessage = "user_message"
	HookDataResponse    = "response"
	HookDataPromptBuild = "prompt_build"

	// HookDataResponseReason carries why the current response is the turn's
	// final one — no pending tools, an empty model response, or a batch
	// containing task_complete.
	//
	// It is NOT the turn's outcome. HookOnAfterResponse fires before the
	// pending tools execute (the hook rewrites the assistant text, which
	// must be persisted before any tool_result can follow it), so nothing
	// that happens during execution can be reflected here: a cancelled
	// task_complete batch still reports task_complete. Handlers that need
	// the outcome must read the done event's ExitReason instead.
	HookDataResponseReason = "response_reason"
)

// Tool execution data keys.
const (
	HookDataToolName = "tool_name"
	HookDataInput    = "input"
	HookDataContent  = "content"
	HookDataIsError  = "is_error"
)

// Task lifecycle data keys.
const (
	HookDataName         = "name"
	HookDataPrompt       = "prompt"
	HookDataDepth        = "depth"
	HookDataOutput       = "output"
	HookDataTurnCount    = "turn_count"
	HookDataInputTokens  = "input_tokens"
	HookDataOutputTokens = "output_tokens"
	HookDataToolsUsed    = "tools_used"
	HookDataError        = "error"
)

// Worktree lifecycle data keys.
const (
	HookDataSubagentName = "subagent_name"
	HookDataWorktreeName = "worktree_name"
)

// Security scan completion data keys.
const (
	HookDataFindings      = "findings"
	HookDataAttackChains  = "attack_chains"
	HookDataErrors        = "errors"
	HookDataFindingsCount = "findings_count"
	HookDataChainCount    = "chain_count"
	HookDataFilesScanned  = "files_scanned"
	HookDataDurationMs    = "duration_ms"
	HookDataErrorsCount   = "errors_count"
)

// Project setup data keys.
const (
	HookDataMode = "mode"
	HookDataDir  = "dir"
)
