package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTombstoned(t *testing.T) {
	require.True(t, IsTombstoned(TombstoneMarker))
	require.False(t, IsTombstoned("normal message"))
}
