package team

import (
	"context"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Spawner creates a new agent process for a teammate.
type Spawner interface {
	Spawn(ctx context.Context, req agentsdk.SpawnRequest) error
}

// Display is an optional UI integration for the coordinator.
type Display interface {
	AddAgent(agentID, name, color string) (string, error)
	MarkDone(agentID string) error
	Stop() error
	IsActive() bool
}

// CoordinatorOption configures a Coordinator.
type CoordinatorOption func(*Coordinator)

// WithDisplay sets the display integration.
func WithDisplay(d Display) CoordinatorOption {
	return func(c *Coordinator) { c.display = d }
}

// Coordinator manages team lifecycle: spawn, message, shutdown.
type Coordinator struct {
	cfg      TeamConfig
	registry *TeamRegistry
	mailbox  *Mailbox
	spawner  Spawner
	display  Display // nil if not configured
	mu       sync.RWMutex
}

// NewCoordinator creates a coordinator with the given config and spawner.
func NewCoordinator(cfg TeamConfig, spawner Spawner, opts ...CoordinatorOption) (*Coordinator, error) {
	if err := cfg.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("ensure team dirs: %w", err)
	}
	c := &Coordinator{
		cfg:      cfg,
		registry: NewTeamRegistry(cfg.TeamName),
		mailbox:  NewMailbox(cfg.InboxesDir()),
		spawner:  spawner,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// SpawnTeammate creates a new teammate if under max and not duplicate.
func (c *Coordinator) SpawnTeammate(ctx context.Context, req agentsdk.SpawnRequest) (*agentsdk.TeammateID, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("spawn teammate %q: %w", req.AgentName, err)
	}

	c.mu.Lock()

	if _, exists := c.registry.GetByName(req.AgentName); exists {
		c.mu.Unlock()
		return nil, fmt.Errorf("teammate %q already exists", req.AgentName)
	}

	if len(c.registry.List()) >= c.cfg.MaxTeammates {
		c.mu.Unlock()
		return nil, fmt.Errorf("max teammates (%d) exceeded", c.cfg.MaxTeammates)
	}

	if err := c.mailbox.EnsureDir(); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("mailbox ensure: %w", err)
	}

	tid := NewTeammateID(req.AgentName)
	c.registry.Register(tid)

	if err := c.spawner.Spawn(ctx, req); err != nil {
		c.registry.Remove(tid.AgentID)
		c.mu.Unlock()
		return nil, fmt.Errorf("spawn teammate %q: %w", req.AgentName, err)
	}

	display := c.display
	c.mu.Unlock()

	// Display I/O outside lock to avoid blocking concurrent operations.
	if display != nil {
		_, _ = display.AddAgent(req.AgentName, req.AgentName, "")
	}

	return &tid, nil
}

// SendMessage sends a direct message or broadcasts if to == "*".
func (c *Coordinator) SendMessage(from, to, text string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if to == "*" {
		return c.broadcast(from, text)
	}

	if from == to {
		return fmt.Errorf("cannot send message to self")
	}

	if _, ok := c.registry.GetByName(to); !ok {
		return fmt.Errorf("unknown teammate %q", to)
	}

	sender, ok := c.registry.GetByName(from)
	if !ok {
		return fmt.Errorf("unknown sender %q", from)
	}

	msg := agentsdk.MailboxMessage{
		From:  from,
		To:    to,
		Text:  text,
		Type:  agentsdk.MessageTypeText,
		Color: sender.Color,
	}
	return c.mailbox.Write(to, msg)
}

func (c *Coordinator) broadcast(from, text string) error {
	sender, ok := c.registry.GetByName(from)
	if !ok {
		return fmt.Errorf("unknown sender %q", from)
	}

	for _, tid := range c.registry.List() {
		if tid.AgentName == from {
			continue
		}
		msg := agentsdk.MailboxMessage{
			From:  from,
			To:    tid.AgentName,
			Text:  text,
			Type:  agentsdk.MessageTypeText,
			Color: sender.Color,
		}
		if err := c.mailbox.Write(tid.AgentName, msg); err != nil {
			return err
		}
	}
	return nil
}

// ShutdownTeammate sends a shutdown request to a teammate.
func (c *Coordinator) ShutdownTeammate(targetName, leaderName string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	target, ok := c.registry.GetByName(targetName)
	if !ok {
		return fmt.Errorf("unknown teammate %q", targetName)
	}

	msg := agentsdk.MailboxMessage{
		From: leaderName,
		To:   targetName,
		Type: agentsdk.MessageTypeShutdownRequest,
		Data: map[string]interface{}{"request_id": fmt.Sprintf("shutdown-%s", target.AgentID)},
	}
	return c.mailbox.Write(targetName, msg)
}

// ShutdownAll sends shutdown requests to all teammates except the leader.
func (c *Coordinator) ShutdownAll(leaderName string) error {
	c.mu.RLock()
	teammates := c.registry.List()
	c.mu.RUnlock()

	for _, tid := range teammates {
		if tid.AgentName == leaderName {
			continue
		}
		msg := agentsdk.MailboxMessage{
			From: leaderName,
			To:   tid.AgentName,
			Type: agentsdk.MessageTypeShutdownRequest,
			Data: map[string]interface{}{"request_id": fmt.Sprintf("shutdown-%s", tid.AgentID)},
		}
		if err := c.mailbox.Write(tid.AgentName, msg); err != nil {
			return err
		}
		if c.display != nil {
			_ = c.display.MarkDone(tid.AgentName)
		}
	}
	if c.display != nil {
		_ = c.display.Stop()
	}
	return nil
}

// ListTeammates returns all registered teammates.
func (c *Coordinator) ListTeammates() []agentsdk.TeammateID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.registry.List()
}

// GetTeammate looks up a teammate by agent ID.
func (c *Coordinator) GetTeammate(agentID string) (agentsdk.TeammateID, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.registry.Get(agentID)
}

// RemoveTeammate removes a teammate from the registry.
func (c *Coordinator) RemoveTeammate(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registry.Remove(agentID)
}

// Registry returns the underlying TeamRegistry.
func (c *Coordinator) Registry() *TeamRegistry {
	return c.registry
}

// Mailbox returns the underlying Mailbox.
func (c *Coordinator) Mailbox() *Mailbox {
	return c.mailbox
}

// DisplayActive returns whether the display is active.
func (c *Coordinator) DisplayActive() bool {
	if c.display == nil {
		return false
	}
	return c.display.IsActive()
}
