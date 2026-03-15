package session

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ToolCall captures a tool invocation plus any associated result.
type ToolCall struct {
	ID      string
	Name    string
	Input   json.RawMessage
	Result  string
	IsError bool
}

// PlanStatus captures the reducer-level execution state for a plan item.
type PlanStatus string

const (
	PlanStatusPending          PlanStatus = "pending"
	PlanStatusInProgress       PlanStatus = "in_progress"
	PlanStatusCompleted        PlanStatus = "completed"
	PlanStatusFailed           PlanStatus = "failed"
	PlanStatusReverifyRequired PlanStatus = "reverify_required"
)

// PlanItem represents a reducer-level task in the current turn.
type PlanItem struct {
	Step   string
	Status PlanStatus
}

// State tracks turn-local session state in a UI-agnostic form.
// It is intentionally small: enough to support verification/debugging and
// future event-log/reducer work without depending on TUI rendering types.
type State struct {
	lastPrompt   string
	toolCalls    []ToolCall
	toolCallArgs map[string]json.RawMessage
	plan         []PlanItem
}

// NewState creates an empty session state.
func NewState() *State {
	return &State{
		toolCallArgs: make(map[string]json.RawMessage),
	}
}

// ResetForPrompt starts a fresh turn state for the given prompt.
func (s *State) ResetForPrompt(prompt string) {
	s.lastPrompt = prompt
	s.toolCalls = nil
	s.plan = nil
	clear(s.toolCallArgs)
	if looksLikeBackendVerificationPrompt(prompt) {
		s.plan = []PlanItem{{
			Step:   "Backend verification",
			Status: PlanStatusInProgress,
		}}
	}
}

// LastPrompt returns the most recent prompt associated with this state.
func (s *State) LastPrompt() string { return s.lastPrompt }

// ToolCalls returns a copy of the accumulated tool calls for the current turn.
func (s *State) ToolCalls() []ToolCall {
	out := make([]ToolCall, len(s.toolCalls))
	copy(out, s.toolCalls)
	return out
}

// Plan returns a copy of the current reducer-level plan.
func (s *State) Plan() []PlanItem {
	out := make([]PlanItem, len(s.plan))
	copy(out, s.plan)
	return out
}

// ApplyEvent folds a single turn event into the current session state.
func (s *State) ApplyEvent(evt agentsdk.TurnEvent) {
	switch evt.Type {
	case "tool_call":
		if evt.ToolCall == nil {
			return
		}
		s.toolCallArgs[evt.ToolCall.ID] = append(json.RawMessage(nil), evt.ToolCall.Input...)
		s.toolCalls = append(s.toolCalls, ToolCall{
			ID:    evt.ToolCall.ID,
			Name:  evt.ToolCall.Name,
			Input: append(json.RawMessage(nil), evt.ToolCall.Input...),
		})
	case "tool_result":
		if evt.ToolResult == nil {
			return
		}
		result := evt.ToolResult.DisplayContent
		if result == "" {
			result = evt.ToolResult.Content
		}
		for i := range s.toolCalls {
			if s.toolCalls[i].ID == evt.ToolResult.ID {
				s.toolCalls[i].Result = result
				s.toolCalls[i].IsError = evt.ToolResult.IsError
				return
			}
		}
		s.toolCalls = append(s.toolCalls, ToolCall{
			ID:      evt.ToolResult.ID,
			Name:    evt.ToolResult.Name,
			Result:  result,
			IsError: evt.ToolResult.IsError,
		})
	}
}

// BuildVerificationSnapshot returns a backend verification snapshot for the
// current turn, or an empty string when the prompt does not look like a
// backend verification task.
func (s *State) BuildVerificationSnapshot() string {
	eval := evaluateVerification(s.lastPrompt, s.toolCalls)
	s.syncPlan(eval)
	return renderVerificationSnapshot(eval)
}

// BuildVerificationSnapshot derives a backend verification summary from a
// prompt and accumulated tool calls.
func BuildVerificationSnapshot(prompt string, toolCalls []ToolCall) string {
	return renderVerificationSnapshot(evaluateVerification(prompt, toolCalls))
}

