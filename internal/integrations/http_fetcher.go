package integrations

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxResponseSize = 1 << 20 // 1 MB

// HTTPFetcher retrieves URL contents with a timeout and response size limit.
type HTTPFetcher struct {
	client *http.Client
}

// NewHTTPFetcher creates a new HTTPFetcher with the given timeout.
func NewHTTPFetcher(timeout time.Duration) *HTTPFetcher {
	return &HTTPFetcher{
		client: &http.Client{Timeout: timeout},
	}
}

// Fetch retrieves the URL content as a string, limited to 1 MB.
func (f *HTTPFetcher) Fetch(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("fetch %q: HTTP %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return string(body), nil
}
