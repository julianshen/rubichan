package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEnvelope(t *testing.T) {
	payload := UserMessagePayload{Text: "hello"}
	env, err := NewEnvelope(TypeUserMessage, "sess-1", 5, payload)
	require.NoError(t, err)

	assert.Equal(t, TypeUserMessage, env.Type)
	assert.Equal(t, "sess-1", env.SessionID)
	assert.Equal(t, int64(5), env.Seq)
	assert.False(t, env.Timestamp.IsZero())

	var got UserMessagePayload
	require.NoError(t, env.ParsePayload(&got))
	assert.Equal(t, "hello", got.Text)
}

func TestNewEnvelope_MarshalError(t *testing.T) {
	// Channels cannot be marshaled.
	_, err := NewEnvelope(TypeEvent, "", 0, make(chan int))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marshal payload")
}

func TestEnvelope_ParsePayload_Empty(t *testing.T) {
	env := Envelope{Type: TypePing}
	var target map[string]any
	assert.NoError(t, env.ParsePayload(&target))
	assert.Nil(t, target)
}

func TestEnvelope_Validate(t *testing.T) {
	tests := []struct {
		name    string
		env     Envelope
		wantErr bool
	}{
		{
			name:    "valid",
			env:     Envelope{Type: TypePing},
			wantErr: false,
		},
		{
			name:    "empty type",
			env:     Envelope{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.env.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnvelope_JSON_RoundTrip(t *testing.T) {
	original := Envelope{
		Type:      TypeTurnEvent,
		SessionID: "abc-123",
		Seq:       42,
		Timestamp: time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC),
		Payload:   json.RawMessage(`{"text":"streaming chunk"}`),
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Envelope
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.SessionID, decoded.SessionID)
	assert.Equal(t, original.Seq, decoded.Seq)
	assert.True(t, original.Timestamp.Equal(decoded.Timestamp))
	assert.JSONEq(t, string(original.Payload), string(decoded.Payload))
}

func TestEnvelope_JSON_OmitsEmptyFields(t *testing.T) {
	env := Envelope{
		Type:      TypePong,
		Timestamp: time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(env)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.NotContains(t, raw, "session_id")
	assert.NotContains(t, raw, "seq")
	assert.NotContains(t, raw, "request_id")
}

func TestSessionCreatePayload_JSON(t *testing.T) {
	p := SessionCreatePayload{Model: "claude-opus-4-6"}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var decoded SessionCreatePayload
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "claude-opus-4-6", decoded.Model)
}

func TestSessionResumePayload_JSON(t *testing.T) {
	p := SessionResumePayload{SessionID: "s1", LastSeq: 99}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var decoded SessionResumePayload
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "s1", decoded.SessionID)
	assert.Equal(t, int64(99), decoded.LastSeq)
}

func TestSessionInfoPayload_JSON(t *testing.T) {
	p := SessionInfoPayload{SessionID: "s1", Status: "active", Model: "gpt-4"}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var decoded SessionInfoPayload
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "s1", decoded.SessionID)
	assert.Equal(t, "active", decoded.Status)
	assert.Equal(t, "gpt-4", decoded.Model)
}

func TestErrorPayload_JSON(t *testing.T) {
	p := ErrorPayload{Code: "gap_too_large", Message: "reconnect buffer exceeded"}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	var decoded ErrorPayload
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "gap_too_large", decoded.Code)
	assert.Equal(t, "reconnect buffer exceeded", decoded.Message)
}

func TestMessageTypeConstants(t *testing.T) {
	// Verify downstream types.
	assert.Equal(t, "event", TypeEvent)
	assert.Equal(t, "turn_event", TypeTurnEvent)
	assert.Equal(t, "ui_request", TypeUIRequest)
	assert.Equal(t, "ui_update", TypeUIUpdate)
	assert.Equal(t, "session_info", TypeSessionInfo)
	assert.Equal(t, "error", TypeError)
	assert.Equal(t, "pong", TypePong)

	// Verify upstream types.
	assert.Equal(t, "user_message", TypeUserMessage)
	assert.Equal(t, "ui_response", TypeUIResponse)
	assert.Equal(t, "session_create", TypeSessionCreate)
	assert.Equal(t, "session_resume", TypeSessionResume)
	assert.Equal(t, "session_list", TypeSessionList)
	assert.Equal(t, "cancel", TypeCancel)
	assert.Equal(t, "ping", TypePing)
}
