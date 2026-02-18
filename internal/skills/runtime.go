package skills

import (
	"context"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
)

// BackendFactory creates a SkillBackend from a manifest. Implementations
// choose the correct backend type (Starlark, Go plugin, process) based on the
// manifest's Implementation.Backend field. The dir parameter is the skill's
// directory on disk, needed by backends like Starlark (to locate .star files)
// and Go plugin (to sandbox file operations). The real factory is wired up
// during agent integration; tests supply a mock.
type BackendFactory func(manifest SkillManifest, dir string) (SkillBackend, error)

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
	mu              sync.RWMutex
	loader          *Loader
	store           *store.Store
	lifecycle       *LifecycleManager
	registry        *tools.Registry
	skills          map[string]*Skill
	active          map[string]*Skill
	autoApprove     []string
	backendFactory  BackendFactory
	sandboxFactory  SandboxFactory
	promptCollector *PromptCollector
	workflowRunner  *WorkflowRunner
	securityAdapter *SecurityRuleAdapter
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
		loader:          loader,
		store:           s,
		lifecycle:       NewLifecycleManager(),
		registry:        registry,
		skills:          make(map[string]*Skill),
		active:          make(map[string]*Skill),
		autoApprove:     autoApprove,
		backendFactory:  backendFactory,
		sandboxFactory:  sandboxFactory,
		promptCollector: NewPromptCollector(),
		workflowRunner:  NewWorkflowRunner(),
		securityAdapter: NewSecurityRuleAdapter(),
	}
}

