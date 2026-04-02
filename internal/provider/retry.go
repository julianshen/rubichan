package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
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

	for attempt := 1; attempt <= defaultMaxAttempts; attempt++ {
		attemptReq, err := cloneRequest(ctx, req)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(attemptReq)
		if err == nil {
			if !shouldRetryStatus(resp.StatusCode) || attempt == defaultMaxAttempts {
				return resp, nil
			}
			delay := retryDelay(attempt)
			if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
				delay = ra
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			if waitErr := waitRetry(ctx, delay); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		if !shouldRetryError(err) || attempt == defaultMaxAttempts {
			return nil, err
		}
		if waitErr := waitRetry(ctx, retryDelay(attempt)); waitErr != nil {
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
	if req.Body != nil {
		if req.GetBody == nil {
			return nil, fmt.Errorf("request body is not replayable")
		}
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
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
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
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func retryDelay(attempt int) time.Duration {
	delay := retryBaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= retryMaxDelay {
			return retryMaxDelay
		}
	}
	return delay
}

func parseRetryAfter(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	when, err := http.ParseTime(v)
	if err != nil {
		return 0, false
	}
	d := time.Until(when)
	if d <= 0 {
		return 0, false
	}
	return d, true
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
