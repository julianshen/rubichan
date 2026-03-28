package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// TextToolCall represents a parsed tool call from a model's text response.
type TextToolCall struct {
	Name  string
	Input json.RawMessage
}

// toolCallRegex matches <tool_use> XML blocks with dotall semantics.
var toolCallRegex = regexp.MustCompile(
	`(?s)<tool_use>\s*<name>\s*(\S+?)\s*</name>\s*<input>\s*(.*?)\s*</input>\s*</tool_use>`,
)

// RenderToolsAsText renders tool definitions as a human-readable system prompt
// section with XML-formatted usage instructions for models that don't support
// native tool_use.
func RenderToolsAsText(tools []provider.ToolDef) string {
	var sb strings.Builder

	sb.WriteString("## Tools\n\n")
	sb.WriteString("You have access to the following tools. To call a tool, respond with a tool_use XML block:\n\n")
	sb.WriteString("```\n")
	sb.WriteString("<tool_use>\n")
	sb.WriteString("<name>TOOL_NAME</name>\n")
	sb.WriteString("<input>{\"param\": \"value\"}</input>\n")
	sb.WriteString("</tool_use>\n")
	sb.WriteString("```\n\n")
	sb.WriteString("You may call multiple tools in a single response by including multiple <tool_use> blocks.\n")

	for _, tool := range tools {
		sb.WriteString("\n### ")
		sb.WriteString(tool.Name)
		sb.WriteString("\n\n")
		if tool.Description != "" {
			sb.WriteString(tool.Description)
			sb.WriteString("\n")
		}

		params := extractParams(tool.InputSchema)
		if len(params) > 0 {
			sb.WriteString("\n**Parameters:**\n")
			for _, p := range params {
				line := fmt.Sprintf("- `%s` (%s)", p.name, p.typ)
				if p.required {
					line += " **(required)**"
				}
				if p.description != "" {
					line += ": " + p.description
				}
				sb.WriteString(line)
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// paramInfo holds extracted information about a single JSON Schema parameter.
type paramInfo struct {
	name        string
	typ         string
	description string
	required    bool
}

// extractParams extracts parameter info from a JSON Schema object.
// Returns an empty slice if the schema is nil or unparseable.
func extractParams(schema json.RawMessage) []paramInfo {
	if schema == nil {
		return nil
	}

	var s struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil
	}

	requiredSet := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		requiredSet[r] = true
	}

	params := make([]paramInfo, 0, len(s.Properties))
	for name, prop := range s.Properties {
		params = append(params, paramInfo{
			name:        name,
			typ:         prop.Type,
			description: prop.Description,
			required:    requiredSet[name],
		})
	}

	// Sort deterministically by name so output is stable.
	sortParamInfos(params)

	return params
}

// sortParamInfos sorts params alphabetically by name (required first, then optional).
func sortParamInfos(params []paramInfo) {
	// Simple insertion sort — param counts are tiny.
	for i := 1; i < len(params); i++ {
		for j := i; j > 0; j-- {
			a, b := params[j-1], params[j]
			// Required before optional; within same required-ness, alphabetical.
			if (!a.required && b.required) ||
				(a.required == b.required && a.name > b.name) {
				params[j-1], params[j] = params[j], params[j-1]
			} else {
				break
			}
		}
	}
}

// ParseTextToolCalls parses <tool_use> XML blocks from a model's text response.
// It uses regex rather than an XML parser because the block format is fixed and
// well-known. Malformed JSON inside <input> is still captured so the agent can
// report the error rather than silently dropping the call.
//
// Returns an empty (non-nil) slice when no tool calls are found.
func ParseTextToolCalls(text string) []TextToolCall {
	matches := toolCallRegex.FindAllStringSubmatch(text, -1)
	calls := make([]TextToolCall, 0, len(matches))
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		rawInput := strings.TrimSpace(m[2])
		calls = append(calls, TextToolCall{
			Name:  name,
			Input: json.RawMessage(rawInput),
		})
	}
	return calls
}
