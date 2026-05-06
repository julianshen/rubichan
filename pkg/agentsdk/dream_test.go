package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDreamParams(t *testing.T) {
	params := DreamParams{
		MemoryRoot:    "/tmp/memories",
		TranscriptDir: "/tmp/transcripts",
		Extra:         "additional context",
	}
	require.Equal(t, "/tmp/memories", params.MemoryRoot)
	require.Equal(t, "/tmp/transcripts", params.TranscriptDir)
	require.Equal(t, "additional context", params.Extra)
}