type verificationEval struct {
	applicable  bool
	dependency  bool
	schema      bool
	runtime     bool
	api         bool
	invalidated bool
	gate        string
	verdict     string
	reason      string
}

func evaluateVerification(prompt string, toolCalls []ToolCall) verificationEval {
	eval := verificationEval{}
	if !looksLikeBackendVerificationPrompt(prompt) {
		return eval
	}
	eval.applicable = true
	lastVerification := -1
	lastEdit := -1

	for i, tc := range toolCalls {
		args := strings.ToLower(string(tc.Input))
		content := strings.ToLower(tc.Result)
		if toolCallLooksLikeDependencyResolution(args, content, tc.IsError) {
			eval.dependency = eval.dependency || !tc.IsError
			if !tc.IsError {
				lastVerification = i
			}
		}
		if toolCallLooksLikeSchemaEvidence(args, content) {
			eval.schema = true
			lastVerification = i
		}
		if toolCallLooksLikeRuntimeEvidence(args, content) {
			eval.runtime = true
			lastVerification = i
		}
		if toolCallLooksLikeAPIEvidence(args, content) {
			eval.api = true
			lastVerification = i
		}
		if toolCallLooksLikeEdit(args) {
			lastEdit = i
		}
	}
	if lastVerification != -1 && lastEdit > lastVerification {
		eval.invalidated = true
	}

	eval.gate = "hard_fail"
	eval.verdict = "failed"
	eval.reason = "missing verification evidence"
	switch {
	case eval.invalidated:
		eval.gate = "hard_fail"
		eval.verdict = "failed"
		eval.reason = "verification was invalidated by later edits"
	case eval.dependency && eval.schema && eval.runtime && eval.api:
		eval.gate = "pass"
		eval.verdict = "passed"
		eval.reason = "dependency resolution, schema/init, runtime, and API round-trip observed"
	case !eval.dependency:
		eval.gate = "hard_fail"
		eval.verdict = "failed"
		eval.reason = "missing dependency resolution evidence"
	case !eval.schema:
		eval.gate = "soft_fail"
		eval.verdict = "passed_with_warnings"
		eval.reason = "missing schema/init evidence"
	case !eval.runtime:
		eval.gate = "hard_fail"
		eval.verdict = "failed"
		eval.reason = "missing runtime evidence"
	case !eval.api:
		eval.gate = "hard_fail"
		eval.verdict = "failed"
		eval.reason = "missing API round-trip evidence"
	}
	return eval
}

func renderVerificationSnapshot(eval verificationEval) string {
	if !eval.applicable {
		return ""
	}
	return fmt.Sprintf("Verification snapshot:\n- gate: %s\n- verdict: %s\n- reason: %s\n- dependency resolution: %t\n- schema/init: %t\n- runtime: %t\n- api round-trip: %t\n",
		eval.gate, eval.verdict, eval.reason, eval.dependency, eval.schema, eval.runtime, eval.api)
}

func (s *State) syncPlan(eval verificationEval) {
	if !eval.applicable {
		s.plan = nil
		return
	}
	if len(s.plan) == 0 {
		s.plan = []PlanItem{{
			Step:   "Backend verification",
			Status: PlanStatusInProgress,
		}}
	}
	status := PlanStatusInProgress
	switch {
	case eval.invalidated:
		status = PlanStatusReverifyRequired
	case eval.gate == "pass" || eval.gate == "soft_fail":
		status = PlanStatusCompleted
	case eval.gate == "hard_fail":
		status = PlanStatusFailed
	}
	// Enforce invariant: at most one in_progress item.
	for i := range s.plan {
		if i == 0 {
			s.plan[i].Status = status
			continue
		}
		if s.plan[i].Status == PlanStatusInProgress {
			s.plan[i].Status = PlanStatusPending
		}
	}
}

func looksLikeBackendVerificationPrompt(prompt string) bool {
	prompt = strings.ToLower(prompt)
	for _, needle := range []string{"backend", "sqlite", "api", "todo", "crud", "database"} {
		if strings.Contains(prompt, needle) {
			return true
		}
	}
	return false
}

