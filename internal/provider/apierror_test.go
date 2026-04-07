package provider

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatAPIError_RateLimit(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`)
	err := FormatAPIError(http.StatusTooManyRequests, body, nil)
	assert.Contains(t, err.Error(), "Rate limited")
	assert.Contains(t, err.Error(), "Rate limit exceeded")
	assert.NotContains(t, err.Error(), "429")
	assert.NotContains(t, err.Error(), `"type"`)
}

func TestFormatAPIError_Unauthorized(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid API key","type":"auth_error"}}`)
	err := FormatAPIError(http.StatusUnauthorized, body, nil)
	assert.Contains(t, err.Error(), "Authentication failed")
	assert.Contains(t, err.Error(), "Invalid API key")
}

func TestFormatAPIError_NotFound(t *testing.T) {
	body := []byte(`{"error":{"message":"Model not found: gpt-99"}}`)
	err := FormatAPIError(http.StatusNotFound, body, nil)
	assert.Contains(t, err.Error(), "Model not found")
	assert.Contains(t, err.Error(), "gpt-99")
}

func TestFormatAPIError_NotFoundWithHTTPDetails(t *testing.T) {
	body := []byte(`{"error":{"message":"The model does not exist: gpt-99"}}`)
	req, _ := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", nil)
	err := FormatAPIError(http.StatusNotFound, body, req)

	msg := err.Error()
	assert.Contains(t, msg, "Model not found")
	assert.Contains(t, msg, "The model does not exist: gpt-99")
	assert.Contains(t, msg, "HTTP Request: POST https://api.openai.com/v1/chat/completions")
	assert.Contains(t, msg, "HTTP Response: 404")
	assert.Contains(t, msg, "Response Body:")
}

func TestFormatAPIError_NotFoundOllamaWithHTTPDetails(t *testing.T) {
	body := []byte(`{"error":"model 'llama3' not found"}`)
	req, _ := http.NewRequest(http.MethodPost, "http://localhost:11434/api/chat", nil)
	err := FormatAPIError(http.StatusNotFound, body, req)

	msg := err.Error()
	assert.Contains(t, msg, "model 'llama3' not found")
	assert.Contains(t, msg, "HTTP Request: POST http://localhost:11434/api/chat")
	assert.Contains(t, msg, "HTTP Response: 404")
}

func TestFormatAPIError_OllamaFormat(t *testing.T) {
	body := []byte(`{"error":"model 'llama3' not found"}`)
	err := FormatAPIError(http.StatusNotFound, body, nil)
	assert.Contains(t, err.Error(), "model 'llama3' not found")
}

func TestFormatAPIError_EmptyBody(t *testing.T) {
	err := FormatAPIError(http.StatusTooManyRequests, nil, nil)
	assert.Contains(t, err.Error(), "Rate limited")
	assert.NotContains(t, err.Error(), "null")
}

func TestFormatAPIError_UnparseableBody(t *testing.T) {
	body := []byte("Internal Server Error")
	err := FormatAPIError(http.StatusInternalServerError, body, nil)
	assert.Contains(t, err.Error(), "Server error")
	assert.Contains(t, err.Error(), "Internal Server Error")
}

func TestFormatAPIError_TruncatesLongBody(t *testing.T) {
	body := []byte(strings.Repeat("x", 500))
	err := FormatAPIError(http.StatusBadRequest, body, nil)
	assert.Contains(t, err.Error(), "...")
	assert.Less(t, len(err.Error()), 300)
}

func TestFormatAPIError_PaymentRequired(t *testing.T) {
	err := FormatAPIError(http.StatusPaymentRequired, []byte(`{"error":{"message":"Insufficient credits"}}`), nil)
	assert.Contains(t, err.Error(), "Payment required")
	assert.Contains(t, err.Error(), "Insufficient credits")
}

func TestFormatAPIError_ServiceUnavailable(t *testing.T) {
	err := FormatAPIError(http.StatusServiceUnavailable, nil, nil)
	assert.Contains(t, err.Error(), "Service unavailable")
}

func TestFormatAPIError_UnknownStatus(t *testing.T) {
	err := FormatAPIError(418, []byte(`{"error":{"message":"I'm a teapot"}}`), nil)
	assert.Contains(t, err.Error(), "HTTP 418")
	assert.Contains(t, err.Error(), "I'm a teapot")
}

// --- ClassifyAPIError tests ---

func TestClassifyAPIError_RateLimited(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`)
	pe := ClassifyAPIError(http.StatusTooManyRequests, body, nil, "anthropic")
	require.NotNil(t, pe)
	assert.Equal(t, ErrRateLimited, pe.Kind)
	assert.Equal(t, "anthropic", pe.Provider)
	assert.True(t, pe.IsRetryable())
	assert.Contains(t, pe.Message, "Rate limit exceeded")
}

func TestClassifyAPIError_RateLimited_RetryAfter(t *testing.T) {
	body := []byte(`{"error":{"message":"too many requests"}}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"30"}},
	}
	pe := ClassifyAPIErrorWithResponse(http.StatusTooManyRequests, body, nil, "openai", resp.Header)
	require.NotNil(t, pe)
	assert.Equal(t, ErrRateLimited, pe.Kind)
	assert.Equal(t, 30*time.Second, pe.RetryAfter)
}

func TestClassifyAPIError_AuthFailed(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid API key"}}`)
	pe := ClassifyAPIError(http.StatusUnauthorized, body, nil, "openai")
	require.NotNil(t, pe)
	assert.Equal(t, ErrAuthFailed, pe.Kind)
	assert.False(t, pe.IsRetryable())
	assert.Contains(t, pe.Message, "Invalid API key")
}