// Discover uses the Loader to find all available skills and stores them in the
// runtime's skill map. Each discovered skill starts in SkillStateInactive.
// The explicit parameter lists skill names explicitly requested (e.g. via --skills).
func (rt *Runtime) Discover(explicit []string) error {
	discovered, warnings, err := rt.loader.Discover(explicit)
	if err != nil {
		return fmt.Errorf("discover skills: %w", err)
	}

	// Log warnings if any were returned (previously discarded).
	_ = warnings // TODO: surface via logger or return value

	rt.mu.Lock()
	defer rt.mu.Unlock()

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
	rt.mu.RLock()
	// Build a DiscoveredSkill slice from the current skill map for trigger evaluation.
	var candidates []DiscoveredSkill
	for _, sk := range rt.skills {
		candidates = append(candidates, DiscoveredSkill{
			Manifest: sk.Manifest,
			Dir:      sk.Dir,
			Source:   sk.Source,
		})
	}
	rt.mu.RUnlock()

	matched := EvaluateTriggers(candidates, ctx)

	for _, ds := range matched {
		name := ds.Manifest.Name
		rt.mu.RLock()
		_, alreadyActive := rt.active[name]
		rt.mu.RUnlock()
		if alreadyActive {
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
//
// Backend creation and loading are performed outside the lock to avoid holding
// the mutex across potentially slow I/O (network, disk, process spawn).
func (rt *Runtime) Activate(name string) error {
	// Phase 1: Read skill metadata and transition to Activating under lock.
	rt.mu.Lock()
	sk, ok := rt.skills[name]
	if !ok {
		rt.mu.Unlock()
		return fmt.Errorf("skill %q not found", name)
	}

	// Guard against concurrent activation: if another goroutine already
	// activated or is activating this skill, return early.
	if _, alreadyActive := rt.active[name]; alreadyActive {
		rt.mu.Unlock()
		return nil
	}

	if err := sk.TransitionTo(SkillStateActivating); err != nil {
		rt.mu.Unlock()
		return fmt.Errorf("activate skill %q: %w", name, err)
	}

	// Snapshot the data we need outside the lock.
	manifest := *sk.Manifest
	permissions := sk.Manifest.Permissions
	source := sk.Source
	skillDir := sk.Dir
	autoApproved := rt.isAutoApproved(name)
	sandboxFactory := rt.sandboxFactory
	backendFactory := rt.backendFactory
	rt.mu.Unlock()

	// Phase 2: Create sandbox and backend outside the lock (may involve I/O).
	sb := sandboxFactory(name, permissions)

	if !autoApproved {
		for _, perm := range permissions {
			if err := sb.CheckPermission(perm); err != nil {
				rt.mu.Lock()
				_ = sk.TransitionTo(SkillStateError)
				_ = sk.TransitionTo(SkillStateInactive)
				rt.mu.Unlock()
				return fmt.Errorf("activate skill %q: %w", name, err)
			}
		}
	}

	backend, err := backendFactory(manifest, skillDir)
	if err != nil {
		rt.mu.Lock()
		_ = sk.TransitionTo(SkillStateError)
		_ = sk.TransitionTo(SkillStateInactive)
		rt.mu.Unlock()
		return fmt.Errorf("create backend for skill %q: %w", name, err)
	}

	if err := backend.Load(manifest, sb); err != nil {
		rt.mu.Lock()
		_ = sk.TransitionTo(SkillStateError)
		_ = sk.TransitionTo(SkillStateInactive)
		rt.mu.Unlock()
		return fmt.Errorf("load skill %q: %w", name, err)
	}

	// Phase 3: Register tools, hooks, and integrations under lock.
	rt.mu.Lock()
	defer rt.mu.Unlock()

	var registeredTools []tools.Tool
	for _, tool := range backend.Tools() {
		if err := rt.registry.Register(tool); err != nil {
			for _, t := range registeredTools {
				_ = rt.registry.Unregister(t.Name())
			}
			_ = sk.TransitionTo(SkillStateError)
			_ = sk.TransitionTo(SkillStateInactive)
			return fmt.Errorf("register tool for skill %q: %w", name, err)
		}
		registeredTools = append(registeredTools, tool)
	}

	priority := sourcePriority(source)
	for phase, handler := range backend.Hooks() {
		rt.lifecycle.Register(phase, name, priority, handler)
	}

	for _, st := range sk.Manifest.Types {
		switch st {
		case SkillTypeTool:
			// Tools are already registered above via backend.Tools().
		case SkillTypePrompt:
			wirePromptSkill(rt, sk)
		case SkillTypeWorkflow:
			wireWorkflowSkill(rt, sk)
		case SkillTypeSecurityRule:
			wireSecurityRuleSkill(rt, sk)
		case SkillTypeTransform:
			wireTransformSkill(rt, sk)
		}
	}

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
	rt.mu.Lock()
	defer rt.mu.Unlock()

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

	// Clean up integration state for all skill types.
	for _, st := range sk.Manifest.Types {
		switch st {
		case SkillTypePrompt:
			rt.promptCollector.RemoveBySkill(name)
		case SkillTypeWorkflow:
			rt.workflowRunner.Unregister(name)
		case SkillTypeSecurityRule:
			rt.securityAdapter.UnregisterBySkill(name)
		}
	}

	// Unload backend. Even if Unload fails, we still transition to Inactive
	// and remove from active map to avoid a stuck skill.
	var unloadErr error
	if sk.Backend != nil {
		unloadErr = sk.Backend.Unload()
	}

	// Always transition to Inactive and clean up regardless of unload error.
	_ = sk.TransitionTo(SkillStateInactive)
	sk.Backend = nil
	delete(rt.active, name)

	if unloadErr != nil {
		return fmt.Errorf("unload skill %q: %w", name, unloadErr)
	}

	return nil
}

// GetActiveSkills returns a copy of the currently active skills.
func (rt *Runtime) GetActiveSkills() []*Skill {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	result := make([]*Skill, 0, len(rt.active))
	for _, sk := range rt.active {
		result = append(result, sk)
	}
	return result
}

// GetPromptFragments returns the prompt fragments collected from all active
// prompt skills.
func (rt *Runtime) GetPromptFragments() []PromptFragment {
	return rt.promptCollector.Fragments()
}

// InvokeWorkflow executes a named workflow handler. Returns an error if no
// workflow is registered with the given name.
func (rt *Runtime) InvokeWorkflow(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	return rt.workflowRunner.Invoke(ctx, name, args)
}

// DispatchHook sends a hook event to all registered handlers for the event's
// phase via the lifecycle manager. This is the public entry point used by
// the agent loop to fire hooks at key points (before tool call, after tool
// result, etc.).
func (rt *Runtime) DispatchHook(event HookEvent) (*HookResult, error) {
	return rt.lifecycle.Dispatch(event)
}

// GetScanners returns the registered security scanners from all active
// security-rule skills.
func (rt *Runtime) GetScanners() []RegisteredScanner {
	return rt.securityAdapter.Scanners()
}
