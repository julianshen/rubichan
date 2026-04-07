package provider

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestProviderError_Error(t *testing.T) {
	tests := []struct {
		kind     ErrorKind
		provider string
		msg      string
		want     string
	}{
		{ErrRateLimited, "anthropic", "slow down", "rate limited (anthropic): slow down"},
		{ErrAuthFailed, "openai", "bad key", "auth failed (openai): bad key"},
		{ErrContextOverflow, "anthropic", "too long", "context overflow (anthropic): too long"},
		{ErrModelNotFound, "ollama", "no such model", "model not found (ollama): no such model"},
		{ErrServerError, "anthropic", "500", "server error (anthropic): 500"},
		{ErrStreamError, "openai", "broken pipe", "stream error (openai): broken pipe"},
		{ErrContentFiltered, "anthropic", "blocked", "content filtered (anthropic): blocked"},
		{ErrInvalidRequest, "openai", "bad param", "invalid request (openai): bad param"},
		{ErrQuotaExceeded, "anthropic", "limit hit", "quota exceeded (anthropic): limit hit"},
		{ErrOther, "ollama", "mystery", "provider error (ollama): mystery"},
	}

	for _, tt := range tests {
		pe := &ProviderError{
			Kind:     tt.kind,
			Provider: tt.provider,
			Message:  tt.msg,
		}
		if got := pe.Error(); got != tt.want {
			t.Errorf("ErrorKind %d: got %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestProviderError_IsRetryable(t *testing.T) {
	retryable := []ErrorKind{ErrRateLimited, ErrServerError, ErrStreamError}
	notRetryable := []ErrorKind{
		ErrAuthFailed, ErrContextOverflow, ErrModelNotFound,
		ErrContentFiltered, ErrInvalidRequest, ErrQuotaExceeded, ErrOther,
	}

	for _, kind := range retryable {
		pe := &ProviderError{Kind: kind, Provider: "test", Message: "test"}
		if !pe.IsRetryable() {
			t.Errorf("ErrorKind %d should be retryable", kind)
		}
	}

	for _, kind := range notRetryable {
		pe := &ProviderError{Kind: kind, Provider: "test", Message: "test"}
		if pe.IsRetryable() {
			t.Errorf("ErrorKind %d should NOT be retryable", kind)
		}
	}
}

func TestProviderError_IsRetryable_ExplicitOverride(t *testing.T) {
	// A normally non-retryable error can be marked retryable explicitly.
	pe := &ProviderError{
		Kind:      ErrOther,
		Provider:  "test",
		Message:   "transient glitch",
		Retryable: true,
	}
	if !pe.IsRetryable() {
		t.Error("explicit Retryable=true should make IsRetryable() return true")
	}
}

func TestProviderError_RetryAfter(t *testing.T) {
	pe := &ProviderError{
		Kind:       ErrRateLimited,
		Provider:   "anthropic",
		Message:    "rate limited",
		RetryAfter: 30 * time.Second,
	}
	if pe.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter: got %v, want 30s", pe.RetryAfter)
	}
}

func TestProviderError_Suggestions(t *testing.T) {
	pe := &ProviderError{
		Kind:        ErrModelNotFound,
		Provider:    "openai",
		Message:     "model gpt-9 not found",
		Suggestions: []string{"gpt-4o", "gpt-4o-mini"},
	}
	if len(pe.Suggestions) != 2 {
		t.Fatalf("Suggestions: got %d, want 2", len(pe.Suggestions))
	}
}

func TestProviderError_ErrorsAs(t *testing.T) {
	pe := &ProviderError{
		Kind:     ErrAuthFailed,
		Provider: "anthropic",
		Message:  "invalid api key",
	}
	wrapped := fmt.Errorf("provider stream: %w", pe)

	var target *ProviderError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As should find *ProviderError in wrapped error")
	}
	if target.Kind != ErrAuthFailed {
		t.Errorf("Kind: got %d, want %d", target.Kind, ErrAuthFailed)
	}
}

func TestProviderError_ErrorsIs(t *testing.T) {
	pe := &ProviderError{
		Kind:     ErrRateLimited,
		Provider: "openai",
		Message:  "too many requests",
	}
	// ProviderError is not comparable with errors.Is by default,
	// but errors.As is the primary mechanism. Verify it doesn't panic.
	if errors.Is(pe, fmt.Errorf("unrelated")) {
		t.Error("should not match unrelated error")
	}
}

func TestErrorKind_String(t *testing.T) {
	tests := []struct {
		kind ErrorKind
		want string
	}{
		{ErrUnknown, "provider error"},
		{ErrRateLimited, "rate limited"},
		{ErrAuthFailed, "auth failed"},
		{ErrContextOverflow, "context overflow"},
		{ErrModelNotFound, "model not found"},
		{ErrServerError, "server error"},
		{ErrStreamError, "stream error"},
		{ErrContentFiltered, "content filtered"},
		{ErrInvalidRequest, "invalid request"},
		{ErrQuotaExceeded, "quota exceeded"},
		{ErrOther, "provider error"},
		{ErrorKind(999), "provider error"},
	}

	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("ErrorKind(%d).String(): got %q, want %q", tt.kind, got, tt.want)
		}
	}
}
