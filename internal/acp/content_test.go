package acp_test

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPromptContentAcceptsTheUngatedVariants pins the two ContentBlock variants
// an agent may always receive. text and resource_link carry no promptCapability
// of their own, so a client may send them against a handshake that declared
// nothing — which is exactly what this agent's handshake declares today.
func TestPromptContentAcceptsTheUngatedVariants(t *testing.T) {
	t.Parallel()

	blocks, err := acp.DecodePromptContent(json.RawMessage(`[
		{"type":"text","text":"summarise this repo"},
		{"type":"resource_link","uri":"file:///src/main.go","name":"main.go"}
	]`), acp.PromptCapabilities{})
	require.NoError(t, err)

	require.Len(t, blocks, 2)
	assert.Equal(t, acp.ContentText, blocks[0].Type)
	assert.Equal(t, "summarise this repo", blocks[0].Text)
	assert.Equal(t, acp.ContentResourceLink, blocks[1].Type)
	assert.Equal(t, "file:///src/main.go", blocks[1].URI)
	assert.Equal(t, "main.go", blocks[1].Name)
}

// TestPromptContentRejectsUndeclaredVariants is the honesty check between the
// handshake and the handler. image, audio and resource are each gated on a
// promptCapability. Accepting one this agent never declared is the same defect
// as declaring a capability no method backs — it just points the other way, and
// leaves the client believing an attachment was read when it was dropped.
func TestPromptContentRejectsUndeclaredVariants(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"image", `[{"type":"image","data":"aGk=","mimeType":"image/png"}]`},
		{"audio", `[{"type":"audio","data":"aGk=","mimeType":"audio/wav"}]`},
		{"resource", `[{"type":"resource","resource":{"text":"hi","uri":"file:///a"}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := acp.DecodePromptContent(json.RawMessage(tc.raw), acp.PromptCapabilities{})
			require.Error(t, err)
			assert.ErrorIs(t, err, acp.ErrInvalidParams,
				"the client's payload is wrong and it can correct it; -32602, not -32603")
			assert.Contains(t, err.Error(), tc.name)
		})
	}
}

// TestPromptContentAcceptsDeclaredVariants is the other half: the gate must key
// off what was actually declared, not off a hardcoded list. Otherwise flipping a
// capability on in the handshake would silently fail to flip it on here.
func TestPromptContentAcceptsDeclaredVariants(t *testing.T) {
	t.Parallel()

	caps := acp.PromptCapabilities{Image: true, Audio: true, EmbeddedContext: true}

	blocks, err := acp.DecodePromptContent(json.RawMessage(`[
		{"type":"image","data":"aGk=","mimeType":"image/png"},
		{"type":"audio","data":"aGk=","mimeType":"audio/wav"},
		{"type":"resource","resource":{"text":"hi","uri":"file:///a"}}
	]`), caps)
	require.NoError(t, err)
	assert.Len(t, blocks, 3)
}

// TestPromptContentRequiresTheVariantsOwnFields checks the fields the spec marks
// required on the two variants this agent actually accepts. A resource_link
// missing its name still decodes into a valid Go struct — the zero value is a
// legal empty string — so nothing but an explicit check catches it, and the
// agent would otherwise be handed a reference it cannot describe to the user.
func TestPromptContentRequiresTheVariantsOwnFields(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"text without text", `[{"type":"text"}]`, "text"},
		{"resource_link without uri", `[{"type":"resource_link","name":"main.go"}]`, "uri"},
		{"resource_link without name", `[{"type":"resource_link","uri":"file:///a"}]`, "name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := acp.DecodePromptContent(json.RawMessage(tc.raw), acp.PromptCapabilities{})
			require.Error(t, err)
			assert.ErrorIs(t, err, acp.ErrInvalidParams)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestPromptContentRejectsUnknownVariant keeps the union closed. An unrecognised
// type is not a forward-compatible no-op: the block carried instruction or
// context the agent is about to answer without.
func TestPromptContentRejectsUnknownVariant(t *testing.T) {
	t.Parallel()

	_, err := acp.DecodePromptContent(json.RawMessage(`[{"type":"video","uri":"file:///a.mp4"}]`),
		acp.PromptCapabilities{Image: true, Audio: true, EmbeddedContext: true})
	require.Error(t, err)
	assert.ErrorIs(t, err, acp.ErrInvalidParams)
}
