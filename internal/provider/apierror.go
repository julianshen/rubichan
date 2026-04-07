package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// FormatAPIError produces a human-readable error from an HTTP status code
// and raw response body. It now returns a *ProviderError (which implements
// the error interface), so callers can use errors.As to extract typed info.
//
// The providerName is set to "" for backward compatibility; callers that
// know the provider should use ClassifyAPIError instead.
func FormatAPIError(statusCode int, body []byte, httpReq *http.Request) error {
	return ClassifyAPIError(statusCode, body, httpReq, "")
}

// ClassifyAPIError creates a typed *ProviderError from an HTTP error response.
// It classifies the error by HTTP status code and error message patterns,
// extracts the human-readable message from known API formats (Anthropic,
// OpenAI, Ollama), and builds a user-friendly error string.
func ClassifyAPIError(statusCode int, body []byte, httpReq *http.Request, providerName string) *ProviderError {
	return ClassifyAPIErrorWithResponse(statusCode, body, httpReq, providerName, nil)
}

// ClassifyAPIErrorWithResponse is like ClassifyAPIError but also accepts
// response headers, allowing extraction of Retry-After for rate-limited errors.
func ClassifyAPIErrorWithResponse(statusCode int, body []byte, httpReq *http.Request, providerName string, headers http.Header) *ProviderError {
	message := extractErrorMessage(body)
	kind := classifyByStatus(statusCode)

	// Refine classification using error message patterns for ambiguous statuses.
	if message != "" && (kind == ErrInvalidRequest || kind == ErrRateLimited) {
		if refined := classifyByMessage(message); refined != ErrOther {
			kind = refined
		}
	}

	// Build human-readable error string (preserving legacy FormatAPIError output).
	friendly := friendlyStatus(statusCode)
	var displayMsg string
	if message != "" {
		displayMsg = fmt.Sprintf("%s: %s", friendly, message)
	} else {
		raw := strings.TrimSpace(string(body))
		if len(raw) > 200 {
			raw = raw[:200] + "..."
		}
		if raw != "" {
			displayMsg = fmt.Sprintf("%s (%s)", friendly, raw)
		} else {
			displayMsg = friendly
		}
	}

	// For 404 errors, append HTTP details to help debug model issues.
	if statusCode == http.StatusNotFound && httpReq != nil {
		displayMsg = fmt.Sprintf("%s\n\nHTTP Request: %s %s\nHTTP Response: %d\nResponse Body:\n%s",
			displayMsg, httpReq.Method, httpReq.URL.String(), statusCode,
			strings.TrimSpace(string(body)))
	}

	pe := &ProviderError{
		Kind:       kind,
		Provider:   providerName,
		Message:    displayMsg,
		StatusCode: statusCode,
	}

	// Extract Retry-After header for rate-limited errors.
	if kind == ErrRateLimited && headers != nil {
		if ra := headers.Get("Retry-After"); ra != "" {
			if d, ok := parseRetryAfter(ra); ok {
				pe.RetryAfter = d
			}
		}
	}

	return pe
}

// classifyByStatus maps an HTTP status code to an ErrorKind.
func classifyByStatus(code int) ErrorKind {
	switch code {
	case http.StatusTooManyRequests: // 429
		return ErrRateLimited
	case http.StatusUnauthorized, http.StatusForbidden: // 401, 403
		return ErrAuthFailed
	case http.StatusNotFound: // 404
		return ErrModelNotFound
	case http.StatusPaymentRequired: // 402
		return ErrQuotaExceeded
	case http.StatusRequestEntityTooLarge: // 413
		return ErrContextOverflow
	case http.StatusBadRequest: // 400
		return ErrInvalidRequest // may be refined by classifyByMessage
	case http.StatusInternalServerError, // 500
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return ErrServerError
	default:
		return ErrOther
	}
}

// contextOverflowPatterns are substrings that indicate a context window overflow
// in error messages from various providers.
var contextOverflowPatterns = []string{
	"maximum context length",
	"context_length_exceeded",
	"prompt is too long",
	"exceed the model's maximum context",
	"token limit",
	"exceeds the maximum number of tokens",
	"input too long",
}

// contentFilterPatterns are substrings that indicate content filtering.
var contentFilterPatterns = []string{
	"content was blocked",
	"content filtering",
	"safety system",
	"content_policy_violation",
	"flagged by our",
}

// quotaExceededPatterns detect quota exhaustion that some providers surface as 429.
var quotaExceededPatterns = []string{
	"insufficient_quota",
	"quota exceeded",
	"billing hard limit",
	"check your plan and billing details",
}

// classifyByMessage inspects the error message text to refine classification
// for ambiguous HTTP statuses (400 Bad Request, 429 with quota messages).
func classifyByMessage(message string) ErrorKind {
	lower := strings.ToLower(message)

	for _, pattern := range contextOverflowPatterns {
		if strings.Contains(lower, pattern) {
			return ErrContextOverflow
		}
	}

	for _, pattern := range quotaExceededPatterns {
		if strings.Contains(lower, pattern) {
			return ErrQuotaExceeded
		}
	}

	for _, pattern := range contentFilterPatterns {
		if strings.Contains(lower, pattern) {
			return ErrContentFiltered
		}
	}

	return ErrOther
}

func friendlyStatus(code int) string {
	switch code {
	case http.StatusTooManyRequests: // 429
		return "Rate limited — too many requests. Please wait a moment and try again"
	case http.StatusUnauthorized: // 401
		return "Authentication failed — check your API key"
	case http.StatusForbidden: // 403
		return "Access denied — your API key may lack permissions for this model"
	case http.StatusNotFound: // 404
		return "Model not found — check the model name in your config"
	case http.StatusBadRequest: // 400
		return "Bad request — the API rejected the request"
	case http.StatusPaymentRequired: // 402
		return "Payment required — check your account billing"
	case http.StatusRequestEntityTooLarge: // 413
		return "Request too large — the prompt exceeds the API's size limit"
	case http.StatusServiceUnavailable: // 503
		return "Service unavailable — the API is temporarily down, try again shortly"
	case http.StatusGatewayTimeout: // 504
		return "Gateway timeout — the API took too long to respond"
	case http.StatusInternalServerError: // 500
		return "Server error — the API encountered an internal error"
	default:
		return fmt.Sprintf("API error (HTTP %d)", code)
	}
}

// extractErrorMessage tries to pull a human-readable message from known
// API error response formats.
func extractErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	// Anthropic format: {"type":"error","error":{"type":"...","message":"..."}}
	var anthropic struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &anthropic) == nil && anthropic.Error.Message != "" {
		return anthropic.Error.Message
	}

	// OpenAI format: {"error":{"message":"...","type":"..."}}
	// Same struct works — both use error.message.

	// Ollama format: {"error":"message string"}
	var ollama struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &ollama) == nil && ollama.Error != "" {
		return ollama.Error
	}

	return ""
}
