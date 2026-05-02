package hooks

import "testing"

func TestPromptHook(t *testing.T) {
	hook := PromptHook{
		Find:    "OLD",
		Replace: "NEW",
	}
	result := hook.Transform("hello OLD world")
	if result != "hello NEW world" {
		t.Errorf("expected 'hello NEW world', got %q", result)
	}
}

func TestPromptHook_NoMatch(t *testing.T) {
	hook := PromptHook{
		Find:    "MISSING",
		Replace: "NEW",
	}
	result := hook.Transform("hello world")
	if result != "hello world" {
		t.Errorf("expected unchanged text, got %q", result)
	}
}

func TestPromptHookChain(t *testing.T) {
	chain := PromptHookChain{
		{Find: "A", Replace: "B"},
		{Find: "B", Replace: "C"},
	}
	result := chain.Transform("A")
	if result != "C" {
		t.Errorf("expected 'C', got %q", result)
	}
}
