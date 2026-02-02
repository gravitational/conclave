package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ResilientAgent wraps an agent with automatic failover to other providers
type ResilientAgent struct {
	primary         Agent
	fallbacks       []Agent
	maxRetries      int
	mu              sync.Mutex
	failedProviders map[string]bool
	lastUsage       Usage
	lastAgent       Agent // The agent that successfully completed (for metadata)
}

// NewResilientAgent creates an agent that fails over to other providers on error
func NewResilientAgent(primary Agent, fallbacks []Agent) *ResilientAgent {
	return &ResilientAgent{
		primary:         primary,
		fallbacks:       fallbacks,
		maxRetries:      len(fallbacks) + 1, // Try primary + all fallbacks
		failedProviders: make(map[string]bool),
	}
}

// Name returns the primary agent's name
func (r *ResilientAgent) Name() string {
	return r.primary.Name()
}

// CurrentProvider returns the name of the currently active provider
func (r *ResilientAgent) CurrentProvider() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.failedProviders[r.primary.Name()] {
		return r.primary.Name()
	}
	for _, fb := range r.fallbacks {
		if !r.failedProviders[fb.Name()] {
			return fb.Name()
		}
	}
	return r.primary.Name() // All failed, will retry primary
}

// LastUsage returns the aggregate usage from all agent attempts
func (r *ResilientAgent) LastUsage() Usage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastUsage
}

// LastSuccessfulAgent returns the agent that successfully completed the last run
func (r *ResilientAgent) LastSuccessfulAgent() Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastAgent
}

// Run executes with automatic failover on errors
func (r *ResilientAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	// Reset usage tracking for this run
	r.mu.Lock()
	r.lastUsage = Usage{}
	r.lastAgent = nil
	r.mu.Unlock()

	go func() {
		defer close(output)
		defer close(errCh)

		var lastErr error
		var accumulatedOutput strings.Builder
		var totalUsage Usage

		// Build list of agents to try
		agents := []Agent{r.primary}
		agents = append(agents, r.fallbacks...)

		for attempt, agent := range agents {
			r.mu.Lock()
			if r.failedProviders[agent.Name()] {
				r.mu.Unlock()
				continue
			}
			r.mu.Unlock()

			// Build prompt with context if this is a retry
			currentPrompt := prompt
			if attempt > 0 && accumulatedOutput.Len() > 0 {
				currentPrompt = fmt.Sprintf(`%s

[CONTEXT: A previous agent started this task but encountered an error. Here is what was produced so far:]

%s

[Please continue from where the previous agent left off, or start fresh if the partial output is not useful.]`,
					prompt, accumulatedOutput.String())
			}

			success, partialOutput, agentUsage, err := r.tryAgent(ctx, agent, currentPrompt, output)

			// Accumulate usage from this attempt
			totalUsage.InputTokens += agentUsage.InputTokens
			totalUsage.OutputTokens += agentUsage.OutputTokens
			totalUsage.TotalTokens += agentUsage.TotalTokens
			totalUsage.CostUSD += agentUsage.CostUSD

			if success {
				// Store final usage and successful agent
				r.mu.Lock()
				r.lastUsage = totalUsage
				r.lastAgent = agent
				r.mu.Unlock()
				errCh <- nil
				return
			}

			// Accumulate output for context
			if partialOutput != "" {
				if accumulatedOutput.Len() > 0 {
					accumulatedOutput.WriteString("\n")
				}
				accumulatedOutput.WriteString(partialOutput)
			}

			lastErr = err
			r.markFailed(agent.Name())

			// Notify about failover
			if attempt < len(agents)-1 {
				nextAgent := r.findNextAgent(agents, attempt+1)
				if nextAgent != nil {
					output <- fmt.Sprintf("\n[!] %s failed: %v", strings.Title(agent.Name()), err)
					output <- fmt.Sprintf("[!] Switching to %s...\n", strings.Title(nextAgent.Name()))
					time.Sleep(500 * time.Millisecond) // Brief pause before retry
				}
			}
		}

		// Store total usage even on failure
		r.mu.Lock()
		r.lastUsage = totalUsage
		r.mu.Unlock()

		// All agents failed
		errCh <- fmt.Errorf("all providers failed, last error: %w", lastErr)
	}()

	return output, errCh
}

func (r *ResilientAgent) tryAgent(ctx context.Context, agent Agent, prompt string, output chan<- string) (success bool, partialOutput string, usage Usage, err error) {
	agentOutput, agentErr := agent.Run(ctx, prompt)

	var outputBuilder strings.Builder

	// Stream output, but only accumulate non-stderr lines for context passing
	for line := range agentOutput {
		output <- line
		// Don't include stderr lines in accumulated context - only real content
		if !strings.HasPrefix(line, "[stderr]") && !strings.HasPrefix(line, "[!]") {
			outputBuilder.WriteString(line)
			outputBuilder.WriteString("\n")
		}
	}

	// Check exit status - trust the CLI's exit code
	agentError := <-agentErr
	finalOutput := outputBuilder.String()

	// Get usage if agent supports it
	var agentUsage Usage
	if up, ok := agent.(UsageProvider); ok {
		agentUsage = up.LastUsage()
	}

	if agentError != nil {
		return false, finalOutput, agentUsage, agentError
	}

	// Command succeeded - trust it
	return true, finalOutput, agentUsage, nil
}

func (r *ResilientAgent) markFailed(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failedProviders[name] = true
}

func (r *ResilientAgent) findNextAgent(agents []Agent, startIdx int) Agent {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := startIdx; i < len(agents); i++ {
		if !r.failedProviders[agents[i].Name()] {
			return agents[i]
		}
	}
	return nil
}

// ResetFailures clears the failed provider list
func (r *ResilientAgent) ResetFailures() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failedProviders = make(map[string]bool)
}
