package terminal

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKittyImage_SmallImage(t *testing.T) {
	// small data that base64-encodes to < 4096 bytes
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	var buf strings.Builder
	KittyImage(&buf, data)

	out := buf.String()
	assert.True(t, strings.HasPrefix(out, "\x1b_G"), "should start with APC introducer")
	assert.True(t, strings.HasSuffix(out, "\x1b\\"), "should end with ST")
	assert.Contains(t, out, "m=0", "single chunk should have m=0")
	assert.Contains(t, out, "a=T", "should have action=transmit")
}

func TestKittyImage_LargeImage(t *testing.T) {
	// 4096 raw bytes → base64 is ~5461 chars, requiring multiple chunks
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}
	var buf strings.Builder
	KittyImage(&buf, data)

	out := buf.String()
	chunks := strings.Split(out, "\x1b\\")
	// last element is empty string after final ST
	require.Greater(t, len(chunks), 2, "should produce multiple chunks")
	assert.True(t, strings.HasPrefix(chunks[0], "\x1b_G"), "first chunk starts with APC")
	assert.Contains(t, chunks[0], "m=1", "first chunk should have m=1 (more to come)")
}

func TestKittyImage_Empty(t *testing.T) {
	var buf strings.Builder
	KittyImage(&buf, nil)
	assert.Empty(t, buf.String())

	KittyImage(&buf, []byte{})
	assert.Empty(t, buf.String())
}

func TestKittyImage_Base64Chunking(t *testing.T) {
	// verify that last chunk has m=0
	data := make([]byte, 4096)
	var buf strings.Builder
	KittyImage(&buf, data)

	out := buf.String()
	// find the last APC sequence
	lastStart := strings.LastIndex(out, "\x1b_G")
	require.NotEqual(t, -1, lastStart)
	lastChunk := out[lastStart:]
	assert.Contains(t, lastChunk, "m=0", "last chunk should have m=0")
}

// helper to verify base64 round-trip for small image
func TestKittyImage_SmallImage_ContentIntegrity(t *testing.T) {
	data := []byte("hello kitty graphics")
	var buf strings.Builder
	KittyImage(&buf, data)

	out := buf.String()
	// extract base64 payload: after the semicolon up to \x1b\\
	semi := strings.Index(out, ";")
	require.NotEqual(t, -1, semi)
	end := strings.Index(out, "\x1b\\")
	require.NotEqual(t, -1, end)
	b64 := out[semi+1 : end]

	decoded, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err)
	assert.Equal(t, data, decoded)
}
