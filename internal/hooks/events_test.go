package hooks

import "testing"

func TestAllEvents(t *testing.T) {
	events := AllEvents()
	if len(events) != 23 {
		t.Errorf("expected 23 events, got %d", len(events))
	}
	seen := make(map[string]bool)
	for _, e := range events {
		if seen[e] {
			t.Errorf("duplicate event: %s", e)
		}
		seen[e] = true
	}
}
