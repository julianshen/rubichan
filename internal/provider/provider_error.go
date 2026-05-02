package provider

import (
	"fmt"
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ErrorKind classifies the category of a provider error.
type ErrorKind int

const (
	// ErrUnknown is the zero value; guards against uninitialized ErrorKind.
	ErrUnknown ErrorKind = iota
	// ErrRateLimited indicates HTTP 429 or equivalent throttling.
	ErrRateLimited
	// ErrAuthFailed indicates invalid or missing credentials.
	ErrAuthFailed
	// ErrContextOverflow indicates the request exceeded the model's context window.
	ErrContextOverflow
	// ErrModelNotFound indicates the requested model does not exist.
	ErrModelNotFound
	// ErrServerError indicates a 5xx server-side failure.
	ErrServerError
	// ErrStreamError indicates a failure during streaming response processing.
	ErrStreamError
	// ErrContentFiltered indicates the provider's safety system blocked the content.
	ErrContentFiltered
	// ErrInvalidRequest indicates a malformed or rejected request (400).
	ErrInvalidRequest
	// ErrQuotaExceeded indicates the account's usage quota has been exhausted.
	ErrQuotaExceeded
	// ErrOther is a catch-all for unclassified errors.
	ErrOther
)

// String returns a human-readable label for the error kind.
func (k ErrorKind) String() string {
	switch k {
	case ErrRateLimited:
		return "rate limited"
	case ErrAuthFailed:
		return "auth failed"
	case ErrContextOverflow:
		return "context overflow"
	case ErrModelNotFound:
		return "model not found"
	case ErrServerError:
		return "server error"
	case ErrStreamError:
		return "stream error"
	case ErrContentFiltered:
		return "content filtered"
	case ErrInvalidRequest:
		return "invalid request"
	case ErrQuotaExceeded:
		return "quota exceeded"
	default:
		return "provider error"
	}
}

// ProviderError is a structured error returned by LLM provider operations.
// It carries enough context for callers to make retry, display, and
// recovery decisions without parsing error strings.
type ProviderError struct {
	// Kind classifies the error category.
	Kind ErrorKind
	// Provider is the provider name (e.g. "anthropic", "openai").
	Provider string
	// Message is a human-readable description of the error.
	Message string
	// StatusCode is the HTTP status code, or 0 if not applicable.
	StatusCode int
	// RetryAfter is the suggested wait duration before retrying (for RateLimited).
	RetryAfter time.Duration
	// Suggestions contains alternative model names (for ModelNotFound).
	Suggestions []string
	// Retryable is an explicit override for the retry decision.
	// When true, IsRetryable returns true regardless of Kind.
	Retryable bool
	// RequestID is the client-generated UUID sent as x-client-request-id for
	// request correlation. Present on stream errors and HTTP errors that were
	// classified after the header was sent. Empty when the error originated
	// before the request was built.
	RequestID string
	// Source classifies the origin of the request for retry behavior.
	// Background tasks should not retry on 529 to avoid amplifying overload.
	Source agentsdk.QuerySource
}

// Error implements the error interface.
func (e *ProviderError) Error() string {
	if e.Provider == "" {
		return e.Message
	}
	return fmt.Sprintf("%s (%s): %s", e.Kind.String(), e.Provider, e.Message)
}

// ProviderErrorKind returns the error kind string, satisfying
// agentsdk.ProviderErrorClassifier so the agent loop can detect
// specific error categories without importing this package.
func (e *ProviderError) ProviderErrorKind() string {
	return e.Kind.String()
}

// IsRetryable reports whether the error is transient and the request
// should be retried. RateLimited, ServerError, and StreamError are
// retryable by default. The Retryable field can override this.
//
// When the error is a 529 (overloaded) and the source is not foreground,
// the error is NOT retryable to avoid amplifying capacity cascades.
func (e *ProviderError) IsRetryable() bool {
	if e.Retryable {
		return true
	}
	// 529 overloaded: only foreground queries retry.
	if e.StatusCode == 529 && !e.Source.ShouldRetryOn529() {
		return false
	}
	switch e.Kind {
	case ErrRateLimited, ErrServerError, ErrStreamError:
		return true
	default:
		return false
	}
}