func toolCallLooksLikeDependencyResolution(args, _ string, isError bool) bool {
	if isError {
		return strings.Contains(args, "npm ci") ||
			strings.Contains(args, "npm install") ||
			strings.Contains(args, "pip install") ||
			strings.Contains(args, "python3 -m pip install") ||
			strings.Contains(args, "go mod tidy") ||
			strings.Contains(args, "go get") ||
			strings.Contains(args, "mvn ")
	}
	return strings.Contains(args, "npm ci") ||
		strings.Contains(args, "npm install") ||
		strings.Contains(args, "pip install") ||
		strings.Contains(args, "python3 -m pip install") ||
		strings.Contains(args, "go mod tidy") ||
		strings.Contains(args, "go get") ||
		(strings.Contains(args, "mvn ") && (strings.Contains(args, "compile") || strings.Contains(args, "package") || strings.Contains(args, "dependency:resolve")))
}

func toolCallLooksLikeSchemaEvidence(args, content string) bool {
	return strings.Contains(args, "schema") ||
		strings.Contains(args, "pragma table_info") ||
		strings.Contains(content, "create table") ||
		strings.Contains(content, "pragma table_info") ||
		strings.Contains(content, "database initialized") ||
		strings.Contains(content, "schema initialized")
}

func toolCallLooksLikeRuntimeEvidence(_ string, content string) bool {
	return strings.Contains(content, "server listening on") ||
		strings.Contains(content, "listening on port") ||
		strings.Contains(content, "status: running") ||
		strings.Contains(content, "localhost:") ||
		strings.Contains(content, "tomcat started on port") ||
		strings.Contains(content, "uvicorn running on")
}

func toolCallLooksLikeAPIEvidence(args, content string) bool {
	hasHTTPClient := strings.Contains(args, "curl") ||
		strings.Contains(args, "http ") ||
		strings.Contains(args, "wget ") ||
		strings.Contains(args, "axios") ||
		strings.Contains(args, "fetch(") ||
		strings.Contains(args, "requests.")
	hasEndpoint := strings.Contains(args, "/") && (strings.Contains(args, "localhost") || strings.Contains(args, "127.0.0.1") || strings.Contains(args, "/api/") || strings.Contains(args, "/todos") || strings.Contains(args, "/stats"))
	hasHTTPStatus := strings.Contains(content, "http/1.1 200") ||
		strings.Contains(content, "http/1.1 201") ||
		strings.Contains(content, "http/2 200") ||
		strings.Contains(content, "status: 200") ||
		strings.Contains(content, "status code: 200")
	hasMethod := strings.Contains(args, " get ") ||
		strings.Contains(args, " post ") ||
		strings.Contains(args, " put ") ||
		strings.Contains(args, " delete ") ||
		strings.Contains(content, "get /") ||
		strings.Contains(content, "post /") ||
		strings.Contains(content, "put /") ||
		strings.Contains(content, "delete /")
	return (hasHTTPClient && (hasEndpoint || hasMethod) && (hasHTTPStatus || hasAPIFields(content))) ||
		(hasEndpoint && hasAPIFields(content)) ||
		(hasMethod && hasAPIFields(content))
}

func hasAPIFields(content string) bool {
	return strings.Contains(content, "\"id\"") ||
		strings.Contains(content, "id:") ||
		strings.Contains(content, "'id'") ||
		strings.Contains(content, "\"title\"") ||
		strings.Contains(content, "title:") ||
		strings.Contains(content, "'title'") ||
		strings.Contains(content, "\"completed\"") ||
		strings.Contains(content, "completed:") ||
		strings.Contains(content, "'completed'") ||
		strings.Contains(content, "\"total\"") ||
		strings.Contains(content, "total:") ||
		strings.Contains(content, "'total'") ||
		strings.Contains(content, "\"items\"") ||
		strings.Contains(content, "\"data\"")
}

func toolCallLooksLikeEdit(args string) bool {
	return strings.Contains(args, `"operation":"write"`) ||
		strings.Contains(args, `"operation":"patch"`) ||
		strings.Contains(args, `"operation":"apply"`) ||
		strings.Contains(args, `"operation":"modify"`) ||
		strings.Contains(args, `"operation":`) ||
		strings.Contains(args, "apply_patch") ||
		strings.Contains(args, "sed -i") ||
		strings.Contains(args, "perl -pi") ||
		strings.Contains(args, "mv ")
}
