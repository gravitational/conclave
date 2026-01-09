package agent

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
)

// CodexAgent implements Agent using the Codex CLI
type CodexAgent struct {
	model string
}

// NewCodexAgent creates a new Codex agent with optional model
func NewCodexAgent(model string) *CodexAgent {
	return &CodexAgent{model: model}
}

// Name returns the agent type name
func (a *CodexAgent) Name() string {
	return "codex"
}

// Run executes a prompt using the Codex CLI
func (a *CodexAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(output)
		defer close(errCh)

		// codex exec --full-auto with prompt via stdin (using "-" to read from stdin)
		args := []string{"exec", "--full-auto"}
		if a.model != "" {
			args = append(args, "--model", a.model)
		}
		args = append(args, "-")

		cmd := exec.CommandContext(ctx, "codex", args...)
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
