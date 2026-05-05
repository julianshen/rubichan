package team

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// TeamConfig holds team-wide settings.
type TeamConfig struct {
	TeamName     string
	WorkspaceDir string
	MaxTeammates int
}

// NewTeamConfig creates a TeamConfig with defaults.
func NewTeamConfig(teamName, workspaceDir string) TeamConfig {
	return TeamConfig{
		TeamName:     teamName,
		WorkspaceDir: workspaceDir,
		MaxTeammates: 10,
	}
}

// TeammatesDir returns the directory for teammate state.
func (c TeamConfig) TeammatesDir() string {
	return filepath.Join(c.WorkspaceDir, ".claude", "teams", c.TeamName)
}

// InboxesDir returns the directory for mailboxes.
func (c TeamConfig) InboxesDir() string {
	return filepath.Join(c.TeammatesDir(), "inboxes")
}

// EnsureDirs creates the team directories.
func (c TeamConfig) EnsureDirs() error {
	if err := os.MkdirAll(c.TeammatesDir(), 0o755); err != nil {
		return fmt.Errorf("create teammates dir: %w", err)
	}
	if err := os.MkdirAll(c.InboxesDir(), 0o755); err != nil {
		return fmt.Errorf("create inboxes dir: %w", err)
	}
	return nil
}

var teammateColorPalette = []string{
	"\033[34m", // blue
	"\033[32m", // green
	"\033[33m", // yellow
	"\033[35m", // magenta
	"\033[36m", // cyan
	"\033[31m", // red
}

// AssignColor deterministically assigns a color to a name.
func AssignColor(name string) string {
	h := sha256.Sum256([]byte(name))
	idx := int(h[0]) % len(teammateColorPalette)
	return teammateColorPalette[idx]
}

var nextTeammateSeq uint64

// NewTeammateID creates a new TeammateID with auto-generated ID and color.
func NewTeammateID(agentName string) agentsdk.TeammateID {
	seq := atomic.AddUint64(&nextTeammateSeq, 1)
	agentID := fmt.Sprintf("tm-%d-%s", seq, agentName)
	return agentsdk.TeammateID{
		AgentID:   agentID,
		AgentName: agentName,
		Color:     AssignColor(agentName),
	}
}

// TeamRegistry tracks teammates by ID and name, thread-safe.
type TeamRegistry struct {
	teamName string
	mu       sync.RWMutex
	byID     map[string]agentsdk.TeammateID
	byName   map[string]agentsdk.TeammateID
}

// NewTeamRegistry creates a new registry.
func NewTeamRegistry(teamName string) *TeamRegistry {
	return &TeamRegistry{
		teamName: teamName,
		byID:     make(map[string]agentsdk.TeammateID),
		byName:   make(map[string]agentsdk.TeammateID),
	}
}

// TeamName returns the team name.
func (r *TeamRegistry) TeamName() string { return r.teamName }

// Register adds a teammate to the registry.
func (r *TeamRegistry) Register(id agentsdk.TeammateID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[id.AgentID] = id
	r.byName[id.AgentName] = id
}

// Get looks up a teammate by ID.
func (r *TeamRegistry) Get(agentID string) (agentsdk.TeammateID, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byID[agentID]
	return id, ok
}

// GetByName looks up a teammate by name.
func (r *TeamRegistry) GetByName(name string) (agentsdk.TeammateID, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byName[name]
	return id, ok
}

// Remove deletes a teammate from the registry.
func (r *TeamRegistry) Remove(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byID[agentID]; ok {
		delete(r.byName, id.AgentName)
		delete(r.byID, agentID)
	}
}

// List returns all registered teammates.
func (r *TeamRegistry) List() []agentsdk.TeammateID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]agentsdk.TeammateID, 0, len(r.byID))
	for _, id := range r.byID {
		result = append(result, id)
	}
	return result
}

// IsTeammate checks if an agent ID is in the registry.
func (r *TeamRegistry) IsTeammate(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byID[agentID]
	return ok
}
