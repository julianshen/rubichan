package httptool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tools/netutil"
)

const (
	defaultTimeout = 30 * time.Second
	maxTimeout     = 2 * time.Minute
	maxBodyBytes   = 128 * 1024
	maxContent     = 30 * 1024
	maxDisplay     = 100 * 1024
)

// ResolveFunc resolves a hostname to IP addresses. Used for SSRF validation.
type ResolveFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

var defaultResolver ResolveFunc = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

type requestInput struct {
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers,omitempty"`
	Query           map[string]string `json:"query,omitempty"`
	Body            json.RawMessage   `json:"body,omitempty"`
	TimeoutMS       int               `json:"timeout_ms,omitempty"`
	FollowRedirects *bool             `json:"follow_redirects,omitempty"`
}

// Tool implements a single HTTP method as an agent tool with SSRF protection.
type Tool struct {
	method   string
	name     string
	resolver ResolveFunc
	// dialContext is a test seam for directing requests to local fixtures
	// without weakening production SSRF checks.
	dialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewGetTool returns an HTTP GET tool.
func NewGetTool() *Tool { return newTool(http.MethodGet, "http_get") }

// NewPostTool returns an HTTP POST tool.
func NewPostTool() *Tool { return newTool(http.MethodPost, "http_post") }

// NewPutTool returns an HTTP PUT tool.
func NewPutTool() *Tool { return newTool(http.MethodPut, "http_put") }

// NewPatchTool returns an HTTP PATCH tool.
func NewPatchTool() *Tool { return newTool(http.MethodPatch, "http_patch") }

// NewDeleteTool returns an HTTP DELETE tool.
func NewDeleteTool() *Tool { return newTool(http.MethodDelete, "http_delete") }

func newTool(method, name string) *Tool {
	return &Tool{method: method, name: name, resolver: defaultResolver}
}

// Name returns the tool name.
func (t *Tool) Name() string { return t.name }

func (t *Tool) SearchHint() string {
	return "api fetch url endpoint rest web network download webhook"
}

// Description returns a human-readable description of the tool.
func (t *Tool) Description() string {
	return fmt.Sprintf("Perform an HTTP %s request with optional headers, query parameters, request body, timeout, and redirect control.", t.method)
}

// InputSchema returns the JSON schema for the tool's input.
func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "HTTP or HTTPS URL to request"},
			"headers": {
				"type": "object",
				"additionalProperties": {"type": "string"},
				"description": "Optional HTTP headers"
			},
			"query": {
				"type": "object",
				"additionalProperties": {"type": "string"},
				"description": "Optional query string parameters to merge into the URL"
			},
			"body": {
				"description": "Optional request body. Strings are sent as plain text; other JSON values are sent as JSON."
			},
			"timeout_ms": {"type": "integer", "description": "Request timeout in milliseconds (max 120000)"},
			"follow_redirects": {"type": "boolean", "description": "Whether to follow redirects (default true)"}
		},
		"required": ["url"]
	}`)
}

// Execute performs the HTTP request with SSRF validation and DNS pinning.
func (t *Tool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in requestInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if in.URL == "" {
		return tools.ToolResult{Content: "url is required", IsError: true}, nil
	}

	u, err := url.Parse(in.URL)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid url: %s", err), IsError: true}, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return tools.ToolResult{Content: "only http and https URLs are allowed", IsError: true}, nil
	}
	addrs, err := validateTarget(ctx, u, t.resolver)
	if err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	values := u.Query()
	for k, v := range in.Query {
		values.Set(k, v)
	}
	u.RawQuery = values.Encode()

	bodyReader, contentType, err := buildBody(in.Body)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid body: %s", err), IsError: true}, nil
	}
	req, err := http.NewRequestWithContext(ctx, t.method, u.String(), bodyReader)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("create request: %s", err), IsError: true}, nil
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}

	timeout := defaultTimeout
	if in.TimeoutMS > 0 {
		timeout = time.Duration(in.TimeoutMS) * time.Millisecond
		if timeout > maxTimeout {
			timeout = maxTimeout
		}
	}
	// Pin the dialer to the already-validated IP addresses to prevent
	// DNS rebinding attacks (where a second DNS lookup resolves to a
	// different, potentially private address).
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transportDialContext := pinnedDialer(dialer, u.Hostname(), addrs, t.resolver)
	if t.dialContext != nil {
		transportDialContext = t.dialContext
	}
	transport := &http.Transport{
		DialContext: transportDialContext,
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	if in.FollowRedirects != nil && !*in.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("http request failed: %s", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	body, truncated, err := readBody(resp.Body)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("read response body: %s", err), IsError: true}, nil
	}

	formattedBody := formatBody(resp.Header.Get("Content-Type"), body)
	result := formatResponse(resp, formattedBody, truncated)
	content, display := truncate(result)
	return tools.ToolResult{Content: content, DisplayContent: display}, nil
}

func buildBody(raw json.RawMessage) (io.Reader, string, error) {
	if len(raw) == 0 {
		return nil, "", nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.NewReader(asString), "text/plain; charset=utf-8", nil
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, "", err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	return bytes.NewReader(encoded), "application/json", nil
}

func readBody(r io.Reader) ([]byte, bool, error) {
	limited := io.LimitReader(r, maxBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if len(body) > maxBodyBytes {
		return body[:maxBodyBytes], true, nil
	}
	return body, false, nil
}

func formatBody(contentType string, body []byte) string {
	if len(body) == 0 {
		return "<empty>"
	}
	if strings.Contains(strings.ToLower(contentType), "json") || json.Valid(body) {
		var out bytes.Buffer
		if err := json.Indent(&out, body, "", "  "); err == nil {
			return out.String()
		}
	}
	return string(body)
}

func formatResponse(resp *http.Response, body string, truncated bool) string {
	var lines []string
	lines = append(lines,
		fmt.Sprintf("status: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		fmt.Sprintf("final_url: %s", resp.Request.URL.String()),
		"headers:",
	)

	headerKeys := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)
	for _, k := range headerKeys {
		lines = append(lines, fmt.Sprintf("%s: %s", k, strings.Join(resp.Header.Values(k), ", ")))
	}

	lines = append(lines, "", "body:", body)
	if truncated {
		lines = append(lines, "", "... response body truncated")
	}
	return strings.Join(lines, "\n")
}

func truncate(s string) (string, string) {
	if len(s) <= maxContent {
		return s, ""
	}
	display := s
	if len(display) > maxDisplay {
		display = display[:maxDisplay] + "\n... output truncated"
	}
	return s[:maxContent] + "\n... output truncated", display
}

func validateTarget(ctx context.Context, u *url.URL, resolve ResolveFunc) ([]net.IPAddr, error) {
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return nil, fmt.Errorf("url host is required")
	}
	addrs, err := resolve(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}
	for _, addr := range addrs {
		if netutil.IsPrivateAddress(addr.IP) {
			return nil, fmt.Errorf("requests to private or local addresses are not allowed")
		}
	}
	return addrs, nil
}

// pinnedDialer returns a DialContext function that connects only to the
// pre-resolved IP addresses for the given host, preventing DNS rebinding.
// For redirect targets (different host), it resolves and validates against
// private addresses before connecting. Host comparison is case-insensitive
// per RFC 7230 §2.7.1.
func pinnedDialer(d *net.Dialer, host string, addrs []net.IPAddr, resolve ResolveFunc) func(ctx context.Context, network, addr string) (net.Conn, error) {
	lowerHost := strings.ToLower(host)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		reqHost, reqPort, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if strings.ToLower(reqHost) != lowerHost {
			// Different host (e.g. redirect) — resolve and validate
			// against private addresses to prevent SSRF via redirect.
			redirectAddrs, resolveErr := resolve(ctx, reqHost)
			if resolveErr != nil {
				return nil, fmt.Errorf("resolve redirect host %q: %w", reqHost, resolveErr)
			}
			for _, a := range redirectAddrs {
				if netutil.IsPrivateAddress(a.IP) {
					return nil, fmt.Errorf("redirect to private or local address is not allowed")
				}
			}
			return dialPinned(ctx, d, network, reqPort, redirectAddrs)
		}
		return dialPinned(ctx, d, network, reqPort, addrs)
	}
}

func dialPinned(ctx context.Context, d *net.Dialer, network, port string, addrs []net.IPAddr) (net.Conn, error) {
	var lastErr error
	for _, ip := range addrs {
		conn, dialErr := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no addresses to connect to")
}
