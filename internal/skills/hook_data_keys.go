package skills

// Hook event data keys. HookEvent.Data is a map[string]any populated at
// dispatch sites and read by skill handlers. These constants are the
// stable surface for that contract: dispatch sites must use them when
// writing keys, handlers should use them when reading. Adding a new
// dispatch-site field requires adding a constant here.
const (
	HookDataUserMessage = "user_message"
	HookDataResponse    = "response"
	HookDataExitReason  = "exit_reason"

	HookDataName         = "name"
	HookDataPrompt       = "prompt"
	HookDataDepth        = "depth"
	HookDataOutput       = "output"
	HookDataTurnCount    = "turn_count"
	HookDataInputTokens  = "input_tokens"
	HookDataOutputTokens = "output_tokens"
	HookDataToolsUsed    = "tools_used"
	HookDataError        = "error"

	HookDataSubagentName = "subagent_name"
	HookDataWorktreeName = "worktree_name"

	HookDataFindings      = "findings"
	HookDataAttackChains  = "attack_chains"
	HookDataErrors        = "errors"
	HookDataFindingsCount = "findings_count"
	HookDataChainCount    = "chain_count"
	HookDataFilesScanned  = "files_scanned"
	HookDataDurationMs    = "duration_ms"
	HookDataErrorsCount   = "errors_count"

	HookDataMode = "mode"
	HookDataDir  = "dir"
)
