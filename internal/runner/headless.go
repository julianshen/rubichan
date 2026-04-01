// internal/runner/headless.go
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/session"
)

// TurnFunc matches the signature of agent.Agent.Turn.
type TurnFunc func(ctx context.Context, msg string) (<-chan agent.TurnEvent, error)

// HeadlessRunner executes a single agent turn and collects the result.
type HeadlessRunner struct {
	turn      TurnFunc
	eventSink session.EventSink
	modelName string
}

// NewHeadlessRunner creates a new HeadlessRunner with the given turn function.
func NewHeadlessRunner(turn TurnFunc) *HeadlessRunner {
	return &HeadlessRunner{turn: turn}
}

// SetEventSink configures a structured event sink for headless execution.
func (r *HeadlessRunner) SetEventSink(sink session.EventSink) {
	r.eventSink = sink
}

// SetModelName sets the model label used in turn_started events.
func (r *HeadlessRunner) SetModelName(name string) {
	r.modelName = strings.TrimSpace(name)
}

// Run executes the agent with the given prompt and collects a RunResult.
func (r *HeadlessRunner) Run(ctx context.Context, prompt, mode string) (*output.RunResult, error) {
	start := time.Now()
	state := session.NewState()
	state.ResetForPrompt(prompt)
	r.emitEvent(session.NewTurnStartedEvent(prompt, r.modelName))
	r.emitEvent(session.NewCheckpointCreatedEvent("turn-1", "turn_started"))

	ch, err := r.turn(ctx, prompt)
	if err != nil {
		return &output.RunResult{
			Prompt:     prompt,
			Mode:       mode,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		}, nil
	}

	var textBuf strings.Builder
	var toolCalls []output.ToolCallLog
	var lastErr string
	turns := 0
	doneDiffSummary := ""
	doneInputTokens := 0
	doneOutputTokens := 0

	for evt := range ch {
		switch evt.Type {
		case "thinking_delta":
			// Thinking is displayed in the TUI but suppressed in headless mode.
		case "text_delta":
			textBuf.WriteString(evt.Text)
		case "tool_call":
			if evt.ToolCall != nil {
				state.ApplyEvent(evt)
				r.emitEvent(session.NewToolCallEvent(evt.ToolCall.ID, evt.ToolCall.Name, evt.ToolCall.Input))
				toolCalls = append(toolCalls, output.ToolCallLog{
					ID:    evt.ToolCall.ID,
					Name:  evt.ToolCall.Name,
					Input: json.RawMessage(evt.ToolCall.Input),
				})
			}
		case "tool_result":
			if evt.ToolResult != nil {
				state.ApplyEvent(evt)
				result := evt.ToolResult.DisplayContent
				if result == "" {
					result = evt.ToolResult.Content
				}
				r.emitEvent(session.NewToolResultEvent(evt.ToolResult.ID, evt.ToolResult.Name, result, evt.ToolResult.IsError))
				for i := range toolCalls {
					if toolCalls[i].ID == evt.ToolResult.ID {
						// Prefer DisplayContent for user-facing output.
						toolCalls[i].Result = result
						toolCalls[i].IsError = evt.ToolResult.IsError
						break
					}
				}
			}
		case "error":
			if evt.Error != nil {
				lastErr = evt.Error.Error()
			}
		case "done":
			turns++
			doneDiffSummary = evt.DiffSummary
			doneInputTokens = evt.InputTokens
			doneOutputTokens = evt.OutputTokens
		}
	}

	response := textBuf.String()
	if strings.TrimSpace(response) != "" {
		r.emitEvent(session.NewAssistantFinalEvent(response))
	}
	if snapshot := state.BuildVerificationSnapshot(); snapshot != "" {
		gate := session.ParseVerificationGate(snapshot)
		verdict, reason := session.ParseVerificationSnapshot(snapshot)
		if plan := state.Plan(); len(plan) > 0 {
			r.emitEvent(session.NewPlanUpdatedEvent("turn_done", plan))
		}
		if gate == "hard_fail" || (gate == "" && verdict != "" && verdict != "passed") {
			r.emitEvent(session.NewGateFailedEvent("verification", reason))
		}
		r.emitEvent(session.NewVerificationSnapshotEvent(snapshot))
	}
	summary := buildHeadlessSummary(response, toolCalls, lastErr)
	evidenceSummary := buildEvidenceSummary(prompt, toolCalls)
	if shouldTreatToolOnlyMaxTurnsAsIncompleteSuccess(prompt, response, toolCalls, lastErr) {
		lastErr = ""
		summary = fmt.Sprintf("Run completed through tool evidence after %d tool call(s), but the model produced no final textual response.", len(toolCalls))
	}
	r.emitEvent(session.NewTurnCompletedEvent(doneDiffSummary, doneInputTokens, doneOutputTokens))

	// When the model produced no textual response but tool calls completed,
	// populate Response with the summary so downstream consumers (JSON, PR
	// comments) receive useful output instead of an empty string.
	if strings.TrimSpace(response) == "" && summary != "" {
		response = summary
	}

	return &output.RunResult{
		Prompt:          prompt,
		Response:        response,
		Summary:         summary,
		EvidenceSummary: evidenceSummary,
		ToolCalls:       toolCalls,
		TurnCount:       turns,
		DurationMs:      time.Since(start).Milliseconds(),
		Mode:            mode,
		Error:           lastErr,
	}, nil
}

