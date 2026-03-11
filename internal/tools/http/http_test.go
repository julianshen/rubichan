package httptool

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func allowResolvedHost(t *testing.T, hosts ...string) {
	t.Helper()
	original := lookupIPAddr
	lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		for _, allowed := range hosts {
			if host == allowed {
				return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
			}
		}
		return original(ctx, host)
	}
	t.Cleanup(func() {
		lookupIPAddr = original
	})
}

func TestGetToolJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"name":"rubichan"}`))
	}))
	defer srv.Close()
	allowResolvedHost(t, "127.0.0.1")

	tool := NewGetTool()
	input := json.RawMessage(`{"url":"` + srv.URL + `","query":{"page":"1"}}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "status: 200")
	assert.Contains(t, result.Content, `"ok": true`)
	assert.Contains(t, result.Content, `"name": "rubichan"`)
}

func TestPostToolStringBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "text/plain; charset=utf-8", r.Header.Get("Content-Type"))
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	allowResolvedHost(t, "127.0.0.1")

	tool := NewPostTool()
	input := json.RawMessage(`{"url":"` + srv.URL + `","body":"hello"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello")
}

func TestRejectNonHTTPURL(t *testing.T) {
	tool := NewGetTool()
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"file:///etc/passwd"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.True(t, strings.Contains(result.Content, "http and https"))
}

func TestRejectPrivateAddressTargets(t *testing.T) {
	tool := NewGetTool()
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"http://127.0.0.1/test"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "private or local addresses")
}
