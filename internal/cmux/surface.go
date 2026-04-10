package cmux

import "fmt"

// Surface represents a cmux surface (pane, browser, etc.).
type Surface struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// Split creates a new surface by splitting in the given direction.
func (c *Client) Split(direction string) (*Surface, error) {
	resp, err := c.Call("surface.split", map[string]string{"direction": direction})
	if err != nil {
		return nil, fmt.Errorf("cmux: surface.split: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("cmux: surface.split: %s", resp.Error)
	}
	var surf Surface
	if err := unmarshalResult(resp, &surf); err != nil {
		return nil, fmt.Errorf("cmux: decode surface: %w", err)
	}
	return &surf, nil
}

// ListSurfaces retrieves all surfaces from cmux.
func (c *Client) ListSurfaces() ([]Surface, error) {
	resp, err := c.Call("surface.list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("cmux: surface.list: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("cmux: surface.list: %s", resp.Error)
	}
	var result struct {
		Surfaces []Surface `json:"surfaces"`
	}
	if err := unmarshalResult(resp, &result); err != nil {
		return nil, fmt.Errorf("cmux: decode surfaces: %w", err)
	}
	return result.Surfaces, nil
}

// FocusSurface focuses the surface with the given id.
func (c *Client) FocusSurface(id string) error {
	resp, err := c.Call("surface.focus", map[string]string{"surface_id": id})
	if err != nil {
		return fmt.Errorf("cmux: surface.focus: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: surface.focus: %s", resp.Error)
	}
	return nil
}

// SendText sends text input to the surface with the given id.
func (c *Client) SendText(surfaceID, text string) error {
	resp, err := c.Call("surface.send_text", map[string]string{
		"surface_id": surfaceID,
		"text":       text,
	})
	if err != nil {
		return fmt.Errorf("cmux: surface.send_text: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: surface.send_text: %s", resp.Error)
	}
	return nil
}

// SendKey sends a key event to the surface with the given id.
func (c *Client) SendKey(surfaceID, key string) error {
	resp, err := c.Call("surface.send_key", map[string]string{
		"surface_id": surfaceID,
		"key":        key,
	})
	if err != nil {
		return fmt.Errorf("cmux: surface.send_key: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: surface.send_key: %s", resp.Error)
	}
	return nil
}
