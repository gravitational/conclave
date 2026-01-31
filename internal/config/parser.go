package config

import (
	"strings"
)

// ParseModelSpec parses a model specification string into a ModelSpec
// Formats:
//   - "claude" - provider only, default model
//   - "claude sonnet" - provider + model
//   - "claude sonnet[1m]" - provider + model (model string passed as-is)
//   - "codex gpt-5.2-pro:xhigh" - provider + model + reasoning effort
func ParseModelSpec(spec string) ModelSpec {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ModelSpec{}
	}

	parts := strings.SplitN(spec, " ", 2)
	provider := strings.ToLower(parts[0])

	result := ModelSpec{
		Provider: provider,
	}

	if len(parts) == 1 {
		// Provider only
		return result
	}

	modelPart := strings.TrimSpace(parts[1])

	// For codex, check for reasoning effort suffix (e.g., "gpt-5.2-pro:xhigh")
	if provider == "codex" && strings.Contains(modelPart, ":") {
		effortParts := strings.SplitN(modelPart, ":", 2)
		result.Model = effortParts[0]
		result.Effort = effortParts[1]
	} else {
		// Model only (may include brackets like "sonnet[1m]")
		result.Model = modelPart
	}

	return result
}

// validProviders is the set of recognized providers
var validProviders = map[string]bool{
	"claude": true,
	"codex":  true,
	"gemini": true,
}

// IsValidProvider returns true if the provider name is recognized
func IsValidProvider(provider string) bool {
	return validProviders[strings.ToLower(provider)]
}

// ValidProviderList returns the list of valid provider names
func ValidProviderList() []string {
	return []string{"claude", "codex", "gemini"}
}
