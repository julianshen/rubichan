package provider

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient(t *testing.T) {
	t.Parallel()

	c := NewHTTPClient()
	require.NotNil(t, c)

	assert.Equal(t, time.Duration(0), c.Timeout, "no top-level timeout for streaming")

	tr, ok := c.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, 10, tr.MaxIdleConnsPerHost)
	assert.Equal(t, 100, tr.MaxIdleConns)
}
