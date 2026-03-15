package session

import (
	"encoding/json"
	"io"
	"log"
	"strings"
	"time"
)

// EventType identifies a structured session event.
type EventType string

const (
	EventTypeTurnStarted          EventType = "turn_started"
	EventTypeToolCall             EventType = "tool_call"
	EventTypeToolResult           EventType = "tool_result"
	EventTypeAssistantFinal       EventType = "assistant_final"
	EventTypeSubagentDone         EventType = "subagent_done"
	EventTypeTurnCompleted        EventType = "turn_completed"
	EventTypeCommandResult        EventType = "command_result"
	EventTypeVerificationSnapshot EventType = "verification_snapshot"
	EventTypePlanUpdated          EventType = "plan_updated"
	EventTypeGateFailed           EventType = "gate_failed"
	EventTypeCheckpointCreated    EventType = "checkpoint_created"
	EventTypeCheckpointRestored   EventType = "checkpoint_restored"
)

// Event is a UI-agnostic session event suitable for logging or external sinks.
type Event struct {
	Timestamp    time.Time                  `json:"timestamp"`
	Type         EventType                  `json:"type"`
	Actor        *Actor                     `json:"actor,omitempty"`
	Turn         *TurnEvent                 `json:"turn,omitempty"`
	ToolCall     *ToolCallLogEvent          `json:"tool_call,omitempty"`
	ToolResult   *ToolResultLogEvent        `json:"tool_result,omitempty"`
	Assistant    *AssistantEvent            `json:"assistant,omitempty"`
	Subagent     *SubagentEvent             `json:"subagent,omitempty"`
	Command      *CommandResultEvent        `json:"command,omitempty"`
	Verification *VerificationSnapshotEvent `json:"verification,omitempty"`
	Plan         *PlanUpdatedEvent          `json:"plan,omitempty"`
	Gate         *GateFailedEvent           `json:"gate,omitempty"`
	Checkpoint   *CheckpointEvent           `json:"checkpoint,omitempty"`
}

// Actor identifies which agent produced an event.
type Actor struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Kind string `json:"kind,omitempty"`
}

// TurnEvent captures turn-level lifecycle events.
type TurnEvent struct {
	Prompt       string `json:"prompt,omitempty"`
	Model        string `json:"model,omitempty"`
	DiffSummary  string `json:"diff_summary,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
}

// ToolCallLogEvent captures a tool call as a structured event.
type ToolCallLogEvent struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input,omitempty"`
	RawInput string          `json:"raw_input,omitempty"`
}

// ToolResultLogEvent captures a tool result as a structured event.
type ToolResultLogEvent struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Content string `json:"content,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

// AssistantEvent captures the final visible assistant output for a turn.
type AssistantEvent struct {
	Content string `json:"content,omitempty"`
}

// SubagentEvent captures completion of a background agent.
type SubagentEvent struct {
	Summary string `json:"summary,omitempty"`
	Output  string `json:"output,omitempty"`
}

// CommandResultEvent captures the observable output of a slash command.
type CommandResultEvent struct {
	Command     string   `json:"command"`
	Output      string   `json:"output,omitempty"`
	Activated   []string `json:"activated,omitempty"`
	Deactivated []string `json:"deactivated,omitempty"`
}

// VerificationSnapshotEvent captures a derived verification snapshot.
type VerificationSnapshotEvent struct {
	Snapshot string `json:"snapshot"`
	Verdict  string `json:"verdict,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// PlanStepEvent captures one plan step and its reducer status.
type PlanStepEvent struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

// PlanUpdatedEvent captures a full plan state update.
type PlanUpdatedEvent struct {
	Reason string          `json:"reason,omitempty"`
	Steps  []PlanStepEvent `json:"steps,omitempty"`
}

// GateFailedEvent captures completion gate failures.
type GateFailedEvent struct {
	Name   string `json:"name"`
	Reason string `json:"reason,omitempty"`
}

