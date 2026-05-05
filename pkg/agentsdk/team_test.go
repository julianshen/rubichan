package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMailboxMessageDefaults(t *testing.T) {
	msg := MailboxMessage{
		From: "alice",
		To:   "bob",
		Text: "hello",
		Type: MessageTypeText,
	}
	require.Equal(t, "alice", msg.From)
	require.Equal(t, "bob", msg.To)
	require.Equal(t, "hello", msg.Text)
	require.Equal(t, MessageTypeText, msg.Type)
	require.False(t, msg.Read)
	require.Nil(t, msg.Data)
}

func TestMessageTypeConstants(t *testing.T) {
	require.Equal(t, MessageType("text"), MessageTypeText)
	require.Equal(t, MessageType("shutdown_request"), MessageTypeShutdownRequest)
	require.Equal(t, MessageType("shutdown_approved"), MessageTypeShutdownApproved)
	require.Equal(t, MessageType("shutdown_rejected"), MessageTypeShutdownRejected)
	require.Equal(t, MessageType("task_update"), MessageTypeTaskUpdate)
	require.Equal(t, MessageType("idle"), MessageTypeIdle)
	require.Equal(t, MessageType("plan_approval"), MessageTypePlanApproval)
}
