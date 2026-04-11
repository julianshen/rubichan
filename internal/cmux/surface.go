package cmux

// Surface represents a cmux surface (pane, browser, etc.).
type Surface struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// Split creates a new surface by splitting in the given direction.
func (c *Client) Split(direction string) (*Surface, error) {
	var surf Surface
	if err := c.callResult("surface.split", map[string]string{"direction": direction}, &surf); err != nil {
		return nil, err
	}
	return &surf, nil
}

// ListSurfaces retrieves all surfaces from cmux.
func (c *Client) ListSurfaces() ([]Surface, error) {
	var result struct {
		Surfaces []Surface `json:"surfaces"`
	}
	if err := c.callResult("surface.list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Surfaces, nil
}

// FocusSurface focuses the surface with the given id.
func (c *Client) FocusSurface(id string) error {
	return c.callVoid("surface.focus", map[string]string{"surface_id": id})
}

// SendText sends text input to the surface with the given id.
func (c *Client) SendText(surfaceID, text string) error {
	return c.callVoid("surface.send_text", map[string]string{
		"surface_id": surfaceID, "text": text,
	})
}

// SendKey sends a key event to the surface with the given id.
func (c *Client) SendKey(surfaceID, key string) error {
	return c.callVoid("surface.send_key", map[string]string{
		"surface_id": surfaceID, "key": key,
	})
}
