package main

import (
	"strings"
	"testing"
)

// TestACPRequiresExplicitToolConsent pins the safety posture of the ACP mode.
//
// ACP's answer to tool approval is session/request_permission, which this agent
// does not implement — Conn.Request can express it, but nothing calls it. So an
// ACP-served agent has no way to ask the user before running a shell command.
//
// That leaves two honest options and one dishonest one. Refusing to start is
// honest. Requiring an explicit opt-in is honest. Silently auto-approving
// because the mode happens to be non-interactive is not: the editor that
// spawned the process would be granting arbitrary command execution on the
// user's machine without anyone saying so.
func TestACPRequiresExplicitToolConsent(t *testing.T) {
	prevAuto, prevApproveCwd := autoApprove, approveCwd
	t.Cleanup(func() { autoApprove, approveCwd = prevAuto, prevApproveCwd })

	autoApprove, approveCwd = false, false
	err := checkACPToolConsent()
	if err == nil {
		t.Fatal("ACP mode must not start without an explicit tool-approval choice")
	}
	if !strings.Contains(err.Error(), "session/request_permission") {
		t.Errorf("the refusal must name the missing capability so the reason is actionable, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--auto-approve") {
		t.Errorf("the refusal must name the flag that resolves it, got: %v", err)
	}
}

// TestACPAcceptsAnExplicitOptIn is the other half: having named the risk, the
// mode must actually be usable once the user accepts it.
func TestACPAcceptsAnExplicitOptIn(t *testing.T) {
	prevAuto, prevApproveCwd := autoApprove, approveCwd
	t.Cleanup(func() { autoApprove, approveCwd = prevAuto, prevApproveCwd })

	for _, tc := range []struct {
		name       string
		auto       bool
		approveCwd bool
	}{
		{"auto-approve", true, false},
		{"approve-cwd", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			autoApprove, approveCwd = tc.auto, tc.approveCwd
			if err := checkACPToolConsent(); err != nil {
				t.Errorf("an explicit opt-in must be accepted, got: %v", err)
			}
		})
	}
}
