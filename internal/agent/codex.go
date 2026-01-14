package agent

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// CodexAgent implements Agent using the Codex CLI
type CodexAgent struct {
	model   string
	verbose bool
}

// NewCodexAgent creates a new Codex agent with optional model
func NewCodexAgent(model string, verbose bool) *CodexAgent {
	return &CodexAgent{model: model, verbose: verbose}
}

// Name returns the agent type name
func (a *CodexAgent) Name() string {
	return "codex"
}

// Model returns the specific model being used
func (a *CodexAgent) Model() string {
	return a.model
}

// Run executes a prompt using the Codex CLI
func (a *CodexAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(output)
		defer close(errCh)

		// codex exec --full-auto with prompt via stdin (using "-" to read from stdin)
		// Run through login shell to pick up user's PATH from shell profile
		codexArgs := "codex exec --full-auto"
		if a.model != "" {
			codexArgs += " --model " + a.model
		}
		codexArgs += " -"

		cmd := exec.CommandContext(ctx, "sh", "-lc", codexArgs)
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
				errCh <- fmt.Errorf("%w\n[codex stderr]:\n%s", err, stderrContent)
			} else {
				errCh <- err
			}
			return
		}

		errCh <- nil
	}()

	return output, errCh
}
