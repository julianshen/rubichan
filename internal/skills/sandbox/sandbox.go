// Package sandbox enforces permission checks and rate limits for the skill
// system. It sits between skill backends and actual system operations,
// ensuring every SDK function call is authorized before execution.
package sandbox

import (
	"fmt"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
)

// SandboxPolicy defines rate limits and timeouts for a single turn.
type SandboxPolicy struct {
	MaxLLMCallsPerTurn  int
	MaxShellExecPerTurn int
	MaxNetFetchPerTurn  int
	ShellExecTimeout    time.Duration
	NetFetchTimeout     time.Duration
}

// DefaultPolicy returns sensible defaults for rate limits and timeouts.
func DefaultPolicy() SandboxPolicy {
	return SandboxPolicy{
		MaxLLMCallsPerTurn:  10,
		MaxShellExecPerTurn: 20,
		MaxNetFetchPerTurn:  10,
		ShellExecTimeout:    30 * time.Second,
		NetFetchTimeout:     15 * time.Second,
	}
}

// Sandbox holds the state for permission enforcement within a single skill.
// Methods that access counters are protected by a mutex for thread safety.
type Sandbox struct {
	mu          sync.Mutex
	store       *store.Store
	skill       string
	declared    map[skills.Permission]bool
	policy      SandboxPolicy
	autoApprove map[string]bool
	counters    map[string]int
}

// New creates a Sandbox for the given skill with declared permissions and policy.
func New(s *store.Store, skillName string, declared []skills.Permission, policy SandboxPolicy) *Sandbox {
	dm := make(map[skills.Permission]bool, len(declared))
	for _, p := range declared {
		dm[p] = true
	}
	return &Sandbox{
		store:       s,
		skill:       skillName,
		declared:    dm,
		policy:      policy,
		autoApprove: make(map[string]bool),
		counters:    make(map[string]int),
	}
}

// CheckPermission verifies that the permission is declared in the skill's
// manifest and that it has been approved (either via the store or auto-approve).
// Returns an error if the permission is not declared or not approved.
func (sb *Sandbox) CheckPermission(perm skills.Permission) error {
	if !sb.declared[perm] {
		return fmt.Errorf("permission %q not declared in skill %q manifest", perm, sb.skill)
	}

	// Auto-approved skills bypass the store check.
	if sb.autoApprove[sb.skill] {
		return nil
	}

	approved, err := sb.store.IsApproved(sb.skill, string(perm))
	if err != nil {
		return fmt.Errorf("check approval for %q: %w", perm, err)
	}
	if !approved {
		return fmt.Errorf("permission %q not approved for skill %q", perm, sb.skill)
	}

	return nil
}

// CheckRateLimit increments the counter for the given resource and returns
// an error if the per-turn limit has been exceeded.
func (sb *Sandbox) CheckRateLimit(resource string) error {
	limit := sb.limitFor(resource)
	if limit <= 0 {
		// No limit configured for this resource.
		return nil
	}

	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.counters[resource]++
	if sb.counters[resource] > limit {
		return fmt.Errorf("rate limit exceeded for %q: %d/%d per turn", resource, sb.counters[resource], limit)
	}

	return nil
}

// ResetTurnLimits zeros all rate limit counters, typically called at the
// start of a new agent turn.
func (sb *Sandbox) ResetTurnLimits() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.counters = make(map[string]int)
}

// SetAutoApprove sets which skills bypass store approval checks.
func (sb *Sandbox) SetAutoApprove(skillNames []string) {
	m := make(map[string]bool, len(skillNames))
	for _, name := range skillNames {
		m[name] = true
	}
	sb.autoApprove = m
}

// limitFor returns the per-turn limit for the given resource string.
func (sb *Sandbox) limitFor(resource string) int {
	switch resource {
	case "llm:call":
		return sb.policy.MaxLLMCallsPerTurn
	case "shell:exec":
		return sb.policy.MaxShellExecPerTurn
	case "net:fetch":
		return sb.policy.MaxNetFetchPerTurn
	default:
		return 0
	}
}
