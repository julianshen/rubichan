package httptool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetToolJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"name":"rubichan"}`))
	}))
	defer srv.Close()

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
