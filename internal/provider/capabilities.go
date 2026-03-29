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

// DefaultCapabilities returns ModelCapabilities with safe defaults.
// This forwards to agentsdk.DefaultCapabilities for convenience.
func DefaultCapabilities() ModelCapabilities {
	return agentsdk.DefaultCapabilities()
}

// DetectCapabilities returns the ModelCapabilities for the given provider and
// model. providerName is the configured provider identifier (e.g. "anthropic",
// "ollama", "openrouter"). modelID is the model identifier string passed to
// the API. An empty providerName is treated as an OpenAI-compatible provider.
func DetectCapabilities(providerName, modelID string) ModelCapabilities {
	var caps ModelCapabilities
	switch providerName {
	case "anthropic":
		caps = detectAnthropicCapabilities(modelID)
	case "ollama":
		caps = detectOllamaCapabilities(modelID)
	default:
		caps = detectOpenAICompatCapabilities(modelID)
	}
	if caps.MaxToolCount < 0 {
		caps.MaxToolCount = 0
	}
	return caps
}

// detectAnthropicCapabilities returns capabilities for Anthropic models.
// All Anthropic models support native tool use, streaming, and system prompts.
// Haiku models are given a tool-discovery hint and a MaxToolCount of 12 to
// compensate for their reduced context window and instruction-following depth.
func detectAnthropicCapabilities(modelID string) ModelCapabilities {
	caps := agentsdk.DefaultCapabilities()
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
	caps := agentsdk.DefaultCapabilities()
	caps.NeedsToolDiscoveryHint = true
	if isSmallModel(modelID) {
		caps.MaxToolCount = 8
	} else {
		caps.MaxToolCount = 15
	}
	return caps
}

// modelProfile defines per-family capability tuning.
type modelProfile struct {
	// keywords matched against the lowercase model ID.
	keywords []string
	// hint enables NeedsToolDiscoveryHint.
	hint bool
	// smallLimit is MaxToolCount for small models (0 = no limit).
	smallLimit int
	// largeLimit is MaxToolCount for large models (0 = no limit).
	largeLimit int
}

// strongProfiles are model families with reliable tool-use that need no hints.
var strongProfiles = []modelProfile{
	{keywords: []string{"gpt-4", "gpt-5", "o1", "o3"}},
	{keywords: []string{"claude"}},
	// Gemini 2.5+ and 3.x are strong tool users with large context windows.
	{keywords: []string{"gemini-2.5", "gemini-3", "gemini-3.1"}},
	// Kimi K2/K2.5: strong coding models with native tool_use and parallel_tool_calls.
	{keywords: []string{"kimi"}},
}

// hintedProfiles are model families that support native tool_use but benefit
// from a discovery hint in the system prompt to improve tool selection.
var hintedProfiles = []modelProfile{
	// Qwen 3.5: supports tool_use but struggled with tool discovery in testing.
	// Large MoE variants (397B) work but need guidance; small variants need fewer tools.
	{keywords: []string{"qwen"}, hint: true, smallLimit: 8},
	// GLM 5/5-turbo (Zhipu AI): supports tool_use and structured_outputs.
	// Strong reasoning but less tested with complex tool schemas.
	{keywords: []string{"glm-5", "glm5"}, hint: true, largeLimit: 15},
	// GLM 4.x: older generation, more conservative limits.
	{keywords: []string{"glm-4", "glm4"}, hint: true, smallLimit: 8, largeLimit: 12},
	// MiniMax M1/M2.x: supports tool_use. M1 is 1M context reasoning model.
	// M2.x series are capable but benefit from tool guidance.
	{keywords: []string{"minimax"}, hint: true, largeLimit: 15},
	// Gemini 2.0 and earlier: decent tool use but benefits from hints.
	{keywords: []string{"gemini-2.0", "gemini-1"}}, // no hint — handled by strong
	// Open-weight families that benefit from guidance.
	{keywords: []string{"llama"}, hint: true, smallLimit: 8},
	{keywords: []string{"gemma"}, hint: true, smallLimit: 8},
	{keywords: []string{"mistral"}, hint: true, smallLimit: 8},
	{keywords: []string{"deepseek"}, hint: true, smallLimit: 8},
}

// detectOpenAICompatCapabilities returns capabilities for OpenAI-compatible
// providers (OpenRouter, self-hosted, etc.). Models are matched against known
// profiles by family. Unknown models get NeedsToolDiscoveryHint=true as a
// safe optimistic default.
func detectOpenAICompatCapabilities(modelID string) ModelCapabilities {
	caps := agentsdk.DefaultCapabilities()
	lower := strings.ToLower(modelID)

	// Check strong profiles first — these get full capabilities with no hints.
	for _, p := range strongProfiles {
		if matchesAnyKeyword(lower, p.keywords) {
			return caps
		}
	}

	// Check hinted profiles — these need various levels of guidance.
	for _, p := range hintedProfiles {
		if matchesAnyKeyword(lower, p.keywords) {
			caps.NeedsToolDiscoveryHint = p.hint
			small := isSmallModel(modelID)
			if small && p.smallLimit > 0 {
				caps.MaxToolCount = p.smallLimit
			} else if !small && p.largeLimit > 0 {
				caps.MaxToolCount = p.largeLimit
			}
			return caps
		}
	}

	// Unknown model: optimistic defaults with discovery hint.
	caps.NeedsToolDiscoveryHint = true
	return caps
}

func matchesAnyKeyword(lower string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// smallModelRe matches size indicators that identify models with <=14B
// parameters. Numeric indicators (e.g. "7b", "13b") are matched only when
// preceded by a non-digit boundary so that "72b" or "22b" do not falsely
// trigger on "2b". Keyword indicators (nano, mini, tiny, small) are matched
// as whole words.
var smallModelRe = regexp.MustCompile(
	`(?i)(?:^|[^0-9])(?:1\.5b|3\.5b|1b|2b|3b|4b|7b|8b|9b|10b|11b|12b|13b|14b)(?:[^0-9]|$)` +
		`|(?i)\b(?:nano|mini|tiny|small)\b`,
)

// isSmallModel reports whether modelID refers to a small model (roughly <=14B
// parameters) based on common size-indicator tokens in the name.
// Matching is case-insensitive.
func isSmallModel(modelID string) bool {
	return smallModelRe.MatchString(modelID)
}
