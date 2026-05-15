package skills

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/tools"
)

// SkillState represents the lifecycle state of a skill.
type SkillState int

const (
	// SkillStateInactive means the skill is not currently active.
	SkillStateInactive SkillState = iota
	// SkillStateActivating means the skill is in the process of loading and activating.
	SkillStateActivating
	// SkillStateActive means the skill is fully loaded and ready to handle requests.
	SkillStateActive
	// SkillStateError means the skill encountered an error during activation.
	SkillStateError
)

// String returns the human-readable name of a SkillState.
func (s SkillState) String() string {
	switch s {
	case SkillStateInactive:
		return "Inactive"
	case SkillStateActivating:
		return "Activating"
	case SkillStateActive:
		return "Active"
	case SkillStateError:
		return "Error"
	default:
		return fmt.Sprintf("SkillState(%d)", s)
	}
}

// validTransitions defines the allowed state transitions for a skill lifecycle.
var validTransitions = map[SkillState]map[SkillState]bool{
	SkillStateInactive: {
		SkillStateActivating: true,
	},
	SkillStateActivating: {
		SkillStateActive: true,
		SkillStateError:  true,
	},
	SkillStateActive: {
		SkillStateInactive: true,
	},
	SkillStateError: {
		SkillStateInactive: true,
	},
}

// HookPhase identifies a specific point in the agent lifecycle where skills
// can inject behavior.
type HookPhase int

const (
	// HookOnActivate is called when a skill is being activated.
	HookOnActivate HookPhase = iota
	// HookOnDeactivate is called when a skill is being deactivated.
	HookOnDeactivate
	// HookOnConversationStart is called at the beginning of a new conversation.
	HookOnConversationStart
	// HookOnBeforePromptBuild is called before the system prompt is assembled.
	HookOnBeforePromptBuild
	// HookOnBeforeToolCall is called before a tool is executed.
	HookOnBeforeToolCall
	// HookOnAfterToolResult is called after a tool execution returns a result.
	HookOnAfterToolResult
	// HookOnAfterResponse is called after the LLM generates a response.
	HookOnAfterResponse
	// HookOnBeforeWikiSection is called before a wiki section is generated.
	HookOnBeforeWikiSection
	// HookOnSecurityScanComplete is called after all security scans finish.
	HookOnSecurityScanComplete
	// HookOnWorktreeCreate is called after a git worktree is successfully created.
	HookOnWorktreeCreate
	// HookOnWorktreeRemove is called before a git worktree is removed.
	HookOnWorktreeRemove
	// HookOnSetup is called during project initialization (rubichan init).
	HookOnSetup
	// HookOnTaskCreated is called after the task tool creates a new task/subagent.
	HookOnTaskCreated
	// HookOnTaskCompleted is called after a task/subagent completes execution.
	HookOnTaskCompleted
)

// String returns the human-readable name of a HookPhase.
func (h HookPhase) String() string {
	switch h {
	case HookOnActivate:
		return "OnActivate"
	case HookOnDeactivate:
		return "OnDeactivate"
	case HookOnConversationStart:
		return "OnConversationStart"
	case HookOnBeforePromptBuild:
		return "OnBeforePromptBuild"
	case HookOnBeforeToolCall:
		return "OnBeforeToolCall"
	case HookOnAfterToolResult:
		return "OnAfterToolResult"
	case HookOnAfterResponse:
		return "OnAfterResponse"
	case HookOnBeforeWikiSection:
		return "OnBeforeWikiSection"
	case HookOnSecurityScanComplete:
		return "OnSecurityScanComplete"
	case HookOnWorktreeCreate:
		return "OnWorktreeCreate"
	case HookOnWorktreeRemove:
		return "OnWorktreeRemove"
	case HookOnSetup:
		return "OnSetup"
	case HookOnTaskCreated:
		return "OnTaskCreated"
	case HookOnTaskCompleted:
		return "OnTaskCompleted"
	default:
		return fmt.Sprintf("HookPhase(%d)", h)
	}
}

// HookEvent carries event data to hook handlers. It provides context about
// the agent lifecycle event that triggered the hook.
type HookEvent struct {
	// Phase identifies which hook phase triggered this event.
	Phase HookPhase

	// SkillName is the name of the skill receiving the event.
	SkillName string

	// Data carries phase-specific event data (e.g., tool name, prompt text).
	Data map[string]any

	// Ctx is the context for the hook invocation.
	Ctx context.Context
}

// HookResult carries the handler's response back to the agent core.
type HookResult struct {
	// Modified holds any data the handler wants to feed back into the pipeline.
	Modified map[string]any

	// Cancel signals the agent to abort the current operation (e.g., cancel a tool call).
	Cancel bool
}

// HookHandler is the function signature for skill hook handlers.
type HookHandler func(event HookEvent) (HookResult, error)

