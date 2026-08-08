package a2a

import (
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
