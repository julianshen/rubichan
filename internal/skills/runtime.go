package skills

import (
	"fmt"

	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
)

// BackendFactory creates a SkillBackend from a manifest. Implementations
// choose the correct backend type (Starlark, Go plugin, process) based on the
// manifest's Implementation.Backend field. The real factory is wired up
// during agent integration; tests supply a mock.
type BackendFactory func(manifest SkillManifest) (SkillBackend, error)

// SandboxFactory creates a PermissionChecker for a skill. This abstraction
// avoids a circular import between the skills and sandbox packages. The real
// factory calls sandbox.New; tests supply a mock.
type SandboxFactory func(skillName string, declared []Permission) PermissionChecker

// sourcePriority returns the hook dispatch priority for a skill source.
func sourcePriority(src Source) int {
	switch src {
	case SourceBuiltin:
		return PriorityBuiltin
	case SourceUser, SourceInline:
		return PriorityUser
	case SourceProject:
		return PriorityProject
	default:
		return PriorityProject
	}
}

// Runtime is the central orchestrator of the skill system. It ties together
// discovery (Loader), permission enforcement (SandboxFactory), hook dispatch
// (LifecycleManager), tool registration (tools.Registry), and backend
// creation (BackendFactory).
type Runtime struct {
	loader         *Loader
	store          *store.Store
	lifecycle      *LifecycleManager
	registry       *tools.Registry
	skills         map[string]*Skill
	active         map[string]*Skill
	autoApprove    []string
	backendFactory BackendFactory
	sandboxFactory SandboxFactory
}

// NewRuntime creates a Runtime with the given dependencies. The autoApprove
// list names skills that bypass store permission checks. The backendFactory
// and sandboxFactory allow the caller to control backend and sandbox creation,
// making the runtime fully testable without import cycles.
func NewRuntime(
	loader *Loader,
	s *store.Store,
	registry *tools.Registry,
	autoApprove []string,
	backendFactory BackendFactory,
	sandboxFactory SandboxFactory,
) *Runtime {
	return &Runtime{
		loader:         loader,
		store:          s,
		lifecycle:      NewLifecycleManager(),
		registry:       registry,
		skills:         make(map[string]*Skill),
		active:         make(map[string]*Skill),
		autoApprove:    autoApprove,
		backendFactory: backendFactory,
		sandboxFactory: sandboxFactory,
	}
}

// Discover uses the Loader to find all available skills and stores them in the
// runtime's skill map. Each discovered skill starts in SkillStateInactive.
// The explicit parameter lists skill names explicitly requested (e.g. via --skills).
func (rt *Runtime) Discover(explicit []string) error {
	discovered, _, err := rt.loader.Discover(explicit)
	if err != nil {
		return fmt.Errorf("discover skills: %w", err)
	}

	for _, ds := range discovered {
		rt.skills[ds.Manifest.Name] = &Skill{
			Manifest: ds.Manifest,
			State:    SkillStateInactive,
			Dir:      ds.Dir,
			Source:   ds.Source,
		}
	}

	return nil
}

// EvaluateAndActivate evaluates triggers against the given context, then
// activates all matching skills that are not yet active.
func (rt *Runtime) EvaluateAndActivate(ctx TriggerContext) error {
	// Build a DiscoveredSkill slice from the current skill map for trigger evaluation.
	var candidates []DiscoveredSkill
	for _, sk := range rt.skills {
		candidates = append(candidates, DiscoveredSkill{
			Manifest: sk.Manifest,
			Dir:      sk.Dir,
			Source:   sk.Source,
		})
	}

	matched := EvaluateTriggers(candidates, ctx)

	for _, ds := range matched {
		name := ds.Manifest.Name
		if _, alreadyActive := rt.active[name]; alreadyActive {
			continue
		}
		if err := rt.Activate(name); err != nil {
			return fmt.Errorf("activate skill %q: %w", name, err)
		}
	}

	return nil
}