func (r *HeadlessRunner) emitEvent(evt session.Event) {
	if r == nil || r.eventSink == nil {
		return
	}
	r.eventSink.Emit(evt.WithActor(session.PrimaryActor()))
}

func buildEvidenceSummary(prompt string, toolCalls []output.ToolCallLog) string {
	if looksLikeBackendFullstackTask(prompt) {
		return buildBackendEvidenceSummary(toolCalls)
	}
	if !looksLikeFrontendAppTask(prompt) {
		return ""
	}

	var lines []string
	verdict, reason := frontendVerificationVerdict(toolCalls)
	lines = append(lines, fmt.Sprintf("- Verification verdict: %s", verdict))
	if reason != "" {
		lines = append(lines, fmt.Sprintf("- Reason: %s", reason))
	}
	if buildLine := frontendBuildEvidenceLine(toolCalls); buildLine != "" {
		lines = append(lines, buildLine)
	}
	if runtimeLine := frontendRuntimeEvidenceLine(toolCalls); runtimeLine != "" {
		lines = append(lines, runtimeLine)
	}
	if filesLine := frontendFileEvidenceLine(toolCalls); filesLine != "" {
		lines = append(lines, filesLine)
	}
	return strings.Join(lines, "\n")
}

func buildBackendEvidenceSummary(toolCalls []output.ToolCallLog) string {
	var lines []string
	verdict, reason := backendVerificationVerdict(toolCalls)
	lines = append(lines, fmt.Sprintf("- Verification verdict: %s", verdict))
	if reason != "" {
		lines = append(lines, fmt.Sprintf("- Reason: %s", reason))
	}
	if validationLine := backendValidationEvidenceLine(toolCalls); validationLine != "" {
		lines = append(lines, validationLine)
	}
	if dependencyLine := backendDependencyEvidenceLine(toolCalls); dependencyLine != "" {
		lines = append(lines, dependencyLine)
	}
	if schemaLine := backendSchemaEvidenceLine(toolCalls); schemaLine != "" {
		lines = append(lines, schemaLine)
	}
	if runtimeLine := backendRuntimeEvidenceLine(toolCalls); runtimeLine != "" {
		lines = append(lines, runtimeLine)
	}
	if apiLine := backendAPIEvidenceLine(toolCalls); apiLine != "" {
		lines = append(lines, apiLine)
	}
	if filesLine := frontendFileEvidenceLine(toolCalls); filesLine != "" {
		lines = append(lines, filesLine)
	}
	return strings.Join(lines, "\n")
}

func backendVerificationVerdict(toolCalls []output.ToolCallLog) (string, string) {
	if reason := backendDependencyFailureReason(toolCalls); reason != "" {
		return "failed", reason
	}
	if !hasSuccessfulDependencyResolution(toolCalls) {
		return "failed", "no dependency resolution evidence"
	}
	if !hasSuccessfulBackendValidationAfterLastEdit(toolCalls) && !hasBackendRuntimeAndAPIAfterLastEdit(toolCalls) {
		if backendVerificationWasInvalidatedByLaterEdit(toolCalls) {
			return "failed", "a previously successful backend verification was invalidated by later file edits"
		}
		if snippet := findBackendValidationFailure(toolCalls); snippet != "" {
			return "failed", snippet
		}
		return "failed", "no validation or runtime/API verification evidence after the latest edit"
	}
	if !hasBackendSchemaEvidence(toolCalls) {
		return "failed", "no schema/init evidence"
	}
	if !hasBackendRuntimeEvidence(toolCalls) {
		return "failed", "no runtime evidence"
	}
	if !hasBackendAPIRoundTripEvidence(toolCalls) {
		return "failed", "no API round-trip evidence"
	}
	return "passed", "dependency resolution, schema/init, runtime, and API round-trip evidence observed"
}

