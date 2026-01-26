package agent

import (
	"bufio"
	"context"
	"encoding/json"
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

// claudeStreamEvent represents a streaming event from Claude CLI
type claudeStreamEvent struct {
	Type  string `json:"type"`
	Event struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
}

// Run executes a prompt using the Claude CLI
func (a *ClaudeAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(output)
		defer close(errCh)

		// Run through login shell to pick up user's PATH from shell profile
		// Using agentic mode with tool access for thorough code analysis
		// --dangerously-skip-permissions auto-approves tool use
		// --tools restricts to read-only operations for safety (no Edit, Write, Bash)
		// --output-format stream-json with --include-partial-messages for real-time streaming
		claudeArgs := "claude --dangerously-skip-permissions"
		claudeArgs += " --tools Read,Grep,Glob,LSP"
		claudeArgs += " --output-format stream-json --verbose --include-partial-messages"
		if a.model != "" {
			claudeArgs += " --model " + a.model
		}
		claudeArgs += " -p " + shellQuote(prompt)

		cmd := exec.CommandContext(ctx, "bash", "-lc", claudeArgs)

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
			}
		}()

		// Stream stdout - parse JSON for streaming text
		scanner := bufio.NewScanner(stdout)
		// Increase buffer size for potentially large JSON lines
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		var textBuffer strings.Builder

		for scanner.Scan() {
			line := scanner.Text()

			// Try to parse as streaming event
			var event claudeStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				// Not JSON, output as-is (shouldn't happen in stream-json mode)
				output <- line
				continue
			}

			// Handle different event types
			switch event.Type {
			case "stream_event":
				// Check for content delta
				if event.Event.Type == "content_block_delta" && event.Event.Delta.Type == "text_delta" {
					text := event.Event.Delta.Text
					textBuffer.WriteString(text)

					// Output complete lines as they arrive
					for {
						content := textBuffer.String()
						idx := strings.Index(content, "\n")
						if idx == -1 {
							break
						}
						output <- content[:idx]
						textBuffer.Reset()
						textBuffer.WriteString(content[idx+1:])
					}
				}
			}
		}

		// Output any remaining text
		if textBuffer.Len() > 0 {
			output <- textBuffer.String()
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
