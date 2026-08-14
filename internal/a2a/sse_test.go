package a2a

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSSEEvents(t *testing.T) {
	body := "event: message\ndata: {\"a\":1}\n\nevent: message\ndata: {\"a\":2}\n\n"
	ch := parseSSEEvents(strings.NewReader(body))

	first := <-ch
	assert.JSONEq(t, `{"a":1}`, string(first))
	second := <-ch
	assert.JSONEq(t, `{"a":2}`, string(second))

	_, ok := <-ch
	assert.False(t, ok, "channel should close after the stream ends")
}

// TestParseSSEEventsLogsScanError covers the case where a single "data:"
// line exceeds the scanner's 1MB buffer: bufio.Scanner stops with
// bufio.ErrTooLong, and without checking scanner.Err() after the loop, the
// channel just closes the same way it would on a clean EOF, leaving the
// failure invisible. This asserts the warning lands in the log instead.
func TestParseSSEEventsLogsScanError(t *testing.T) {
	oversized := "data: " + strings.Repeat("a", 2*1024*1024) + "\n\n"

	var logBuf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logBuf)
	defer func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	}()

	ch := parseSSEEvents(strings.NewReader(oversized))

	_, ok := <-ch
	assert.False(t, ok, "channel should close after a scan error")
	assert.Contains(t, logBuf.String(), "a2a sse stream ended with a scan error")
}
