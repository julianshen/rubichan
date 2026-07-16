package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ToolLookup abstracts a tool registry for execution. *Registry satisfies
// it; so do the internal registries that already expose Get.
type ToolLookup interface {
	Get(name string) (Tool, bool)
}

// ToolExecOutcome is the result of dispatching one tool call: the content
// for the conversation, optional display-oriented content for UIs, and the
// error flag. Execution failures are folded into an error outcome rather
// than returned as a Go error, so a misbehaving tool never aborts the turn.
type ToolExecOutcome struct {
	Content        string
	DisplayContent string
	IsError        bool
}

// ExecuteTool dispatches a single tool call against a registry: name
// lookup (with a "did you mean" suggestion when the lookup also implements
// ToolNamer), streaming-aware execution, and error wrapping. It is the
// shared execution core used by the SDK loop and internal/toolexec.
//
// When emit is non-nil and the tool implements StreamingTool, execution
// streams progress events through emit; otherwise plain Execute runs.
func ExecuteTool(ctx context.Context, lookup ToolLookup, name string, input json.RawMessage, emit ToolEventEmitter) ToolExecOutcome {
	tool, ok := lookup.Get(name)
	if !ok {
		msg := fmt.Sprintf("unknown tool: %s", name)
		// Try to suggest the closest matching tool name.
		if namer, ok := lookup.(ToolNamer); ok {
			names := namer.Names()
			sort.Strings(names)
			if suggestion := SuggestToolName(name, names); suggestion != "" {
				msg = fmt.Sprintf("unknown tool: %s. Did you mean %q? Available tools: %s",
					name, suggestion, strings.Join(names, ", "))
			}
		}
		return ToolExecOutcome{Content: msg, IsError: true}
	}

	var (
		tr  ToolResult
		err error
	)
	if st, ok := tool.(StreamingTool); ok && emit != nil {
		tr, err = st.ExecuteStream(ctx, input, emit)
	} else {
		tr, err = tool.Execute(ctx, input)
	}
	if err != nil {
		return ToolExecOutcome{
			Content: fmt.Sprintf("tool execution error: %s", err.Error()),
			IsError: true,
		}
	}

	return ToolExecOutcome{
		Content:        tr.Content,
		DisplayContent: tr.DisplayContent,
		IsError:        tr.IsError,
	}
}

// MakeToolCallEvent builds a tool_call TurnEvent from a ToolUseBlock.
// All "about to run this tool" emission sites go through here so the
// wire shape stays uniform.
func MakeToolCallEvent(tc ToolUseBlock) TurnEvent {
	return TurnEvent{
		Type: "tool_call",
		ToolCall: &ToolCallEvent{
			ID:    tc.ID,
			Name:  tc.Name,
			Input: tc.Input,
		},
	}
}

// MakeToolResultEvent builds a tool_result TurnEvent.
func MakeToolResultEvent(id, name, content, displayContent string, isError bool) TurnEvent {
	return TurnEvent{
		Type: "tool_result",
		ToolResult: &ToolResultEvent{
			ID:             id,
			Name:           name,
			Content:        content,
			DisplayContent: displayContent,
			IsError:        isError,
		},
	}
}

// MakeToolProgressEmitter adapts a TurnEvent sink into the ToolEventEmitter
// a StreamingTool expects, stamping each progress event with the tool call's
// identity.
func MakeToolProgressEmitter(id, name string, sink func(TurnEvent)) ToolEventEmitter {
	return func(ev ToolEvent) {
		sink(TurnEvent{
			Type: "tool_progress",
			ToolProgress: &ToolProgressEvent{
				ID:      id,
				Name:    name,
				Stage:   ev.Stage,
				Content: ev.Content,
				IsError: ev.IsError,
			},
		})
	}
}
