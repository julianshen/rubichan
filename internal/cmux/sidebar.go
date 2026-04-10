package cmux

import "fmt"

// SidebarState holds the current state of the cmux sidebar panel.
type SidebarState struct {
	CWD       string     `json:"cwd"`
	GitBranch string     `json:"git_branch"`
	Ports     []int      `json:"ports"`
	Status    []Status   `json:"status"`
	Progress  *Progress  `json:"progress"`
	Logs      []LogEntry `json:"logs"`
}

// Status is a named key/value pair shown in the sidebar status section.
type Status struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// Progress represents a progress indicator shown in the sidebar.
type Progress struct {
	Value float64 `json:"value"`
	Label string  `json:"label"`
}

// LogEntry is a single log message shown in the sidebar log section.
type LogEntry struct {
	Message string `json:"message"`
	Level   string `json:"level"`
	Source  string `json:"source"`
}

// SetStatus sets a named status entry in the sidebar.
func (c *Client) SetStatus(key, value, icon, color string) error {
	resp, err := c.Call("set-status", map[string]string{
		"key":   key,
		"value": value,
		"icon":  icon,
		"color": color,
	})
	if err != nil {
		return fmt.Errorf("cmux: set-status: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: set-status: %s", resp.Error)
	}
	return nil
}

// ClearStatus removes the named status entry from the sidebar.
func (c *Client) ClearStatus(key string) error {
	resp, err := c.Call("clear-status", map[string]string{"key": key})
	if err != nil {
		return fmt.Errorf("cmux: clear-status: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: clear-status: %s", resp.Error)
	}
	return nil
}

// SetProgress sets the sidebar progress indicator.
func (c *Client) SetProgress(fraction float64, label string) error {
	resp, err := c.Call("set-progress", map[string]any{
		"value": fraction,
		"label": label,
	})
	if err != nil {
		return fmt.Errorf("cmux: set-progress: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: set-progress: %s", resp.Error)
	}
	return nil
}

// ClearProgress removes the sidebar progress indicator.
func (c *Client) ClearProgress() error {
	resp, err := c.Call("clear-progress", map[string]any{})
	if err != nil {
		return fmt.Errorf("cmux: clear-progress: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: clear-progress: %s", resp.Error)
	}
	return nil
}

// Log appends a message to the sidebar log section.
func (c *Client) Log(message, level, source string) error {
	resp, err := c.Call("log", map[string]string{
		"message": message,
		"level":   level,
		"source":  source,
	})
	if err != nil {
		return fmt.Errorf("cmux: log: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: log: %s", resp.Error)
	}
	return nil
}

// ClearLog removes all entries from the sidebar log section.
func (c *Client) ClearLog() error {
	resp, err := c.Call("clear-log", map[string]any{})
	if err != nil {
		return fmt.Errorf("cmux: clear-log: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: clear-log: %s", resp.Error)
	}
	return nil
}

// SidebarState retrieves the current state of the sidebar panel.
func (c *Client) SidebarState() (*SidebarState, error) {
	resp, err := c.Call("sidebar-state", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("cmux: sidebar-state: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("cmux: sidebar-state: %s", resp.Error)
	}
	var state SidebarState
	if err := unmarshalResult(resp, &state); err != nil {
		return nil, fmt.Errorf("cmux: decode sidebar-state: %w", err)
	}
	return &state, nil
}
