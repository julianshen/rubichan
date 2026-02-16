package anthropic

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSSEEvents(t *testing.T) {
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

	events, err := parseSSEEvents(strings.NewReader(input))
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

func TestParseSSEEventsEmpty(t *testing.T) {
	events, err := parseSSEEvents(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestParseSSEEventsWithToolUse(t *testing.T) {
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

	events, err := parseSSEEvents(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, events, 5)

	assert.Equal(t, "content_block_start", events[0].Event)
	assert.Contains(t, events[0].Data, `"tool_use"`)

	assert.Equal(t, "content_block_delta", events[1].Event)
	assert.Contains(t, events[1].Data, `input_json_delta`)

	assert.Equal(t, "message_stop", events[4].Event)
}

func TestParseSSEEventsSkipsComments(t *testing.T) {
	input := `: this is a comment
event: message_stop
data: {"type":"message_stop"}

`

	events, err := parseSSEEvents(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "message_stop", events[0].Event)
}
