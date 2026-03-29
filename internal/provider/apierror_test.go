package provider

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatAPIError_RateLimit(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`)
	err := FormatAPIError(http.StatusTooManyRequests, body)
	assert.Contains(t, err.Error(), "Rate limited")
	assert.Contains(t, err.Error(), "Rate limit exceeded")
	assert.NotContains(t, err.Error(), "429")
	assert.NotContains(t, err.Error(), `"type"`)
}

func TestFormatAPIError_Unauthorized(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid API key","type":"auth_error"}}`)
	err := FormatAPIError(http.StatusUnauthorized, body)
	assert.Contains(t, err.Error(), "Authentication failed")
	assert.Contains(t, err.Error(), "Invalid API key")
}

func TestFormatAPIError_NotFound(t *testing.T) {
	body := []byte(`{"error":{"message":"Model not found: gpt-99"}}`)
	err := FormatAPIError(http.StatusNotFound, body)
	assert.Contains(t, err.Error(), "Model not found")
	assert.Contains(t, err.Error(), "gpt-99")
}

func TestFormatAPIError_OllamaFormat(t *testing.T) {
	body := []byte(`{"error":"model 'llama3' not found"}`)
	err := FormatAPIError(http.StatusNotFound, body)
	assert.Contains(t, err.Error(), "model 'llama3' not found")
}

func TestFormatAPIError_EmptyBody(t *testing.T) {
	err := FormatAPIError(http.StatusTooManyRequests, nil)
	assert.Contains(t, err.Error(), "Rate limited")
	assert.NotContains(t, err.Error(), "null")
}

func TestFormatAPIError_UnparseableBody(t *testing.T) {
	body := []byte("Internal Server Error")
	err := FormatAPIError(http.StatusInternalServerError, body)
	assert.Contains(t, err.Error(), "Server error")
	assert.Contains(t, err.Error(), "Internal Server Error")
}

func TestFormatAPIError_TruncatesLongBody(t *testing.T) {
	body := []byte(strings.Repeat("x", 500))
	err := FormatAPIError(http.StatusBadRequest, body)
	assert.Contains(t, err.Error(), "...")
	assert.Less(t, len(err.Error()), 300)
}

func TestFormatAPIError_PaymentRequired(t *testing.T) {
	err := FormatAPIError(http.StatusPaymentRequired, []byte(`{"error":{"message":"Insufficient credits"}}`))
	assert.Contains(t, err.Error(), "Payment required")
	assert.Contains(t, err.Error(), "Insufficient credits")
}

func TestFormatAPIError_ServiceUnavailable(t *testing.T) {
	err := FormatAPIError(http.StatusServiceUnavailable, nil)
	assert.Contains(t, err.Error(), "Service unavailable")
}

func TestFormatAPIError_UnknownStatus(t *testing.T) {
	err := FormatAPIError(418, []byte(`{"error":{"message":"I'm a teapot"}}`))
	assert.Contains(t, err.Error(), "HTTP 418")
	assert.Contains(t, err.Error(), "I'm a teapot")
}
