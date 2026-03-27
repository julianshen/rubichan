package shell

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistory_AddAndRetrieve(t *testing.T) {
	t.Parallel()
	h := NewCommandHistory(100)

	h.Add("ls -la")
	h.Add("git status")
	h.Add("go test ./...")

	entries := h.Entries()
	assert.Equal(t, []string{"ls -la", "git status", "go test ./..."}, entries)
}

func TestHistory_DuplicateSuppression(t *testing.T) {
	t.Parallel()
	h := NewCommandHistory(100)

	h.Add("ls -la")
	h.Add("ls -la")
	h.Add("git status")
	h.Add("git status")

	entries := h.Entries()
	assert.Equal(t, []string{"ls -la", "git status"}, entries)
}

func TestHistory_MaxSize(t *testing.T) {
	t.Parallel()
	h := NewCommandHistory(3)

	h.Add("cmd1")
	h.Add("cmd2")
	h.Add("cmd3")
	h.Add("cmd4")

	entries := h.Entries()
	assert.Len(t, entries, 3)
	assert.Equal(t, []string{"cmd2", "cmd3", "cmd4"}, entries)
}

func TestHistory_Persistence(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "history.txt")

	h1 := NewCommandHistory(100)
	h1.Add("cmd1")
	h1.Add("cmd2")
	err := h1.Save(path)
	require.NoError(t, err)

	h2 := NewCommandHistory(100)
	err = h2.Load(path)
	require.NoError(t, err)

	assert.Equal(t, h1.Entries(), h2.Entries())
}

func TestHistory_EmptyHistory(t *testing.T) {
	t.Parallel()
	h := NewCommandHistory(100)

	assert.Empty(t, h.Entries())

	path := filepath.Join(t.TempDir(), "history.txt")
	err := h.Save(path)
	require.NoError(t, err)
}

func TestHistory_PreviousNext(t *testing.T) {
	t.Parallel()
	h := NewCommandHistory(100)

	h.Add("cmd1")
	h.Add("cmd2")
	h.Add("cmd3")

	// Navigate backward
	entry, ok := h.Previous()
	assert.True(t, ok)
	assert.Equal(t, "cmd3", entry)

	entry, ok = h.Previous()
	assert.True(t, ok)
	assert.Equal(t, "cmd2", entry)

	entry, ok = h.Previous()
	assert.True(t, ok)
	assert.Equal(t, "cmd1", entry)

	// At the beginning, Previous returns false
	_, ok = h.Previous()
	assert.False(t, ok)

	// Navigate forward
	entry, ok = h.Next()
	assert.True(t, ok)
	assert.Equal(t, "cmd2", entry)

	entry, ok = h.Next()
	assert.True(t, ok)
	assert.Equal(t, "cmd3", entry)

	// Past the end, Next returns false
	_, ok = h.Next()
	assert.False(t, ok)
}
