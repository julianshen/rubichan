package anthropic

import (
	"bufio"
	"io"
	"strings"
)

// sseEvent represents a single Server-Sent Event.
type sseEvent struct {
	Event string
	Data  string
}

// parseSSEEvents reads all SSE events from an io.Reader.
// Events are delimited by empty lines. Each event can have
// "event:" and "data:" fields. Lines starting with ":" are comments and are skipped.
func parseSSEEvents(r io.Reader) ([]sseEvent, error) {
	scanner := bufio.NewScanner(r)
	var events []sseEvent
	var currentEvent sseEvent
	hasData := false

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line signals end of current event
		if line == "" {
			if hasData || currentEvent.Event != "" {
				events = append(events, currentEvent)
				currentEvent = sseEvent{}
				hasData = false
			}
			continue
		}

		// Skip SSE comments
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			currentEvent.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if hasData {
				currentEvent.Data += "\n" + data
			} else {
				currentEvent.Data = data
				hasData = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return events, err
	}

	// Handle case where stream ends without trailing empty line
	if hasData || currentEvent.Event != "" {
		events = append(events, currentEvent)
	}

	return events, nil
}
