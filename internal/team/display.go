package team

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// TmuxController abstracts tmux operations for testing and mocking.
type TmuxController interface {
	Available() bool
	SessionExists(name string) bool
	KillSession(name string) error
	CreateSession(name string) error
	CreateWindow(sessionName, windowName string) (string, error)
	SendText(paneID, text string) error
}

const defaultSessionName = "rubichan-swarm"

const maxToolResultDisplayLen = 200

type agentDisplay struct {
	name   string
	paneID string
	color  string
	done   bool
}

// TmuxDisplay manages the tmux session and display for the swarm.
type TmuxDisplay struct {
	mu          sync.Mutex
	tmux        TmuxController
	sessionName string
	agents      map[string]*agentDisplay
	started     bool
	disabled    bool
}

func NewTmuxDisplay(tmux TmuxController) *TmuxDisplay {
	return NewTmuxDisplayWithSession(tmux, defaultSessionName)
}

func NewTmuxDisplayWithSession(tmux TmuxController, sessionName string) *TmuxDisplay {
	return &TmuxDisplay{
		tmux:        tmux,
		sessionName: sessionName,
		agents:      make(map[string]*agentDisplay),
	}
}

func (d *TmuxDisplay) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.tmux.Available() {
		d.disabled = true
		return nil
	}

	if d.tmux.SessionExists(d.sessionName) {
		if err := d.tmux.KillSession(d.sessionName); err != nil {
			return err
		}
	}

	if err := d.tmux.CreateSession(d.sessionName); err != nil {
		return err
	}

	d.started = true
	return nil
}

func (d *TmuxDisplay) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.started = false
	return nil
}

// Cleanup kills the session and clears all agents.
func (d *TmuxDisplay) Cleanup() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.disabled {
		for k := range d.agents {
			delete(d.agents, k)
		}
		d.started = false
		return nil
	}

	// Ignore error: session may already be dead.
	_ = d.tmux.KillSession(d.sessionName)
	for k := range d.agents {
		delete(d.agents, k)
	}
	d.started = false
	return nil
}

func (d *TmuxDisplay) IsActive() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.started && !d.disabled
}

func sanitizeWindowName(name string) string {
	return strings.NewReplacer(":", "-", ".", "-").Replace(name)
}

func (d *TmuxDisplay) AddAgent(agentID, name, color string) (string, error) {
	name = sanitizeWindowName(name)
	d.mu.Lock()
	disabled := d.disabled
	sessionName := d.sessionName
	d.mu.Unlock()

	if disabled {
		return "", nil
	}

	paneID, err := d.tmux.CreateWindow(sessionName, name)
	if err != nil {
		return "", err
	}

	header := fmt.Sprintf("=== Agent: %s [%s] ===", name, agentID)
	if err := d.tmux.SendText(paneID, header); err != nil {
		return "", err
	}

	d.mu.Lock()
	d.agents[agentID] = &agentDisplay{
		name:   name,
		paneID: paneID,
		color:  color,
	}
	d.mu.Unlock()

	return paneID, nil
}

func (d *TmuxDisplay) MarkDone(agentID string) error {
	d.mu.Lock()
	agent, exists := d.agents[agentID]
	if !exists || agent.done {
		d.mu.Unlock()
		return nil
	}
	agent.done = true
	paneID := agent.paneID
	name := agent.name
	d.mu.Unlock()

	footer := fmt.Sprintf("=== Agent %s finished ===", name)
	return d.tmux.SendText(paneID, footer)
}

func (d *TmuxDisplay) WriteEvent(agentID string, msg agentsdk.DisplayMessage) error {
	d.mu.Lock()
	if !d.started || d.disabled {
		d.mu.Unlock()
		return nil
	}
	agent, exists := d.agents[agentID]
	if !exists || agent.done {
		d.mu.Unlock()
		return nil
	}
	paneID := agent.paneID
	d.mu.Unlock()

	text := formatMessage(msg)
	if text == "" {
		return nil
	}
	return d.tmux.SendText(paneID, text)
}

func formatMessage(msg agentsdk.DisplayMessage) string {
	if len(msg.Content) == 0 {
		return ""
	}

	timestamp := time.Now().Format("15:04:05")
	lines := make([]string, 0, len(msg.Content))
	for _, block := range msg.Content {
		switch block.Type {
		case agentsdk.BlockTypeText:
			lines = append(lines, fmt.Sprintf("[%s] %s", timestamp, block.Text))
		case agentsdk.BlockTypeToolUse:
			keys := formatInputKeys(block.Input)
			lines = append(lines, fmt.Sprintf("[%s] [tool_use] %s(%s)", timestamp, block.Name, keys))
		case agentsdk.BlockTypeToolResult:
			tag := "[tool_result]"
			if block.IsError {
				tag = "[tool_result:error]"
			}
			content := truncate(string(block.Text), maxToolResultDisplayLen)
			lines = append(lines, fmt.Sprintf("[%s] %s %s", timestamp, tag, content))
		}
	}

	return strings.Join(lines, "\n")
}

func formatInputKeys(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