func backendValidationEvidenceLine(toolCalls []output.ToolCallLog) string {
	filtered := toolCallsAfterLastEdit(toolCalls)
	for i := len(filtered) - 1; i >= 0; i-- {
		tc := filtered[i]
		if !isBackendValidationCommand(tc) {
			continue
		}
		if tc.IsError {
			if snippet := extractBuildFailureSnippet(tc.Result); snippet != "" {
				return fmt.Sprintf("- Validation: failed (%s)", snippet)
			}
			return "- Validation: failed"
		}
		return "- Validation: passed"
	}
	if hasBackendRuntimeEvidence(filtered) && hasBackendAPIRoundTripEvidence(filtered) {
		return "- Validation: runtime/API verification only"
	}
	return "- Validation: no evidence"
}

func backendDependencyEvidenceLine(toolCalls []output.ToolCallLog) string {
	for i := len(toolCalls) - 1; i >= 0; i-- {
		tc := toolCalls[i]
		if !isDependencyResolutionCommand(tc) {
			continue
		}
		if tc.IsError {
			return "- Dependency resolution: failed"
		}
		return "- Dependency resolution: passed"
	}
	return "- Dependency resolution: no evidence"
}

func backendDependencyFailureReason(toolCalls []output.ToolCallLog) string {
	for i := len(toolCalls) - 1; i >= 0; i-- {
		tc := toolCalls[i]
		if !isDependencyResolutionCommand(tc) {
			continue
		}
		if !tc.IsError {
			return ""
		}
		if snippet := extractBuildFailureSnippet(tc.Result); snippet != "" {
			return snippet
		}
		return "dependency resolution failed"
	}
	return ""
}

func backendSchemaEvidenceLine(toolCalls []output.ToolCallLog) string {
	if !hasBackendSchemaEvidence(toolCalls) {
		return "- Schema/init: no evidence"
	}
	return "- Schema/init: observed"
}

func backendRuntimeEvidenceLine(toolCalls []output.ToolCallLog) string {
	if !hasBackendRuntimeEvidence(toolCalls) {
		return "- Runtime: no evidence"
	}
	return "- Runtime: server started"
}

func backendAPIEvidenceLine(toolCalls []output.ToolCallLog) string {
	if !hasBackendAPIRoundTripEvidence(toolCalls) {
		return "- API round-trip: no evidence"
	}
	return "- API round-trip: observed"
}

func frontendVerificationVerdict(toolCalls []output.ToolCallLog) (string, string) {
	if len(toolCalls) == 0 {
		return "failed", "no tool evidence"
	}
	lastEdit := -1
	lastBuild := -1
	for i, tc := range toolCalls {
		if isFileModification(tc) {
			lastEdit = i
		}
		if isFrontendBuildCommand(tc) {
			lastBuild = i
		}
	}
	if lastBuild == -1 || lastBuild <= lastEdit {
		return "failed", "no build evidence"
	}

	tc := toolCalls[lastBuild]
	if tc.IsError {
		if snippet := extractBuildFailureSnippet(tc.Result); snippet != "" {
			return "failed", snippet
		}
		return "failed", "build command failed"
	}
	return "passed", "latest build evidence is green"
}

func frontendBuildEvidenceLine(toolCalls []output.ToolCallLog) string {
	filtered := toolCallsAfterLastEdit(toolCalls)
	for i := len(filtered) - 1; i >= 0; i-- {
		tc := filtered[i]
		if !isFrontendBuildCommand(tc) {
			continue
		}
		if tc.IsError {
			if snippet := extractBuildFailureSnippet(tc.Result); snippet != "" {
				return fmt.Sprintf("- Build: failed (%s)", snippet)
			}
			return "- Build: failed"
		}
		return "- Build: passed"
	}
	return "- Build: no evidence"
}

