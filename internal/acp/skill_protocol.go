package acp

import (
	"encoding/json"
	"fmt"
)

// SkillInvokeRequest is a request to invoke a skill action.
type SkillInvokeRequest struct {
	SkillName string          `json:"skillName"`
	Action    string          `json:"action"` // "transform", "prompt", "workflow", etc.
	Input     json.RawMessage `json:"input"`
	Options   json.RawMessage `json:"options,omitempty"`
}

// SkillInvokeResponse is the response from a skill invocation.
type SkillInvokeResponse struct {
	SkillName string          `json:"skillName"`
	Action    string          `json:"action"`
	Output    json.RawMessage `json:"output"`
	Status    string          `json:"status"` // "success", "error", "escalate"
}

// SkillListRequest requests a list of available skills.
type SkillListRequest struct {
	Filter string `json:"filter,omitempty"` // optional filter by type: "all", "transform", "prompt", "workflow"
}

// SkillListResponse returns available skills.
type SkillListResponse struct {
	Skills []SkillSummary `json:"skills"`
}

// SkillSummary is a brief description of a skill.
type SkillSummary struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Type        string   `json:"type"`        // "starlark", "plugin", "process"
	Actions     []string `json:"actions"`     // ["transform", "prompt", "workflow"]
	Permissions []string `json:"permissions"` // ["file:read", "shell:exec"]
}

// SkillManifestRequest requests the manifest of a specific skill.
type SkillManifestRequest struct {
	SkillName string `json:"skillName"`
}

// SkillManifestResponse returns the full manifest of a skill.
type SkillManifestResponse struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Manifest    json.RawMessage `json:"manifest"` // Raw SKILL.yaml content
	Permissions []string        `json:"permissions"`
	Backend     string          `json:"backend"` // "starlark", "plugin", "process"
	Status      string          `json:"status"`  // "loaded", "error"
	Error       string          `json:"error,omitempty"`
}

// SkillApprovalRequest is a request for user approval of a skill action.
type SkillApprovalRequest struct {
	SkillName   string   `json:"skillName"`
	Action      string   `json:"action"`
	Permissions []string `json:"permissions"`
	Reason      string   `json:"reason"`
}

// SkillApprovalResponse is the user's response to an approval request.
type SkillApprovalResponse struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// SkillInvoker is the interface that handles skill invocations.
type SkillInvoker interface {
	Invoke(req SkillInvokeRequest) (SkillInvokeResponse, error)
	List(req SkillListRequest) SkillListResponse
	Manifest(req SkillManifestRequest) (SkillManifestResponse, error)
}

// RegisterSkillMethods registers the skill-related ACP methods.
func RegisterSkillMethods(registry *CapabilityRegistry, skillInvoker SkillInvoker) {
	registry.RegisterMethod("skill/invoke", func(params json.RawMessage) (json.RawMessage, error) {
		var req SkillInvokeRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("unmarshal skill invoke request: %w", err)
		}
		resp, err := skillInvoker.Invoke(req)
		if err != nil {
			return nil, fmt.Errorf("invoke skill: %w", err)
		}
		respData, err := json.Marshal(resp)
		if err != nil {
			return nil, fmt.Errorf("marshal skill response: %w", err)
		}
		return respData, nil
	})

	registry.RegisterMethod("skill/list", func(params json.RawMessage) (json.RawMessage, error) {
		var req SkillListRequest
		// Empty params are valid for list (returns all skills)
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("unmarshal skill list request: %w", err)
			}
		}
		resp := skillInvoker.List(req)
		return json.Marshal(resp)
	})

	registry.RegisterMethod("skill/manifest", func(params json.RawMessage) (json.RawMessage, error) {
		var req SkillManifestRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		resp, err := skillInvoker.Manifest(req)
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp)
	})
}
