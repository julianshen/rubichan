package wiki

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestIsResponseTruncated(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"complete sentence", "This is complete.\n", false},
		{"ends with heading", "## Section\nContent here.\n", false},
		{"mid-sentence", "The Todo", true},
		{"mid-word", "The applic", true},
		{"ends with colon", "The following:", true},
		{"ends with comma", "including foo, bar,", true},
		{"ends with semicolon", "var x = 1;", true},
		{"code block open", "```go\nfunc main() {\n", true},
		{"code block closed", "```go\nfunc main() {}\n```\n", false},
		{"empty", "", false},
		{"list item", "- Item one\n- Item two\n", false},
		{"table row", "| A | B |\n|---|---|\n| 1 | 2 |\n", false},
		{"ends with exclamation", "Done!\n", false},
		{"ends with question", "Is this done?\n", false},
		{"ends with paren", "see section (above)", true},
		{"whitespace only", "   \t  ", false},
		{"heading only", "## Overview", false},
		{"star list", "* Item one\n* Item two\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isResponseTruncated(tt.input); got != tt.want {
				t.Errorf("isResponseTruncated(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// sequenceLLMCompleter returns responses in order, one per call.
type sequenceLLMCompleter struct {
	responses []string
	errors    []error
	idx       int
	mu        sync.Mutex
}

func (s *sequenceLLMCompleter) Complete(_ context.Context, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.idx
	s.idx++
	if i < len(s.errors) && s.errors[i] != nil {
		return "", s.errors[i]
	}
	if i < len(s.responses) {
		return s.responses[i], nil
	}
	return "default", nil
}

func TestCompleteLLMResponse_NoRetryWhenComplete(t *testing.T) {
	llm := &sequenceLLMCompleter{
		responses: []string{"This is a complete response."},
	}
	resp, err := completeLLMResponse(context.Background(), "prompt", llm, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "This is a complete response." {
		t.Errorf("got %q, want complete response unchanged", resp)
	}
	if llm.idx != 1 {
		t.Errorf("expected 1 LLM call, got %d", llm.idx)
	}
}

func TestCompleteLLMResponse_RetryOnTruncation(t *testing.T) {
	llm := &sequenceLLMCompleter{
		responses: []string{
			"The applic",         // truncated
			"ation is complete.", // continuation
		},
	}
	resp, err := completeLLMResponse(context.Background(), "prompt", llm, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "The applic" + "ation is complete."
	if resp != want {
		t.Errorf("got %q, want %q", resp, want)
	}
	if llm.idx != 2 {
		t.Errorf("expected 2 LLM calls, got %d", llm.idx)
	}
}

func TestCompleteLLMResponse_RetryErrorReturnsPartial(t *testing.T) {
	llm := &sequenceLLMCompleter{
		responses: []string{"The applic", ""},
		errors:    []error{nil, errors.New("LLM unavailable")},
	}
	resp, err := completeLLMResponse(context.Background(), "prompt", llm, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "The applic" {
		t.Errorf("got %q, want partial response", resp)
	}
}

func TestCompleteLLMResponse_InitialError(t *testing.T) {
	llm := &sequenceLLMCompleter{
		errors: []error{errors.New("initial failure")},
	}
	_, err := completeLLMResponse(context.Background(), "prompt", llm, 1)
	if err == nil {
		t.Fatal("expected error on initial LLM failure")
	}
}

func TestCompleteLLMResponse_RespectsMaxRetries(t *testing.T) {
	llm := &sequenceLLMCompleter{
		responses: []string{
			"The applic",   // truncated
			"ation contin", // still truncated
			"ues here.",    // complete — but should not be reached with maxRetries=1
		},
	}
	resp, err := completeLLMResponse(context.Background(), "prompt", llm, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With maxRetries=1, only one retry happens: first call + one continuation.
	if llm.idx != 2 {
		t.Errorf("expected 2 LLM calls (maxRetries=1), got %d", llm.idx)
	}
	want := "The applic" + "ation contin"
	if resp != want {
		t.Errorf("got %q, want %q", resp, want)
	}
}
