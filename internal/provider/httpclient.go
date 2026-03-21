package provider

import (
	"net"
	"net/http"
	"time"
)

// NewHTTPClient returns an *http.Client configured for LLM provider use.
// It sets generous timeouts suitable for long-running streaming requests
// while ensuring idle connections are reaped promptly.
func NewHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   10,
		MaxIdleConns:          100,
	}

	return &http.Client{
		// No top-level Timeout — streaming responses can run for minutes.
		// Per-request deadlines are enforced via context cancellation.
		Transport: transport,
	}
}
