package ws

import (
	"encoding/json"
	"fmt"
	"time"
)

// Envelope is the wire format for all WebSocket messages.
type Envelope struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Seq       int64           `json:"seq,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
	RequestID string          `json:"request_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// Downstream message types (server → client).
const (
	TypeEvent             = "event"
	TypeTurnEvent         = "turn_event"
	TypeUIRequest         = "ui_request"
	TypeUIUpdate          = "ui_update"
	TypeSessionInfo       = "session_info"
	TypeError             = "error"
	TypeSessionListResult = "session_list_result"
	TypePong              = "pong"
)

// Upstream message types (client → server).
const (
	TypeUserMessage   = "user_message"
	TypeUIResponse    = "ui_response"
	TypeSessionCreate = "session_create"
	TypeSessionResume = "session_resume"
	TypeSessionList   = "session_list"
	TypeCancel        = "cancel"
	TypePing          = "ping"
)

// UserMessagePayload is the payload for "user_message" upstream messages.
type UserMessagePayload struct {
	Text string `json:"text"`
}

// SessionCreatePayload is the payload for "session_create" upstream messages.
type SessionCreatePayload struct {
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
}

// SessionResumePayload is the payload for "session_resume" upstream messages.
type SessionResumePayload struct {
	SessionID string `json:"session_id"`
	LastSeq   int64  `json:"last_seq"`
}

// SessionInfoPayload is the payload for "session_info" downstream messages.
type SessionInfoPayload struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Model     string `json:"model,omitempty"`
}

// ErrorPayload is the payload for "error" messages.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewEnvelope creates a downstream envelope with the given type and payload.
// The payload is marshaled to JSON. Timestamp is set to now UTC.
func NewEnvelope(msgType, sessionID string, seq int64, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal payload: %w", err)
	}
	return Envelope{
		Type:      msgType,
		SessionID: sessionID,
		Seq:       seq,
		Timestamp: time.Now().UTC(),
		Payload:   raw,
	}, nil
}

// ParsePayload unmarshals the envelope payload into the provided target.
func (e Envelope) ParsePayload(target any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(e.Payload, target)
}

// MarshalJSON implements a fast path that avoids re-marshaling Payload.
// The default encoding/json behavior already handles json.RawMessage correctly,
// so this is provided for documentation clarity — the struct tags are sufficient.

// Validate checks that the envelope has required fields.
// This is a minimal structural check; semantic validation happens at the handler level.
func (e Envelope) Validate() error {
	if e.Type == "" {
		return fmt.Errorf("envelope: missing type")
	}
	return nil
}
