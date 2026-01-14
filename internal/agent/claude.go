package agent

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// ClaudeAgent implements Agent using the Claude CLI
type ClaudeAgent struct {
	model   string
	verbose bool
}

// NewClaudeAgent creates a new Claude agent with optional model
func NewClaudeAgent(model string, verbose bool) *ClaudeAgent {
	return &ClaudeAgent{model: model, verbose: verbose}
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

		// Collect stderr in background for error reporting
		var stderrLines []string
		var stderrMu sync.Mutex
		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			stderrScanner := bufio.NewScanner(stderr)
			for stderrScanner.Scan() {
				line := stderrScanner.Text()
				stderrMu.Lock()
				stderrLines = append(stderrLines, line)
				stderrMu.Unlock()
				// Output stderr with prefix - filter to errors only unless verbose
				if a.verbose || looksLikeError(line) {
					output <- "[stderr] " + line
				}
			}
		}()

		// Stream stdout
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			output <- scanner.Text()
		}

		// Wait for stderr collection to complete
		wg.Wait()

		if err := cmd.Wait(); err != nil {
			// Include stderr content in error for better diagnostics
			stderrMu.Lock()
			stderrContent := strings.Join(stderrLines, "\n")
			stderrMu.Unlock()

			if stderrContent != "" {
				errCh <- fmt.Errorf("%w\n[claude stderr]:\n%s", err, stderrContent)
			} else {
				errCh <- err
			}
			return
		}

		errCh <- nil
	}()

	return output, errCh
}