// isAutoApproved returns true if the skill name is in the auto-approve list.
func (rt *Runtime) isAutoApproved(name string) bool {
	for _, n := range rt.autoApprove {
		if n == name {
			return true
		}
	}
	return false
}

// Activate transitions a skill from Inactive to Active. It creates a sandbox,
// a backend, loads the backend, registers tools, and registers hooks. If any
// step fails, the skill transitions to Error state.
func (rt *Runtime) Activate(name string) error {
	sk, ok := rt.skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}

	// Transition to Activating.
	if err := sk.TransitionTo(SkillStateActivating); err != nil {
		return fmt.Errorf("activate skill %q: %w", name, err)
	}

	// Create sandbox (permission checker).
	sb := rt.sandboxFactory(name, sk.Manifest.Permissions)

	// Check all declared permissions before loading.
	if !rt.isAutoApproved(name) {
		for _, perm := range sk.Manifest.Permissions {
			if err := sb.CheckPermission(perm); err != nil {
				// Transition to Error, then back to Inactive.
				_ = sk.TransitionTo(SkillStateError)
				_ = sk.TransitionTo(SkillStateInactive)
				return fmt.Errorf("activate skill %q: %w", name, err)
			}
		}
	}

	// Create backend.
	backend, err := rt.backendFactory(*sk.Manifest)
	if err != nil {
		_ = sk.TransitionTo(SkillStateError)
		_ = sk.TransitionTo(SkillStateInactive)
		return fmt.Errorf("create backend for skill %q: %w", name, err)
	}

	// Load backend.
	if err := backend.Load(*sk.Manifest, sb); err != nil {
		_ = sk.TransitionTo(SkillStateError)
		_ = sk.TransitionTo(SkillStateInactive)
		return fmt.Errorf("load skill %q: %w", name, err)
	}

	// Register tools.
	for _, tool := range backend.Tools() {
		if err := rt.registry.Register(tool); err != nil {
			_ = sk.TransitionTo(SkillStateError)
			_ = sk.TransitionTo(SkillStateInactive)
			return fmt.Errorf("register tool for skill %q: %w", name, err)
		}
	}

	// Register hooks.
	priority := sourcePriority(sk.Source)
	for phase, handler := range backend.Hooks() {
		rt.lifecycle.Register(phase, name, priority, handler)
	}

	// Store the backend reference and transition to Active.
	sk.Backend = backend
	if err := sk.TransitionTo(SkillStateActive); err != nil {
		return fmt.Errorf("activate skill %q: %w", name, err)
	}

	rt.active[name] = sk
	return nil
}

// Deactivate transitions a skill from Active to Inactive. It unregisters
// tools, unregisters hooks, calls backend.Unload, and clears the backend.
func (rt *Runtime) Deactivate(name string) error {
	sk, ok := rt.active[name]
	if !ok {
		return fmt.Errorf("skill %q is not active", name)
	}

	// Unregister tools.
	if sk.Backend != nil {
		for _, tool := range sk.Backend.Tools() {
			_ = rt.registry.Unregister(tool.Name())
		}
	}

	// Unregister hooks.
	rt.lifecycle.Unregister(name)

	// Unload backend.
	if sk.Backend != nil {
		if err := sk.Backend.Unload(); err != nil {
			return fmt.Errorf("unload skill %q: %w", name, err)
		}
	}

	// Transition to Inactive.
	if err := sk.TransitionTo(SkillStateInactive); err != nil {
		return fmt.Errorf("deactivate skill %q: %w", name, err)
	}

	sk.Backend = nil
	delete(rt.active, name)

	return nil
}

// GetActiveSkills returns a copy of the currently active skills.
func (rt *Runtime) GetActiveSkills() []*Skill {
	result := make([]*Skill, 0, len(rt.active))
	for _, sk := range rt.active {
		result = append(result, sk)
	}
	return result
}
