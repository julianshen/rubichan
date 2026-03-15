package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// DecodeJSONLEvents reads session events from a JSONL stream.
func DecodeJSONLEvents(r io.Reader) ([]Event, error) {
	if r == nil {
		return nil, nil
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	events := make([]Event, 0)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			return nil, fmt.Errorf("decode session event line %d: %w", lineNo, err)
		}
		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan session events: %w", err)
	}
	return events, nil
}

// BuildTranscript renders a concise plain-text transcript from session events.
func BuildTranscript(events []Event) string {
	var b strings.Builder
	for _, evt := range events {
		prefix := actorPrefix(evt.Actor)
		switch evt.Type {
		case EventTypeTurnStarted:
			if evt.Turn != nil && strings.TrimSpace(evt.Turn.Prompt) != "" {
				fmt.Fprintf(&b, "User%s: %s\n", prefix, strings.TrimSpace(evt.Turn.Prompt))
			}
		case EventTypeToolCall:
			if evt.ToolCall != nil {
				if len(evt.ToolCall.Input) > 0 {
					fmt.Fprintf(&b, "Tool call%s [%s]: %s\n", prefix, evt.ToolCall.Name, strings.TrimSpace(string(evt.ToolCall.Input)))
				} else if strings.TrimSpace(evt.ToolCall.RawInput) != "" {
					fmt.Fprintf(&b, "Tool call%s [%s]: %s\n", prefix, evt.ToolCall.Name, strings.TrimSpace(evt.ToolCall.RawInput))
				} else {
					fmt.Fprintf(&b, "Tool call%s [%s]\n", prefix, evt.ToolCall.Name)
				}
			}
		case EventTypeToolResult:
			if evt.ToolResult != nil {
				content := strings.TrimSpace(evt.ToolResult.Content)
				if content == "" {
					content = "(no output)"
				}
				if evt.ToolResult.IsError {
					fmt.Fprintf(&b, "Tool result%s [%s] error: %s\n", prefix, evt.ToolResult.Name, content)
				} else {
					fmt.Fprintf(&b, "Tool result%s [%s]: %s\n", prefix, evt.ToolResult.Name, content)
				}
			}
		case EventTypeAssistantFinal:
			if evt.Assistant != nil && strings.TrimSpace(evt.Assistant.Content) != "" {
				fmt.Fprintf(&b, "Assistant%s: %s\n", prefix, strings.TrimSpace(evt.Assistant.Content))
			}
		case EventTypeSubagentDone:
			if evt.Subagent != nil {
				line := fmt.Sprintf("Subagent%s done", prefix)
				if summary := strings.TrimSpace(evt.Subagent.Summary); summary != "" {
					line += ": " + summary
				}
				b.WriteString(line + "\n")
			}
		case EventTypeVerificationSnapshot:
			if evt.Verification != nil {
				line := "Verification" + prefix
				if evt.Verification.Verdict != "" {
					line += " [" + evt.Verification.Verdict + "]"
				}
				if evt.Verification.Reason != "" {
					line += ": " + evt.Verification.Reason
				}
				b.WriteString(line + "\n")
			}
		case EventTypePlanUpdated:
			if evt.Plan != nil {
				parts := make([]string, 0, len(evt.Plan.Steps))
				for _, step := range evt.Plan.Steps {
					parts = append(parts, fmt.Sprintf("%s=%s", strings.TrimSpace(step.Step), strings.TrimSpace(step.Status)))
				}
				line := "Plan updated" + prefix
				if reason := strings.TrimSpace(evt.Plan.Reason); reason != "" {
					line += ": " + reason
				}
				if len(parts) > 0 {
					line += " | " + strings.Join(parts, ", ")
				}
				b.WriteString(line + "\n")
			}
		case EventTypeGateFailed:
			if evt.Gate != nil {
				line := "Gate failed" + prefix + " [" + strings.TrimSpace(evt.Gate.Name) + "]"
				if reason := strings.TrimSpace(evt.Gate.Reason); reason != "" {
					line += ": " + reason
				}
				b.WriteString(line + "\n")
			}
		case EventTypeCheckpointCreated, EventTypeCheckpointRestored:
			if evt.Checkpoint != nil {
				action := "Checkpoint created"
				if evt.Type == EventTypeCheckpointRestored {
					action = "Checkpoint restored"
				}
				line := action + prefix + " [" + strings.TrimSpace(evt.Checkpoint.ID) + "]"
				if reason := strings.TrimSpace(evt.Checkpoint.Reason); reason != "" {
					line += ": " + reason
				}
				b.WriteString(line + "\n")
			}
		case EventTypeCommandResult:
			if evt.Command != nil {
				line := "Command: " + strings.TrimSpace(evt.Command.Command)
				if out := strings.TrimSpace(evt.Command.Output); out != "" {
					line += " => " + out
				}
				if len(evt.Command.Activated) > 0 {
					line += " | activated=" + strings.Join(evt.Command.Activated, ",")
				}
				if len(evt.Command.Deactivated) > 0 {
					line += " | deactivated=" + strings.Join(evt.Command.Deactivated, ",")
				}
				b.WriteString(line + "\n")
			}
		case EventTypeTurnCompleted:
			if evt.Turn != nil && (evt.Turn.InputTokens > 0 || evt.Turn.OutputTokens > 0 || strings.TrimSpace(evt.Turn.DiffSummary) != "") {
				line := fmt.Sprintf("Turn completed: input_tokens=%d output_tokens=%d", evt.Turn.InputTokens, evt.Turn.OutputTokens)
				if diff := strings.TrimSpace(evt.Turn.DiffSummary); diff != "" {
					line += " | diff=" + diff
				}
				b.WriteString(line + "\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// BuildTranscriptEvent renders a single event as transcript text.
func BuildTranscriptEvent(evt Event) string {
	return BuildTranscript([]Event{evt})
}

func actorPrefix(actor *Actor) string {
	if actor == nil || strings.TrimSpace(actor.Name) == "" {
		return ""
	}
	return fmt.Sprintf(" (%s)", strings.TrimSpace(actor.Name))
}

// ReplaySummary captures a concise summary derived from a session event stream.
type ReplaySummary struct {
	EventCount              int      `json:"event_count"`
	Turns                   int      `json:"turns"`
	ToolCalls               int      `json:"tool_calls"`
	ToolResults             int      `json:"tool_results"`
	Commands                int      `json:"commands"`
	SubagentsCompleted      int      `json:"subagents_completed"`
	PlanUpdates             int      `json:"plan_updates"`
	GateFailures            int      `json:"gate_failures"`
	CheckpointsCreated      int      `json:"checkpoints_created"`
	CheckpointsRestored     int      `json:"checkpoints_restored"`
	Actors                  []string `json:"actors,omitempty"`
	LastAssistantFinal      string   `json:"last_assistant_final,omitempty"`
	LastVerificationGate    string   `json:"last_verification_gate,omitempty"`
	LastVerificationVerdict string   `json:"last_verification_verdict,omitempty"`
	LastVerificationReason  string   `json:"last_verification_reason,omitempty"`
	LastGateFailure         string   `json:"last_gate_failure,omitempty"`
}

// BuildSummary derives a concise summary from session events.
func BuildSummary(events []Event) ReplaySummary {
	summary := ReplaySummary{EventCount: len(events)}
	actors := map[string]struct{}{}
	state := NewState()
	for _, evt := range events {
		if evt.Actor != nil && strings.TrimSpace(evt.Actor.Name) != "" {
			actors[strings.TrimSpace(evt.Actor.Name)] = struct{}{}
		}
		switch evt.Type {
		case EventTypeTurnStarted:
			summary.Turns++
			if evt.Turn != nil {
				state.ResetForPrompt(evt.Turn.Prompt)
			}
		case EventTypeToolCall:
			summary.ToolCalls++
			if evt.ToolCall != nil {
				state.ApplyEvent(agentsdk.TurnEvent{
					Type: "tool_call",
					ToolCall: &agentsdk.ToolCallEvent{
						ID:    evt.ToolCall.ID,
						Name:  evt.ToolCall.Name,
						Input: evt.ToolCall.Input,
					},
				})
			}
		case EventTypeToolResult:
			summary.ToolResults++
			if evt.ToolResult != nil {
				state.ApplyEvent(agentsdk.TurnEvent{
					Type: "tool_result",
					ToolResult: &agentsdk.ToolResultEvent{
						ID:             evt.ToolResult.ID,
						Name:           evt.ToolResult.Name,
						Content:        evt.ToolResult.Content,
						DisplayContent: evt.ToolResult.Content,
						IsError:        evt.ToolResult.IsError,
					},
				})
			}
		case EventTypeCommandResult:
			summary.Commands++
		case EventTypeSubagentDone:
			summary.SubagentsCompleted++
		case EventTypePlanUpdated:
			summary.PlanUpdates++
		case EventTypeGateFailed:
			summary.GateFailures++
			if evt.Gate != nil {
				summary.LastGateFailure = strings.TrimSpace(evt.Gate.Name)
				if reason := strings.TrimSpace(evt.Gate.Reason); reason != "" {
					summary.LastGateFailure += ": " + reason
				}
			}
		case EventTypeCheckpointCreated:
			summary.CheckpointsCreated++
		case EventTypeCheckpointRestored:
			summary.CheckpointsRestored++
		case EventTypeAssistantFinal:
			if evt.Assistant != nil {
				summary.LastAssistantFinal = strings.TrimSpace(evt.Assistant.Content)
			}
		case EventTypeVerificationSnapshot:
			if evt.Verification != nil {
				summary.LastVerificationGate = ParseVerificationGate(evt.Verification.Snapshot)
				summary.LastVerificationVerdict = strings.TrimSpace(evt.Verification.Verdict)
				summary.LastVerificationReason = strings.TrimSpace(evt.Verification.Reason)
				if summary.LastVerificationGate == "" {
					summary.LastVerificationGate = inferGateFromVerdict(summary.LastVerificationVerdict)
				}
			}
		}
	}
	if summary.LastVerificationVerdict == "" {
		gate, verdict, reason := parseVerificationSnapshot(state.BuildVerificationSnapshot())
		summary.LastVerificationGate = gate
		summary.LastVerificationVerdict = verdict
		summary.LastVerificationReason = reason
	}
	if summary.LastVerificationGate == "" {
		summary.LastVerificationGate = inferGateFromVerdict(summary.LastVerificationVerdict)
	}
	for name := range actors {
		summary.Actors = append(summary.Actors, name)
	}
	return summary
}

func parseVerificationSnapshot(snapshot string) (string, string, string) {
	if strings.TrimSpace(snapshot) == "" {
		return "", "", ""
	}
	var gate string
	var verdict string
	var reason string
	for _, line := range strings.Split(snapshot, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "- gate:"):
			gate = strings.TrimSpace(strings.TrimPrefix(line, "- gate:"))
		case strings.HasPrefix(line, "- verdict:"):
			verdict = strings.TrimSpace(strings.TrimPrefix(line, "- verdict:"))
		case strings.HasPrefix(line, "- reason:"):
			reason = strings.TrimSpace(strings.TrimPrefix(line, "- reason:"))
		}
	}
	return gate, verdict, reason
}

func inferGateFromVerdict(verdict string) string {
	switch strings.TrimSpace(verdict) {
	case "passed":
		return "pass"
	case "passed_with_warnings":
		return "soft_fail"
	case "failed":
		return "hard_fail"
	default:
		return ""
	}
}

// BuildSummaryText renders a summary as plain text.
func BuildSummaryText(summary ReplaySummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Events: %d\n", summary.EventCount)
	fmt.Fprintf(&b, "Turns: %d\n", summary.Turns)
	fmt.Fprintf(&b, "Tool calls: %d\n", summary.ToolCalls)
	fmt.Fprintf(&b, "Tool results: %d\n", summary.ToolResults)
	fmt.Fprintf(&b, "Commands: %d\n", summary.Commands)
	fmt.Fprintf(&b, "Subagents completed: %d\n", summary.SubagentsCompleted)
	fmt.Fprintf(&b, "Plan updates: %d\n", summary.PlanUpdates)
	fmt.Fprintf(&b, "Gate failures: %d\n", summary.GateFailures)
	fmt.Fprintf(&b, "Checkpoints created: %d\n", summary.CheckpointsCreated)
	fmt.Fprintf(&b, "Checkpoints restored: %d\n", summary.CheckpointsRestored)
	if len(summary.Actors) > 0 {
		fmt.Fprintf(&b, "Actors: %s\n", strings.Join(summary.Actors, ", "))
	}
	if summary.LastGateFailure != "" {
		fmt.Fprintf(&b, "Last gate failure: %s\n", summary.LastGateFailure)
	}
	if summary.LastVerificationGate != "" {
		fmt.Fprintf(&b, "Last gate: %s\n", summary.LastVerificationGate)
	}
	if summary.LastVerificationVerdict != "" {
		fmt.Fprintf(&b, "Last verification: %s", summary.LastVerificationVerdict)
		if summary.LastVerificationReason != "" {
			fmt.Fprintf(&b, " (%s)", summary.LastVerificationReason)
		}
		b.WriteString("\n")
	}
	if summary.LastAssistantFinal != "" {
		fmt.Fprintf(&b, "Last assistant final: %s\n", summary.LastAssistantFinal)
	}
	return strings.TrimSpace(b.String())
}

// MarshalEventsJSON marshals events as pretty JSON.
func MarshalEventsJSON(events []Event) ([]byte, error) {
	return json.MarshalIndent(events, "", "  ")
}

// MarshalSummaryJSON marshals a replay summary as pretty JSON.
func MarshalSummaryJSON(summary ReplaySummary) ([]byte, error) {
	return json.MarshalIndent(summary, "", "  ")
}
