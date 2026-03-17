package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/skills"
)

const defaultTimeout = 30 * time.Second

// UserHookConfig describes a single user-configured shell hook entry.
type UserHookConfig struct {
	Event       string
	Pattern     string
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

			cmd := expandTemplateVars(hookCfg.Command, event)

			eventCtx := event.Ctx
			if eventCtx == nil {
				eventCtx = context.Background()
			}
			ctx, cancel := context.WithTimeout(eventCtx, timeout)
			defer cancel()

			c := exec.CommandContext(ctx, "sh", "-c", cmd)
			c.Dir = workDir
			output, err := c.CombinedOutput()

			if err != nil && isPreEvent {
				log.Printf("user hook %q blocked: %s (output: %s)", hookCfg.Description, err, strings.TrimSpace(string(output)))
				return skills.HookResult{Cancel: true}, nil
			}
			if err != nil {
				log.Printf("user hook %q failed (non-blocking): %s", hookCfg.Description, err)
			}

			return skills.HookResult{}, nil
		})
	}
}

// mapEventToPhase converts a user-facing event name to a HookPhase, a flag
// indicating whether failures should cancel the operation, and a filter
// function for narrowing which tool calls the hook applies to.
func mapEventToPhase(event string) (skills.HookPhase, bool, func(skills.HookEvent, string) bool) {
	noFilter := func(_ skills.HookEvent, _ string) bool { return true }

	switch event {
	case "pre_tool":
		return skills.HookOnBeforeToolCall, true, noFilter
	case "post_tool":
		return skills.HookOnAfterToolResult, false, noFilter
	case "pre_edit":
		return skills.HookOnBeforeToolCall, true, filterFileWritePatch
	case "post_edit":
		return skills.HookOnAfterToolResult, false, filterFileWritePatch
	case "pre_shell":
		return skills.HookOnBeforeToolCall, true, filterShellTool
	case "session_start":
		return skills.HookOnConversationStart, false, noFilter
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
// Embedded single quotes are escaped as '\''.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
