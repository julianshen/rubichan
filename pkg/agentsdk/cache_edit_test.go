package agentsdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheEditJSON(t *testing.T) {
	edit := CacheEdit{Type: "delete", CacheReference: "tu_01"}
	data, err := json.Marshal(edit)
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"delete"`)
	require.Contains(t, string(data), `"cache_reference":"tu_01"`)
}
