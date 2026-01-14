package agent

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// GeminiAgent implements Agent using the Gemini CLI
type GeminiAgent struct {
	model   string
	verbose bool
}

// NewGeminiAgent creates a new Gemini agent with optional model
func NewGeminiAgent(model string, verbose bool) *GeminiAgent {
	return &GeminiAgent{model: model, verbose: verbose}
}

// Name returns the agent type name
func (a *GeminiAgent) Name() string {
	return "gemini"
}

// Model returns the specific model being used
func (a *GeminiAgent) Model() string {
	return a.model
}

// Run executes a prompt using the Gemini CLI
func (a *GeminiAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(output)
		defer close(errCh)

		// gemini -y (yolo mode) with prompt via stdin
		// Run through login shell to pick up user's PATH from shell profile
		geminiArgs := "gemini -y"
		if a.model != "" {
			geminiArgs += " --model " + a.model
		}

		cmd := exec.CommandContext(ctx, "sh", "-lc", geminiArgs)
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
				// Only show stderr in verbose mode; otherwise just collect for error reporting
				if a.verbose {
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
				errCh <- fmt.Errorf("%w\n[gemini stderr]:\n%s", err, stderrContent)
			} else {
				errCh <- err
			}
			return
		}

		errCh <- nil
	}()

	return output, errCh
}
