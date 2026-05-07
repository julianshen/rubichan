package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCachedMicrocompactQueueAndTake(t *testing.T) {
	s := NewCachedMicrocompactService(true)
	s.QueueEdit("tu_01")
	s.QueueEdit("tu_02")

	require.True(t, s.HasEdits())
	edits := s.TakeEdits()
	require.Len(t, edits, 2)
	require.Equal(t, "tu_01", edits[0].CacheReference)
	require.False(t, s.HasEdits())
}

func TestCachedMicrocompactDisabled(t *testing.T) {
	s := NewCachedMicrocompactService(false)
	s.QueueEdit("tu_01")
	require.False(t, s.HasEdits())
}

func TestCachedMicrocompactReset(t *testing.T) {
	s := NewCachedMicrocompactService(true)
	s.QueueEdit("tu_01")
	s.Reset()
	require.False(t, s.HasEdits())
}
