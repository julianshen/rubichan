package agentsdk

// TombstoneMarker is the text content of a tombstoned message. It replaces
// the original content so the message is skipped when building API requests.
const TombstoneMarker = "[This message was tombstoned]"

// IsTombstoned reports whether content equals the tombstone marker string.
func IsTombstoned(content string) bool {
	return content == TombstoneMarker
}

// IsTombstonedMessage reports whether a message's first content block is
// the tombstone marker. Messages with no content blocks are not tombstoned.
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