func frontendRuntimeEvidenceLine(toolCalls []output.ToolCallLog) string {
	for i := len(toolCalls) - 1; i >= 0; i-- {
		tc := toolCalls[i]
		result := strings.ToLower(tc.Result)
		if tc.Name == "process" && strings.Contains(result, "localhost:") {
			return "- Runtime: server started"
		}
		if (tc.Name == "shell" || tc.Name == "process") && strings.Contains(result, "http/1.1 200 ok") {
			return "- Runtime: reachable (HTTP 200)"
		}
	}
	return ""
}

func frontendFileEvidenceLine(toolCalls []output.ToolCallLog) string {
	var files []string
	seen := map[string]bool{}
	for _, tc := range toolCalls {
		path, ok := modifiedFilePath(tc)
		if !ok || path == "" || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
		if len(files) == 4 {
			break
		}
	}
	if len(files) == 0 {
		return ""
	}
	return fmt.Sprintf("- Files changed: %s", strings.Join(files, ", "))
}

func modifiedFilePath(tc output.ToolCallLog) (string, bool) {
	if tc.Name != "file" {
		return "", false
	}
	var in struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(tc.Input, &in); err != nil {
		return "", false
	}
	if in.Operation != "write" && in.Operation != "patch" {
		return "", false
	}
	return in.Path, true
}

func shouldTreatToolOnlyMaxTurnsAsIncompleteSuccess(prompt, response string, toolCalls []output.ToolCallLog, lastErr string) bool {
	if strings.TrimSpace(response) != "" {
		return false
	}
	if !strings.Contains(lastErr, "max turns") {
		return false
	}
	if len(toolCalls) == 0 {
		return false
	}
	for _, tc := range toolCalls {
		if tc.IsError {
			return false
		}
	}
	if looksLikeBackendFullstackTask(prompt) && !hasSuccessfulBackendVerificationAfterLastEdit(toolCalls) {
		return false
	}
	if looksLikeFrontendAppTask(prompt) && !hasSuccessfulFrontendBuildAfterLastEdit(toolCalls) {
		return false
	}
	return true
}

func looksLikeBackendFullstackTask(prompt string) bool {
	prompt = strings.ToLower(prompt)
	hasBackendNeedle := false
	for _, needle := range []string{
		"fullstack",
		"backend",
		"sqlite",
		"database",
		"api",
		"node.js",
		"nodejs",
		"python backend",
		"go backend",
		"java backend",
	} {
		if strings.Contains(prompt, needle) {
			hasBackendNeedle = true
			break
		}
	}
	return hasBackendNeedle
}

func looksLikeFrontendAppTask(prompt string) bool {
	prompt = strings.ToLower(prompt)
	for _, needle := range []string{
		"react",
		"vite",
		"next.js",
		"nextjs",
		"frontend",
		"shadcn",
		"todo app",
		"single-page app",
		"dashboard",
		"landing page",
	} {
		if strings.Contains(prompt, needle) {
			return true
		}
	}
	return false
}

func hasSuccessfulBackendVerificationAfterLastEdit(toolCalls []output.ToolCallLog) bool {
	if !hasSuccessfulDependencyResolution(toolCalls) {
		return false
	}
	if !hasSuccessfulBackendValidationAfterLastEdit(toolCalls) && !hasBackendRuntimeAndAPIAfterLastEdit(toolCalls) {
		return false
	}
	return hasBackendSchemaEvidence(toolCalls) &&
		hasBackendRuntimeEvidence(toolCalls) &&
		hasBackendAPIRoundTripEvidence(toolCalls)
}

func hasBackendRuntimeAndAPIAfterLastEdit(toolCalls []output.ToolCallLog) bool {
	lastEdit := -1
	lastRuntime := -1
	lastAPI := -1
	for i, tc := range toolCalls {
		if isFileModification(tc) {
			lastEdit = i
		}
		if backendRuntimeEvidenceInToolCall(tc) {
			lastRuntime = i
		}
		if backendAPIEvidenceInToolCall(tc) {
			lastAPI = i
		}
	}
	if lastRuntime == -1 || lastAPI == -1 {
		return false
	}
	if lastEdit == -1 {
		return true
	}
	return lastRuntime > lastEdit && lastAPI > lastEdit
}