// CheckpointEvent captures checkpoint lifecycle metadata.
type CheckpointEvent struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

// EventSink receives structured session events.
type EventSink interface {
	Emit(Event)
}

// SinkFunc adapts a function to EventSink.
type SinkFunc func(Event)

// Emit implements EventSink.
func (f SinkFunc) Emit(evt Event) {
	if f != nil {
		f(evt)
	}
}

// MultiSink broadcasts events to multiple sinks.
type MultiSink []EventSink

// Emit implements EventSink.
func (s MultiSink) Emit(evt Event) {
	for _, sink := range s {
		if sink != nil {
			sink.Emit(evt)
		}
	}
}

// NewLogSink writes one JSON event per log line.
func NewLogSink(logger func(string, ...any)) EventSink {
	if logger == nil {
		logger = log.Printf
	}
	return SinkFunc(func(evt Event) {
		if evt.Timestamp.IsZero() {
			evt.Timestamp = time.Now().UTC()
		}
		payload, err := json.Marshal(evt)
		if err != nil {
			logger("session event marshal error: %v", err)
			return
		}
		logger("session event: %s", payload)
	})
}

// NewJSONLSink writes one JSON event per line to the provided writer.
func NewJSONLSink(w io.Writer) EventSink {
	if w == nil {
		return SinkFunc(func(Event) {})
	}
	enc := json.NewEncoder(w)
	return SinkFunc(func(evt Event) {
		if evt.Timestamp.IsZero() {
			evt.Timestamp = time.Now().UTC()
		}
		if err := enc.Encode(evt); err != nil {
			log.Printf("session event encode error: type=%s timestamp=%s err=%v", evt.Type, evt.Timestamp.UTC().Format(time.RFC3339Nano), err)
		}
	})
}

// NewCommandResultEvent constructs a command result event.
func NewCommandResultEvent(command, output string, activated, deactivated []string) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeCommandResult,
		Command: &CommandResultEvent{
			Command:     strings.TrimSpace(command),
			Output:      strings.TrimSpace(output),
			Activated:   append([]string(nil), activated...),
			Deactivated: append([]string(nil), deactivated...),
		},
	}
}

// WithActor annotates an event with the producing actor identity.
func (e Event) WithActor(actor Actor) Event {
	if e.Actor == nil {
		e.Actor = &Actor{}
	}
	if strings.TrimSpace(e.Actor.ID) == "" {
		e.Actor.ID = strings.TrimSpace(actor.ID)
	}
	if strings.TrimSpace(e.Actor.Name) == "" {
		e.Actor.Name = strings.TrimSpace(actor.Name)
	}
	if strings.TrimSpace(e.Actor.Kind) == "" {
		e.Actor.Kind = strings.TrimSpace(actor.Kind)
	}
	return e
}

// PrimaryActor identifies the main interactive agent.
func PrimaryActor() Actor {
	return Actor{Name: "primary", Kind: "agent"}
}

// NewTurnStartedEvent constructs a turn_started event.
func NewTurnStartedEvent(prompt, model string) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeTurnStarted,
		Turn: &TurnEvent{
			Prompt: strings.TrimSpace(prompt),
			Model:  strings.TrimSpace(model),
		},
	}
}

// NewToolCallEvent constructs a tool_call event.
func NewToolCallEvent(id, name string, input json.RawMessage) Event {
	validInput, rawInput := normalizeToolInput(input)
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeToolCall,
		ToolCall: &ToolCallLogEvent{
			ID:       id,
			Name:     name,
			Input:    validInput,
			RawInput: rawInput,
		},
	}
}

// NewToolResultEvent constructs a tool_result event.
func NewToolResultEvent(id, name, content string, isError bool) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeToolResult,
		ToolResult: &ToolResultLogEvent{
			ID:      id,
			Name:    name,
			Content: strings.TrimSpace(content),
			IsError: isError,
		},
	}
}

