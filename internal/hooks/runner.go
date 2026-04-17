package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/skills"
)

const defaultTimeout = 30 * time.Second

// Event name constants for user-facing hook event configuration.
const (
	EventPreTool       = "pre_tool"
	EventPostTool      = "post_tool"
	EventPreEdit       = "pre_edit"
	EventPostEdit      = "post_edit"
	EventPreShell      = "pre_shell"
	EventPrePrompt     = "pre_prompt"
	EventPostResponse  = "post_response"
	EventSessionStart  = "session_start"
	EventSetup         = "setup"
	EventTaskCreated   = "task_created"
	EventTaskCompleted = "task_completed"
)

// ParseHookTimeout parses a duration string, returning defaultTimeout (30s)
// if the string is empty or unparseable.
func ParseHookTimeout(s string) time.Duration {
	if s == "" {
		return defaultTimeout
	}
	if parsed, err := time.ParseDuration(s); err == nil {
		return parsed
	}
	return defaultTimeout
}

// UserHookConfig describes a single user-configured shell hook entry.
type UserHookConfig struct {
	Event       string
	Pattern     string
	If          string
	Command     string
	Description string
	Timeout     time.Duration
	Source      string
}

// UserHookRunner registers shell hooks from AGENT.md/config into the skill runtime.
type UserHookRunner struct {
	hooks   []UserHookConfig
	workDir string
}

// NewUserHookRunner creates a UserHookRunner with the given hook configs and
// working directory for command execution.
func NewUserHookRunner(hooks []UserHookConfig, workDir string) *UserHookRunner {
	return &UserHookRunner{hooks: hooks, workDir: workDir}
}

// hookRegistrar abstracts hook registration so both Runtime and LifecycleManager
// can be used (e.g. in tests).
type hookRegistrar interface {
	RegisterHook(phase skills.HookPhase, name string, priority int, handler skills.HookHandler)
}

// lmRegistrar wraps LifecycleManager to satisfy hookRegistrar.
type lmRegistrar struct {
	lm *skills.LifecycleManager
}

func (w *lmRegistrar) RegisterHook(phase skills.HookPhase, name string, priority int, handler skills.HookHandler) {
	w.lm.Register(phase, name, priority, handler)
}

// RegisterInto registers all hooks into a skills.Runtime.
func (r *UserHookRunner) RegisterInto(rt *skills.Runtime) {
	r.registerInto(rt)
}

// RegisterIntoLM registers all hooks directly into a LifecycleManager (for testing).
func (r *UserHookRunner) RegisterIntoLM(lm *skills.LifecycleManager) {
	r.registerInto(&lmRegistrar{lm: lm})
}

func (r *UserHookRunner) registerInto(reg hookRegistrar) {
	for i, h := range r.hooks {
		phase, isPreEvent, filter := mapEventToPhase(h.Event)
		if phase == 0 {
			log.Printf("user hook: unknown event %q, skipping", h.Event)
			continue
		}

		hookCfg := h
		name := fmt.Sprintf("user-hook-%d-%s", i, h.Event)
		timeout := h.Timeout
		if timeout == 0 {
			timeout = defaultTimeout
		}

		workDir := r.workDir
		reg.RegisterHook(phase, name, skills.PriorityUserHook, func(event skills.HookEvent) (skills.HookResult, error) {
			if !filter(event, hookCfg.Pattern) {
				return skills.HookResult{}, nil
			}

			// If "if" pattern is set, check if tool_name + input matches.
			if hookCfg.If != "" && !matchesIfPattern(hookCfg.If, event.Data) {
				return skills.HookResult{}, nil // skip
			}

			cmd := expandTemplateVars(hookCfg.Command, event)

			eventCtx := event.Ctx
			if eventCtx == nil {
				eventCtx = context.Background()
			}
			ctx, cancel := context.WithTimeout(eventCtx, timeout)
			defer cancel()

			c := exec.CommandContext(ctx, "sh", "-c", cmd)
			c.Dir = workDir

			// Pipe event data as JSON to stdin when there's tool context.
			if len(event.Data) > 0 {
				hookInput := map[string]any{
					"event": hookCfg.Event,
				}
				for k, v := range event.Data {
					hookInput[k] = v
				}
				if inputJSON, err := json.Marshal(hookInput); err == nil {
					c.Stdin = bytes.NewReader(inputJSON)
				}
			}

			var stdout, stderr bytes.Buffer
			c.Stdout = &stdout
			c.Stderr = &stderr

			err := c.Run()

			if err != nil && isPreEvent {
				combined := stdout.String() + stderr.String()
				log.Printf("user hook %q blocked: %s (output: %s)", hookCfg.Description, err, strings.TrimSpace(combined))
				return skills.HookResult{Cancel: true}, nil
			}
			if err != nil {
				log.Printf("user hook %q failed (non-blocking): %s", hookCfg.Description, err)
				return skills.HookResult{}, nil
			}

			// Try to parse stdout as JSON for structured hook responses.
			output := stdout.Bytes()
			var hookResponse struct {
				Decision string         `json:"decision"`
				Modified map[string]any `json:"modified"`
				Message  string         `json:"message"`
			}
			if json.Unmarshal(output, &hookResponse) == nil {
				if hookResponse.Decision == "block" {
					return skills.HookResult{Cancel: true}, nil
				}
				if hookResponse.Modified != nil {
					return skills.HookResult{Modified: hookResponse.Modified}, nil
				}
			}

			return skills.HookResult{}, nil
		})
	}
}

