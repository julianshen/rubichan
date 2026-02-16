package anthropic

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectSSEEvents drains an sseScanner into a slice for testing.
func collectSSEEvents(r io.Reader) ([]sseEvent, error) {
	s := newSSEScanner(r)
	var events []sseEvent
	for s.Next() {
		events = append(events, s.Event())
	}
	return events, s.Err()
}

func TestSSEScannerEvents(t *testing.T) {
	input := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

`

	events, err := collectSSEEvents(strings.NewReader(input))
	require.NoError(t, err)

	require.Len(t, events, 6)

	assert.Equal(t, "message_start", events[0].Event)
	assert.Contains(t, events[0].Data, `"type":"message_start"`)

	assert.Equal(t, "content_block_start", events[1].Event)
	assert.Contains(t, events[1].Data, `"type":"content_block_start"`)

	assert.Equal(t, "content_block_delta", events[2].Event)
	assert.Contains(t, events[2].Data, `"text":"Hello"`)

	assert.Equal(t, "content_block_delta", events[3].Event)
	assert.Contains(t, events[3].Data, `"text":" world"`)

	assert.Equal(t, "content_block_stop", events[4].Event)

	assert.Equal(t, "message_stop", events[5].Event)
}

func TestSSEScannerEmpty(t *testing.T) {
	events, err := collectSSEEvents(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestSSEScannerWithToolUse(t *testing.T) {
	input := `event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"/tmp/test\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_stop
data: {"type":"message_stop"}

`

	events, err := collectSSEEvents(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, events, 5)

	assert.Equal(t, "content_block_start", events[0].Event)
	assert.Contains(t, events[0].Data, `"tool_use"`)

	assert.Equal(t, "content_block_delta", events[1].Event)
	assert.Contains(t, events[1].Data, `input_json_delta`)

	assert.Equal(t, "message_stop", events[4].Event)
}

func TestSSEScannerMultilineData(t *testing.T) {
	input := "event: test\ndata: first line\ndata: second line\n\n"
	events, err := collectSSEEvents(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "first line\nsecond line", events[0].Data)
}

func TestSSEScannerNoTrailingNewline(t *testing.T) {
	input := "event: test\ndata: some data"
	events, err := collectSSEEvents(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "test", events[0].Event)
	assert.Equal(t, "some data", events[0].Data)
}

func TestSSEScannerSkipsComments(t *testing.T) {
	input := `: this is a comment
event: message_stop
data: {"type":"message_stop"}

`

	events, err := collectSSEEvents(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "message_stop", events[0].Event)
}

func TestSSEScannerStreamsIncrementally(t *testing.T) {
	// Verify events are yielded one at a time via Next()
	input := "event: first\ndata: one\n\nevent: second\ndata: two\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	assert.Equal(t, "first", s.Event().Event)
	assert.Equal(t, "one", s.Event().Data)

	require.True(t, s.Next())
	assert.Equal(t, "second", s.Event().Event)
	assert.Equal(t, "two", s.Event().Data)

	assert.False(t, s.Next())
	assert.NoError(t, s.Err())
}
