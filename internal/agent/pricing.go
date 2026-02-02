package agent

// ModelPricing holds input and output pricing per 1M tokens in USD
type ModelPricing struct {
	Input  float64
	Output float64
}

// Pricing is a hardcoded table of model prices (USD per 1M tokens)
var Pricing = map[string]ModelPricing{
	// Claude models
	"claude-sonnet-4-20250514": {3.00, 15.00},
	"claude-3-5-sonnet":        {3.00, 15.00},
	"claude-opus-4":            {15.00, 75.00},
	"sonnet":                   {3.00, 15.00},  // shorthand
	"opus":                     {15.00, 75.00}, // shorthand

	// Gemini models
	"gemini-2.5-pro":     {1.25, 10.00},
	"gemini-2.5-flash":   {0.075, 0.30},
	"gemini-2.0-flash":   {0.10, 0.40},
	"gemini-1.5-pro":     {1.25, 5.00},
	"gemini-1.5-flash":   {0.075, 0.30},
	"gemini-exp-1206":    {0.00, 0.00}, // experimental, free
	"gemini-2.0-flash-thinking-exp": {0.00, 0.00}, // experimental

	// OpenAI/Codex models
	"o3":       {10.00, 40.00},
	"o3-mini":  {1.10, 4.40},
	"o1":       {15.00, 60.00},
	"gpt-4o":   {2.50, 10.00},
	"gpt-4":    {30.00, 60.00},
	"gpt-4-turbo": {10.00, 30.00},

	// Default fallback
	"_default": {3.00, 15.00},
}

// CalculateCost computes the USD cost for a usage record given a model name
func CalculateCost(model string, u Usage) float64 {
	pricing, ok := Pricing[model]
	if !ok {
		pricing = Pricing["_default"]
	}

	// Convert from per-1M tokens to actual cost
	inputCost := float64(u.InputTokens) * pricing.Input / 1_000_000
	outputCost := float64(u.OutputTokens) * pricing.Output / 1_000_000

	return inputCost + outputCost
}

// LookupPricing returns pricing for a model, falling back to default
func LookupPricing(model string) ModelPricing {
	if pricing, ok := Pricing[model]; ok {
		return pricing
	}
	return Pricing["_default"]
}
