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

	t.Run("12b model is small", func(t *testing.T) {
		caps := DetectCapabilities("ollama", "phi-3-medium:12b")
		if caps.MaxToolCount != 8 {
			t.Errorf("expected MaxToolCount=8 for 12b model, got %d", caps.MaxToolCount)
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

func TestDetectCapabilities_TargetModels(t *testing.T) {
	// Gemini 3.1 Pro: strong model, no hints needed.
	t.Run("gemini-3.1-pro is strong", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "google/gemini-3.1-pro-preview")
		if caps.NeedsToolDiscoveryHint {
			t.Error("Gemini 3.1 Pro should not need discovery hint")
		}
		if caps.MaxToolCount != 0 {
			t.Errorf("Gemini 3.1 Pro should have unlimited tools, got %d", caps.MaxToolCount)
		}
	})

	// Gemini 2.5 Pro: also strong.
	t.Run("gemini-2.5-pro is strong", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "google/gemini-2.5-pro")
		if caps.NeedsToolDiscoveryHint {
			t.Error("Gemini 2.5 Pro should not need discovery hint")
		}
	})

	// GLM 5: hinted, large model gets MaxToolCount=15.
	t.Run("glm-5 gets hint and tool limit", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "z-ai/glm-5")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("GLM 5 should get discovery hint")
		}
		if caps.MaxToolCount != 15 {
			t.Errorf("GLM 5 should get MaxToolCount=15, got %d", caps.MaxToolCount)
		}
	})

	// GLM 5 Turbo: same profile as GLM 5.
	t.Run("glm-5-turbo gets hint and tool limit", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "z-ai/glm-5-turbo")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("GLM 5 Turbo should get discovery hint")
		}
		if caps.MaxToolCount != 15 {
			t.Errorf("GLM 5 Turbo should get MaxToolCount=15, got %d", caps.MaxToolCount)
		}
	})

	// Claude Opus 4.6 via OpenRouter: strong, no hints.
	t.Run("claude-opus-4.6 via openrouter is strong", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "anthropic/claude-opus-4.6")
		if caps.NeedsToolDiscoveryHint {
			t.Error("Claude Opus 4.6 should not need discovery hint")
		}
		if caps.MaxToolCount != 0 {
			t.Errorf("Claude Opus 4.6 should have unlimited tools, got %d", caps.MaxToolCount)
		}
	})

	// Qwen 3.5 397B: hinted, no small-model limit.
	t.Run("qwen-3.5-397b gets hint but no small limit", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "qwen/qwen3.5-397b-a17b")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("Qwen 3.5 397B should get discovery hint")
		}
		if caps.MaxToolCount != 0 {
			t.Errorf("Qwen 3.5 397B should have unlimited tools, got %d", caps.MaxToolCount)
		}
	})

	// Qwen 3.5 9B: hinted + small model limit.
	t.Run("qwen-3.5-9b gets hint and small limit", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "qwen/qwen3.5-9b")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("Qwen 3.5 9B should get discovery hint")
		}
		if caps.MaxToolCount != 8 {
			t.Errorf("Qwen 3.5 9B should get MaxToolCount=8, got %d", caps.MaxToolCount)
		}
	})

	// Kimi K2.5: strong model, no hints.
	t.Run("kimi-k2.5 is strong", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "moonshotai/kimi-k2.5")
		if caps.NeedsToolDiscoveryHint {
			t.Error("Kimi K2.5 should not need discovery hint")
		}
		if caps.MaxToolCount != 0 {
			t.Errorf("Kimi K2.5 should have unlimited tools, got %d", caps.MaxToolCount)
		}
	})

	// Kimi K2: also strong.
	t.Run("kimi-k2 is strong", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "moonshotai/kimi-k2")
		if caps.NeedsToolDiscoveryHint {
			t.Error("Kimi K2 should not need discovery hint")
		}
	})

	// MiniMax M2.7: hinted, large limit.
	t.Run("minimax-m2.7 gets hint and tool limit", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "minimax/minimax-m2.7")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("MiniMax M2.7 should get discovery hint")
		}
		if caps.MaxToolCount != 15 {
			t.Errorf("MiniMax M2.7 should get MaxToolCount=15, got %d", caps.MaxToolCount)
		}
	})

	// MiniMax M1: hinted, reasoning model with 1M context.
	t.Run("minimax-m1 gets hint and tool limit", func(t *testing.T) {
		caps := DetectCapabilities("openrouter", "minimax/minimax-m1")
		if !caps.NeedsToolDiscoveryHint {
			t.Error("MiniMax M1 should get discovery hint")
		}
		if caps.MaxToolCount != 15 {
			t.Errorf("MiniMax M1 should get MaxToolCount=15, got %d", caps.MaxToolCount)
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
		"model-10b",
		"model-11b",
		"model-12b",
		"model-13b",
		"model-14b",
		"model-1.5b",
		"model-3.5b",
		"phi-3-medium-12b-instruct",
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
		"model-22b",
		"model-27b",
		"model-32b",
	}
	for _, m := range largeCases {
		t.Run(m, func(t *testing.T) {
			if isSmallModel(m) {
				t.Errorf("expected isSmallModel(%q)=false", m)
			}
		})
	}
}

func TestDefaultCapabilities(t *testing.T) {
	caps := DefaultCapabilities()
	if !caps.SupportsNativeToolUse {
		t.Error("default should have SupportsNativeToolUse=true")
	}
	if !caps.SupportsSystemPrompt {
		t.Error("default should have SupportsSystemPrompt=true")
	}
	if caps.NeedsToolDiscoveryHint {
		t.Error("default should have NeedsToolDiscoveryHint=false")
	}
	if caps.MaxToolCount != 0 {
		t.Errorf("default should have MaxToolCount=0, got %d", caps.MaxToolCount)
	}
}

func TestDetectCapabilitiesEmptyProvider(t *testing.T) {
	// Empty provider falls through to OpenAI-compat path with optimistic defaults.
	caps := DetectCapabilities("", "some-model")
	if !caps.SupportsNativeToolUse {
		t.Error("empty provider should still default to SupportsNativeToolUse=true")
	}
	if !caps.NeedsToolDiscoveryHint {
		t.Error("empty provider with unknown model should get NeedsToolDiscoveryHint=true")
	}
}