// matchesIfPattern checks whether the event data matches the hook's "if"
// pattern. The pattern format is "ToolName(glob)" where the tool name is
// matched against the tool_name field and the glob is matched against the
// primary input field (command for shell, path for file). A bare glob
// without parentheses matches against the primary input of any tool.
func matchesIfPattern(ifPattern string, data map[string]any) bool {
	if ifPattern == "" {
		return true
	}

	toolName, _ := data["tool_name"].(string)
	inputStr, _ := data["input"].(string)

	var parsed map[string]any
	if inputStr != "" {
		_ = json.Unmarshal([]byte(inputStr), &parsed)
	}

	// Extract primary input field based on tool type.
	primaryInput := extractPrimaryInput(toolName, parsed)

	// Check for ToolName(pattern) format.
	if idx := strings.Index(ifPattern, "("); idx >= 0 && strings.HasSuffix(ifPattern, ")") {
		patternTool := strings.ToLower(ifPattern[:idx])
		glob := ifPattern[idx+1 : len(ifPattern)-1]

		// Map common aliases.
		actualTool := strings.ToLower(toolName)
		toolAliases := map[string]string{
			"bash": "shell", "sh": "shell",
			"file": "file", "read": "file", "write": "file",
		}
		if alias, ok := toolAliases[patternTool]; ok {
			patternTool = alias
		}
		if alias, ok := toolAliases[actualTool]; ok {
			actualTool = alias
		}

		if patternTool != actualTool {
			return false
		}
		return globMatch(glob, primaryInput)
	}

	// Bare glob — match against primary input regardless of tool.
	return globMatch(ifPattern, primaryInput)
}

// extractPrimaryInput returns the primary input field for a tool: command for
// shell, path for file.
func extractPrimaryInput(toolName string, parsed map[string]any) string {
	if parsed == nil {
		return ""
	}
	switch strings.ToLower(toolName) {
	case "shell", "bash":
		if cmd, ok := parsed["command"].(string); ok {
			return cmd
		}
	case "file":
		if p, ok := parsed["path"].(string); ok {
			return p
		}
	}
	// Fallback: try "command", then "path", then first string value.
	if cmd, ok := parsed["command"].(string); ok {
		return cmd
	}
	if p, ok := parsed["path"].(string); ok {
		return p
	}
	return ""
}

// globMatch matches a pattern against a value. Uses filepath.Match for
// standard globs (e.g., "*.go"), plus prefix matching when the pattern
// ends with "*" (e.g., "git *" matches "git status").
func globMatch(pattern, value string) bool {
	if pattern == "" {
		return true
	}
	if matched, err := filepath.Match(pattern, value); err == nil && matched {
		return true
	}
	if matched, err := filepath.Match(pattern, filepath.Base(value)); err == nil && matched {
		return true
	}
	// Prefix matching: trailing "*" matches any continuation.
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, pattern[:len(pattern)-1])
	}
	return false
}

// mapEventToPhase converts a user-facing event name to a HookPhase, a flag
// indicating whether failures should cancel the operation, and a filter
// function for narrowing which tool calls the hook applies to.
func mapEventToPhase(event string) (skills.HookPhase, bool, func(skills.HookEvent, string) bool) {
	noFilter := func(_ skills.HookEvent, _ string) bool { return true }

	switch event {
	case EventPreTool:
		return skills.HookOnBeforeToolCall, true, noFilter
	case EventPostTool:
		return skills.HookOnAfterToolResult, false, noFilter
	case EventPreEdit:
		return skills.HookOnBeforeToolCall, true, filterFileWritePatch
	case EventPostEdit:
		return skills.HookOnAfterToolResult, false, filterFileWritePatch
	case EventPreShell:
		return skills.HookOnBeforeToolCall, true, filterShellTool
	case EventPrePrompt:
		return skills.HookOnBeforePromptBuild, false, noFilter
	case EventPostResponse:
		return skills.HookOnAfterResponse, false, noFilter
	case EventSessionStart:
		return skills.HookOnConversationStart, false, noFilter
	case EventSetup:
		return skills.HookOnSetup, false, noFilter
	case EventTaskCreated:
		return skills.HookOnTaskCreated, false, noFilter
	case EventTaskCompleted:
		return skills.HookOnTaskCompleted, false, noFilter
	default:
		return 0, false, nil
	}
}

func filterFileWritePatch(event skills.HookEvent, pattern string) bool {
	toolName, _ := event.Data["tool_name"].(string)
	if toolName != "file" {
		return false
	}
	inputStr, _ := event.Data["input"].(string)
	var input struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal([]byte(inputStr), &input); err != nil {
		return false
	}
	if input.Operation != "write" && input.Operation != "patch" {
		return false
	}
	if pattern == "" {
		return true
	}
	matched, _ := path.Match(pattern, path.Base(input.Path))
	return matched
}

func filterShellTool(event skills.HookEvent, _ string) bool {
	toolName, _ := event.Data["tool_name"].(string)
	return toolName == "shell"
}

func expandTemplateVars(cmd string, event skills.HookEvent) string {
	toolName, _ := event.Data["tool_name"].(string)
	inputStr, _ := event.Data["input"].(string)

	var filePath, shellCmd string
	var parsed map[string]any
	if err := json.Unmarshal([]byte(inputStr), &parsed); err == nil {
		if p, ok := parsed["path"].(string); ok {
			filePath = p
		}
		if c, ok := parsed["command"].(string); ok {
			shellCmd = c
		}
	}

	// Shell-escape all substituted values to prevent injection.
	// Template variables come from LLM-generated tool inputs which
	// the user does not fully control (prompt injection risk).
	return strings.NewReplacer(
		"{tool}", shellQuote(toolName),
		"{file}", shellQuote(filePath),
		"{command}", shellQuote(shellCmd),
	).Replace(cmd)
}

// shellQuote wraps a string in single quotes for safe shell interpolation.
// Embedded single quotes are escaped as '\”.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
