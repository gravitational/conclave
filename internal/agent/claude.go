package agent

import (
	"bufio"
	"context"
	"os/exec"
)

// ClaudeAgent implements Agent using the Claude CLI
type ClaudeAgent struct {
	model string
}

// NewClaudeAgent creates a new Claude agent with optional model
func NewClaudeAgent(model string) *ClaudeAgent {
	return &ClaudeAgent{model: model}
}

// Name returns the agent type name
func (a *ClaudeAgent) Name() string {
	return "claude"
}

// Model returns the specific model being used
func (a *ClaudeAgent) Model() string {
	return a.model
}

// Run executes a prompt using the Claude CLI
func (a *ClaudeAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(output)
		defer close(errCh)

		args := []string{"-p", prompt}
		if a.model != "" {
			args = append([]string{"--model", a.model}, args...)
		}

		cmd := exec.CommandContext(ctx, "claude", args...)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			errCh <- err
			return
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			errCh <- err
			return
		}

		if err := cmd.Start(); err != nil {
			errCh <- err
			return
		}

		// Stream stdout
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			output <- scanner.Text()
		}

		// Also capture stderr
		stderrScanner := bufio.NewScanner(stderr)
		for stderrScanner.Scan() {
			output <- stderrScanner.Text()
		}

		if err := cmd.Wait(); err != nil {
			errCh <- err
			return
		}

		errCh <- nil
	}()

	return output, errCh
}
