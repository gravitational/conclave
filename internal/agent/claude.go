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
	model     string
	verbose   bool
	lastUsage Usage
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
		Type    string `json:"type"`
		Message struct {
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"event"`
	// For result events
	TotalCostUSD float64 `json:"total_cost_usd"`
	Usage        struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	// For error events
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// LastUsage returns the usage from the most recent Run call
func (a *ClaudeAgent) LastUsage() Usage {
	return a.lastUsage
}

// Run executes a prompt using the Claude CLI
func (a *ClaudeAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	// Reset usage for this run
	a.lastUsage = Usage{}

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

		// Track usage - will be populated from result event
		var finalUsage Usage
		var gotResultEvent bool

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
				switch event.Event.Type {
				case "content_block_delta":
					// Check for text content
					if event.Event.Delta.Type == "text_delta" {
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

			case "error":
				// Capture error message from stream (e.g. auth errors) for diagnostics
				if event.Error.Message != "" {
					stderrMu.Lock()
					stderrLines = append(stderrLines, event.Error.Message)
					stderrMu.Unlock()
				}

			case "result":
				// Final result event has accurate usage and pre-calculated cost
				gotResultEvent = true
				finalUsage = Usage{
					InputTokens:     event.Usage.InputTokens,
					OutputTokens:    event.Usage.OutputTokens,
					CacheReadTokens: event.Usage.CacheReadInputTokens,
					CacheWriteTokens: event.Usage.CacheCreationInputTokens,
					TotalTokens:     event.Usage.InputTokens + event.Usage.OutputTokens,
					CostUSD:         event.TotalCostUSD,
				}
			}
		}

		// Output any remaining text
		if textBuffer.Len() > 0 {
			output <- textBuffer.String()
		}

		// Store final usage from result event, or calculate fallback
		if gotResultEvent {
			a.lastUsage = finalUsage
		} else {
			// Fallback if no result event (shouldn't happen normally)
			a.lastUsage = Usage{}
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
