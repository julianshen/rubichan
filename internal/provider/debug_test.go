package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogRequest_NilLogger(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://api.example.com/v1/chat", nil)
	// Should not panic with nil logger.
	LogRequest(nil, req, []byte(`{"model":"test"}`))
}

func TestLogRequest_LogsMethodAndURL(t *testing.T) {
	var logs []string
	logger := func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}

	req, _ := http.NewRequest(http.MethodPost, "https://api.example.com/v1/chat", nil)
	req.Header.Set("Content-Type", "application/json")

	LogRequest(logger, req, []byte(`{"model":"gpt-4"}`))

	require.NotEmpty(t, logs)
	assert.Contains(t, logs[0], "POST")
	assert.Contains(t, logs[0], "https://api.example.com/v1/chat")

	// Body should be logged.
	joined := strings.Join(logs, "\n")
	assert.Contains(t, joined, "gpt-4")
}

func TestLogRequest_RedactsAPIKey(t *testing.T) {
	var logs []string
	logger := func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}

	req, _ := http.NewRequest(http.MethodPost, "https://api.example.com/v1/chat", nil)
	req.Header.Set("Authorization", "Bearer sk-1234567890abcdef")
	req.Header.Set("x-api-key", "sk-ant-secret-key-here")

	LogRequest(logger, req, nil)

	joined := strings.Join(logs, "\n")
	// Full key should NOT appear.
	assert.NotContains(t, joined, "sk-1234567890abcdef")
	assert.NotContains(t, joined, "sk-ant-secret-key-here")
	// But partial redacted key should.
	assert.Contains(t, joined, "...")
}

func TestLogResponse_LogsStatusAndBody(t *testing.T) {
	var logs []string
	logger := func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}

	headers := http.Header{"Content-Type": {"application/json"}}
	body := []byte(`{"error":{"message":"model not found"}}`)

	LogResponse(logger, 404, headers, body)

	joined := strings.Join(logs, "\n")
	assert.Contains(t, joined, "404")
	assert.Contains(t, joined, "Not Found")
	assert.Contains(t, joined, "model not found")
}

func TestLogResponse_NilLogger(t *testing.T) {
	// Should not panic.
	LogResponse(nil, 404, nil, []byte("body"))
}

func TestLogRequest_TruncatesLargeBody(t *testing.T) {
	var logs []string
	logger := func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}

	req, _ := http.NewRequest(http.MethodPost, "https://api.example.com/v1/chat", nil)
	largeBody := []byte(strings.Repeat("x", 3000))

	LogRequest(logger, req, largeBody)

	joined := strings.Join(logs, "\n")
	assert.Contains(t, joined, "truncated")
	assert.Contains(t, joined, "...")
}
