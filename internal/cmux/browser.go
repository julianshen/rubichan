package cmux

import "fmt"

// BrowserNavigate navigates the browser surface to the given URL.
func (c *Client) BrowserNavigate(surfaceID, url string) error {
	resp, err := c.Call("browser.navigate", map[string]string{
		"surface_id": surfaceID,
		"url":        url,
	})
	if err != nil {
		return fmt.Errorf("cmux: browser.navigate: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: browser.navigate: %s", resp.Error)
	}
	return nil
}

// BrowserSnapshot returns the DOM snapshot of the browser surface.
func (c *Client) BrowserSnapshot(surfaceID string) (string, error) {
	resp, err := c.Call("browser.snapshot", map[string]string{"surface_id": surfaceID})
	if err != nil {
		return "", fmt.Errorf("cmux: browser.snapshot: %w", err)
	}
	if !resp.OK {
		return "", fmt.Errorf("cmux: browser.snapshot: %s", resp.Error)
	}
	var result struct {
		DOM string `json:"dom"`
	}
	if err := unmarshalResult(resp, &result); err != nil {
		return "", fmt.Errorf("cmux: decode snapshot: %w", err)
	}
	return result.DOM, nil
}

// BrowserClick clicks the element identified by ref in the browser surface.
func (c *Client) BrowserClick(surfaceID, ref string) error {
	resp, err := c.Call("browser.click", map[string]string{
		"surface_id": surfaceID,
		"ref":        ref,
	})
	if err != nil {
		return fmt.Errorf("cmux: browser.click: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: browser.click: %s", resp.Error)
	}
	return nil
}

// BrowserType types text into the element identified by ref in the browser surface.
func (c *Client) BrowserType(surfaceID, ref, text string) error {
	resp, err := c.Call("browser.type", map[string]string{
		"surface_id": surfaceID,
		"ref":        ref,
		"text":       text,
	})
	if err != nil {
		return fmt.Errorf("cmux: browser.type: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: browser.type: %s", resp.Error)
	}
	return nil
}

// BrowserWait waits for the browser surface to reach the given load state.
func (c *Client) BrowserWait(surfaceID, loadState string) error {
	resp, err := c.Call("browser.wait", map[string]string{
		"surface_id": surfaceID,
		"load_state": loadState,
	})
	if err != nil {
		return fmt.Errorf("cmux: browser.wait: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: browser.wait: %s", resp.Error)
	}
	return nil
}
