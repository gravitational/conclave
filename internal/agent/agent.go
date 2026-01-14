package agent

import (
	"context"
	"strings"
)

// Agent represents an LLM agent that can run prompts
type Agent interface {
	// Run executes a prompt and returns a channel that streams output lines
	Run(ctx context.Context, prompt string) (<-chan string, <-chan error)

	// Name returns the agent type name
	Name() string
}

// shellQuote quotes a string for safe use in shell commands
func shellQuote(s string) string {
	// Use single quotes and escape any single quotes in the string
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// looksLikeError checks if a stderr line appears to be an actual error message
// rather than informational output like config dumps or status messages
func looksLikeError(line string) bool {
	lower := strings.ToLower(line)
	// Check for common error indicators
	return strings.Contains(lower, "error") ||
		strings.Contains(lower, "failed") ||
		strings.Contains(lower, "fatal") ||
		strings.Contains(lower, "panic") ||
		strings.Contains(lower, "exception") ||
		strings.Contains(lower, "denied") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "429") ||
		strings.Contains(lower, "500") ||
		strings.Contains(lower, "502") ||
		strings.Contains(lower, "503")
}

// RunAndCollect runs a prompt and collects all output into a single string
func RunAndCollect(ag Agent, prompt string) (string, error) {
	ctx := context.Background()
	output, errCh := ag.Run(ctx, prompt)

	var result string
	for line := range output {
		result += line + "\n"
	}

	if err := <-errCh; err != nil {
		return result, err
	}

	return result, nil
}
