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

// AgentMeta captures identity information about an agent
type AgentMeta struct {
	Provider string // "codex", "claude", "gemini"
	Model    string // e.g., "o3", "sonnet", "gemini-2.5-pro"
}

// GetMeta extracts metadata from an agent
func GetMeta(ag Agent) AgentMeta {
	meta := AgentMeta{Provider: ag.Name()}
	if m, ok := ag.(interface{ Model() string }); ok {
		meta.Model = m.Model()
	}
	return meta
}

// AgentResult pairs output content with agent metadata
type AgentResult struct {
	Content string
	Agent   AgentMeta
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
