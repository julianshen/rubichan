package cmux

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
	return c.callVoid("set-status", map[string]string{
		"key": key, "value": value, "icon": icon, "color": color,
	})
}

// ClearStatus removes the named status entry from the sidebar.
func (c *Client) ClearStatus(key string) error {
	return c.callVoid("clear-status", map[string]string{"key": key})
}

// SetProgress sets the sidebar progress indicator.
func (c *Client) SetProgress(fraction float64, label string) error {
	return c.callVoid("set-progress", map[string]any{"value": fraction, "label": label})
}

// ClearProgress removes the sidebar progress indicator.
func (c *Client) ClearProgress() error {
	return c.callVoid("clear-progress", map[string]any{})
}

// Log appends a message to the sidebar log section.
func (c *Client) Log(message, level, source string) error {
	return c.callVoid("log", map[string]string{
		"message": message, "level": level, "source": source,
	})
}

// ClearLog removes all entries from the sidebar log section.
func (c *Client) ClearLog() error {
	return c.callVoid("clear-log", map[string]any{})
}

// SidebarState retrieves the current state of the sidebar panel.
func (c *Client) SidebarState() (*SidebarState, error) {
	var state SidebarState
	if err := c.callResult("sidebar-state", map[string]any{}, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
