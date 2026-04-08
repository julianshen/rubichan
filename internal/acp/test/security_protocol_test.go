package acp_test

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
)

func TestSecurityVerdictNotification(t *testing.T) {
	notif := acp.SecurityVerdictNotification{
		ID:         "sec-1",
		Status:     "flagged",
		Severity:   "high",
		Message:    "Hardcoded secret detected",
		Confidence: 0.95,
		File:       "config.py",
		Line:       42,
	}

	data, err := json.Marshal(notif)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SecurityVerdictNotification
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ID != "sec-1" {
		t.Errorf("got %q, want sec-1", decoded.ID)
	}
	if decoded.Severity != "high" {
		t.Errorf("got %q, want high", decoded.Severity)
	}
}

func TestSecurityApprovalRequest(t *testing.T) {
	req := acp.SecurityApprovalRequest{
		VerdictID: "sec-1",
		Severity:  "high",
		Message:   "API key in environment variable",
		File:      ".env.example",
		Options:   []string{"approve", "escalate", "block"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SecurityApprovalRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Options) != 3 {
		t.Errorf("got %d options, want 3", len(decoded.Options))
	}
}

func TestSecurityApprovalResponse(t *testing.T) {
	resp := acp.SecurityApprovalResponse{
		Decision: "approve",
		Reason:   "False positive; this is a template",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SecurityApprovalResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Decision != "approve" {
		t.Errorf("got %q, want approve", decoded.Decision)
	}
}

func TestSecurityScanRequest(t *testing.T) {
	req := acp.SecurityScanRequest{
		Scope:       "project",
		Target:      "./",
		Scanners:    []string{"secret_scanner", "injection_scanner"},
		Interactive: true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SecurityScanRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Scope != "project" {
		t.Errorf("got %q, want project", decoded.Scope)
	}
}

func TestSecurityScanResponse(t *testing.T) {
	resp := acp.SecurityScanResponse{
		Summary: acp.SecurityAuditSummary{
			TotalFindings: 3,
			Critical:      0,
			High:          2,
			Medium:        1,
			Low:           0,
			Info:          0,
			Approved:      1,
			Escalated:     0,
			Blocked:       0,
		},
		Findings: []acp.SecurityVerdictNotification{
			{
				ID:       "sec-1",
				Status:   "flagged",
				Severity: "high",
				Message:  "Finding 1",
			},
		},
		Duration: 2.5,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SecurityScanResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Summary.TotalFindings != 3 {
		t.Errorf("got %d findings, want 3", decoded.Summary.TotalFindings)
	}
	if len(decoded.Findings) != 1 {
		t.Errorf("got %d findings, want 1", len(decoded.Findings))
	}
}

func TestSecurityAuditSummary(t *testing.T) {
	summary := acp.SecurityAuditSummary{
		TotalFindings: 5,
		Critical:      1,
		High:          2,
		Medium:        1,
		Low:           1,
		Info:          0,
		Approved:      1,
		Escalated:     0,
		Blocked:       0,
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SecurityAuditSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.TotalFindings != 5 {
		t.Errorf("got %d, want 5", decoded.TotalFindings)
	}
	if decoded.Critical != 1 {
		t.Errorf("got %d critical, want 1", decoded.Critical)
	}
}

func TestRegisterSecurityMethods(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	handler := &MockSecurityHandler{}

	acp.RegisterSecurityMethods(registry, handler)

	methods := registry.GetMethods()
	if len(methods) != 2 {
		t.Errorf("got %d methods, want 2", len(methods))
	}

	// Test security/scan method
	params := json.RawMessage(`{"scope":"project","target":"./","interactive":false}`)
	result, err := registry.Call("security/scan", params)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var resp acp.SecurityScanResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !handler.scanCalled {
		t.Error("expected Scan to be called")
	}

	// Test security/approve method
	approveParams := json.RawMessage(`{"decision":"approve","reason":"test"}`)
	_, err = registry.Call("security/approve", approveParams)
	if err != nil {
		t.Fatalf("Approve call failed: %v", err)
	}

	if !handler.approveCalled {
		t.Error("expected Approve to be called")
	}
}

// MockSecurityHandler for testing
type MockSecurityHandler struct {
	scanCalled    bool
	approveCalled bool
}

func (m *MockSecurityHandler) Scan(req acp.SecurityScanRequest) (acp.SecurityScanResponse, error) {
	m.scanCalled = true
	return acp.SecurityScanResponse{
		Summary: acp.SecurityAuditSummary{
			TotalFindings: 0,
		},
		Findings: []acp.SecurityVerdictNotification{},
		Duration: 0.1,
	}, nil
}

func (m *MockSecurityHandler) Approve(decision acp.SecurityApprovalResponse) error {
	m.approveCalled = true
	return nil
}
