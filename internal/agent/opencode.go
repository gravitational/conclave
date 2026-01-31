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

// OpenCodeAgent implements Agent using the OpenCode CLI
type OpenCodeAgent struct {
	provider string // "anthropic", "openai", "google", "ollama", etc.
	model    string // "claude-sonnet-4", "o3", "gemini-2.5-pro", etc.
	variant  string // "high", "max" for reasoning effort (OpenAI)
	verbose  bool
}

// NewOpenCodeAgent creates a new OpenCode agent
func NewOpenCodeAgent(provider, model, variant string, verbose bool) *OpenCodeAgent {
	return &OpenCodeAgent{
		provider: provider,
		model:    model,
		variant:  variant,
		verbose:  verbose,
	}
}

// Name returns the agent type name (maps back to original provider names for display)
func (a *OpenCodeAgent) Name() string {
	switch a.provider {
	case "anthropic":
		return "claude"
	case "openai":
		return "codex"
	case "google":
		return "gemini"
	default:
		return "opencode"
	}
}

// Model returns the specific model being used
func (a *OpenCodeAgent) Model() string {
	modelStr := a.model
	if modelStr == "" {
		return ""
	}
	if a.variant != "" {
		return modelStr + " (effort=" + a.variant + ")"
	}
	return modelStr
}

// opencodeStreamEvent represents a streaming event from OpenCode CLI
type opencodeStreamEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
	SessionID string `json:"sessionID"`
	Part      struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		Tool  string `json:"tool"`
		State struct {
			Title string `json:"title"`
		} `json:"state"`
	} `json:"part"`
}

// Run executes a prompt using the OpenCode CLI
func (a *OpenCodeAgent) Run(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	output := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(output)
		defer close(errCh)

		// Build the opencode command
		// --format json enables streaming JSON output
		// --agent plan uses read-only tool access (matches current safety restrictions)
		modelSpec := a.provider + "/" + a.model
		opencodeArgs := fmt.Sprintf("opencode run --format json --agent plan --model %s", modelSpec)
		if a.variant != "" {
			opencodeArgs += " --variant " + a.variant
		}
		opencodeArgs += " " + shellQuote(prompt)

		// Run through login shell to pick up user's PATH
		cmd := exec.CommandContext(ctx, "bash", "-lc", opencodeArgs)

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
				if a.verbose {
					output <- "[stderr] " + line
				}
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
			var event opencodeStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				// Not JSON, output as-is
				output <- line
				continue
			}

			// Handle different event types
			switch event.Type {
			case "text":
				if event.Part.Type == "text" {
					text := event.Part.Text
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
			case "tool_use":
				// Optionally show tool usage in verbose mode
				if a.verbose && event.Part.State.Title != "" {
					output <- "[tool] " + event.Part.State.Title
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
				errCh <- fmt.Errorf("%w\n[opencode stderr]:\n%s", err, stderrContent)
			} else {
				errCh <- err
			}
			return
		}

		errCh <- nil
	}()

	return output, errCh
}
