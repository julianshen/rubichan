package cmux

import "fmt"

// Notification represents a cmux notification entry.
type Notification struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Body     string `json:"body"`
}

// Notify creates a new notification via cmux.
func (c *Client) Notify(title, subtitle, body string) error {
	resp, err := c.Call("notification.create", map[string]string{
		"title":    title,
		"subtitle": subtitle,
		"body":     body,
	})
	if err != nil {
		return fmt.Errorf("cmux: notification.create: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: notification.create: %s", resp.Error)
	}
	return nil
}

// ListNotifications retrieves all current notifications from cmux.
func (c *Client) ListNotifications() ([]Notification, error) {
	resp, err := c.Call("notification.list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("cmux: notification.list: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("cmux: notification.list: %s", resp.Error)
	}
	var notifications []Notification
	if err := unmarshalResult(resp, &notifications); err != nil {
		return nil, fmt.Errorf("cmux: decode notifications: %w", err)
	}
	return notifications, nil
}

// ClearNotifications removes all notifications from cmux.
func (c *Client) ClearNotifications() error {
	resp, err := c.Call("notification.clear", map[string]any{})
	if err != nil {
		return fmt.Errorf("cmux: notification.clear: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("cmux: notification.clear: %s", resp.Error)
	}
	return nil
}
