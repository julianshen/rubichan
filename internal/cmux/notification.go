package cmux

// Notification represents a cmux notification entry.
type Notification struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Body     string `json:"body"`
}

// Notify creates a new notification via cmux.
func (c *Client) Notify(title, subtitle, body string) error {
	return c.callVoid("notification.create", map[string]string{
		"title": title, "subtitle": subtitle, "body": body,
	})
}

// ListNotifications retrieves all current notifications from cmux.
func (c *Client) ListNotifications() ([]Notification, error) {
	var notifications []Notification
	if err := c.callResult("notification.list", map[string]any{}, &notifications); err != nil {
		return nil, err
	}
	return notifications, nil
}

// ClearNotifications removes all notifications from cmux.
func (c *Client) ClearNotifications() error {
	return c.callVoid("notification.clear", map[string]any{})
}
