package cmux

// Workspace represents a cmux workspace.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListWorkspaces retrieves all workspaces from cmux.
func (c *Client) ListWorkspaces() ([]Workspace, error) {
	var result struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := c.callResult("workspace.list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Workspaces, nil
}

// CreateWorkspace creates a new workspace in cmux.
func (c *Client) CreateWorkspace() (*Workspace, error) {
	var ws Workspace
	if err := c.callResult("workspace.create", map[string]any{}, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// SelectWorkspace switches cmux to the workspace with the given id.
func (c *Client) SelectWorkspace(id string) error {
	return c.callVoid("workspace.select", map[string]string{"workspace_id": id})
}

// CurrentWorkspace retrieves the currently active workspace.
func (c *Client) CurrentWorkspace() (*Workspace, error) {
	var ws Workspace
	if err := c.callResult("workspace.current", map[string]any{}, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// CloseWorkspace closes the workspace with the given id.
func (c *Client) CloseWorkspace(id string) error {
	return c.callVoid("workspace.close", map[string]string{"workspace_id": id})
}
