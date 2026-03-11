package agent

import (
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Type aliases — all existing code using agent.TurnEvent etc. compiles unchanged.
// Types are canonical in pkg/agentsdk/; these aliases keep internal/ transparent.

// --- Events ---

// TurnEvent represents a streaming event emitted during an agent turn.
type TurnEvent = agentsdk.TurnEvent

// ToolCallEvent contains details about a tool being called.
type ToolCallEvent = agentsdk.ToolCallEvent

// ToolResultEvent contains details about a tool execution result.
type ToolResultEvent = agentsdk.ToolResultEvent

// ToolProgressEvent contains a streaming progress chunk from a tool execution.
type ToolProgressEvent = agentsdk.ToolProgressEvent

// --- Approval ---

// ApprovalFunc is called before executing a tool to get user approval.
type ApprovalFunc = agentsdk.ApprovalFunc

// AutoApproveChecker tests if a tool would be auto-approved without blocking.
type AutoApproveChecker = agentsdk.AutoApproveChecker

// AlwaysAutoApprove is an AutoApproveChecker that approves all tools.
type AlwaysAutoApprove = agentsdk.AlwaysAutoApprove

// ToolParallelPolicy determines whether a tool call may be executed in parallel.
type ToolParallelPolicy = agentsdk.ToolParallelPolicy

// AllowAllParallel is a ToolParallelPolicy that permits all tools to run in parallel.
type AllowAllParallel = agentsdk.AllowAllParallel

// ApprovalResult represents the approval decision for a tool call.
type ApprovalResult = agentsdk.ApprovalResult

// Re-export approval result constants.
const (
	ApprovalRequired  = agentsdk.ApprovalRequired
	AutoApproved      = agentsdk.AutoApproved
	TrustRuleApproved = agentsdk.TrustRuleApproved
	AutoDenied        = agentsdk.AutoDenied
)

// ApprovalChecker determines the approval status of a tool call.
type ApprovalChecker = agentsdk.ApprovalChecker

// CompositeApprovalChecker chains multiple ApprovalCheckers.
type CompositeApprovalChecker = agentsdk.CompositeApprovalChecker

// NewCompositeApprovalChecker creates a checker that evaluates each checker in order.
var NewCompositeApprovalChecker = agentsdk.NewCompositeApprovalChecker

// --- Compaction ---

// CompactionStrategy defines a strategy for reducing conversation size.
type CompactionStrategy = agentsdk.CompactionStrategy

// ContextBudget tracks token usage by component category.
type ContextBudget = agentsdk.ContextBudget

// CompactResult reports what happened during a compaction.
type CompactResult = agentsdk.CompactResult

// --- Summarizer ---

// Summarizer condenses a sequence of messages into a short text summary.
type Summarizer = agentsdk.Summarizer

// --- Memory ---

// MemoryStore is the persistence interface for cross-session memories.
type MemoryStore = agentsdk.MemoryStore

// MemoryEntry represents a single cross-session memory.
type MemoryEntry = agentsdk.MemoryEntry

// --- Subagent ---

// SubagentConfig defines how a child agent is created.
type SubagentConfig = agentsdk.SubagentConfig

// SubagentResult is returned when a child agent completes.
type SubagentResult = agentsdk.SubagentResult

// SubagentSpawner creates and runs child agents.
type SubagentSpawner = agentsdk.SubagentSpawner

// --- Logger ---

// Logger provides structured logging for the agent core.
type Logger = agentsdk.Logger
