package provider

import (
	"regexp"
	"strings"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ModelCapabilities is an alias for the canonical agentsdk.ModelCapabilities.
// All provider code uses provider.ModelCapabilities; the canonical definition
// lives in pkg/agentsdk/.
type ModelCapabilities = agentsdk.ModelCapabilities

// DetectCapabilities returns the ModelCapabilities for the given provider and
// model. providerName is the configured provider identifier (e.g. "anthropic",
// "ollama", "openrouter"). modelID is the model identifier string passed to
// the API.
func DetectCapabilities(providerName, modelID string) ModelCapabilities {
	switch providerName {
	case "anthropic":
		return detectAnthropicCapabilities(modelID)
	case "ollama":
		return detectOllamaCapabilities(modelID)
	default:
		// All other providers use the OpenAI-compatible path.
		return detectOpenAICompatCapabilities(modelID)
	}
}

// detectAnthropicCapabilities returns capabilities for Anthropic models.
// All Anthropic models support native tool use, streaming, and system prompts.
// Haiku models are given a tool-discovery hint and a MaxToolCount of 12 to
// compensate for their reduced context window and instruction-following depth.
func detectAnthropicCapabilities(modelID string) ModelCapabilities {
	caps := ModelCapabilities{
		SupportsNativeToolUse: true,
		SupportsStreaming:     true,
		SupportsSystemPrompt:  true,
	}
	if strings.Contains(strings.ToLower(modelID), "haiku") {
		caps.NeedsToolDiscoveryHint = true
		caps.MaxToolCount = 12
	}
	return caps
}

// detectOllamaCapabilities returns capabilities for Ollama-hosted models.
// All Ollama models support native tool use but benefit from a tool-discovery
// hint. Small models (<=14B parameters) are given a lower tool cap to reduce
// context pressure.
func detectOllamaCapabilities(modelID string) ModelCapabilities {
	caps := ModelCapabilities{
		SupportsNativeToolUse:  true,
		SupportsStreaming:      true,
		SupportsSystemPrompt:   true,
		NeedsToolDiscoveryHint: true,
	}
	if isSmallModel(modelID) {
		caps.MaxToolCount = 8
	} else {
		caps.MaxToolCount = 15
	}
	return caps
}

// knownStrongFamilies are OpenAI-compatible model families with reliable
// tool-use that do not need a discovery hint.
var knownStrongFamilies = []string{"gpt-4", "gpt-5", "claude", "o1", "o3"}

// knownWeakerFamilies are open-weight families that benefit from a
// tool-discovery hint in the system prompt.
var knownWeakerFamilies = []string{"qwen", "llama", "gemma", "mistral", "deepseek"}

// detectOpenAICompatCapabilities returns capabilities for OpenAI-compatible
// providers (OpenRouter, self-hosted, etc.). GPT-4/5 and Claude routing
// through an OpenAI-compat endpoint get full capabilities. Open-weight
// families (Qwen, Llama, Gemma, Mistral, DeepSeek) get a tool-discovery hint
// and, when small, a reduced tool count. Completely unknown models default to
// NeedsToolDiscoveryHint=true as an optimistic safe default.
func detectOpenAICompatCapabilities(modelID string) ModelCapabilities {
	caps := ModelCapabilities{
		SupportsNativeToolUse: true,
		SupportsStreaming:     true,
		SupportsSystemPrompt:  true,
	}

	lower := strings.ToLower(modelID)

	for _, family := range knownStrongFamilies {
		if strings.Contains(lower, family) {
			// Strong model — no additional hints needed.
			return caps
		}
	}

	for _, family := range knownWeakerFamilies {
		if strings.Contains(lower, family) {
			caps.NeedsToolDiscoveryHint = true
			if isSmallModel(modelID) {
				caps.MaxToolCount = 8
			}
			return caps
		}
	}

	// Unknown model: optimistic defaults — assume tool use works but include
	// the discovery hint to maximise reliability.
	caps.NeedsToolDiscoveryHint = true
	return caps
}

// smallModelRe matches size indicators that identify models with <=14B
// parameters. Numeric indicators (e.g. "7b", "13b") are matched only when
// preceded by a non-digit boundary so that "72b" or "22b" do not falsely
// trigger on "2b". Keyword indicators (nano, mini, tiny, small) are matched
// as whole words.
var smallModelRe = regexp.MustCompile(
	`(?i)(?:^|[^0-9])(?:1\.5b|3\.5b|1b|2b|3b|4b|7b|8b|9b|13b|14b)(?:[^0-9]|$)` +
		`|(?i)\b(?:nano|mini|tiny|small)\b`,
)

// isSmallModel reports whether modelID refers to a small model (roughly <=14B
// parameters) based on common size-indicator tokens in the name.
// Matching is case-insensitive.
func isSmallModel(modelID string) bool {
	return smallModelRe.MatchString(strings.ToLower(modelID))
}
