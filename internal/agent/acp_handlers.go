package agent

import (
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/acp"
)

// registerACPCapabilities registers all ACP capabilities and method handlers.
func (a *Agent) registerACPCapabilities() error {
	// Register tools from the tool registry
	for _, toolName := range a.tools.Names() {
		tool, ok := a.tools.Get(toolName)
		if !ok {
			continue
		}
		a.acpRegistry.RegisterTool(acp.Tool{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}

	// Register agent methods
	a.acpRegistry.RegisterMethod("agent/prompt", a.handlePrompt)
	a.acpRegistry.RegisterMethod("tool/execute", a.handleToolExecute)

	// Register skill methods
	acp.RegisterSkillMethods(a.acpRegistry, a)

	// Register security methods
	acp.RegisterSecurityMethods(a.acpRegistry, a)

	return nil
}

// handlePrompt handles agent/prompt ACP requests.
// This method allows clients to submit prompts to the agent for processing.
func (a *Agent) handlePrompt(params json.RawMessage) (json.RawMessage, error) {
	var promptReq struct {
		Prompt   string `json:"prompt"`
		MaxTurns int    `json:"maxTurns,omitempty"`
	}

	if err := json.Unmarshal(params, &promptReq); err != nil {
		return nil, fmt.Errorf("unmarshal prompt request: %w", err)
	}

	if promptReq.Prompt == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}

	// Return a stub response indicating the request was received.
	// In a full implementation, this would process the prompt through
	// the agent's Turn() loop and return the result.
	result := map[string]interface{}{
		"status":   "processing",
		"prompt":   promptReq.Prompt,
		"maxTurns": promptReq.MaxTurns,
	}

	return json.Marshal(result)
}

// handleToolExecute handles tool/execute ACP requests.
// This method allows clients to invoke tools directly through the ACP interface.
func (a *Agent) handleToolExecute(params json.RawMessage) (json.RawMessage, error) {
	var toolReq struct {
		Tool  string          `json:"tool"`
		Input json.RawMessage `json:"input"`
	}

	if err := json.Unmarshal(params, &toolReq); err != nil {
		return nil, fmt.Errorf("unmarshal tool request: %w", err)
	}

	if toolReq.Tool == "" {
		return nil, fmt.Errorf("tool name cannot be empty")
	}

	// Verify tool exists
	_, exists := a.tools.Get(toolReq.Tool)
	if !exists {
		return nil, fmt.Errorf("tool not found: %s", toolReq.Tool)
	}

	// Return a stub response indicating the request was received.
	// In a full implementation, this would execute the tool and return
	// the result through the pipeline.
	result := map[string]interface{}{
		"status": "executed",
		"tool":   toolReq.Tool,
	}

	return json.Marshal(result)
}

// Invoke implements the acp.SkillInvoker interface.
// This method handles skill/invoke ACP requests.
func (a *Agent) Invoke(req acp.SkillInvokeRequest) (acp.SkillInvokeResponse, error) {
	if req.SkillName == "" {
		return acp.SkillInvokeResponse{}, fmt.Errorf("skill name cannot be empty")
	}

	// In a full implementation, this would delegate to the skill runtime.
	// For now, return a success stub.
	return acp.SkillInvokeResponse{
		SkillName: req.SkillName,
		Action:    req.Action,
		Output:    json.RawMessage(`{"status":"ok"}`),
		Status:    "success",
	}, nil
}

// List implements the acp.SkillInvoker interface.
// This method returns a list of available skills.
func (a *Agent) List(req acp.SkillListRequest) acp.SkillListResponse {
	// In a full implementation, this would enumerate skills from the skill runtime.
	// For now, return an empty list.
	return acp.SkillListResponse{
		Skills: []acp.SkillSummary{},
	}
}

// Manifest implements the acp.SkillInvoker interface.
// This method returns the manifest of a specific skill.
func (a *Agent) Manifest(req acp.SkillManifestRequest) (acp.SkillManifestResponse, error) {
	if req.SkillName == "" {
		return acp.SkillManifestResponse{}, fmt.Errorf("skill name cannot be empty")
	}

	// In a full implementation, this would load the skill manifest from the runtime.
	// For now, return a stub response.
	return acp.SkillManifestResponse{
		Name:   req.SkillName,
		Status: "loaded",
	}, nil
}

// Scan implements the acp.SecurityHandler interface.
// This method handles security/scan ACP requests.
func (a *Agent) Scan(req acp.SecurityScanRequest) (acp.SecurityScanResponse, error) {
	if req.Target == "" {
		return acp.SecurityScanResponse{}, fmt.Errorf("target cannot be empty")
	}

	// In a full implementation, this would delegate to the security engine.
	// For now, return an empty summary.
	return acp.SecurityScanResponse{
		Summary:  acp.SecurityAuditSummary{},
		Findings: []acp.SecurityVerdictNotification{},
		Duration: 0,
	}, nil
}

// Approve implements the acp.SecurityHandler interface.
// This method records security approval decisions from clients.
func (a *Agent) Approve(decision acp.SecurityApprovalResponse) error {
	if decision.Decision == "" {
		return fmt.Errorf("decision cannot be empty")
	}

	// In a full implementation, this would record the approval decision
	// and notify the security engine to proceed or escalate.
	return nil
}
