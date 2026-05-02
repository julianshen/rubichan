package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuerySourceString(t *testing.T) {
	require.Equal(t, "foreground", QuerySourceForeground.String())
	require.Equal(t, "background", QuerySourceBackground.String())
	require.Equal(t, "hook", QuerySourceHook.String())
	require.Equal(t, "unknown", QuerySource(99).String())
}

func TestQuerySourceShouldRetryOn529(t *testing.T) {
	require.True(t, QuerySourceForeground.ShouldRetryOn529(), "foreground should retry on 529")
	require.False(t, QuerySourceBackground.ShouldRetryOn529(), "background should not retry on 529")
	require.False(t, QuerySourceHook.ShouldRetryOn529(), "hook should not retry on 529")
	require.False(t, QuerySource(99).ShouldRetryOn529(), "unknown should not retry on 529")
}
