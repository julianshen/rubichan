package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDisplayMessage(t *testing.T) {
	msg := DisplayMessage{
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: "hello"},
		},
	}
	require.Equal(t, "assistant", msg.Role)
	require.Len(t, msg.Content, 1)
	require.Equal(t, "hello", msg.Content[0].Text)
}
