package httptool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestWebFetchTool(srv *httptest.Server) *WebFetchTool {
	wf := NewWebFetchTool()
	wf.resolver = testResolver("127.0.0.1")
	wf.dialContext = testDialContext(srv.URL)
	return wf
}

func TestWebFetchPreferLlmsTxt(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/llms.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "# My Site\n> A great site for docs.")
	})
	mux.HandleFunc("/docs/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body>API docs</body></html>")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := newTestWebFetchTool(srv)
	input, _ := json.Marshal(webFetchInput{URL: srv.URL + "/docs/api"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "llms.txt")
	assert.Contains(t, result.Content, "# My Site")
}

func TestWebFetchFallsBackToMarkdownVariant(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/docs/guide.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		fmt.Fprint(w, "# Guide\nThis is the markdown version.")
	})
	mux.HandleFunc("/docs/guide", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h1>Guide</h1></body></html>")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := newTestWebFetchTool(srv)
	input, _ := json.Marshal(webFetchInput{URL: srv.URL + "/docs/guide"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "markdown variant")
	assert.Contains(t, result.Content, "# Guide")
}

func TestWebFetchFallsBackToRawURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Plain text content.")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := newTestWebFetchTool(srv)
	input, _ := json.Marshal(webFetchInput{URL: srv.URL + "/page"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "original URL")
	assert.Contains(t, result.Content, "Plain text content.")
}

func TestWebFetchStripsHTML(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><head><script>var x=1;</script><style>body{color:red}</style></head><body><h1>Title</h1><p>Hello world</p></body></html>")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := newTestWebFetchTool(srv)
	input, _ := json.Marshal(webFetchInput{URL: srv.URL + "/page"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Title")
	assert.Contains(t, result.Content, "Hello world")
	assert.NotContains(t, result.Content, "<script>")
	assert.NotContains(t, result.Content, "var x=1")
	assert.NotContains(t, result.Content, "color:red")
}

func TestWebFetchEmptyURL(t *testing.T) {
	tool := NewWebFetchTool()
	input, _ := json.Marshal(webFetchInput{URL: ""})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "url is required")
}

func TestWebFetchInvalidScheme(t *testing.T) {
	tool := NewWebFetchTool()
	input, _ := json.Marshal(webFetchInput{URL: "ftp://example.com/file"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "only http and https")
}

func TestWebFetchSkipsBinaryContent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/llms.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte{0x00, 0x01, 0x02})
	})
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Actual text.")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := newTestWebFetchTool(srv)
	input, _ := json.Marshal(webFetchInput{URL: srv.URL + "/file"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Actual text.")
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "basic tags",
			html: "<p>Hello <b>world</b></p>",
			want: "Hello world",
		},
		{
			name: "script removed",
			html: "Before <script>alert('x')</script> After",
			want: "Before After",
		},
		{
			name: "style removed",
			html: "Before <style>.x{color:red}</style> After",
			want: "Before After",
		},
		{
			name: "whitespace collapsed",
			html: "  Hello    world  \n\n  foo  ",
			want: "Hello world foo",
		},
		{
			name: "empty",
			html: "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.html)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsTextContent(t *testing.T) {
	assert.True(t, isTextContent("text/html; charset=utf-8"))
	assert.True(t, isTextContent("text/plain"))
	assert.True(t, isTextContent("text/markdown"))
	assert.True(t, isTextContent("application/json"))
	assert.True(t, isTextContent("application/xml"))
	assert.False(t, isTextContent("application/octet-stream"))
	assert.False(t, isTextContent("image/png"))
}

func TestWebFetchToolInterface(t *testing.T) {
	tool := NewWebFetchTool()
	assert.Equal(t, "web_fetch", tool.Name())
	assert.NotEmpty(t, tool.Description())
	assert.NotEmpty(t, tool.SearchHint())

	schema := tool.InputSchema()
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(schema, &parsed))
	props := parsed["properties"].(map[string]any)
	assert.Contains(t, props, "url")
	assert.Contains(t, props, "timeout_ms")
}

func TestWebFetchPriorityOrder(t *testing.T) {
	// When both llms.txt and .md exist, llms.txt wins.
	var fetchOrder []string
	mux := http.NewServeMux()
	mux.HandleFunc("/llms.txt", func(w http.ResponseWriter, r *http.Request) {
		fetchOrder = append(fetchOrder, "llms.txt")
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "LLM summary")
	})
	mux.HandleFunc("/docs/page.md", func(w http.ResponseWriter, r *http.Request) {
		fetchOrder = append(fetchOrder, "page.md")
		w.Header().Set("Content-Type", "text/markdown")
		fmt.Fprint(w, "# Page MD")
	})
	mux.HandleFunc("/docs/page", func(w http.ResponseWriter, r *http.Request) {
		fetchOrder = append(fetchOrder, "page")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<p>Page HTML</p>")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := newTestWebFetchTool(srv)
	input, _ := json.Marshal(webFetchInput{URL: srv.URL + "/docs/page"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)

	// llms.txt should be returned (first priority)
	assert.Contains(t, result.Content, "LLM summary")
	assert.Contains(t, result.Content, "llms.txt")
	// Only llms.txt should have been fetched
	assert.Equal(t, []string{"llms.txt"}, fetchOrder)
}

func TestStripHTMLPreservesContent(t *testing.T) {
	// Ensure entities-like content survives (we don't decode HTML entities,
	// but we shouldn't eat the ampersand either)
	got := stripHTML("A &amp; B")
	assert.Contains(t, got, "&amp;")
	assert.Contains(t, got, "A")
	assert.Contains(t, got, "B")
}

func TestWebFetchHandlesNon200LlmsTxt(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/llms.txt", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/page.md", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Fallback content")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := newTestWebFetchTool(srv)
	input, _ := json.Marshal(webFetchInput{URL: srv.URL + "/page"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "original URL")
	assert.Contains(t, result.Content, "Fallback content")
}

func TestWebFetchSetsAcceptHeader(t *testing.T) {
	var gotAccept string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := newTestWebFetchTool(srv)
	input, _ := json.Marshal(webFetchInput{URL: srv.URL + "/"})
	tool.Execute(context.Background(), input)

	assert.True(t, strings.Contains(gotAccept, "text/markdown"))
}