// NewAssistantFinalEvent constructs an assistant_final event.
func NewAssistantFinalEvent(content string) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeAssistantFinal,
		Assistant: &AssistantEvent{
			Content: strings.TrimSpace(content),
		},
	}
}

// NewSubagentDoneEvent constructs a subagent_done event.
func NewSubagentDoneEvent(name, summary, output string) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeSubagentDone,
		Actor: &Actor{
			Name: strings.TrimSpace(name),
			Kind: "subagent",
		},
		Subagent: &SubagentEvent{
			Summary: strings.TrimSpace(summary),
			Output:  strings.TrimSpace(output),
		},
	}
}

// NewTurnCompletedEvent constructs a turn_completed event.
func NewTurnCompletedEvent(diffSummary string, inputTokens, outputTokens int) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeTurnCompleted,
		Turn: &TurnEvent{
			DiffSummary:  strings.TrimSpace(diffSummary),
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		},
	}
}

// NewVerificationSnapshotEvent constructs a verification snapshot event.
func NewVerificationSnapshotEvent(snapshot string) Event {
	verdict, reason := ParseVerificationSnapshot(snapshot)
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeVerificationSnapshot,
		Verification: &VerificationSnapshotEvent{
			Snapshot: strings.TrimSpace(snapshot),
			Verdict:  verdict,
			Reason:   reason,
		},
	}
}

// NewPlanUpdatedEvent constructs a plan_updated event.
func NewPlanUpdatedEvent(reason string, items []PlanItem) Event {
	steps := make([]PlanStepEvent, 0, len(items))
	for _, item := range items {
		steps = append(steps, PlanStepEvent{
			Step:   strings.TrimSpace(item.Step),
			Status: strings.TrimSpace(string(item.Status)),
		})
	}
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypePlanUpdated,
		Plan: &PlanUpdatedEvent{
			Reason: strings.TrimSpace(reason),
			Steps:  steps,
		},
	}
}

// NewGateFailedEvent constructs a gate_failed event.
func NewGateFailedEvent(name, reason string) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeGateFailed,
		Gate: &GateFailedEvent{
			Name:   strings.TrimSpace(name),
			Reason: strings.TrimSpace(reason),
		},
	}
}

// NewCheckpointCreatedEvent constructs a checkpoint_created event.
func NewCheckpointCreatedEvent(id, reason string) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeCheckpointCreated,
		Checkpoint: &CheckpointEvent{
			ID:     strings.TrimSpace(id),
			Reason: strings.TrimSpace(reason),
		},
	}
}

// NewCheckpointRestoredEvent constructs a checkpoint_restored event.
func NewCheckpointRestoredEvent(id, reason string) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      EventTypeCheckpointRestored,
		Checkpoint: &CheckpointEvent{
			ID:     strings.TrimSpace(id),
			Reason: strings.TrimSpace(reason),
		},
	}
}

// ParseVerificationSnapshot extracts verdict and reason from a snapshot string.
func ParseVerificationSnapshot(snapshot string) (verdict string, reason string) {
	for _, line := range strings.Split(snapshot, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "- verdict:"):
			verdict = strings.TrimSpace(strings.TrimPrefix(line, "- verdict:"))
		case strings.HasPrefix(line, "- reason:"):
			reason = strings.TrimSpace(strings.TrimPrefix(line, "- reason:"))
		}
	}
	return verdict, reason
}

// ParseVerificationGate extracts gate level from a snapshot string.
func ParseVerificationGate(snapshot string) (gate string) {
	for _, line := range strings.Split(snapshot, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- gate:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "- gate:"))
		}
	}
	return ""
}

func normalizeToolInput(input json.RawMessage) (json.RawMessage, string) {
	trimmed := strings.TrimSpace(string(input))
	if trimmed == "" {
		return nil, ""
	}
	if json.Valid([]byte(trimmed)) {
		return append(json.RawMessage(nil), []byte(trimmed)...), ""
	}
	return nil, trimmed
}
