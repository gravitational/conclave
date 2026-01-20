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

// geminiStreamEvent represents a streaming event from Gemini CLI
type geminiStreamEvent struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Delta   bool   `json:"delta"`
}

// Run executes a prompt using the Gemini CLI
func (a *GeminiAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(output)
		defer close(errCh)

		// gemini -y (yolo mode) with stream-json for real-time streaming
		// Run through login shell to pick up user's PATH from shell profile
		geminiArgs := "gemini -y --output-format stream-json"
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
			var event geminiStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				// Not JSON, output as-is (e.g., "YOLO mode is enabled" message)
				if !strings.HasPrefix(line, "YOLO") && !strings.HasPrefix(line, "Loaded") {
					output <- line
				}
				continue
			}

			// Handle assistant messages with delta flag (streaming content)
			if event.Type == "message" && event.Role == "assistant" && event.Delta {
				textBuffer.WriteString(event.Content)

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
