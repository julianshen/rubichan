package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// toolCallRegex matches <tool_use> XML blocks. The input content is captured
// lazily up to the first </input> that is immediately followed (modulo
// whitespace) by </tool_use>. This handles both multiple sequential blocks
// and JSON payloads that don't contain the exact sequence "</input>\s*</tool_use>".
var toolCallRegex = regexp.MustCompile(
	`(?s)<tool_use>\s*<name>\s*(\S+?)\s*</name>\s*<input>\s*(.*?)</input>\s*</tool_use>`,
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

		params, ok := extractParams(tool.InputSchema)
		if ok && len(params) > 0 {
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
		} else if !ok && len(tool.InputSchema) > 0 {
			// Schema exists but could not be parsed into parameters.
			// Render the raw schema so the model still has some guidance.
			sb.WriteString("\n**Input schema:** `")
			sb.Write(tool.InputSchema)
			sb.WriteString("`\n")
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
// Returns (nil, true) for nil/empty schemas, (params, true) on success,
// and (nil, false) when the schema cannot be parsed.
func extractParams(schema json.RawMessage) ([]paramInfo, bool) {
	if len(schema) == 0 {
		return nil, true
	}

	var s struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, false
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

	sortParamInfos(params)

	return params, true
}

// sortParamInfos sorts params alphabetically by name (required first, then optional).
func sortParamInfos(params []paramInfo) {
	for i := 1; i < len(params); i++ {
		for j := i; j > 0; j-- {
			a, b := params[j-1], params[j]
			if (!a.required && b.required) ||
				(a.required == b.required && a.name > b.name) {
				params[j-1], params[j] = params[j], params[j-1]
			} else {
				break
			}
		}
	}
}

// ParseTextToolCalls parses <tool_use> XML blocks from a model's text response
// and returns them as ToolUseBlocks with auto-generated IDs. It uses regex
// rather than an XML parser because the block format is fixed and well-known.
// Malformed JSON inside <input> is still captured so the agent can report the
// error rather than silently dropping the call.
//
// Returns an empty (non-nil) slice when no tool calls are found.
func ParseTextToolCalls(text string) []provider.ToolUseBlock {
	matches := toolCallRegex.FindAllStringSubmatch(text, -1)
	calls := make([]provider.ToolUseBlock, 0, len(matches))
	for i, m := range matches {
		name := strings.TrimSpace(m[1])
		rawInput := strings.TrimSpace(m[2])
		if rawInput == "" {
			rawInput = "{}"
		}
		calls = append(calls, provider.ToolUseBlock{
			ID:    fmt.Sprintf("text_call_%d", i+1),
			Name:  name,
			Input: json.RawMessage(rawInput),
		})
	}
	return calls
}

// StripToolUseXML removes <tool_use>...</tool_use> blocks from text, returning
// the remaining content. Used to clean up conversation history so models don't
// see their own XML format echoed back.
func StripToolUseXML(text string) string {
	return toolCallRegex.ReplaceAllString(text, "")
}
