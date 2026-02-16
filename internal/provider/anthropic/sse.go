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

// sseScanner reads SSE events from an io.Reader one at a time,
// enabling true streaming instead of buffering all events.
// Usage follows the bufio.Scanner pattern:
//
//	s := newSSEScanner(r)
//	for s.Next() {
//	    evt := s.Event()
//	    // process evt
//	}
//	if err := s.Err(); err != nil { ... }
type sseScanner struct {
	scanner *bufio.Scanner
	event   sseEvent
	err     error
	done    bool
}

// newSSEScanner creates a streaming SSE parser over the given reader.
func newSSEScanner(r io.Reader) *sseScanner {
	return &sseScanner{scanner: bufio.NewScanner(r)}
}

// Next advances to the next SSE event. Returns false when no more events
// are available or an error occurred. Call Event() to retrieve the event
// and Err() to check for errors after Next returns false.
func (s *sseScanner) Next() bool {
	if s.done {
		return false
	}

	var current sseEvent
	hasData := false

	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Empty line signals end of current event
		if line == "" {
			if hasData || current.Event != "" {
				s.event = current
				return true
			}
			continue
		}

		// Skip SSE comments
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			current.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if hasData {
				current.Data += "\n" + data
			} else {
				current.Data = data
				hasData = true
			}
		}
	}

	s.err = s.scanner.Err()
	s.done = true

	// Handle stream ending without trailing empty line
	if hasData || current.Event != "" {
		s.event = current
		return true
	}

	return false
}

// Event returns the most recent SSE event read by Next.
func (s *sseScanner) Event() sseEvent {
	return s.event
}

// Err returns the first non-EOF error encountered by the scanner.
func (s *sseScanner) Err() error {
	return s.err
}
