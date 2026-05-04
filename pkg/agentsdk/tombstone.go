package agentsdk

// TombstoneMarker is the text content of a tombstoned message.
const TombstoneMarker = "[This message was tombstoned due to model fallback]"

// IsTombstoned checks if a message content is a tombstone marker.
func IsTombstoned(content string) bool {
	return content == TombstoneMarker
}

// IsTombstonedMessage checks if a message is tombstoned.
func IsTombstonedMessage(m Message) bool {
	if len(m.Content) == 0 {
		return false
	}
	return IsTombstoned(m.Content[0].Text)
}

// TombstoneReason explains why a message was tombstoned.
type TombstoneReason string

const (
	// TombstoneReasonModelFallback means the message was orphaned
	// when switching to a fallback model.
	TombstoneReasonModelFallback TombstoneReason = "model_fallback"
	// TombstoneReasonStreamError means the message was partial due
	// to a stream error.
	TombstoneReasonStreamError TombstoneReason = "stream_error"
	// TombstoneReasonUserAbort means the user aborted mid-stream.
	TombstoneReasonUserAbort TombstoneReason = "user_abort"
)