func backendVerificationWasInvalidatedByLaterEdit(toolCalls []output.ToolCallLog) bool {
	lastEdit := -1
	lastGoodVerification := -1
	for i, tc := range toolCalls {
		if isFileModification(tc) {
			lastEdit = i
		}
		if isSuccessfulBackendValidation(tc) || backendRuntimeEvidenceInToolCall(tc) || backendAPIEvidenceInToolCall(tc) {
			lastGoodVerification = i
		}
	}
	return lastGoodVerification != -1 && lastEdit != -1 && lastGoodVerification < lastEdit
}

func hasSuccessfulDependencyResolution(toolCalls []output.ToolCallLog) bool {
	for _, tc := range toolCalls {
		if isDependencyResolutionCommand(tc) && !tc.IsError {
			return true
		}
	}
	return false
}

func hasSuccessfulBackendValidationAfterLastEdit(toolCalls []output.ToolCallLog) bool {
	lastEdit := -1
	lastValidation := -1
	for i, tc := range toolCalls {
		if isFileModification(tc) {
			lastEdit = i
		}
		if isSuccessfulBackendValidation(tc) {
			lastValidation = i
		}
	}
	if lastValidation == -1 {
		return false
	}
	if lastEdit == -1 {
		return true
	}
	return lastValidation > lastEdit
}

func hasSuccessfulFrontendBuildAfterLastEdit(toolCalls []output.ToolCallLog) bool {
	lastEdit := -1
	lastSuccessfulBuild := -1
	for i, tc := range toolCalls {
		if isFileModification(tc) {
			lastEdit = i
		}
		if isSuccessfulFrontendBuild(tc) {
			lastSuccessfulBuild = i
		}
	}
	if lastSuccessfulBuild == -1 {
		return false
	}
	if lastEdit == -1 {
		return true
	}
	return lastSuccessfulBuild > lastEdit
}

func toolCallsAfterLastEdit(toolCalls []output.ToolCallLog) []output.ToolCallLog {
	lastEdit := -1
	for i, tc := range toolCalls {
		if isFileModification(tc) {
			lastEdit = i
		}
	}
	if lastEdit == -1 {
		return toolCalls
	}
	if lastEdit+1 >= len(toolCalls) {
		return nil
	}
	return toolCalls[lastEdit+1:]
}

func isFileModification(tc output.ToolCallLog) bool {
	switch tc.Name {
	case "file":
		var in struct {
			Operation string `json:"operation"`
		}
		if err := json.Unmarshal(tc.Input, &in); err != nil {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(in.Operation)) {
		case "write", "patch", "delete", "create", "rename", "move", "append":
			return true
		default:
			return false
		}
	case "shell":
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(tc.Input, &in); err != nil {
			return false
		}
		cmd := strings.ToLower(in.Command)
		for _, needle := range []string{
			"apply_patch",
			"applypatch",
			"sed -i",
			"perl -pi",
			"| tee",
			" tee ",
			"> ",
			">>",
			"2>",
			"mv ",
			"cp ",
			"mkdir ",
			"touch ",
			"install -m",
			"cat >",
		} {
			if strings.Contains(cmd, needle) {
				return true
			}
		}
	}
	return false
}

func isSuccessfulFrontendBuild(tc output.ToolCallLog) bool {
	if tc.IsError {
		return false
	}

	var command string
	switch tc.Name {
	case "shell":
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(tc.Input, &in); err == nil {
			command = strings.ToLower(in.Command)
		}
	case "process":
		var in struct {
			Operation string `json:"operation"`
			Command   string `json:"command,omitempty"`
		}
		if err := json.Unmarshal(tc.Input, &in); err == nil && in.Operation == "exec" {
			command = strings.ToLower(in.Command)
		}
	}

	if command == "" {
		return false
	}
	buildCommand := strings.Contains(command, "npm run build") ||
		strings.Contains(command, "pnpm build") ||
		strings.Contains(command, "yarn build") ||
		strings.Contains(command, "vite build") ||
		strings.Contains(command, "next build")
	if !buildCommand {
		return false
	}

	result := strings.ToLower(tc.Result)
	return strings.Contains(result, "built in") ||
		strings.Contains(result, "build succeeded") ||
		strings.Contains(result, "build complete")
}

