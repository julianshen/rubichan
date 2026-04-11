package cmux

// BrowserNavigate navigates the browser surface to the given URL.
func (c *Client) BrowserNavigate(surfaceID, url string) error {
	return c.callVoid("browser.navigate", map[string]string{
		"surface_id": surfaceID, "url": url,
	})
}

// BrowserSnapshot returns the DOM snapshot of the browser surface.
func (c *Client) BrowserSnapshot(surfaceID string) (string, error) {
	var result struct {
		DOM string `json:"dom"`
	}
	if err := c.callResult("browser.snapshot", map[string]string{"surface_id": surfaceID}, &result); err != nil {
		return "", err
	}
	return result.DOM, nil
}

// BrowserClick clicks the element identified by ref in the browser surface.
func (c *Client) BrowserClick(surfaceID, ref string) error {
	return c.callVoid("browser.click", map[string]string{
		"surface_id": surfaceID, "ref": ref,
	})
}

// BrowserType types text into the element identified by ref in the browser surface.
func (c *Client) BrowserType(surfaceID, ref, text string) error {
	return c.callVoid("browser.type", map[string]string{
		"surface_id": surfaceID, "ref": ref, "text": text,
	})
}

// BrowserWait waits for the browser surface to reach the given load state.
func (c *Client) BrowserWait(surfaceID, loadState string) error {
	return c.callVoid("browser.wait", map[string]string{
		"surface_id": surfaceID, "load_state": loadState,
	})
}
