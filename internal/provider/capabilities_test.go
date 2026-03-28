package provider

import (
	"testing"
)

func TestDetectCapabilities_Anthropic(t *testing.T) {
	t.Run("claude-3-5-sonnet gets full capabilities", func(t *testing.T) {
		caps := DetectCapabilities("anthropic", "claude-3-5-sonnet-20241022")
		if !caps.SupportsNativeToolUse {
			t.Error("expected SupportsNativeToolUse=true")
		}
		if !caps.SupportsStreaming {
			t.Error("expected SupportsStreaming=true")
		}
		if !caps.SupportsSystemPrompt {
			t.Error("expected SupportsSystemPrompt=true")
		}
		if caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=false for non-haiku")
		}
		if caps.MaxToolCount != 0 {
			t.Errorf("expected MaxToolCount=0, got %d", caps.MaxToolCount)
		}
	})

	t.Run("haiku gets NeedsToolDiscoveryHint and MaxToolCount=12", func(t *testing.T) {
		caps := DetectCapabilities("anthropic", "claude-3-haiku-20240307")
		if !caps.SupportsNativeToolUse {
			t.Error("expected SupportsNativeToolUse=true")
		}
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for haiku")
		}
		if caps.MaxToolCount != 12 {
			t.Errorf("expected MaxToolCount=12 for haiku, got %d", caps.MaxToolCount)
		}
	})

	t.Run("claude-haiku-3-5 is also haiku", func(t *testing.T) {
		caps := DetectCapabilities("anthropic", "claude-haiku-3-5")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for haiku variant")
		}
		if caps.MaxToolCount != 12 {
			t.Errorf("expected MaxToolCount=12 for haiku variant, got %d", caps.MaxToolCount)
		}
	})
}

func TestDetectCapabilities_Ollama(t *testing.T) {
	t.Run("all ollama models get NeedsToolDiscoveryHint", func(t *testing.T) {
		caps := DetectCapabilities("ollama", "llama3.1:70b")
		if !caps.SupportsNativeToolUse {
			t.Error("expected SupportsNativeToolUse=true")
		}
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for ollama")
		}
	})

	t.Run("small ollama model gets MaxToolCount=8", func(t *testing.T) {
		caps := DetectCapabilities("ollama", "qwen2.5:7b")
		if caps.MaxToolCount != 8 {
			t.Errorf("expected MaxToolCount=8 for 7b model, got %d", caps.MaxToolCount)
		}
	})

	t.Run("14b is still small, gets MaxToolCount=8", func(t *testing.T) {
		caps := DetectCapabilities("ollama", "qwen2.5-coder:14b")
		if caps.MaxToolCount != 8 {
			t.Errorf("expected MaxToolCount=8 for 14b model, got %d", caps.MaxToolCount)
		}
	})

	t.Run("large ollama model gets MaxToolCount=15", func(t *testing.T) {
		caps := DetectCapabilities("ollama", "llama3.1:70b")
		if caps.MaxToolCount != 15 {
			t.Errorf("expected MaxToolCount=15 for 70b model, got %d", caps.MaxToolCount)
		}
	})

	t.Run("nano model is small", func(t *testing.T) {
		caps := DetectCapabilities("ollama", "gemma2:nano")
		if caps.MaxToolCount != 8 {
			t.Errorf("expected MaxToolCount=8 for nano model, got %d", caps.MaxToolCount)
		}
	})
}

func TestDetectCapabilities_OpenAI(t *testing.T) {
	t.Run("gpt-4 gets full capabilities", func(t *testing.T) {
		caps := DetectCapabilities("openai", "gpt-4o")
		if !caps.SupportsNativeToolUse {
			t.Error("expected SupportsNativeToolUse=true")
		}
		if caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=false for gpt-4")
		}
		if caps.MaxToolCount != 0 {
			t.Errorf("expected MaxToolCount=0 for gpt-4, got %d", caps.MaxToolCount)
		}
	})

	t.Run("gpt-5 gets full capabilities", func(t *testing.T) {
		caps := DetectCapabilities("openai", "gpt-5")
		if caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=false for gpt-5")
		}
	})

	t.Run("claude via openai compat gets full capabilities", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "anthropic/claude-3-5-sonnet")
		if caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=false for claude via openai compat")
		}
	})

	t.Run("qwen gets NeedsToolDiscoveryHint", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "qwen/qwen3-coder:free")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for qwen")
		}
	})

	t.Run("small qwen also gets MaxToolCount=8", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "qwen/qwen2.5-7b-instruct")
		if caps.MaxToolCount != 8 {
			t.Errorf("expected MaxToolCount=8 for small qwen, got %d", caps.MaxToolCount)
		}
	})

	t.Run("large qwen gets NeedsToolDiscoveryHint but not small MaxToolCount", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "qwen/qwen-72b")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for qwen")
		}
		if caps.MaxToolCount != 0 {
			t.Errorf("expected MaxToolCount=0 for large qwen, got %d", caps.MaxToolCount)
		}
	})

	t.Run("llama gets NeedsToolDiscoveryHint", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "meta-llama/llama-3.1-70b-instruct")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for llama")
		}
	})

	t.Run("small llama gets MaxToolCount=8", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "meta-llama/llama-3.2-3b-instruct")
		if caps.MaxToolCount != 8 {
			t.Errorf("expected MaxToolCount=8 for 3b llama, got %d", caps.MaxToolCount)
		}
	})

	t.Run("gemma gets NeedsToolDiscoveryHint", func(t *testing.T) {
		caps := DetectCapabilities("openai", "google/gemma-2-9b-it")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for gemma")
		}
	})

	t.Run("mistral gets NeedsToolDiscoveryHint", func(t *testing.T) {
		caps := DetectCapabilities("openai", "mistralai/mistral-7b-instruct")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for mistral")
		}
	})

	t.Run("deepseek gets NeedsToolDiscoveryHint", func(t *testing.T) {
		caps := DetectCapabilities("openai", "deepseek/deepseek-coder-v2")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for deepseek")
		}
	})

	t.Run("unknown model gets NeedsToolDiscoveryHint as optimistic default", func(t *testing.T) {
		caps := DetectCapabilities("some-provider", "unknown-model-v1")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("expected NeedsToolDiscoveryHint=true for unknown model")
		}
		if !caps.SupportsNativeToolUse {
			t.Error("expected SupportsNativeToolUse=true as optimistic default")
		}
	})
}

func TestIsSmallModel(t *testing.T) {
	smallCases := []string{
		"model-1b",
		"model-2b",
		"model-3b",
		"model-4b",
		"model-7b",
		"model-8b",
		"model-9b",
		"model-13b",
		"model-14b",
		"model-1.5b",
		"model-3.5b",
		"gemma-nano",
		"phi-mini",
		"model-tiny",
		"model-small",
		"LLAMA-7B",                   // case insensitive
		"qwen2.5:7b",                 // colon-separated tag
		"nvidia/nemotron-nano-9b-v2", // multi-part name
	}
	for _, m := range smallCases {
		t.Run(m, func(t *testing.T) {
			if !isSmallModel(m) {
				t.Errorf("expected isSmallModel(%q)=true", m)
			}
		})
	}

	largeCases := []string{
		"llama3.1:70b",
		"qwen2.5:72b",
		"mixtral-8x22b",
		"gpt-4o",
		"claude-3-5-sonnet",
	}
	for _, m := range largeCases {
		t.Run(m, func(t *testing.T) {
			if isSmallModel(m) {
				t.Errorf("expected isSmallModel(%q)=false", m)
			}
		})
	}
}
