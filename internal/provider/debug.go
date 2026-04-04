package provider

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// DebugLogger is a function that logs debug messages, matching log.Printf signature.
type DebugLogger func(format string, args ...any)

// DebugLogConfigurer is implemented by providers that support debug logging.
// Defined here so the factory can type-assert without importing provider sub-packages.
type DebugLogConfigurer interface {
	SetDebugLogger(logger DebugLogger)
}

// EnableDebugLogging sets a stderr-based debug logger on the provider if it
// supports the DebugLogConfigurer interface.
func EnableDebugLogging(p LLMProvider) {
	if dlc, ok := p.(DebugLogConfigurer); ok {
		dlc.SetDebugLogger(log.Printf)
	}
}

// LogRequest logs the HTTP request details (method, URL, headers, body) at debug level.
// API keys in headers are redacted.
func LogRequest(logger DebugLogger, httpReq *http.Request, body []byte) {
	if logger == nil {
		return
	}

	logger("[DEBUG] >>> HTTP Request: %s %s", httpReq.Method, httpReq.URL.String())

	// Log headers with redacted sensitive values.
	for name, values := range httpReq.Header {
		val := strings.Join(values, ", ")
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "x-api-key" {
			if len(val) > 8 {
				val = val[:8] + "..." + val[len(val)-4:]
			} else {
				val = "***"
			}
		}
		logger("[DEBUG] >>> Header: %s: %s", name, val)
	}

	// Log request body (pretty-printed JSON if possible).
	if len(body) > 0 {
		var pretty json.RawMessage
		if json.Unmarshal(body, &pretty) == nil {
			indented, err := json.MarshalIndent(pretty, "", "  ")
			if err == nil {
				logger("[DEBUG] >>> Body:\n%s", string(indented))
				return
			}
		}
		if len(body) > 2000 {
			logger("[DEBUG] >>> Body (%d bytes, truncated):\n%s...", len(body), string(body[:2000]))
		} else {
			logger("[DEBUG] >>> Body:\n%s", string(body))
		}
	}
}

// LogResponse logs the HTTP response details (status, headers, body) at debug level.
func LogResponse(logger DebugLogger, statusCode int, headers http.Header, body []byte) {
	if logger == nil {
		return
	}

	logger("[DEBUG] <<< HTTP Response: %d %s", statusCode, http.StatusText(statusCode))

	for name, values := range headers {
		logger("[DEBUG] <<< Header: %s: %s", name, strings.Join(values, ", "))
	}

	if len(body) > 0 {
		if len(body) > 2000 {
			logger("[DEBUG] <<< Body (%d bytes, truncated):\n%s...", len(body), string(body[:2000]))
		} else {
			logger("[DEBUG] <<< Body:\n%s", string(body))
		}
	}
}
