package agentsdk

import "fmt"

// Compile-time assertion that TurnExitReason satisfies fmt.Stringer.
// If someone accidentally changes the receiver signature of String(),
// this line will fail the build.
var _ fmt.Stringer = TurnExitReason(0)

// TurnExitReason enumerates why a turn stopped. Every "done" TurnEvent
// carries exactly one of these. New reasons must be added here and nowhere
// else; callers switch on this value.
type TurnExitReason int

const (
	// ExitUnknown is the zero value. Emitting a done event with this reason
	// is a bug — it means a code path forgot to set a reason.
	ExitUnknown TurnExitReason = iota

	// ExitCompleted: the model returned no tool calls and no error.
	ExitCompleted

	// ExitMaxTurns: the loop hit its maxTurns ceiling.
	ExitMaxTurns

	// ExitCancelled: ctx.Err() was observed (user abort, timeout, etc.).
	ExitCancelled

	// ExitProviderError: the provider returned an unrecoverable error.
	ExitProviderError

	// ExitRateLimited: the rate limiter returned an error from Wait().
	ExitRateLimited

	// ExitSkillActivationFailed: skill runtime could not evaluate triggers.
	ExitSkillActivationFailed

	// ExitTaskComplete: model invoked the task_complete tool.
	ExitTaskComplete

	// ExitNoProgress: maxRepeatedPendingToolRounds reached.
	ExitNoProgress

	// ExitEmptyResponse: model returned no text and no tool calls.
	ExitEmptyResponse

	// ExitCompactionFailed: compaction circuit breaker tripped after
	// repeated no-shrink attempts.
	ExitCompactionFailed

	// ExitProtocolViolation: orphaned tool_use blocks were detected
	// and could not be recovered. Reserved for future use — not emitted
	// today because the orphan sweeper always produces a valid
	// conversation state from non-fatal exit paths.
	ExitProtocolViolation

	// ExitPanic: a panic was recovered in Turn's deferred handler.
	ExitPanic
)

// String returns a stable lowercase identifier usable in logs and tests.
func (r TurnExitReason) String() string {
	switch r {
	case ExitCompleted:
		return "completed"
	case ExitMaxTurns:
		return "max_turns"
	case ExitCancelled:
		return "cancelled"
	case ExitProviderError:
		return "provider_error"
	case ExitRateLimited:
		return "rate_limited"
	case ExitSkillActivationFailed:
		return "skill_activation_failed"
	case ExitTaskComplete:
		return "task_complete"
	case ExitNoProgress:
		return "no_progress"
	case ExitEmptyResponse:
		return "empty_response"
	case ExitCompactionFailed:
		return "compaction_failed"
	case ExitProtocolViolation:
		return "protocol_violation"
	case ExitPanic:
		return "panic"
	default:
		return "unknown"
	}
}
