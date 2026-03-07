package testutil

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNewServer(t *testing.T) {
	server := NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/ping", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if !strings.HasPrefix(server.URL, "mem://") {
		t.Fatalf("server URL = %q, want mem://", server.URL)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", string(body))
	}
}
