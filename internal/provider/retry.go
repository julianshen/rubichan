package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const defaultMaxAttempts = 3

var (
	retryBaseDelay = 200 * time.Millisecond
	retryMaxDelay  = 2 * time.Second
)

// DoWithRetry executes an HTTP request with bounded exponential backoff for
// transient API failures.
func DoWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	if client == nil {
		return nil, fmt.Errorf("http client is nil")
	}

	attempts := defaultMaxAttempts
	for attempt := 1; attempt <= attempts; attempt++ {
		attemptReq, err := cloneRequest(ctx, req)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(attemptReq)
		if err == nil {
			if !shouldRetryStatus(resp.StatusCode) || attempt == attempts {
				return resp, nil
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		} else {
			if !shouldRetryError(err) || attempt == attempts {
				return nil, err
			}
		}

		if waitErr := waitRetry(ctx, attempt); waitErr != nil {
			return nil, waitErr
		}
	}

	return nil, fmt.Errorf("retry loop exhausted")
}

func cloneRequest(ctx context.Context, req *http.Request) (*http.Request, error) {
	if req == nil {
		return nil, fmt.Errorf("http request is nil")
	}
	clone := req.Clone(ctx)
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("clone request body: %w", err)
		}
		clone.Body = body
	}
	return clone, nil
}

func shouldRetryStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func shouldRetryError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return true
}

func waitRetry(ctx context.Context, attempt int) error {
	delay := retryBaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= retryMaxDelay {
			delay = retryMaxDelay
			break
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
