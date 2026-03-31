package provider

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
