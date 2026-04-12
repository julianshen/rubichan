package httptool

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPToolConstructorsAndMetadata covers every constructor plus the
// Name/SearchHint/Description/InputSchema accessors.
func TestHTTPToolConstructorsAndMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tool *Tool
		want string
	}{
		{"get", NewGetTool(), "http_get"},
		{"post", NewPostTool(), "http_post"},
		{"put", NewPutTool(), "http_put"},
		{"patch", NewPatchTool(), "http_patch"},
		{"delete", NewDeleteTool(), "http_delete"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.tool.Name())
			assert.NotEmpty(t, tc.tool.Description())
			assert.NotEmpty(t, tc.tool.SearchHint())

			schema := tc.tool.InputSchema()
			var parsed map[string]any
			require.NoError(t, json.Unmarshal(schema, &parsed))
			assert.Equal(t, "object", parsed["type"])
			props := parsed["properties"].(map[string]any)
			assert.Contains(t, props, "url")
		})
	}
}

// TestHTTPInvalidJSONInput covers the top-level input unmarshal error.
func TestHTTPInvalidJSONInput(t *testing.T) {
	t.Parallel()

	tool := NewGetTool()
	result, err := tool.Execute(context.Background(), json.RawMessage(`{not json`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

// TestHTTPMissingURL covers the "url is required" branch.
func TestHTTPMissingURL(t *testing.T) {
	t.Parallel()

	tool := NewGetTool()
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "url is required")
}

// TestHTTPInvalidURL covers the url.Parse error branch.
func TestHTTPInvalidURL(t *testing.T) {
	t.Parallel()

	tool := NewGetTool()
	// A URL containing control characters fails url.Parse.
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"http://\n"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	// Either parse-failure or scheme-rejection path is acceptable.
	assert.True(t, strings.Contains(result.Content, "invalid url") || strings.Contains(result.Content, "http and https"))
}

// TestHTTPPostJSONBody exercises buildBody's JSON path.
func TestHTTPPostJSONBody(t *testing.T) {
	t.Parallel()

	var gotBody string
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tool := NewPostTool()
	tool.resolver = testResolver("127.0.0.1")
	tool.dialContext = testDialContext(srv.URL)

	input := json.RawMessage(`{"url":"` + srv.URL + `","body":{"k":"v","n":42}}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "application/json", gotCT)
	assert.Contains(t, gotBody, `"k":"v"`)
}

// TestHTTPPostRespectsExplicitContentType ensures the user-supplied header wins.
func TestHTTPPostRespectsExplicitContentType(t *testing.T) {
	t.Parallel()

	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tool := NewPostTool()
	tool.resolver = testResolver("127.0.0.1")
	tool.dialContext = testDialContext(srv.URL)

	input := json.RawMessage(`{"url":"` + srv.URL + `","headers":{"Content-Type":"application/xml"},"body":"<x/>"}`)
	_, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "application/xml", gotCT)
}

// TestHTTPTimeoutClamping covers the timeout_ms > maxTimeout branch.
func TestHTTPTimeoutClamping(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tool := NewGetTool()
	tool.resolver = testResolver("127.0.0.1")
	tool.dialContext = testDialContext(srv.URL)

	// 10 minutes requested → clamped to maxTimeout.
	input := json.RawMessage(`{"url":"` + srv.URL + `","timeout_ms":600000}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

// TestHTTPNoFollowRedirects covers the custom CheckRedirect branch.
func TestHTTPNoFollowRedirects(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dest", http.StatusFound)
	})
	mux.HandleFunc("/dest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("final"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := NewGetTool()
	tool.resolver = testResolver("127.0.0.1")
	tool.dialContext = testDialContext(srv.URL)

	input := json.RawMessage(`{"url":"` + srv.URL + `/start","follow_redirects":false}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Not following means we see the redirect, not the destination body.
	assert.Contains(t, result.Content, "status: 302")
	assert.Contains(t, result.Content, "Location: /dest")
}

// TestHTTPFollowRedirectsDefault exercises normal redirect behavior.
func TestHTTPFollowRedirectsDefault(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dest", http.StatusFound)
	})
	mux.HandleFunc("/dest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("final"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tool := NewGetTool()
	tool.resolver = testResolver("127.0.0.1")
	tool.dialContext = testDialContext(srv.URL)

	input := json.RawMessage(`{"url":"` + srv.URL + `/start"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "status: 200")
	assert.Contains(t, result.Content, "final_url")
}

// TestHTTPBodyTruncation triggers the readBody truncation path.
// A body just slightly above maxContent but well under maxDisplay lets the
// display buffer retain the tail containing the "response body truncated"
// notice that readBody appends to the formatted response.
func TestHTTPBodyTruncation(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("a", maxContent+500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	tool := NewGetTool()
	tool.resolver = testResolver("127.0.0.1")
	tool.dialContext = testDialContext(srv.URL)

	input := json.RawMessage(`{"url":"` + srv.URL + `"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Main Content clipped by truncate(), DisplayContent retains the full tail
	// including the "response body truncated" notice emitted during readBody.
	assert.Contains(t, result.Content, "output truncated")
	assert.NotEmpty(t, result.DisplayContent)
}

// TestHTTPReadBodyTruncation covers the readBody truncation branch directly by
// exercising a body that exceeds maxBodyBytes but returns a small enough
// formatted response to fit in a single content slot.
func TestHTTPReadBodyTruncation(t *testing.T) {
	t.Parallel()

	// Body precisely above maxBodyBytes so readBody returns truncated=true.
	body := strings.NewReader(strings.Repeat("b", maxBodyBytes+10))
	got, truncated, err := readBody(body)
	require.NoError(t, err)
	assert.True(t, truncated)
	assert.Len(t, got, maxBodyBytes)
}

// TestHTTPContentTruncation covers the final truncate() helper with oversized formatted output.
func TestHTTPContentTruncation(t *testing.T) {
	t.Parallel()

	// Big body plus headers will push the final content over maxContent.
	big := strings.Repeat("z", maxContent+5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	tool := NewGetTool()
	tool.resolver = testResolver("127.0.0.1")
	tool.dialContext = testDialContext(srv.URL)

	input := json.RawMessage(`{"url":"` + srv.URL + `"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "output truncated")
	// When content is truncated, DisplayContent holds the untruncated/longer view.
	assert.NotEmpty(t, result.DisplayContent)
}

// TestFormatBodyNonJSONFallback covers the non-JSON body path in formatBody.
func TestFormatBodyNonJSONFallback(t *testing.T) {
	t.Parallel()

	body := formatBody("text/plain", []byte("hello"))
	assert.Equal(t, "hello", body)
}

// TestFormatBodyEmpty covers the empty-body branch.
func TestFormatBodyEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "<empty>", formatBody("text/plain", nil))
	assert.Equal(t, "<empty>", formatBody("application/json", []byte{}))
}

// TestFormatBodyBadJSONFallback exercises the "content type is JSON but body
// won't parse" path — it should fall through to the string conversion.
func TestFormatBodyBadJSONFallback(t *testing.T) {
	t.Parallel()
	got := formatBody("application/json", []byte("not { json"))
	assert.Equal(t, "not { json", got)
}

// TestTruncateHelper verifies the helper returns a truncated main + display view.
func TestTruncateHelper(t *testing.T) {
	t.Parallel()

	small := "short"
	got, disp := truncate(small)
	assert.Equal(t, small, got)
	assert.Empty(t, disp)

	mid := strings.Repeat("x", maxContent+5)
	got, disp = truncate(mid)
	assert.Contains(t, got, "output truncated")
	// mid is under maxDisplay, so disp holds the full original.
	assert.Equal(t, mid, disp)

	huge := strings.Repeat("y", maxDisplay+1000)
	got, disp = truncate(huge)
	assert.Contains(t, got, "output truncated")
	assert.Contains(t, disp, "output truncated")
}

// TestHTTPValidateTargetEmptyHost ensures URLs like "http://" fail cleanly.
func TestHTTPValidateTargetEmptyHost(t *testing.T) {
	t.Parallel()

	tool := NewGetTool()
	// "http://" has an empty host.
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"http://"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// TestPinnedDialerSameHost exercises pinnedDialer + dialPinned for the
// non-redirect code path.
func TestPinnedDialerSameHost(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, port, err := parseHostPort(srv.URL)
	require.NoError(t, err)

	addrs := []net.IPAddr{{IP: net.ParseIP(host)}}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	dc := pinnedDialer(dialer, host, addrs, func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return addrs, nil
	})

	conn, err := dc(context.Background(), "tcp", net.JoinHostPort(host, port))
	require.NoError(t, err)
	_ = conn.Close()
}

// TestPinnedDialerRedirectValidatesHost exercises the redirect branch of
// pinnedDialer, including the private-address rejection path.
func TestPinnedDialerRedirectValidatesHost(t *testing.T) {
	t.Parallel()

	addrs := []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}
	dialer := &net.Dialer{Timeout: 1 * time.Second}

	// Resolver that returns a private IP for the redirect host.
	dc := pinnedDialer(dialer, "example.com", addrs, func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host == "badhost" {
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
		}
		return nil, fmt.Errorf("unknown host: %s", host)
	})

	_, err := dc(context.Background(), "tcp", "badhost:80")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private or local address")

	// Resolver returns an error for the redirect target.
	_, err = dc(context.Background(), "tcp", "missing:80")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve redirect host")

	// Malformed addr — SplitHostPort fails.
	_, err = dc(context.Background(), "tcp", "not-a-valid-addr")
	require.Error(t, err)
}

// TestDialPinnedEmptyAddrs exercises the "no addresses" branch of dialPinned.
func TestDialPinnedEmptyAddrs(t *testing.T) {
	t.Parallel()

	_, err := dialPinned(context.Background(), &net.Dialer{}, "tcp", "80", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no addresses")
}

// TestDialPinnedAllFail exercises the "all candidates fail" branch.
func TestDialPinnedAllFail(t *testing.T) {
	t.Parallel()

	addrs := []net.IPAddr{
		{IP: net.ParseIP("127.0.0.1")},
	}
	// Port 1 is essentially guaranteed to refuse.
	_, err := dialPinned(context.Background(), &net.Dialer{Timeout: 500 * time.Millisecond}, "tcp", "1", addrs)
	require.Error(t, err)
}

// parseHostPort extracts the host and port from an http[s]:// URL string.
func parseHostPort(rawURL string) (string, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", err
	}
	return net.SplitHostPort(u.Host)
}

// TestHTTPRequestFailure exercises the transport error branch by pointing the
// dial context at an unreachable target.
func TestHTTPRequestFailure(t *testing.T) {
	t.Parallel()

	// Start a server and immediately close it to force a dial failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	closedURL := srv.URL
	srv.Close()

	tool := NewGetTool()
	tool.resolver = testResolver("127.0.0.1")
	tool.dialContext = testDialContext(closedURL)

	input := json.RawMessage(fmt.Sprintf(`{"url":"%s"}`, closedURL))
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "http request failed")
}
