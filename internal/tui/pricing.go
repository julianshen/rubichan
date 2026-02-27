package tui

// modelPricing maps model names to (input_cost_per_1M_tokens, output_cost_per_1M_tokens).
var modelPricing = map[string][2]float64{
	"claude-sonnet-4-5": {3.0, 15.0},
	"claude-opus-4-5":   {15.0, 75.0},
	"claude-haiku-3-5":  {0.80, 4.0},
	"gpt-4o":            {2.50, 10.0},
	"gpt-4o-mini":       {0.15, 0.60},
}

// EstimateCost returns the estimated cost in dollars for the given model and token counts.
func EstimateCost(model string, inputTokens, outputTokens int) float64 {
	pricing, ok := modelPricing[model]
	if !ok {
		return 0.0
	}
	return (float64(inputTokens)/1_000_000)*pricing[0] + (float64(outputTokens)/1_000_000)*pricing[1]
}
