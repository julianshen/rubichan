package a2a

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"strings"
)

// parseSSEEvents reads Server-Sent Events framed as "event: message\ndata:
// <json>\n\n" blocks and emits each event's data payload. It follows the
// same line-scanning shape as internal/tools/mcp's SSE transport
// (event:/data: prefix detection over a bufio.Scanner) adapted for A2A's
// single-response-stream model: no separate "endpoint" handshake event,
// every "message" event carries one JSON-RPC-wrapped Task snapshot.
func parseSSEEvents(body io.Reader) <-chan json.RawMessage {
	out := make(chan json.RawMessage, 8)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		var eventType string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				eventType = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				if eventType == "message" {
					out <- json.RawMessage(strings.TrimPrefix(line, "data: "))
				}
				eventType = ""
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("warning: a2a sse stream ended with a scan error: %v", err)
		}
	}()
	return out
}