// PermissionChecker abstracts the permission and rate-limit enforcement
// provided by the sandbox. This interface lives in the skills package to
// avoid an import cycle between skills and skills/sandbox.
type PermissionChecker interface {
	CheckPermission(perm Permission) error
	CheckRateLimit(resource string) error
	ResetTurnLimits()
}

// AgentDefinition describes a pre-configured subagent template contributed
// by a skill backend. This type mirrors agent.AgentDef but lives in the
// skills package to avoid a circular import (agent imports skills).
type AgentDefinition struct {
	Name          string
	Description   string
	SystemPrompt  string
	Tools         []string
	MaxTurns      int
	MaxDepth      int
	Model         string
	InheritSkills *bool
	ExtraSkills   []string
	DisableSkills []string
	Isolation     string // "", "worktree"
}

// AgentDefRegistrar abstracts the agent definition registry so the skills
// package can register/unregister agent definitions without importing the
// agent package. This interface lives here to break the import cycle
// (same pattern as PermissionChecker).
type AgentDefRegistrar interface {
	Register(def *AgentDefinition) error
	Unregister(name string) error
}

// SkillBackend is the interface that all skill backends (Starlark, Go plugin,
// external process) must implement. It handles loading, tool registration,
// hook registration, and cleanup.
type SkillBackend interface {
	// Load initialises the backend with the given manifest and sandbox.
	Load(manifest SkillManifest, sandbox PermissionChecker) error

	// Tools returns the tools provided by this backend.
	Tools() []tools.Tool

	// Hooks returns hook handlers registered by this backend, keyed by phase.
	Hooks() map[HookPhase]HookHandler

	// Commands returns slash commands provided by this backend.
	Commands() []commands.SlashCommand

	// Agents returns agent definitions contributed by this backend.
	Agents() []*AgentDefinition

	// Workflows returns workflow handlers registered by this backend, keyed by name.
	// Workflow skills register handlers via register_workflow() in Starlark or
	// equivalent APIs in other backends. The runtime wires these into WorkflowRunner
	// during activation.
	Workflows() map[string]WorkflowHandler

	// Unload releases resources held by this backend.
	Unload() error
}

// Skill is the runtime representation of a loaded skill. It combines the
// static manifest with runtime state, filesystem location, discovery source,
// and the implementation backend.
type Skill struct {
	// Manifest is the parsed SKILL.yaml for this skill.
	Manifest *SkillManifest

	// State is the current lifecycle state.
	State SkillState

	// Dir is the directory on disk where the skill resides.
	Dir string

	// Source indicates where the skill was discovered (builtin, user, project, inline).
	Source Source

	// Backend is the loaded implementation backend (nil when inactive).
	Backend SkillBackend

	// InstructionBody holds the markdown body for instruction skills (SKILL.md).
	// Empty for regular SKILL.yaml skills.
	InstructionBody string
}

// TransitionTo validates and performs a state transition. It returns an error
// if the transition is not allowed by the lifecycle state machine.
func (s *Skill) TransitionTo(newState SkillState) error {
	allowed, ok := validTransitions[s.State]
	if !ok || !allowed[newState] {
		return fmt.Errorf("invalid state transition from %s to %s", s.State, newState)
	}
	s.State = newState
	return nil
}

// SkillIndex is a lightweight representation of a skill for system prompt building
// and trigger evaluation. It contains only the minimal metadata needed to list
// skills in the prompt (~50 tokens per skill) without exposing the full manifest.
type SkillIndex struct {
	// Name is the skill's unique identifier.
	Name string

	// Description is a short human-readable summary.
	Description string

	// Types lists the skill types (tool, prompt, workflow, etc.).
	Types []SkillType

	// Triggers holds the activation trigger configuration.
	Triggers TriggerConfig

	// Source indicates where the skill was discovered.
	Source Source

	// Dir is the directory on disk where the skill resides.
	Dir string
}

// NewSkillIndex creates a SkillIndex from a manifest, source, and directory.
func NewSkillIndex(manifest *SkillManifest, source Source, dir string) SkillIndex {
	if manifest == nil {
		return SkillIndex{Source: source, Dir: dir}
	}
	typesCopy := make([]SkillType, len(manifest.Types))
	copy(typesCopy, manifest.Types)
	return SkillIndex{
		Name:        manifest.Name,
		Description: manifest.Description,
		Types:       typesCopy,
		Triggers:    manifest.Triggers,
		Source:      source,
		Dir:         dir,
	}
}

// SkillSummary is a lightweight snapshot of a skill's current state, suitable
// for display in the /skill command.
type SkillSummary struct {
	Name        string
	Description string
	Source      Source
	State       SkillState
	Types       []SkillType
}

// ContextBudget controls the total context window budget allocated to skill
// prompt fragments. When set, the PromptCollector enforces these limits during
// prompt building, using priority-based allocation.
type ContextBudget struct {
	// MaxTotalTokens is the global cap on total tokens from all skill fragments.
	// Zero means unlimited (backward compatible).
	MaxTotalTokens int

	// MaxPerSkillTokens is the maximum tokens any single skill may contribute.
	// Zero means unlimited.
	MaxPerSkillTokens int
}

