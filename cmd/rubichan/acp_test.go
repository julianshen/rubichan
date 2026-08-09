package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
)

// TestACPDoesNotAutoApproveWithoutTheFlag replaces the gate that used to refuse
// to start this mode at all.
//
// The refusal existed because a served agent could not ask anyone: ACP's
// session/request_permission was unimplemented, so the only two postures were
// "run everything unattended" and "do not serve". Now that the agent can ask,
// the flag reverts to meaning what it means everywhere else — skip the user —
// and the invariant worth pinning is that its absence does not silently skip
// them anyway. AlwaysAutoApprove short-circuits the approval flow before the
// client is ever consulted, so adding it unconditionally would put a permission
// prompt in the code that no user ever sees.
func TestACPDoesNotAutoApproveWithoutTheFlag(t *testing.T) {
	prevAuto, prevApproveCwd := autoApprove, approveCwd
	t.Cleanup(func() { autoApprove, approveCwd = prevAuto, prevApproveCwd })

	// --approve-cwd is set to pin the correction it already carried: it governs
	// folder access, and grants no licence to run tools unattended.
	autoApprove, approveCwd = false, true

	checkers := acpApprovalCheckers(&config.Config{}, t.TempDir())
	got := agent.NewCompositeApprovalChecker(checkers...).CheckApproval("shell", json.RawMessage(`{"command":"rm -rf /"}`))

	if got == agent.AutoApproved || got == agent.TrustRuleApproved {
		t.Fatalf("without --auto-approve a shell call must reach the user, got %v", got)
	}
}

// TestACPAutoApprovesOnTheExplicitOptIn is the other half: a user who accepts
// unattended execution gets it, and the client is not asked about every call.
func TestACPAutoApprovesOnTheExplicitOptIn(t *testing.T) {
	prevAuto, prevApproveCwd := autoApprove, approveCwd
	t.Cleanup(func() { autoApprove, approveCwd = prevAuto, prevApproveCwd })

	autoApprove, approveCwd = true, false

	checkers := acpApprovalCheckers(&config.Config{}, t.TempDir())
	got := agent.NewCompositeApprovalChecker(checkers...).CheckApproval("shell", json.RawMessage(`{"command":"ls"}`))

	if got != agent.AutoApproved {
		t.Fatalf("--auto-approve must skip the prompt, got %v", got)
	}
}

// TestACPApprovalWithoutAClientDenies covers the backstop.
//
// The agent's approval flow prefers the UI handler and falls back to this
// function, so it runs only if the handler was never installed — a wiring
// mistake rather than a user decision. Returning true there, as this mode did
// while it demanded --auto-approve, would turn that mistake into silent
// unattended execution.
func TestACPApprovalWithoutAClientDenies(t *testing.T) {
	approved, err := acpApprovalWithoutAClient(context.Background(), "shell", json.RawMessage(`{}`))

	if approved {
		t.Fatal("a tool must not run when nobody was asked")
	}
	if err == nil || !strings.Contains(err.Error(), "shell") {
		t.Errorf("the refusal must name the tool it refused, got: %v", err)
	}
}
