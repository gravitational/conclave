package agent

import (
	"bufio"
	"context"
	"os/exec"
)

// ClaudeAgent implements Agent using the Claude CLI
type ClaudeAgent struct{}

// NewClaudeAgent creates a new Claude agent
func NewClaudeAgent() *ClaudeAgent {
	return &ClaudeAgent{}
}

// Name returns the agent type name
func (a *ClaudeAgent) Name() string {
	return "claude"
}

// Run executes a prompt using the Claude CLI
func (a *ClaudeAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(output)
		defer close(errCh)

		cmd := exec.CommandContext(ctx, "claude", "-p", prompt)

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
			output <- scanner.Text()
		}

		if err := cmd.Wait(); err != nil {
			errCh <- err
			return
		}

		errCh <- nil
	}()

	return output, errCh
}
