package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rob-picard-teleport/conclave/internal/display"
)

// GlobalRegistry is the shared agent registry for control purposes
var GlobalRegistry = NewRegistry()

// ANSI color codes
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorWhite   = "\033[37m"
)

var (
	// outputMutex ensures clean output when multiple agents stream simultaneously
	outputMutex sync.Mutex
)

// extractAndRecordUsage extracts usage from an agent and records it in the global session
func extractAndRecordUsage(ag Agent) Usage {
	var usage Usage
	if up, ok := ag.(UsageProvider); ok {
		usage = up.LastUsage()
		// Add to global session
		meta := GetMeta(ag)
		GlobalSession.Add(meta.Provider, meta.Model, usage)
	}
	return usage
}

// StreamWithPrefix runs an agent and streams output with a colored prefix
// Returns the collected output as a string
func StreamWithPrefix(ag Agent, prompt string, prefix string, color string) string {
	ctx := context.Background()
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")

		outputMutex.Lock()
		fmt.Printf("%s[%s]%s %s\n", color, prefix, ColorReset, line)
		outputMutex.Unlock()
	}

	if err := <-errCh; err != nil {
		outputMutex.Lock()
		fmt.Printf("%s[%s]%s Error: %v\n", color, prefix, ColorReset, err)
		outputMutex.Unlock()
	}

	return result.String()
}

// StreamWithStatusResult holds the result of StreamWithStatus
type StreamWithStatusResult struct {
	Content string
	Usage   Usage
}

// StreamWithStatus runs an agent and updates a status display
// Returns the collected output as a string
func StreamWithStatus(ag Agent, prompt string, idx int, sd *display.StatusDisplay) StreamWithStatusResult {
	ctx := context.Background()
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")
		sd.AddLine(idx, line)
	}

	// Extract and record usage
	usage := extractAndRecordUsage(ag)

	// Update display with usage if available
	if !usage.IsEmpty() {
		sd.SetUsage(idx, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.CostUSD)
	}

	if err := <-errCh; err != nil {
		sd.SetError(idx, err)
		return StreamWithStatusResult{Content: result.String(), Usage: usage}
	}

	sd.SetDone(idx)
	return StreamWithStatusResult{Content: result.String(), Usage: usage}
}

// StreamSilentResult holds output and any error from a silent stream
type StreamSilentResult struct {
	Content string
	Error   error
	Usage   Usage
}

// StreamSilent runs an agent and collects output without displaying
// Shows a simple spinner instead
func StreamSilent(ag Agent, prompt string, description string) string {
	result := StreamSilentWithError(ag, prompt, description)
	return result.Content
}

// StreamSilentWithError runs an agent and collects output, returning both content and error
func StreamSilentWithError(ag Agent, prompt string, description string) StreamSilentResult {
	ctx := context.Background()
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder
	lineCount := 0

	// Simple inline status
	fmt.Printf("  %s...", description)

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")
		lineCount++

		// Update inline count occasionally
		if lineCount%10 == 0 {
			fmt.Printf("\r  %s... (%d lines)", description, lineCount)
		}
	}

	// Extract and record usage
	usage := extractAndRecordUsage(ag)

	if err := <-errCh; err != nil {
		fmt.Printf("\r  %s... %s✗%s\n", description, ColorRed, ColorReset)
		for _, line := range strings.Split(err.Error(), "\n") {
			if strings.TrimSpace(line) != "" {
				fmt.Printf("  %s%s%s\n", ColorRed, line, ColorReset)
			}
		}
		return StreamSilentResult{Content: result.String(), Error: err, Usage: usage}
	}

	fmt.Printf("\r  %s... %s✓%s (%d lines)\n", description, ColorGreen, ColorReset, lineCount)
	return StreamSilentResult{Content: result.String(), Error: nil, Usage: usage}
}

// StreamMultiple runs multiple agents in parallel with different prefixes
func StreamMultiple(agents []Agent, prompts []string, prefixes []string, colors []string) []string {
	if len(agents) != len(prompts) || len(agents) != len(prefixes) || len(agents) != len(colors) {
		panic("mismatched slice lengths")
	}

	results := make([]string, len(agents))
	var wg sync.WaitGroup

	for i := range agents {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = StreamWithPrefix(agents[idx], prompts[idx], prefixes[idx], colors[idx])
		}(i)
	}

	wg.Wait()
	return results
}

// StreamMultipleWithStatus runs multiple agents with a unified status display
func StreamMultipleWithStatus(agents []Agent, prompts []string, names []string) []AgentResult {
	n := len(agents)
	if len(prompts) != n || len(names) != n {
		panic("mismatched slice lengths")
	}

	results := make([]AgentResult, n)
	sd := display.NewStatusDisplay(n, false)

	// Configure agents
	for i, ag := range agents {
		model := ""
		if m, ok := ag.(interface{ Model() string }); ok {
			model = m.Model()
		}
		sd.SetAgent(i, names[i], ag.Name(), model)
	}

	sd.Start()

	var wg sync.WaitGroup
	for i := range agents {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res := StreamWithStatus(agents[idx], prompts[idx], idx, sd)
			results[idx] = AgentResult{
				Content: res.Content,
				Agent:   GetMeta(agents[idx]),
				Usage:   res.Usage,
			}
		}(i)
	}

	wg.Wait()
	sd.Stop()

	return results
}

// extractActivity extracts meaningful activity from a log line
func extractActivity(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	// Skip noise
	lower := strings.ToLower(line)
	noisePatterns := []string{"```", "---", "===", "***", "thinking", "let me", "i'll", "i will"}
	for _, p := range noisePatterns {
		if strings.HasPrefix(lower, p) {
			return ""
		}
	}

	// Look for file paths or action words
	if strings.Contains(line, "/") || strings.Contains(line, ".go") || strings.Contains(line, ".js") {
		if len(line) < 80 {
			return line
		}
	}

	actionPrefixes := []string{"Reading", "Analyzing", "Checking", "Reviewing", "Found", "Scanning", "Looking", "Examining", "Processing"}
	for _, prefix := range actionPrefixes {
		if strings.HasPrefix(line, prefix) {
			if len(line) > 60 {
				return line[:57] + "..."
			}
			return line
		}
	}

	return ""
}
