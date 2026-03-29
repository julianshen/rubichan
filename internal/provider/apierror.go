package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// FormatAPIError produces a human-readable error message from an HTTP
// status code and raw response body. It extracts the message field from
// known API error formats (Anthropic, OpenAI, Ollama) and provides
// friendly descriptions for common status codes.
func FormatAPIError(statusCode int, body []byte) error {
	friendly := friendlyStatus(statusCode)
	message := extractErrorMessage(body)

	if message != "" {
		return fmt.Errorf("%s: %s", friendly, message)
	}

	// Fallback: include truncated raw body for debugging.
	raw := strings.TrimSpace(string(body))
	if len(raw) > 200 {
		raw = raw[:200] + "..."
	}
	if raw != "" {
		return fmt.Errorf("%s (%s)", friendly, raw)
	}
	return fmt.Errorf("%s", friendly)
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