func TestClassifyAPIError_AuthFailed_403(t *testing.T) {
	body := []byte(`{"error":{"message":"Forbidden"}}`)
	pe := ClassifyAPIError(http.StatusForbidden, body, nil, "anthropic")
	require.NotNil(t, pe)
	assert.Equal(t, ErrAuthFailed, pe.Kind)
}

func TestClassifyAPIError_ModelNotFound(t *testing.T) {
	body := []byte(`{"error":{"message":"The model does not exist: gpt-99"}}`)
	req, _ := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", nil)
	pe := ClassifyAPIError(http.StatusNotFound, body, req, "openai")
	require.NotNil(t, pe)
	assert.Equal(t, ErrModelNotFound, pe.Kind)
	assert.False(t, pe.IsRetryable())
	assert.Equal(t, http.StatusNotFound, pe.StatusCode)
}

func TestClassifyAPIError_ContextOverflow_Status413(t *testing.T) {
	body := []byte(`{"error":{"message":"Request too large"}}`)
	pe := ClassifyAPIError(http.StatusRequestEntityTooLarge, body, nil, "anthropic")
	require.NotNil(t, pe)
	assert.Equal(t, ErrContextOverflow, pe.Kind)
	assert.False(t, pe.IsRetryable())
}

func TestClassifyAPIError_ContextOverflow_MessagePattern(t *testing.T) {
	patterns := []string{
		`{"error":{"message":"maximum context length exceeded"}}`,
		`{"error":{"message":"prompt is too long"}}`,
		`{"error":{"message":"context_length_exceeded for model"}}`,
		`{"type":"error","error":{"message":"This request would exceed the model's maximum context length"}}`,
	}
	for _, body := range patterns {
		pe := ClassifyAPIError(http.StatusBadRequest, []byte(body), nil, "openai")
		require.NotNil(t, pe, "body: %s", body)
		assert.Equal(t, ErrContextOverflow, pe.Kind, "body: %s", body)
	}
}

func TestClassifyAPIError_ServerError(t *testing.T) {
	for _, code := range []int{500, 502, 503, 504} {
		pe := ClassifyAPIError(code, []byte(`{"error":{"message":"internal error"}}`), nil, "anthropic")
		require.NotNil(t, pe, "status: %d", code)
		assert.Equal(t, ErrServerError, pe.Kind, "status: %d", code)
		assert.True(t, pe.IsRetryable(), "status: %d", code)
	}
}

func TestClassifyAPIError_ContentFiltered(t *testing.T) {
	patterns := []string{
		`{"error":{"message":"content was blocked by our safety system"}}`,
		`{"error":{"message":"Output blocked by content filtering policy"}}`,
	}
	for _, body := range patterns {
		pe := ClassifyAPIError(http.StatusBadRequest, []byte(body), nil, "anthropic")
		require.NotNil(t, pe, "body: %s", body)
		assert.Equal(t, ErrContentFiltered, pe.Kind, "body: %s", body)
	}
}

func TestClassifyAPIError_QuotaExceeded(t *testing.T) {
	pe := ClassifyAPIError(http.StatusPaymentRequired, []byte(`{"error":{"message":"Insufficient credits"}}`), nil, "openai")
	require.NotNil(t, pe)
	assert.Equal(t, ErrQuotaExceeded, pe.Kind)
}

func TestClassifyAPIError_QuotaExceeded_As429(t *testing.T) {
	// Providers like OpenAI surface quota exhaustion as 429 with specific message.
	pe := ClassifyAPIError(http.StatusTooManyRequests, []byte(`{"error":{"message":"You exceeded your current quota, please check your plan and billing details"}}`), nil, "openai")
	require.NotNil(t, pe)
	assert.Equal(t, ErrQuotaExceeded, pe.Kind)
	assert.False(t, pe.IsRetryable(), "quota exhaustion should not be retryable")
}

func TestClassifyAPIError_InvalidRequest(t *testing.T) {
	body := []byte(`{"error":{"message":"invalid parameter: temperature must be between 0 and 2"}}`)
	pe := ClassifyAPIError(http.StatusBadRequest, body, nil, "openai")
	require.NotNil(t, pe)
	assert.Equal(t, ErrInvalidRequest, pe.Kind)
}

func TestClassifyAPIError_UnknownStatus(t *testing.T) {
	pe := ClassifyAPIError(418, []byte(`{"error":{"message":"I'm a teapot"}}`), nil, "test")
	require.NotNil(t, pe)
	assert.Equal(t, ErrOther, pe.Kind)
	assert.Equal(t, 418, pe.StatusCode)
}

func TestClassifyAPIError_EmptyBody(t *testing.T) {
	pe := ClassifyAPIError(http.StatusTooManyRequests, nil, nil, "anthropic")
	require.NotNil(t, pe)
	assert.Equal(t, ErrRateLimited, pe.Kind)
}

func TestClassifyAPIError_PreservesLegacyBehavior(t *testing.T) {
	// FormatAPIError should still return the same user-friendly strings.
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`)
	legacyErr := FormatAPIError(http.StatusTooManyRequests, body, nil)
	assert.Contains(t, legacyErr.Error(), "Rate limited")
	assert.Contains(t, legacyErr.Error(), "Rate limit exceeded")

	// And it should be extractable as a ProviderError.
	var pe *ProviderError
	assert.True(t, errors.As(legacyErr, &pe))
	assert.Equal(t, ErrRateLimited, pe.Kind)
}