func isSuccessfulBackendValidation(tc output.ToolCallLog) bool {
	if tc.IsError || !isBackendValidationCommand(tc) {
		return false
	}
	result := strings.ToLower(tc.Result)
	return strings.Contains(result, "build succeeded") ||
		strings.Contains(result, "build complete") ||
		strings.Contains(result, "built in") ||
		strings.Contains(result, "build successful") ||
		strings.Contains(result, "success") ||
		strings.Contains(result, "ok")
}

func buildHeadlessSummary(response string, toolCalls []output.ToolCallLog, lastErr string) string {
	if strings.TrimSpace(response) != "" {
		return ""
	}

	toolErrors := 0
	for _, tc := range toolCalls {
		if tc.IsError {
			toolErrors++
		}
	}
	buildFailure := findFrontendBuildFailure(toolCalls)
	backendFailure := findBackendValidationFailure(toolCalls)

	switch {
	case buildFailure != "":
		return fmt.Sprintf("Run ended with a frontend build failure: %s", buildFailure)
	case backendFailure != "":
		return fmt.Sprintf("Run ended with a backend validation failure: %s", backendFailure)
	case lastErr != "" && len(toolCalls) > 0:
		return fmt.Sprintf("Run failed after %d tool call(s); %d returned errors. Last error: %s", len(toolCalls), toolErrors, lastErr)
	case lastErr != "":
		return fmt.Sprintf("Run failed before producing a textual response. Error: %s", lastErr)
	case len(toolCalls) > 0:
		return fmt.Sprintf("Run completed without a textual response after %d tool call(s); %d returned errors.", len(toolCalls), toolErrors)
	default:
		return "Run completed without a textual response."
	}
}

func findFrontendBuildFailure(toolCalls []output.ToolCallLog) string {
	for i := len(toolCalls) - 1; i >= 0; i-- {
		tc := toolCalls[i]
		if !isFrontendBuildCommand(tc) {
			continue
		}
		if !tc.IsError {
			return ""
		}
		if snippet := extractBuildFailureSnippet(tc.Result); snippet != "" {
			return snippet
		}
		return "build command failed"
	}
	return ""
}

func findBackendValidationFailure(toolCalls []output.ToolCallLog) string {
	for i := len(toolCalls) - 1; i >= 0; i-- {
		tc := toolCalls[i]
		if !isBackendValidationCommand(tc) && !isDependencyResolutionCommand(tc) {
			continue
		}
		if !tc.IsError {
			return ""
		}
		if snippet := extractBuildFailureSnippet(tc.Result); snippet != "" {
			return snippet
		}
		return "validation command failed"
	}
	return ""
}

func isFrontendBuildCommand(tc output.ToolCallLog) bool {
	command := commandString(tc)
	return strings.Contains(command, "npm run build") ||
		strings.Contains(command, "pnpm build") ||
		strings.Contains(command, "yarn build") ||
		strings.Contains(command, "vite build") ||
		strings.Contains(command, "next build")
}

func isBackendValidationCommand(tc output.ToolCallLog) bool {
	command := commandString(tc)
	for _, needle := range []string{
		"npm run build",
		"pnpm build",
		"yarn build",
		"vite build",
		"next build",
		"go build",
		"go test",
		"mvn ",
		"gradle build",
		"./gradlew build",
		"pytest",
		"python -m py_compile",
		"python -m compileall",
		"uv run pytest",
		"flask --app",
	} {
		if strings.Contains(command, needle) {
			if needle == "mvn " && !strings.Contains(command, "compile") && !strings.Contains(command, "package") && !strings.Contains(command, "test") {
				continue
			}
			return true
		}
	}
	return false
}

func isDependencyResolutionCommand(tc output.ToolCallLog) bool {
	command := commandString(tc)
	for _, needle := range []string{
		"npm ci",
		"npm install",
		"pnpm install",
		"yarn install",
		"go get",
		"go mod tidy",
		"mvn ",
		"gradle build",
		"./gradlew build",
		"pip install",
		"uv sync",
		"poetry install",
	} {
		if strings.Contains(command, needle) {
			if needle == "mvn " && !strings.Contains(command, "dependency:resolve") && !strings.Contains(command, "compile") && !strings.Contains(command, "package") && !strings.Contains(command, "test") {
				continue
			}
			return true
		}
	}
	return false
}

