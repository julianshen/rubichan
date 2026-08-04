package acp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNumericID covers the type reconciliation that request/response
// correlation depends on. Marshalling a request writes the id as a JSON number
// and unmarshalling the peer's reply yields float64, so an id that survives the
// round trip only matches if both forms normalise to the same key. The previous
// dispatcher carried a whole normalizeID helper for this reason.
func TestNumericID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   any
		want int64
		ok   bool
	}{
		{"int64 as sent", int64(7), 7, true},
		{"int", 7, 7, true},
		{"float64 as decoded from JSON", float64(7), 7, true},
		{"json.Number", json.Number("7"), 7, true},
		{"non-numeric json.Number", json.Number("seven"), 0, false},
		{"string id is not correlatable here", "7", 0, false},
		{"absent id", nil, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := numericID(tc.in)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNumericIDRoundTripsThroughJSON is the case that actually bites: the id
// this side writes must match the id that comes back after encode/decode.
func TestNumericIDRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()

	sent := Request{JSONRPC: "2.0", ID: int64(42), Method: "session/prompt"}
	data, err := json.Marshal(sent)
	require.NoError(t, err)

	var back Response
	require.NoError(t, json.Unmarshal(data, &back))

	id, ok := numericID(back.ID)
	require.True(t, ok, "an id must survive the wire round trip")
	assert.Equal(t, int64(42), id, "otherwise responses correlate to nothing")
}

// TestMarshalParams pins that an already-encoded payload passes through
// untouched rather than being double-encoded into a JSON string.
func TestMarshalParams(t *testing.T) {
	t.Parallel()

	raw, err := marshalParams(json.RawMessage(`{"sessionId":"s1"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"sessionId":"s1"}`, string(raw))

	fromStruct, err := marshalParams(map[string]string{"sessionId": "s1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"sessionId":"s1"}`, string(fromStruct))

	none, err := marshalParams(nil)
	require.NoError(t, err)
	assert.Nil(t, none, "omitted params must stay absent, not become null")

	_, err = marshalParams(make(chan int))
	require.Error(t, err, "an unencodable payload must fail before it reaches the wire")
}
