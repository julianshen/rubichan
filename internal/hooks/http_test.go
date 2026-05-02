package hooks

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecuteHTTPHook(t *testing.T) {
	var receivedPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"continue": true}`))
	}))
	defer server.Close()

	cfg := UserHookConfig{
		Event:   EventPreTool,
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	result, err := executeHTTPHook(cfg, map[string]interface{}{"tool": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("expected continue=true")
	}
	data, ok := receivedPayload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map in payload, got %T", receivedPayload["data"])
	}
	if data["tool"] != "test" {
		t.Errorf("expected tool=test in payload.data, got %v", data["tool"])
	}
}

func TestExecuteHTTPHook_NonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer server.Close()

	cfg := UserHookConfig{
		Event:   EventPreTool,
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	result, err := executeHTTPHook(cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("expected continue=true for non-JSON response")
	}
}

func TestExecuteHTTPHook_CancelResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cancel": true, "message": "blocked"}`))
	}))
	defer server.Close()

	cfg := UserHookConfig{
		Event:   EventPreTool,
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	result, err := executeHTTPHook(cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Cancel {
		t.Error("expected cancel=true")
	}
	if result.Message != "blocked" {
		t.Errorf("expected message=blocked, got %q", result.Message)
	}
}

func TestExecuteHTTPHook_NetworkError(t *testing.T) {
	cfg := UserHookConfig{
		Event:   EventPreTool,
		URL:     "http://localhost:1", // unlikely to be listening
		Timeout: 1 * time.Second,
	}

	result, err := executeHTTPHook(cfg, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if !result.Continue {
		t.Error("expected continue=true on network error (fail-open)")
	}
}
