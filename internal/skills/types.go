package skills

import (
	"context"
	"fmt"

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
