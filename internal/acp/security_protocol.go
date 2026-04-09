package acp

import (
	"encoding/json"
)

// SecurityVerdictNotification is sent by server to inform client of a finding.
type SecurityVerdictNotification struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`   // "flagged", "approved", "escalated", "blocked"
	Severity   string  `json:"severity"` // "critical", "high", "medium", "low", "info"
	Message    string  `json:"message"`
	Confidence float64 `json:"confidence"`
	Evidence   string  `json:"evidence,omitempty"`
	File       string  `json:"file,omitempty"`
	Line       int     `json:"line,omitempty"`
}

// SecurityApprovalRequest is sent by server requesting user approval.
type SecurityApprovalRequest struct {
	VerdictID string   `json:"verdictID"`
	Severity  string   `json:"severity"`
	Message   string   `json:"message"`
	Evidence  string   `json:"evidence,omitempty"`
	File      string   `json:"file,omitempty"`
	Line      int      `json:"line,omitempty"`
	Options   []string `json:"options"`           // ["approve", "escalate", "block"]
	Timeout   int      `json:"timeout,omitempty"` // seconds
}

// SecurityApprovalResponse is sent by client with approval decision.
type SecurityApprovalResponse struct {
	Decision string `json:"decision"` // "approve", "escalate", "block"
	Reason   string `json:"reason,omitempty"`
	// For "escalate": approval goes to team/manager
	// For "block": execution halts
}

// SecurityScanRequest initiates a security scan.
type SecurityScanRequest struct {
	Scope       string   `json:"scope"`              // "file", "directory", "project"
	Target      string   `json:"target"`             // file path or directory
	Scanners    []string `json:"scanners,omitempty"` // specific scanners to run
	Interactive bool     `json:"interactive"`        // if true, request approvals; else log and continue
}

// SecurityScanResponse reports scan results.
type SecurityScanResponse struct {
	Summary  SecurityAuditSummary          `json:"summary"`
	Findings []SecurityVerdictNotification `json:"findings"`
	Duration float64                       `json:"duration"` // seconds
}

// SecurityAuditSummary aggregates security findings.
type SecurityAuditSummary struct {
	TotalFindings int `json:"totalFindings"`
	Critical      int `json:"critical"`
	High          int `json:"high"`
	Medium        int `json:"medium"`
	Low           int `json:"low"`
	Info          int `json:"info"`
	Approved      int `json:"approved"`
	Escalated     int `json:"escalated"`
	Blocked       int `json:"blocked"`
}

// SecurityHandler is the interface that handles security operations.
type SecurityHandler interface {
	Scan(req SecurityScanRequest) (SecurityScanResponse, error)
	Approve(decision SecurityApprovalResponse) error
}

// RegisterSecurityMethods registers security-related ACP methods.
func RegisterSecurityMethods(registry *CapabilityRegistry, securityHandler SecurityHandler) {
	registry.RegisterMethod("security/scan", func(params json.RawMessage) (json.RawMessage, error) {
		var req SecurityScanRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		resp, err := securityHandler.Scan(req)
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp)
	})

	registry.RegisterMethod("security/approve", func(params json.RawMessage) (json.RawMessage, error) {
		var decision SecurityApprovalResponse
		if err := json.Unmarshal(params, &decision); err != nil {
			return nil, err
		}
		if err := securityHandler.Approve(decision); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"status": "recorded"})
	})
}
