package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/store"
)

func TestResultStoreOffloadBelowThreshold(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.CreateSession(store.Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	rs := NewResultStore(s, "s1", 100) // threshold = 100 bytes
	result, err := rs.OffloadResult("shell", "t1", "small output")
	require.NoError(t, err)
	assert.Equal(t, "small output", result, "below threshold should return unchanged")
}

func TestResultStoreOffloadAboveThreshold(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.CreateSession(store.Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	rs := NewResultStore(s, "s1", 20)
	bigContent := "this is a large tool result that exceeds the threshold limit by quite a bit"
	result, err := rs.OffloadResult("shell", "t1", bigContent)
	require.NoError(t, err)

	assert.Contains(t, result, "Tool result stored")
	assert.Contains(t, result, "shell")
	assert.Contains(t, result, "read_result")
}

func TestResultStoreRetrieve(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.CreateSession(store.Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	rs := NewResultStore(s, "s1", 20)
	bigContent := "this is a large tool result that exceeds the threshold limit by quite a bit"
	_, err = rs.OffloadResult("shell", "t1", bigContent)
	require.NoError(t, err)

	// Retrieve using the stored ref ID.
	refs := rs.RefIDs()
	require.Len(t, refs, 1)

	retrieved, err := rs.Retrieve(refs[0])
	require.NoError(t, err)
	assert.Equal(t, bigContent, retrieved)
}