// DefaultContextBudget returns a ContextBudget with sensible defaults.
func DefaultContextBudget() ContextBudget {
	return ContextBudget{
		MaxTotalTokens:    8000,
		MaxPerSkillTokens: 2000,
	}
}

// estimateTokens provides a rough token estimate based on character count.
// The approximation is ~4 characters per token, which is a reasonable average
// for English text and code.
func estimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + 3) / 4 // round up
}

// SkillTokenBudget caps skill descriptions at a percentage of the context window.
type SkillTokenBudget struct {
	// MaxChars is the maximum total characters for all skill descriptions.
	MaxChars int
}

// DefaultSkillTokenBudget returns a budget of ~1% of a 128K context window.
func DefaultSkillTokenBudget() SkillTokenBudget {
	return SkillTokenBudget{MaxChars: 8192} // 1% of 128K context
}

// BudgetSkillIndexes truncates skill descriptions to fit within the budget.
// Bundled/built-in skills are not truncated. Non-bundled skills are truncated
// proportionally to their original description length.
func BudgetSkillIndexes(indexes []SkillIndex, budget SkillTokenBudget) []SkillIndex {
	if budget.MaxChars <= 0 {
		return indexes
	}

	// Separate bundled and non-bundled skills.
	var bundled, nonBundled []SkillIndex
	for _, idx := range indexes {
		if idx.Source == SourceBuiltin {
			bundled = append(bundled, idx)
		} else {
			nonBundled = append(nonBundled, idx)
		}
	}

	if len(nonBundled) == 0 {
		return indexes
	}

	// Calculate total description length for non-bundled skills.
	totalLen := 0
	for _, idx := range nonBundled {
		totalLen += len(idx.Description)
	}

	// If within budget, return as-is.
	if totalLen <= budget.MaxChars {
		return indexes
	}

	// Allocate budget proportionally by original description length.
	// First pass: compute target lengths.
	targetLens := make([]int, len(nonBundled))
	remainingBudget := budget.MaxChars
	for i, idx := range nonBundled {
		// Proportional allocation: (descLen / totalLen) * budget.
		target := len(idx.Description) * budget.MaxChars / totalLen
		// Ensure we don't exceed original length.
		if target > len(idx.Description) {
			target = len(idx.Description)
		}
		// Reserve 3 chars for "..." if truncation will occur.
		if target < len(idx.Description) {
			target -= 3
		}
		// Minimum meaningful description: 10 chars + "...".
		if target < 10 {
			target = 10
		}
		targetLens[i] = target
		remainingBudget -= target + 3 // target + "..."
	}

	// Redistribute any remaining budget to skills that can use it.
	for remainingBudget > 0 {
		redistributed := false
		for i, idx := range nonBundled {
			currentMax := targetLens[i] + 3 // current allocation including "..."
			if currentMax < len(idx.Description) {
				// Can grow by 1 char.
				targetLens[i]++
				remainingBudget--
				redistributed = true
				if remainingBudget == 0 {
					break
				}
			}
		}
		if !redistributed {
			break // no more room to redistribute
		}
	}

	// Build result with truncated descriptions.
	result := make([]SkillIndex, 0, len(indexes))
	result = append(result, bundled...)

	for i, idx := range nonBundled {
		truncated := idx
		if len(truncated.Description) > targetLens[i]+3 {
			truncated.Description = truncated.Description[:targetLens[i]] + "..."
		}
		result = append(result, truncated)
	}

	return result
}

// ExecutionMode determines how a skill is executed.
type ExecutionMode string

const (
	// ExecutionModeInline runs the skill in the main agent context (default).
	ExecutionModeInline ExecutionMode = "inline"
	// ExecutionModeFork runs the skill in an isolated sub-agent.
	ExecutionModeFork ExecutionMode = "fork"
)

// SubagentConfig configures a sub-agent spawn.
type SubagentConfig struct {
	Name          string
	MaxTurns      int
	MaxDepth      int
	Depth         int
	Model         string
	SystemPrompt  string
	Tools         []string
	Isolation     string
	ContextBudget int
	InheritSkills bool
	ExtraSkills   []string
	DisableSkills []string
}

// SubagentResult is the output of a sub-agent execution.
type SubagentResult struct {
	Name         string
	Output       string
	Error        error
	InputTokens  int
	OutputTokens int
	ToolsUsed    []string
}

// sourceBudgetPriority returns a priority value for context budget allocation.
// Higher values mean higher priority (included first). This is different from
// hook dispatch priority where lower = higher.
func sourceBudgetPriority(src Source) int {
	switch src {
	case SourceInline:
		return 50
	case SourceBuiltin:
		return 40
	case SourceUser:
		return 30
	case SourceProject:
		return 20
	case SourceConfigured:
		return 15
	case SourceMCP:
		return 10
	default:
		return 0
	}
}
