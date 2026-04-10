package cmux

import "fmt"

// Workspace represents a cmux workspace.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListWorkspaces retrieves all workspaces from cmux.
func (c *Client) ListWorkspaces() ([]Workspace, error) {
	resp, err := c.Call("workspace.list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("cmux: workspace.list: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("cmux: workspace.list: %s", resp.Error)
	}
	var result struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := unmarshalResult(resp, &result); err != nil {
		return nil, fmt.Errorf("cmux: decode workspaces: %w", err)
	}
	return result.Workspaces, nil
}

// CreateWorkspace creates a new workspace in cmux.
func (c *Client) CreateWorkspace() (*Workspace, error) {
	resp, err := c.Call("workspace.create", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("cmux: workspace.create: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("cmux: workspace.create: %s", resp.Error)
	}
	var ws Workspace
	if err := unmarshalResult(resp, &ws); err != nil {
		return nil, fmt.Errorf("cmux: decode workspace: %w", err)
	}
	return &ws, nil
}

// SelectWorkspace switches cmux to the workspace with the given id.
func (c *Client) SelectWorkspace(id string) error {
	resp, err := c.Call("workspace.select", map[string]string{"workspace_id": id})
	if err != nil {
		return fmt.Errorf("cmux: workspace.select: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: workspace.select: %s", resp.Error)
	}
	return nil
}

// CurrentWorkspace retrieves the currently active workspace.
func (c *Client) CurrentWorkspace() (*Workspace, error) {
	resp, err := c.Call("workspace.current", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("cmux: workspace.current: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("cmux: workspace.current: %s", resp.Error)
	}
	var ws Workspace
	if err := unmarshalResult(resp, &ws); err != nil {
		return nil, fmt.Errorf("cmux: decode workspace: %w", err)
	}
	return &ws, nil
}

// CloseWorkspace closes the workspace with the given id.
func (c *Client) CloseWorkspace(id string) error {
	resp, err := c.Call("workspace.close", map[string]string{"workspace_id": id})
	if err != nil {
		return fmt.Errorf("cmux: workspace.close: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: workspace.close: %s", resp.Error)
	}
	return nil
}
