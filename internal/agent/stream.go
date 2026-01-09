package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rob-picard-teleport/conclave/internal/display"
)

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

// StreamWithStatus runs an agent and updates a status display
// Returns the collected output as a string
func StreamWithStatus(ag Agent, prompt string, idx int, sd *display.StatusDisplay) string {
	ctx := context.Background()
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")
		sd.AddLine(idx, line)
	}

	if err := <-errCh; err != nil {
		sd.SetError(idx, err)
		return result.String()
	}

	sd.SetDone(idx)
	return result.String()
}

// StreamSilent runs an agent and collects output without displaying
// Shows a simple spinner instead
func StreamSilent(ag Agent, prompt string, description string) string {
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

	if err := <-errCh; err != nil {
		fmt.Printf("\r  %s... %s✗%s\n", description, ColorRed, ColorReset)
		return result.String()
	}

	fmt.Printf("\r  %s... %s✓%s (%d lines)\n", description, ColorGreen, ColorReset, lineCount)
	return result.String()
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
func StreamMultipleWithStatus(agents []Agent, prompts []string, names []string) []string {
	n := len(agents)
	if len(prompts) != n || len(names) != n {
		panic("mismatched slice lengths")
	}

	results := make([]string, n)
	sd := display.NewStatusDisplay(n, false)

	// Configure agents
	for i, ag := range agents {
		sd.SetAgent(i, names[i], ag.Name())
	}

	sd.Start()

	var wg sync.WaitGroup
	for i := range agents {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = StreamWithStatus(agents[idx], prompts[idx], idx, sd)
		}(i)
	}

	wg.Wait()
	sd.Stop()

	return results
}
