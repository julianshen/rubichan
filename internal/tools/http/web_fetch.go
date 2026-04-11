package httptool

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/tools"
)

// WebFetchTool fetches web content optimized for LLM consumption.
// It checks for llms.txt and .md alternatives before falling back to the raw URL.
type WebFetchTool struct {
	resolver    ResolveFunc
	dialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewWebFetchTool returns a web_fetch tool.
func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{resolver: defaultResolver}
}

func (t *WebFetchTool) Name() string { return "web_fetch" }

func (t *WebFetchTool) SearchHint() string {
	return "fetch url web page docs documentation llms.txt markdown read website"
}

func (t *WebFetchTool) Description() string {
	return "Fetch web content optimized for LLM consumption. Checks for llms.txt and Markdown alternatives before falling back to the raw URL."
}

func (t *WebFetchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "URL to fetch content from"},
			"timeout_ms": {"type": "integer", "description": "Request timeout in milliseconds (default 30000, max 120000)"}
		},
		"required": ["url"]
	}`)
}

type webFetchInput struct {
	URL       string `json:"url"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// fetchResult captures a successful fetch with its source label.
type fetchResult struct {
	body   string
	source string
}

func (t *WebFetchTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in webFetchInput
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

	timeout := defaultTimeout
	if in.TimeoutMS > 0 {
		timeout = time.Duration(in.TimeoutMS) * time.Millisecond
		if timeout > maxTimeout {
			timeout = maxTimeout
		}
	}

	// Try sources in order: llms.txt → {url}.md → raw URL.
	origin := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	candidates := []struct {
		url   string
		label string
	}{
		{origin + "/llms.txt", "llms.txt"},
		{strings.TrimSuffix(in.URL, "/") + ".md", "markdown variant"},
	}

	for _, c := range candidates {
		result, fetchErr := t.tryFetch(ctx, c.url, timeout)
		if fetchErr == nil && result != nil {
			content := fmt.Sprintf("[Source: %s — %s]\n\n%s", c.label, c.url, result.body)
			content, display := truncate(content)
			return tools.ToolResult{Content: content, DisplayContent: display}, nil
		}
	}

	// Fallback: fetch the raw URL.
	result, fetchErr := t.tryFetch(ctx, in.URL, timeout)
	if fetchErr != nil {
		return tools.ToolResult{Content: fmt.Sprintf("fetch failed: %s", fetchErr), IsError: true}, nil
	}
	if result == nil {
		return tools.ToolResult{Content: "fetch returned empty response", IsError: true}, nil
	}

	content := fmt.Sprintf("[Source: original URL — %s]\n\n%s", in.URL, result.body)
	content, display := truncate(content)
	return tools.ToolResult{Content: content, DisplayContent: display}, nil
}

// tryFetch attempts to GET a URL and returns the body if the response is 200
// with a text-like content type. Returns nil (no error) if the URL returns
// a non-200 status or non-text content, so the caller can try the next candidate.
func (t *WebFetchTool) tryFetch(ctx context.Context, rawURL string, timeout time.Duration) (*fetchResult, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil // skip bad URLs silently
	}

	addrs, err := validateTarget(ctx, u, t.resolver)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transportDialContext := pinnedDialer(dialer, u.Hostname(), addrs, t.resolver)
	if t.dialContext != nil {
		transportDialContext = t.dialContext
	}
	transport := &http.Transport{DialContext: transportDialContext}
	client := &http.Client{Timeout: timeout, Transport: transport}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil
	}
	req.Header.Set("User-Agent", "rubichan/web_fetch")
	req.Header.Set("Accept", "text/markdown, text/plain, text/html;q=0.9, */*;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil // non-200 → skip to next candidate
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !isTextContent(ct) {
		return nil, nil // binary content → skip
	}

	body, _, err := readBody(resp.Body)
	if err != nil {
		return nil, err
	}

	text := string(body)
	if strings.Contains(ct, "html") {
		text = stripHTML(text)
	}

	return &fetchResult{body: text, source: rawURL}, nil
}

// isTextContent returns true for content types likely to contain useful text.
func isTextContent(ct string) bool {
	return strings.Contains(ct, "text/") ||
		strings.Contains(ct, "application/json") ||
		strings.Contains(ct, "application/xml") ||
		strings.Contains(ct, "application/markdown")
}

// stripHTML removes HTML tags to produce readable plain text.
// This is a simple regex-free approach — not a full parser, but good enough
// for extracting readable content from documentation pages.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	lastWasSpace := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '<':
			// Check for <script> and <style> blocks — skip their content entirely.
			if i+7 < len(s) && strings.EqualFold(s[i:i+7], "<script") {
				end := strings.Index(strings.ToLower(s[i:]), "</script>")
				if end != -1 {
					i += end + len("</script>") - 1
					continue
				}
			}
			if i+6 < len(s) && strings.EqualFold(s[i:i+6], "<style") {
				end := strings.Index(strings.ToLower(s[i:]), "</style>")
				if end != -1 {
					i += end + len("</style>") - 1
					continue
				}
			}
			inTag = true
		case c == '>':
			inTag = false
		case !inTag:
			if c == '\n' || c == '\r' || c == '\t' || c == ' ' {
				if !lastWasSpace {
					b.WriteByte(' ')
					lastWasSpace = true
				}
			} else {
				b.WriteByte(c)
				lastWasSpace = false
			}
		}
	}
	return strings.TrimSpace(b.String())
}
