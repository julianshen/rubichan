package agentsdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheEditJSON(t *testing.T) {
	edit := CacheEdit{Type: CacheEditDelete, CacheReference: "tu_01"}
	data, err := json.Marshal(edit)
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"delete"`)
	require.Contains(t, string(data), `"cache_reference":"tu_01"`)
}

func TestCacheReferenceJSON(t *testing.T) {
	ref := CacheReference{Type: "cache_reference", ID: "tu_01"}
	data, err := json.Marshal(ref)
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"cache_reference"`)
	require.Contains(t, string(data), `"id":"tu_01"`)
}