func commandString(tc output.ToolCallLog) string {
	var command string
	switch tc.Name {
	case "shell":
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(tc.Input, &in); err == nil {
			command = strings.ToLower(in.Command)
		}
	case "process":
		var in struct {
			Operation string `json:"operation"`
			Command   string `json:"command,omitempty"`
		}
		if err := json.Unmarshal(tc.Input, &in); err == nil && in.Operation == "exec" {
			command = strings.ToLower(in.Command)
		}
	}
	return command
}

func hasBackendSchemaEvidence(toolCalls []output.ToolCallLog) bool {
	for _, tc := range toolCalls {
		if backendSchemaEvidenceInToolCall(tc) {
			return true
		}
	}
	return false
}

func backendSchemaEvidenceInToolCall(tc output.ToolCallLog) bool {
	switch tc.Name {
	case "file":
		var in struct {
			Operation string `json:"operation"`
			Path      string `json:"path"`
			Content   string `json:"content"`
		}
		if err := json.Unmarshal(tc.Input, &in); err != nil {
			return false
		}
		if in.Operation != "write" && in.Operation != "patch" {
			return false
		}
		path := strings.ToLower(in.Path)
		content := strings.ToLower(in.Content)
		return strings.Contains(path, "schema") ||
			strings.Contains(path, "migration") ||
			strings.Contains(path, ".sql") ||
			strings.Contains(content, "create table") ||
			strings.Contains(content, "sqlite")
	case "shell", "process":
		result := strings.ToLower(tc.Result)
		return strings.Contains(result, "create table") ||
			strings.Contains(result, "schema initialized") ||
			strings.Contains(result, "database initialized")
	}
	return false
}

func hasBackendRuntimeEvidence(toolCalls []output.ToolCallLog) bool {
	for _, tc := range toolCalls {
		if backendRuntimeEvidenceInToolCall(tc) {
			return true
		}
	}
	return false
}

func hasBackendAPIRoundTripEvidence(toolCalls []output.ToolCallLog) bool {
	for _, tc := range toolCalls {
		if backendAPIEvidenceInToolCall(tc) {
			return true
		}
	}
	return false
}

func backendRuntimeEvidenceInToolCall(tc output.ToolCallLog) bool {
	if tc.Name != "process" && tc.Name != "shell" {
		return false
	}
	result := strings.ToLower(tc.Result)
	return strings.Contains(result, "server listening on") ||
		strings.Contains(result, "listening on :") ||
		strings.Contains(result, "running on http://") ||
		strings.Contains(result, "localhost:") ||
		strings.Contains(result, "http/1.1 200 ok")
}

func backendAPIEvidenceInToolCall(tc output.ToolCallLog) bool {
	command := commandString(tc)
	result := strings.ToLower(tc.Result)
	targetedRequest := strings.Contains(command, "curl") &&
		(strings.Contains(command, "localhost") || strings.Contains(command, "127.0.0.1")) &&
		(strings.Contains(command, "post ") || strings.Contains(command, "patch ") || strings.Contains(command, "put ") || strings.Contains(command, "get ") || strings.Contains(command, "/todos"))
	if !targetedRequest && !strings.Contains(result, "/api/") && !strings.Contains(result, "/todos") {
		if !(hasDoubleQuoteAPIFields(result) || hasSingleQuoteAPIFields(result)) {
			return false
		}
	}
	return strings.Contains(result, "http/1.1 200 ok") ||
		strings.Contains(result, "http/1.1 201 created") ||
		(strings.Contains(result, "post /") && strings.Contains(result, "201")) ||
		(strings.Contains(result, "get /") && strings.Contains(result, "200")) ||
		hasDoubleQuoteAPIFields(result) ||
		hasSingleQuoteAPIFields(result) ||
		(strings.Contains(result, "|") && strings.Contains(result, "todo"))
}

func hasDoubleQuoteAPIFields(result string) bool {
	return strings.Contains(result, "\"id\"") && strings.Contains(result, "\"title\"")
}

func hasSingleQuoteAPIFields(result string) bool {
	return strings.Contains(result, "'id'") && strings.Contains(result, "'title'")
}

func extractBuildFailureSnippet(result string) string {
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, ".tsx:") ||
			strings.Contains(line, ".jsx:") ||
			strings.Contains(line, ".ts:") ||
			strings.Contains(line, ".js:") {
			return line
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), "error") ||
			strings.Contains(strings.ToLower(line), "failed") {
			return line
		}
	}
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}
