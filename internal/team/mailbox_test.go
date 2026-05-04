package team

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestMailboxWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	msg := agentsdk.MailboxMessage{
		From: "alice",
		To:   "bob",
		Text: "hello",
		Type: agentsdk.MessageTypeText,
	}
	require.NoError(t, mb.Write("bob", msg))

	messages, err := mb.Read("bob")
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "alice", messages[0].From)
	require.Equal(t, "hello", messages[0].Text)
}

func TestMailboxReadEmpty(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	messages, err := mb.Read("nobody")
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestMailboxReadUnread(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg1", Type: agentsdk.MessageTypeText}))
	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg2", Type: agentsdk.MessageTypeText}))

	unread, err := mb.ReadUnread("bob")
	require.NoError(t, err)
	require.Len(t, unread, 2)

	require.NoError(t, mb.MarkRead("bob", 0))

	unread, err = mb.ReadUnread("bob")
	require.NoError(t, err)
	require.Len(t, unread, 1)
	require.Equal(t, "msg2", unread[0].Text)
}

func TestMailboxMarkReadOutOfRange(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	err := mb.MarkRead("bob", 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

func TestMailboxMarkAllRead(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg1", Type: agentsdk.MessageTypeText}))
	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg2", Type: agentsdk.MessageTypeText}))
	require.NoError(t, mb.MarkAllRead("bob"))

	unread, err := mb.ReadUnread("bob")
	require.NoError(t, err)
	require.Empty(t, unread)
}

func TestMailboxClear(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	require.NoError(t, mb.Write("bob", agentsdk.MailboxMessage{From: "alice", Text: "msg1", Type: agentsdk.MessageTypeText}))
	require.NoError(t, mb.Clear("bob"))

	messages, err := mb.Read("bob")
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestMailboxClearMissing(t *testing.T) {
	dir := t.TempDir()
	mb := NewMailbox(dir)

	require.NoError(t, mb.Clear("nobody"))
}

func TestFormatMessagesAsXML(t *testing.T) {
	messages := []agentsdk.MailboxMessage{
		{From: "alice", Text: "hello", Color: "\033[34m"},
		{From: "bob", Text: "world", Summary: "greeting"},
	}
	xml := FormatMessagesAsXML(messages)
	require.Contains(t, xml, `<teammate_message teammate_id="alice" color="`)
	require.Contains(t, xml, `hello`)
	require.Contains(t, xml, `<teammate_message teammate_id="bob" summary="greeting">`)
	require.Contains(t, xml, `world`)
}
