package hooks

import (
	"crypto/sha256"
	"fmt"
)

// HookApprovalStore persists hook configuration approvals keyed by project path
// and hook hash.
type HookApprovalStore interface {
	CheckHookApproval(projectPath, hookHash string) (bool, error)
	ApproveHook(projectPath, hookHash string) error
}

// CheckTrust returns true if the given hook set has been previously approved
// for the project path.
func CheckTrust(s HookApprovalStore, projectPath string, hooks []UserHookConfig) (bool, error) {
	hash := computeHookHash(hooks)
	return s.CheckHookApproval(projectPath, hash)
}

// ApproveTrust records approval of the given hook set for the project path.
func ApproveTrust(s HookApprovalStore, projectPath string, hooks []UserHookConfig) error {
	hash := computeHookHash(hooks)
	return s.ApproveHook(projectPath, hash)
}

// computeHookHash returns a deterministic SHA-256 hash over the event+command
// pairs in the hook list. Changing any command or adding/removing hooks
// invalidates the hash.
func computeHookHash(hooks []UserHookConfig) string {
	h := sha256.New()
	for _, hk := range hooks {
		fmt.Fprintf(h, "%s:%s\n", hk.Event, hk.Command)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
