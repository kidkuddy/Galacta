package agent

// modelPricing maps model names to [inputCostPer1M, outputCostPer1M] in USD.
var modelPricing = map[string][2]float64{
	"claude-sonnet-4-6":       {3.0, 15.0},
	"claude-sonnet-4-5":       {3.0, 15.0},
	"claude-opus-4-6":         {15.0, 75.0},
	"claude-opus-4-5":         {15.0, 75.0},
	"claude-haiku-4-5":        {0.80, 4.0},
	"claude-haiku-4-5-20251001": {0.80, 4.0},
}

// CalculateCost computes the cost in USD for given token counts and model.
func CalculateCost(model string, inputTokens, outputTokens int) float64 {
	pricing, ok := modelPricing[model]
	if !ok {
		return 0
	}
	inputCost := float64(inputTokens) / 1_000_000 * pricing[0]
	outputCost := float64(outputTokens) / 1_000_000 * pricing[1]
	return inputCost + outputCost
}
