package agent

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
)

// GeminiAgent implements Agent using the Gemini CLI
type GeminiAgent struct{}

// NewGeminiAgent creates a new Gemini agent
func NewGeminiAgent() *GeminiAgent {
	return &GeminiAgent{}
}

// Name returns the agent type name
func (a *GeminiAgent) Name() string {
	return "gemini"
}

// Run executes a prompt using the Gemini CLI
func (a *GeminiAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(output)
		defer close(errCh)

		// gemini -y (yolo mode) with prompt via stdin
		cmd := exec.CommandContext(ctx, "gemini", "-y")
		cmd.Stdin = strings.NewReader(prompt)

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
